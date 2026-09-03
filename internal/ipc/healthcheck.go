package ipc

import (
	"context"
	"sync"
	"time"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

type HealthCheckManager struct {
	client   *Client
	interval time.Duration
	timeout  time.Duration
	logger   *zap.Logger

	mu     sync.Mutex
	stopCh chan struct{}
	done   chan struct{}
	pongCh chan struct{}
}

func NewHealthCheckManager(client *Client, cfg config.DiscordConfig, logger *zap.Logger) *HealthCheckManager {
	interval := cfg.HealthCheckInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	timeout := cfg.HealthCheckTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &HealthCheckManager{
		client:   client,
		interval: interval,
		timeout:  timeout,
		logger:   logger,
	}
}

func (h *HealthCheckManager) Start(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stopCh = make(chan struct{})
	h.done = make(chan struct{})
	h.pongCh = make(chan struct{}, 1)
	go h.run(ctx)
}

func (h *HealthCheckManager) Stop() {
	h.mu.Lock()
	if h.stopCh != nil {
		close(h.stopCh)
		h.stopCh = nil
	}
	done := h.done
	h.done = nil
	h.mu.Unlock()

	if done != nil {
		<-done
	}
}

func (h *HealthCheckManager) Done() <-chan struct{} {
	h.mu.Lock()
	done := h.done
	h.mu.Unlock()
	return done
}

func (h *HealthCheckManager) HandlePong() {
	h.mu.Lock()
	pongCh := h.pongCh
	h.mu.Unlock()

	if pongCh == nil {
		return
	}
	select {
	case pongCh <- struct{}{}:
	default:
	}
}

func (h *HealthCheckManager) run(ctx context.Context) {
	h.mu.Lock()
	stopCh := h.stopCh
	pongCh := h.pongCh
	h.mu.Unlock()

	defer close(h.done)

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	timeoutTimer := time.NewTimer(h.timeout)
	if !timeoutTimer.Stop() {
		<-timeoutTimer.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
		}

		select {
		case <-pongCh:
		default:
		}

		if err := h.client.SendRaw(OpPing, nil); err != nil {
			h.logger.Debug("health check ping failed", zap.Error(err))
			return
		}

		h.logger.Debug("health check ping sent")

		timeoutTimer.Reset(h.timeout)

		select {
		case <-ctx.Done():
			if !timeoutTimer.Stop() {
				select {
				case <-timeoutTimer.C:
				default:
				}
			}
			return
		case <-stopCh:
			if !timeoutTimer.Stop() {
				select {
				case <-timeoutTimer.C:
				default:
				}
			}
			return
		case <-pongCh:
			if !timeoutTimer.Stop() {
				select {
				case <-timeoutTimer.C:
				default:
				}
			}
			h.logger.Debug("health check pong received")
		case <-timeoutTimer.C:
			h.logger.Error("health check timed out: no pong received within timeout",
				zap.Duration("timeout", h.timeout))
			_ = h.client.Close()
			return
		}
	}
}
