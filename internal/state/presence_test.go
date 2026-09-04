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

func TestDiffNoBroadcastOnIdentical(t *testing.T) {
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

	activity := types.Activity{Details: "same", State: "same", Type: types.ActivityPlaying}
	p.Update(activity)
	time.Sleep(10 * time.Millisecond)
	p.Update(activity)

	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}

	time.Sleep(100 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("subscriber called %d times, want 1 (coalesced, no duplicate for identical activity)", got)
	}
}

func TestDiffPartialActivityChange(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	var count atomic.Int32
	notifyCh := make(chan PresenceUpdate, 5)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		count.Add(1)
		select {
		case notifyCh <- u:
		default:
		}
	})
	defer unsub()

	p.Update(types.Activity{Details: "original", State: "state", Type: types.ActivityPlaying})

	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first notification")
	}

	p.Update(types.Activity{Details: "changed", State: "state", Type: types.ActivityPlaying})

	select {
	case update := <-notifyCh:
		if update.Activity.Details != "changed" {
			t.Errorf("received Details = %q, want %q", update.Activity.Details, "changed")
		}
		if update.Activity.State != "state" {
			t.Errorf("received State = %q, want %q", update.Activity.State, "state")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second notification")
	}

	if got := count.Load(); got != 2 {
		t.Errorf("subscriber called %d times, want 2", got)
	}
}

func TestDiffTimestampChangeOnly(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	notifyCh := make(chan PresenceUpdate, 5)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		select {
		case notifyCh <- u:
		default:
		}
	})
	defer unsub()

	p.Update(types.Activity{Details: "game", Type: types.ActivityPlaying})
	<-notifyCh

	start := int64(1000)
	p.Update(types.Activity{
		Details:    "game",
		Type:       types.ActivityPlaying,
		Timestamps: &types.Timestamps{Start: start},
	})

	select {
	case update := <-notifyCh:
		if update.Activity.Timestamps == nil || update.Activity.Timestamps.Start != start {
			t.Errorf("received Timestamps = %v, want Start=%d", update.Activity.Timestamps, start)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestDiffAssetsChange(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	notifyCh := make(chan PresenceUpdate, 5)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		select {
		case notifyCh <- u:
		default:
		}
	})
	defer unsub()

	p.Update(types.Activity{Details: "game", Type: types.ActivityPlaying})
	<-notifyCh

	p.Update(types.Activity{
		Details: "game",
		Type:    types.ActivityPlaying,
		Assets:  &types.Assets{LargeImage: "img.png", LargeText: "Image"},
	})

	select {
	case update := <-notifyCh:
		if update.Activity.Assets == nil || update.Activity.Assets.LargeImage != "img.png" {
			t.Errorf("received Assets = %v, want LargeImage=img.png", update.Activity.Assets)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestDiffButtonsChange(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	notifyCh := make(chan PresenceUpdate, 5)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		select {
		case notifyCh <- u:
		default:
		}
	})
	defer unsub()

	p.Update(types.Activity{Details: "game", Type: types.ActivityPlaying})
	<-notifyCh

	p.Update(types.Activity{
		Details: "game",
		Type:    types.ActivityPlaying,
		Buttons: []types.Button{{Label: "Click", URL: "https://example.com"}},
	})

	select {
	case update := <-notifyCh:
		if len(update.Activity.Buttons) != 1 || update.Activity.Buttons[0].Label != "Click" {
			t.Errorf("received Buttons = %v, want [{Click https://example.com}]", update.Activity.Buttons)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestDiffTypeChange(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	notifyCh := make(chan PresenceUpdate, 5)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		select {
		case notifyCh <- u:
		default:
		}
	})
	defer unsub()

	p.Update(types.Activity{Details: "game", Type: types.ActivityPlaying})
	<-notifyCh

	p.Update(types.Activity{Details: "game", Type: types.ActivityStreaming})

	select {
	case update := <-notifyCh:
		if update.Activity.Type != types.ActivityStreaming {
			t.Errorf("received Type = %v, want %v", update.Activity.Type, types.ActivityStreaming)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestTimerResetBehavior(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	var count atomic.Int32
	notifyCh := make(chan PresenceUpdate, 10)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		count.Add(1)
		select {
		case notifyCh <- u:
		default:
		}
	})
	defer unsub()

	for i := 0; i < 5; i++ {
		p.Update(types.Activity{Details: string(rune('A' + i)), Type: types.ActivityPlaying})
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case update := <-notifyCh:
		if update.Activity.Details != string(rune('A'+4)) {
			t.Errorf("received Details = %q, want %q (last value)", update.Activity.Details, string(rune('A'+4)))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}

	time.Sleep(100 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("subscriber called %d times, want 1 (timer reset, only final value emitted)", got)
	}
}

func TestMultipleStartStopCycles(t *testing.T) {
	p := newTestPresence(t)

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		p.Start(ctx)

		p.Update(types.Activity{Details: string(rune('A' + i)), Type: types.ActivityPlaying})
		time.Sleep(10 * time.Millisecond)

		cancel()
		p.Stop()
	}
}

func TestSubscribeDuringFlush(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	flushStarted := make(chan struct{})
	flushUnblock := make(chan struct{})
	blockOnce := sync.Once{}

	p.Subscribe(func(u PresenceUpdate) {
		blockOnce.Do(func() {
			close(flushStarted)
			<-flushUnblock
		})
	})

	p.Update(types.Activity{Details: "test", Type: types.ActivityPlaying})

	select {
	case <-flushStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("flush did not start")
	}

	unsub := p.Subscribe(func(u PresenceUpdate) {})
	defer unsub()

	close(flushUnblock)

	time.Sleep(50 * time.Millisecond)
}

func TestNotifySemExhaustion(t *testing.T) {
	logger := zap.NewNop()
	p := &Presence{
		interval:    50 * time.Millisecond,
		logger:      logger,
		subscribers: make(map[uint64]*subscriber),
		notifySem:   make(chan struct{}, 2),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	for i := 0; i < 5; i++ {
		p.Subscribe(func(u PresenceUpdate) {
			time.Sleep(100 * time.Millisecond)
		})
	}

	done := make(chan struct{})
	go func() {
		p.Update(types.Activity{Details: "exhaust", Type: types.ActivityPlaying})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock detected")
	}

	time.Sleep(200 * time.Millisecond)
}

func TestCurrentReturnsClone(t *testing.T) {
	p := newTestPresence(t)

	activity := types.Activity{
		Details:    "game",
		State:      "state",
		Type:       types.ActivityPlaying,
		Timestamps: &types.Timestamps{Start: 1000},
		Assets:     &types.Assets{LargeImage: "img.png"},
		Party:      &types.Party{ID: "party1"},
		Buttons:    []types.Button{{Label: "Click", URL: "https://example.com"}},
	}

	p.Update(activity)
	got := p.Current()

	got.Details = "modified"
	got.State = "modified"
	got.Timestamps.Start = 2000
	got.Assets.LargeImage = "modified.png"
	got.Party.ID = "modified"
	got.Buttons[0].Label = "Modified"

	original := p.Current()
	if original.Details != "game" {
		t.Errorf("Details = %q, want %q", original.Details, "game")
	}
	if original.State != "state" {
		t.Errorf("State = %q, want %q", original.State, "state")
	}
	if original.Timestamps == nil || original.Timestamps.Start != 1000 {
		t.Errorf("Timestamps.Start = %v, want 1000", original.Timestamps)
	}
	if original.Assets == nil || original.Assets.LargeImage != "img.png" {
		t.Errorf("Assets.LargeImage = %v, want img.png", original.Assets)
	}
	if original.Party == nil || original.Party.ID != "party1" {
		t.Errorf("Party.ID = %v, want party1", original.Party)
	}
	if len(original.Buttons) != 1 || original.Buttons[0].Label != "Click" {
		t.Errorf("Buttons[0].Label = %v, want Click", original.Buttons)
	}
}

func TestEmptyActivityHandling(t *testing.T) {
	p := newTestPresence(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	notifyCh := make(chan PresenceUpdate, 5)
	unsub := p.Subscribe(func(u PresenceUpdate) {
		select {
		case notifyCh <- u:
		default:
		}
	})
	defer unsub()

	p.Update(types.Activity{})

	select {
	case update := <-notifyCh:
		if !update.Activity.IsEmpty() {
			t.Errorf("received non-empty activity for empty update: %+v", update.Activity)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}
