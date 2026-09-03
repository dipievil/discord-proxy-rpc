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

type subscriberEntry struct {
	id int
	fn func(PresenceUpdate)
}

type Presence struct {
	cached       types.Activity
	lastNotified types.Activity
	mu           sync.RWMutex
	subscribers  []subscriberEntry
	subMu        sync.Mutex
	nextSubID    int
	timer        *time.Timer
	interval     time.Duration
	logger       *zap.Logger
	stopCh       chan struct{}
	done         chan struct{}
	gen          uint64
	stopped      bool
}

func NewPresence(interval time.Duration, logger *zap.Logger) *Presence {
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	return &Presence{
		interval: interval,
		logger:   logger,
	}
}

func (p *Presence) Start(ctx context.Context) {
	p.mu.Lock()
	p.stopCh = make(chan struct{})
	p.done = make(chan struct{})
	p.stopped = false
	p.mu.Unlock()
	go p.run(ctx)
}

func (p *Presence) Stop() {
	p.mu.Lock()
	p.stopped = true
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

	if p.stopped {
		return
	}

	p.cached = activity
	p.gen++
	gen := p.gen

	if p.timer == nil || !p.timer.Stop() {
		p.timer = time.AfterFunc(p.interval, func() { p.flush(gen) })
	} else {
		p.timer.Reset(p.interval)
	}
}

func (p *Presence) Current() types.Activity {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cached.Clone()
}

func (p *Presence) Subscribe(fn func(PresenceUpdate)) func() {
	p.subMu.Lock()
	id := p.nextSubID
	p.nextSubID++
	p.subscribers = append(p.subscribers, subscriberEntry{id: id, fn: fn})
	p.subMu.Unlock()

	return func() {
		p.subMu.Lock()
		defer p.subMu.Unlock()
		for i := range p.subscribers {
			if p.subscribers[i].id == id {
				p.subscribers = append(p.subscribers[:i], p.subscribers[i+1:]...)
				return
			}
		}
	}
}

func (p *Presence) notify(update PresenceUpdate) {
	p.subMu.Lock()
	subs := make([]subscriberEntry, len(p.subscribers))
	copy(subs, p.subscribers)
	p.subMu.Unlock()

	for _, s := range subs {
		go func(f func(PresenceUpdate)) {
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error("subscriber panic recovered", zap.Any("recover", r))
				}
			}()
			f(update)
		}(s.fn)
	}
}

func (p *Presence) flush(gen uint64) {
	p.mu.Lock()
	if gen != p.gen {
		p.mu.Unlock()
		return
	}
	if p.cached.Equals(p.lastNotified) {
		p.timer = nil
		p.mu.Unlock()
		return
	}
	update := PresenceUpdate{
		Activity:  p.cached,
		Timestamp: time.Now(),
	}
	p.lastNotified = p.cached
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
