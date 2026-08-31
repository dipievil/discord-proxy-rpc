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

	interval := rm.baseInterval << uint(attempt)
	if interval > rm.maxInterval {
		interval = rm.maxInterval
	}

	jitter := float64(interval) * 0.1
	return time.Duration(float64(interval) + rand.Float64()*2*jitter - jitter)
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
		runErr := c.Run(ctx)
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
