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

const DefaultMaxConcurrentCallbacks = 50

type subscriber struct {
	id   uint64
	fn   func(PresenceUpdate)
	once sync.Once
}

type Presence struct {
	cached      types.Activity
	mu          sync.RWMutex
	subscribers map[uint64]*subscriber
	subMu       sync.Mutex
	nextSubID   uint64
	timer       *time.Timer
	interval    time.Duration
	logger      *zap.Logger
	stopCh      chan struct{}
	done        chan struct{}
	gen         uint64
	stopped     bool
	notifySem   chan struct{}
}

func NewPresence(interval time.Duration, logger *zap.Logger) *Presence {
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	return &Presence{
		interval:    interval,
		logger:      logger,
		subscribers: make(map[uint64]*subscriber),
		notifySem:   make(chan struct{}, DefaultMaxConcurrentCallbacks),
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

	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(p.interval, func() { p.flush(gen) })
}

func (p *Presence) Current() types.Activity {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cached.Clone()
}

func (p *Presence) Subscribe(fn func(PresenceUpdate)) func() {
	p.subMu.Lock()
	p.nextSubID++
	id := p.nextSubID
	s := &subscriber{id: id, fn: fn}
	p.subscribers[id] = s
	p.subMu.Unlock()

	return func() {
		s.once.Do(func() {
			p.subMu.Lock()
			delete(p.subscribers, id)
			p.subMu.Unlock()
		})
	}
}

func (p *Presence) notify(update PresenceUpdate) {
	p.subMu.Lock()
	subs := make([]*subscriber, 0, len(p.subscribers))
	for _, s := range p.subscribers {
		subs = append(subs, s)
	}
	p.subMu.Unlock()

	for _, s := range subs {
		p.notifySem <- struct{}{}
		go func(sub *subscriber) {
			defer func() { <-p.notifySem }()
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error("subscriber panic recovered", zap.Any("recover", r))
				}
			}()
			sub.fn(update)
		}(s)
	}
}

func (p *Presence) flush(gen uint64) {
	p.mu.Lock()
	if gen != p.gen {
		p.mu.Unlock()
		return
	}
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
