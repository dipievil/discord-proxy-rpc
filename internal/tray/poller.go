package tray

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Update is one poll result. Err is set when the proxy could not be read; the
// Status then carries the unreachable state so the tray still has a valid icon
// and tooltip.
type Update struct {
	Status Status
	Err    error
}

type ticker interface {
	ticks() <-chan time.Time
	stop()
}

type realTicker struct{ ticker *time.Ticker }

func (r realTicker) ticks() <-chan time.Time { return r.ticker.C }
func (r realTicker) stop()                   { r.ticker.Stop() }

func newRealTicker(interval time.Duration) ticker {
	return realTicker{ticker: time.NewTicker(interval)}
}

// Poller polls a state source and reports status transitions. It emits the
// first result immediately and then only when the status changes, so a quiet
// proxy does not flood the tray.
type Poller struct {
	fetcher   Fetcher
	interval  time.Duration
	onUpdate  func(Update)
	newTicker func(time.Duration) ticker

	mu   sync.Mutex
	last Status
	seen bool
}

// NewPoller validates its inputs and returns a poller ready for Run.
func NewPoller(fetcher Fetcher, interval time.Duration, onUpdate func(Update)) (*Poller, error) {
	return newPoller(fetcher, interval, onUpdate, newRealTicker)
}

// newPoller is the test seam: the ticker factory keeps the poll loop free of
// sleeps while NewPoller wires the real clock.
func newPoller(
	fetcher Fetcher,
	interval time.Duration,
	onUpdate func(Update),
	newTicker func(time.Duration) ticker,
) (*Poller, error) {
	if fetcher == nil {
		return nil, errors.New("poller needs a state fetcher")
	}
	if onUpdate == nil {
		return nil, errors.New("poller needs an update callback")
	}
	if newTicker == nil {
		return nil, errors.New("poller needs a ticker factory")
	}
	if err := ValidateInterval(interval); err != nil {
		return nil, err
	}
	return &Poller{
		fetcher:   fetcher,
		interval:  interval,
		onUpdate:  onUpdate,
		newTicker: newTicker,
	}, nil
}

// Poll fetches the state once and reports it when it differs from the last
// reported status. A cancelled context is not a status transition: the tray is
// shutting down, so no update is reported.
func (p *Poller) Poll(ctx context.Context) Update {
	state, err := p.fetcher.State(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Update{Status: StatusFor(StateUnreachable), Err: ctxErr}
	}
	update := Update{Status: StatusFor(state), Err: err}

	p.mu.Lock()
	changed := !p.seen || update.Status != p.last
	if changed {
		p.seen = true
		p.last = update.Status
	}
	p.mu.Unlock()

	if changed {
		p.onUpdate(update)
	}
	return update
}

// Run polls immediately and then every interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.Poll(ctx)

	t := p.newTicker(p.interval)
	defer t.stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.ticks():
			p.Poll(ctx)
		}
	}
}
