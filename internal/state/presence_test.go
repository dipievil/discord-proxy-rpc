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

func TestNewPresenceDefaultsInterval(t *testing.T) {
	p := NewPresence(0, zap.NewNop())
	if p.interval != 50*time.Millisecond {
		t.Errorf("interval = %v, want 50ms (default)", p.interval)
	}

	p2 := NewPresence(-1*time.Second, zap.NewNop())
	if p2.interval != 50*time.Millisecond {
		t.Errorf("interval = %v, want 50ms (default for negative)", p2.interval)
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

	firstCh := make(chan struct{}, 1)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		select {
		case firstCh <- struct{}{}:
		default:
		}
	})

	p.Update(types.Activity{Details: "first"})
	select {
	case <-firstCh:
	case <-time.After(2 * time.Second):
		t.Fatal("first notification not received")
	}

	unsub()

	secondNotify := make(chan struct{}, 1)
	p.Subscribe(func(u PresenceUpdate) {
		select {
		case secondNotify <- struct{}{}:
		default:
		}
	})

	p.Update(types.Activity{Details: "second"})
	select {
	case <-secondNotify:
	case <-time.After(2 * time.Second):
		t.Fatal("second subscriber not notified")
	}

	select {
	case <-firstCh:
		t.Error("unsubscribed callback was invoked after unsubscribe")
	default:
	}
}

func TestCoalesceBehavior(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	var count atomic.Int32
	notifyCh := make(chan struct{}, 1)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		count.Add(1)
		select {
		case notifyCh <- struct{}{}:
		default:
		}
	})
	defer unsub()

	for i := 0; i < 10; i++ {
		p.Update(types.Activity{Details: "game", State: "state"})
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("coalesced notification not received")
	}

	time.Sleep(20 * time.Millisecond)

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

	p.mu.RLock()
	timerNil := p.timer == nil
	p.mu.RUnlock()

	if !timerNil {
		t.Error("timer should be nil after Stop")
	}
}

func TestUpdateAfterStopIsNoOp(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)

	notifyCh := make(chan struct{}, 1)
	p.Subscribe(func(u PresenceUpdate) {
		notifyCh <- struct{}{}
	})

	p.Update(types.Activity{Details: "before stop"})
	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("notification not received before stop")
	}

	cancel()
	p.Stop()

	p.Update(types.Activity{Details: "after stop"})

	select {
	case <-notifyCh:
		t.Error("notification received after Stop")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStaleFlushDoesNotEmit(t *testing.T) {
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

	for i := 0; i < 20; i++ {
		p.Update(types.Activity{Details: "rapid", State: string(rune('A' + i%26))})
		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("subscriber called %d times, want 1 (coalesced, no duplicate from stale flush)", got)
	}
}

func Test1000ConcurrentSubscribers(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	const numSubs = 1000
	var received atomic.Int32
	done := make(chan struct{}, numSubs)

	unsubs := make([]func(), 0, numSubs)
	for i := 0; i < numSubs; i++ {
		unsub := p.Subscribe(func(u PresenceUpdate) {
			received.Add(1)
			done <- struct{}{}
		})
		unsubs = append(unsubs, unsub)
	}

	p.Update(types.Activity{Details: "1000 subs"})

	timeout := time.After(10 * time.Second)
	for i := 0; i < numSubs; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatalf("timed out waiting for subscriber %d, received %d/%d", i, received.Load(), numSubs)
		}
	}

	for _, unsub := range unsubs {
		unsub()
	}

	if got := received.Load(); got != numSubs {
		t.Errorf("received %d notifications, want %d", got, numSubs)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	logger := zap.NewNop()
	p := &Presence{
		interval:  50 * time.Millisecond,
		logger:    logger,
		notifySem: make(chan struct{}, 3),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	const numSubs = 20
	var maxConcurrent atomic.Int32
	var currentConcurrent atomic.Int32
	done := make(chan struct{}, numSubs)

	for i := 0; i < numSubs; i++ {
		p.Subscribe(func(u PresenceUpdate) {
			cur := currentConcurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			currentConcurrent.Add(-1)
			done <- struct{}{}
		})
	}

	p.Update(types.Activity{Details: "limit test"})

	timeout := time.After(10 * time.Second)
	for i := 0; i < numSubs; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatalf("timed out waiting for subscriber %d", i)
		}
	}

	if got := maxConcurrent.Load(); got > 3 {
		t.Errorf("max concurrent goroutines = %d, want <= 3", got)
	}
}

func TestSubscribeIdempotentUnsubscribe(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	var count atomic.Int32
	unsub := p.Subscribe(func(u PresenceUpdate) {
		count.Add(1)
	})

	p.Update(types.Activity{Details: "test"})
	time.Sleep(100 * time.Millisecond)
	if got := count.Load(); got != 1 {
		t.Errorf("expected 1 notification, got %d", got)
	}

	unsub()

	p.Update(types.Activity{Details: "test2"})
	time.Sleep(100 * time.Millisecond)
	if got := count.Load(); got != 1 {
		t.Errorf("expected still 1 notification after unsub, got %d", got)
	}

	unsub()
}
