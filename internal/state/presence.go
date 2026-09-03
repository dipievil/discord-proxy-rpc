package state

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

type PresenceUpdate struct {
	Activity  types.Activity
	Timestamp time.Time
}

type Presence struct {
	cached      types.Activity
	mu          sync.RWMutex
	subscribers []func(PresenceUpdate)
	subMu       sync.Mutex
	timer       *time.Timer
	interval    time.Duration
	logger      *zap.Logger
	stopCh      chan struct{}
	done        chan struct{}
}

func NewPresence(interval time.Duration, logger *zap.Logger) *Presence {
	return &Presence{
		interval: interval,
		logger:   logger,
	}
}

func (p *Presence) Start(ctx context.Context) {
	p.stopCh = make(chan struct{})
	p.done = make(chan struct{})
	go p.run(ctx)
}

func (p *Presence) Stop() {
	p.mu.Lock()
	if p.stopCh != nil {
		close(p.stopCh)
		p.stopCh = nil
	}
	done := p.done
	p.mu.Unlock()

	if done != nil {
		<-done
	}
}

func (p *Presence) Update(activity types.Activity) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cached = activity

	if p.timer == nil || !p.timer.Stop() {
		p.timer = time.AfterFunc(p.interval, p.flush)
	} else {
		p.timer.Reset(p.interval)
	}
}

func (p *Presence) Current() types.Activity {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cached
}

func (p *Presence) Subscribe(fn func(PresenceUpdate)) func() {
	p.subMu.Lock()
	p.subscribers = append(p.subscribers, fn)
	p.subMu.Unlock()

	return func() {
		p.subMu.Lock()
		defer p.subMu.Unlock()
		for i := range p.subscribers {
			if p.subscribers[i] == fn {
				p.subscribers = append(p.subscribers[:i], p.subscribers[i+1:]...)
				return
			}
		}
	}
}

func (p *Presence) notify(update PresenceUpdate) {
	p.subMu.Lock()
	subs := make([]func(PresenceUpdate), len(p.subscribers))
	copy(subs, p.subscribers)
	p.subMu.Unlock()

	for _, fn := range subs {
		go func(f func(PresenceUpdate)) {
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error("subscriber panic recovered", zap.Any("recover", r))
				}
			}()
			f(update)
		}(fn)
	}
}

func (p *Presence) flush() {
	p.mu.Lock()
	update := PresenceUpdate{
		Activity:  p.cached,
		Timestamp: time.Now(),
	}
	p.timer = nil
	p.mu.Unlock()

	p.notify(update)
}

func (p *Presence) run(ctx context.Context) {
	defer close(p.done)

	p.mu.Lock()
	stopCh := p.stopCh
	p.mu.Unlock()

	select {
	case <-ctx.Done():
	case <-stopCh:
	}

	p.mu.Lock()
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.mu.Unlock()
}
