package ipc

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

const maxReconnectAttempts = 50

var (
	ErrMaxReconnectAttempts  = errMaxReconnectAttempts{}
	ErrAutoReconnectDisabled = errAutoReconnectDisabled{}
)

type ReconnectManager struct {
	client        *Client
	baseInterval  time.Duration
	maxInterval   time.Duration
	autoReconnect bool
	logger        *zap.Logger

	mu      sync.Mutex
	attempt int
	stopCh  chan struct{}
}

func NewReconnectManager(client *Client, cfg config.DiscordConfig, logger *zap.Logger) *ReconnectManager {
	return &ReconnectManager{
		client:        client,
		baseInterval:  cfg.ReconnectBaseInterval,
		maxInterval:   cfg.MaxReconnectInterval,
		autoReconnect: cfg.AutoReconnect,
		logger:        logger,
	}
}

func (rm *ReconnectManager) NextInterval() time.Duration {
	rm.mu.Lock()
	attempt := rm.attempt
	rm.mu.Unlock()

	interval := cappedDouble(rm.baseInterval, rm.maxInterval, attempt)

	jitter := float64(interval) * 0.1
	return time.Duration(float64(interval) + rand.Float64()*2*jitter - jitter)
}

// cappedDouble returns value * 2^times, saturating at cap. Doubling stops as
// soon as the cap is reached, so the result can never overflow time.Duration
// even for large shift counts.
func cappedDouble(value, cap time.Duration, times int) time.Duration {
	for i := 0; i < times && value < cap; i++ {
		value *= 2
	}
	if value > cap {
		return cap
	}
	return value
}

func (rm *ReconnectManager) Reset() {
	rm.mu.Lock()
	rm.attempt = 0
	rm.mu.Unlock()
}

func (rm *ReconnectManager) Reconnect(ctx context.Context) error {
	if !rm.autoReconnect {
		return ErrAutoReconnectDisabled
	}

	rm.mu.Lock()
	rm.stopCh = make(chan struct{})
	rm.mu.Unlock()

	for {
		rm.mu.Lock()
		attempt := rm.attempt
		rm.mu.Unlock()

		if attempt >= maxReconnectAttempts {
			rm.logger.Error("max reconnect attempts reached",
				zap.Int("attempts", attempt))
			return ErrMaxReconnectAttempts
		}

		rm.client.setState(StateReconnecting)

		interval := rm.NextInterval()
		rm.logger.Info("reconnecting",
			zap.Duration("interval", interval),
			zap.Int("attempt", attempt+1))

		rm.mu.Lock()
		stopCh := rm.stopCh
		rm.mu.Unlock()

		timer := time.NewTimer(interval)

		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-stopCh:
			timer.Stop()
			return nil
		case <-timer.C:
		}

		if err := rm.client.Connect(); err != nil {
			rm.logger.Warn("reconnect failed",
				zap.Int("attempt", attempt+1),
				zap.Error(err))

			rm.mu.Lock()
			rm.attempt++
			rm.mu.Unlock()
			continue
		}

		rm.Reset()
		rm.logger.Info("reconnected to Discord")
		return nil
	}
}

func (rm *ReconnectManager) Stop() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.stopCh != nil {
		close(rm.stopCh)
		rm.stopCh = nil
	}
}

type errMaxReconnectAttempts struct{}

func (errMaxReconnectAttempts) Error() string {
	return "ipc: max reconnect attempts reached"
}

type errAutoReconnectDisabled struct{}

func (errAutoReconnectDisabled) Error() string {
	return "ipc: auto-reconnect is disabled"
}

func (c *Client) RunWithReconnect(ctx context.Context, cfg config.DiscordConfig) error {
	rm := NewReconnectManager(c, cfg, c.logger)

	for {
		var hc *HealthCheckManager
		if c.State() == StateConnected {
			hc = NewHealthCheckManager(c, cfg, c.logger)
			c.SetHealthCheck(hc)
			hc.Start(ctx)
		}

		runErr := runInterruptible(ctx, c)

		if hc != nil {
			hc.Stop()
			c.SetHealthCheck(nil)
		}

		if ctx.Err() != nil {
			rm.Stop()
			return ctx.Err()
		}

		if !cfg.AutoReconnect {
			return runErr
		}

		c.logger.Warn("connection lost, attempting reconnect", zap.Error(runErr))

		if err := rm.Reconnect(ctx); err != nil {
			return err
		}
	}
}

// runInterruptible runs c.Run until it returns or ctx is cancelled. A blocked
// Read call is unblocked by closing the connection, so cancellation returns
// promptly even while the IPC session is idle.
func runInterruptible(ctx context.Context, c *Client) error {
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = c.Close()
		return ctx.Err()
	}
}
