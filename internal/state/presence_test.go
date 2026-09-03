package state

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func newTestPresence(t *testing.T) *Presence {
	t.Helper()
	return NewPresence(50*time.Millisecond, zap.NewNop())
}

func TestNewPresence(t *testing.T) {
	logger := zap.NewNop()
	interval := 5 * time.Second
	p := NewPresence(interval, logger)

	if p == nil {
		t.Fatal("NewPresence returned nil")
	}
	if p.interval != interval {
		t.Errorf("interval = %v, want %v", p.interval, interval)
	}
	if got := p.Current(); !got.IsEmpty() {
		t.Error("initial activity should be empty")
	}
}

func TestUpdateAndCurrent(t *testing.T) {
	p := newTestPresence(t)

	activity := types.Activity{
		Details: "Playing a game",
		State:   "In a match",
		Type:    types.ActivityPlaying,
	}

	p.Update(activity)
	got := p.Current()

	if got.Details != activity.Details {
		t.Errorf("Details = %q, want %q", got.Details, activity.Details)
	}
	if got.State != activity.State {
		t.Errorf("State = %q, want %q", got.State, activity.State)
	}
	if got.Type != activity.Type {
		t.Errorf("Type = %v, want %v", got.Type, activity.Type)
	}
}

func TestSubscribeReceivesUpdate(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	received := make(chan PresenceUpdate, 1)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		received <- u
	})
	defer unsub()

	activity := types.Activity{Details: "test game", Type: types.ActivityPlaying}
	p.Update(activity)

	select {
	case update := <-received:
		if update.Activity.Details != "test game" {
			t.Errorf("received Details = %q, want %q", update.Activity.Details, "test game")
		}
		if update.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscriber notification")
	}
}

func TestUnsubscribe(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	var count atomic.Int32
	unsub := p.Subscribe(func(u PresenceUpdate) {
		count.Add(1)
	})

	p.Update(types.Activity{Details: "first"})
	time.Sleep(150 * time.Millisecond)
	firstCount := count.Load()

	unsub()

	p.Update(types.Activity{Details: "second"})
	time.Sleep(150 * time.Millisecond)
	secondCount := count.Load()

	if secondCount != firstCount {
		t.Errorf("callback fired %d times after unsubscribe, want %d", secondCount, firstCount)
	}
}

func TestCoalesceBehavior(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	var count atomic.Int32
	unsub := p.Subscribe(func(u PresenceUpdate) {
		count.Add(1)
	})
	defer unsub()

	for i := 0; i < 10; i++ {
		p.Update(types.Activity{Details: "game", State: "state"})
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("subscriber called %d times, want 1 (coalesced)", got)
	}

	got := p.Current()
	if got.Details != "game" || got.State != "state" {
		t.Errorf("cached activity = %+v, want Details=game, State=state", got)
	}
}

func TestLatestValueEmitted(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	received := make(chan PresenceUpdate, 1)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		received <- u
	})
	defer unsub()

	p.Update(types.Activity{Details: "first"})
	p.Update(types.Activity{Details: "second"})
	p.Update(types.Activity{Details: "third"})

	select {
	case update := <-received:
		if update.Activity.Details != "third" {
			t.Errorf("received Details = %q, want %q (latest value)", update.Activity.Details, "third")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	var wg sync.WaitGroup
	wg.Add(2)

	p.Subscribe(func(u PresenceUpdate) { wg.Done() })
	p.Subscribe(func(u PresenceUpdate) { wg.Done() })

	p.Update(types.Activity{Details: "multi"})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("not all subscribers notified")
	}
}

func TestSubscriberPanicRecovery(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	received := make(chan PresenceUpdate, 1)
	p.Subscribe(func(u PresenceUpdate) {
		panic("test panic")
	})
	p.Subscribe(func(u PresenceUpdate) {
		received <- u
	})

	p.Update(types.Activity{Details: "panic test"})

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("non-panicking subscriber was not called")
	}
}

func TestConcurrentUpdateAndRead(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			p.Update(types.Activity{Details: "concurrent"})
		}(i)
		go func() {
			defer wg.Done()
			_ = p.Current()
		}()
	}
	wg.Wait()
}

func TestStopCleansUp(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	cancel()
	p.Stop()

	if p.timer != nil {
		t.Error("timer should be nil after Stop")
	}
}
