package tray

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeFetcher returns a scripted state and records how often it is called.
type fakeFetcher struct {
	mu    sync.Mutex
	state ProxyState
	err   error
	calls int
}

func (f *fakeFetcher) set(state ProxyState, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = state
	f.err = err
}

func (f *fakeFetcher) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeFetcher) State(context.Context) (ProxyState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.state, f.err
}

// fakeTicker lets a test deliver ticks by hand instead of sleeping.
type fakeTicker struct {
	ch chan time.Time
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time, 8)}
}

func (f *fakeTicker) ticks() <-chan time.Time { return f.ch }
func (f *fakeTicker) stop()                   {}
func (f *fakeTicker) tick()                   { f.ch <- time.Now() }

func collectUpdates() (func(Update), <-chan Update) {
	updates := make(chan Update, 16)
	return func(update Update) { updates <- update }, updates
}

func waitUpdate(t *testing.T, updates <-chan Update) Update {
	t.Helper()
	select {
	case update := <-updates:
		return update
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a tray update")
		return Update{}
	}
}

func waitClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("poller did not stop after cancellation")
	}
}

// expectNoUpdate asserts that no update is pending. The callback runs
// synchronously inside Poll, so a plain non-blocking check is deterministic.
func expectNoUpdate(t *testing.T, updates <-chan Update) {
	t.Helper()
	select {
	case update := <-updates:
		t.Errorf("unexpected update: state=%q", update.Status.State)
	default:
	}
}

func TestNewPollerValidation(t *testing.T) {
	onUpdate := func(Update) {}

	if _, err := NewPoller(nil, DefaultInterval, onUpdate); err == nil {
		t.Error("NewPoller accepted a nil fetcher")
	}
	fetcher := &fakeFetcher{state: StateConnected}
	if _, err := NewPoller(fetcher, DefaultInterval, nil); err == nil {
		t.Error("NewPoller accepted a nil callback")
	}
	if _, err := newPoller(fetcher, DefaultInterval, onUpdate, nil); err == nil {
		t.Error("newPoller accepted a nil ticker factory")
	}
	if _, err := NewPoller(fetcher, time.Millisecond, onUpdate); err == nil {
		t.Error("NewPoller accepted an interval below the minimum")
	}
}

func TestPollEmitsFirstStatusOnly(t *testing.T) {
	fetcher := &fakeFetcher{state: StateConnected}
	onUpdate, updates := collectUpdates()
	poller, err := NewPoller(fetcher, DefaultInterval, onUpdate)
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}

	update := poller.Poll(context.Background())
	if update.Status.State != StateConnected {
		t.Errorf("first status = %q, want connected", update.Status.State)
	}
	if got := waitUpdate(t, updates); got.Status.State != StateConnected {
		t.Errorf("emitted %q, want connected", got.Status.State)
	}

	expectNoUpdate(t, updates)

	fetcher.set(StateDisconnected, nil)
	if update := poller.Poll(context.Background()); update.Status.State != StateDisconnected {
		t.Errorf("status = %q, want disconnected", update.Status.State)
	}
	if got := waitUpdate(t, updates); got.Status.State != StateDisconnected {
		t.Errorf("emitted %q, want disconnected", got.Status.State)
	}

	fetcher.set(StateDisconnected, nil)
	poller.Poll(context.Background())
	expectNoUpdate(t, updates)
}

func TestPollMapsErrorsToDisconnectedIcon(t *testing.T) {
	fetcher := &fakeFetcher{state: StateUnreachable, err: errors.New("connection refused")}
	onUpdate, updates := collectUpdates()
	poller, err := NewPoller(fetcher, DefaultInterval, onUpdate)
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}

	update := poller.Poll(context.Background())
	if update.Err == nil {
		t.Fatal("Err = nil, want the transport error")
	}
	if update.Status.Icon != IconDisconnected {
		t.Errorf("Icon = %v, want disconnected", update.Status.Icon)
	}
	if got := waitUpdate(t, updates); got.Status.Icon != IconDisconnected {
		t.Errorf("emitted icon = %v, want disconnected", got.Status.Icon)
	}
}

func TestPollCancelledContextIsNotATransition(t *testing.T) {
	fetcher := &fakeFetcher{state: StateUnreachable, err: context.Canceled}
	onUpdate, updates := collectUpdates()
	poller, err := NewPoller(fetcher, DefaultInterval, onUpdate)
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if update := poller.Poll(ctx); update.Err == nil {
		t.Fatal("Poll returned no error for a cancelled context")
	}
	expectNoUpdate(t, updates)
}

func TestRunPollsOnTicksAndStops(t *testing.T) {
	fetcher := &fakeFetcher{state: StateConnected}
	onUpdate, updates := collectUpdates()
	fakeTick := newFakeTicker()

	poller, err := newPoller(fetcher, DefaultInterval, onUpdate, func(time.Duration) ticker { return fakeTick })
	if err != nil {
		t.Fatalf("newPoller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		poller.Run(ctx)
	}()

	if got := waitUpdate(t, updates); got.Status.State != StateConnected {
		t.Fatalf("initial status = %q, want connected", got.Status.State)
	}

	fetcher.set(StateReconnecting, nil)
	fakeTick.tick()
	if got := waitUpdate(t, updates); got.Status.State != StateReconnecting {
		t.Fatalf("status after tick = %q, want reconnecting", got.Status.State)
	}
	if calls := fetcher.callsCount(); calls < 2 {
		t.Errorf("fetcher called %d times, want at least 2", calls)
	}

	cancel()
	waitClosed(t, done)
}
