package tray

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeNotifier struct {
	mu       sync.Mutex
	err      error
	failures int
	calls    int
	closed   bool
	status   chan Status
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{status: make(chan Status, 16)}
}

func (n *fakeNotifier) Set(status Status) error {
	n.mu.Lock()
	n.calls++
	err := n.err
	if n.failures > 0 {
		n.failures--
		err = errors.New("icon update failed")
	}
	n.mu.Unlock()
	n.status <- status
	return err
}

func (n *fakeNotifier) callsCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

func (n *fakeNotifier) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed = true
	return nil
}

func (n *fakeNotifier) closedNow() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.closed
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitStatus(t *testing.T, statuses <-chan Status, want ProxyState) Status {
	t.Helper()
	select {
	case status := <-statuses:
		if status.State != want {
			t.Fatalf("status = %q, want %q", status.State, want)
		}
		return status
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %q", want)
		return Status{}
	}
}

func TestNewAppValidation(t *testing.T) {
	notifier := newFakeNotifier()
	fetcher := &fakeFetcher{state: StateConnected}

	if _, err := NewApp(nil, notifier, DefaultInterval, nil); err == nil {
		t.Error("NewApp accepted a nil fetcher")
	}
	if _, err := NewApp(fetcher, nil, DefaultInterval, nil); err == nil {
		t.Error("NewApp accepted a nil notifier")
	}
	if _, err := NewApp(fetcher, notifier, time.Millisecond, nil); err == nil {
		t.Error("NewApp accepted an interval below the minimum")
	}
}

func TestAppFollowsConnectedToDisconnectedTransition(t *testing.T) {
	var (
		mu     sync.Mutex
		status = string(StateConnected)
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		current := status
		mu.Unlock()
		_, _ = w.Write([]byte(`{"status":"` + current + `"}`))
	}))
	defer server.Close()

	setStatus := func(next string) {
		mu.Lock()
		defer mu.Unlock()
		status = next
	}

	client, err := NewClient(server.URL, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	notifier := newFakeNotifier()
	fakeTick := newFakeTicker()

	app, err := NewApp(client, notifier, DefaultInterval, testLogger())
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.newTicker = func(time.Duration) ticker { return fakeTick }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if runErr := app.Run(ctx); runErr != nil {
			t.Errorf("Run: %v", runErr)
		}
	}()

	first := waitStatus(t, notifier.status, StateConnected)
	if first.Icon != IconConnected {
		t.Errorf("connected icon = %v, want connected", first.Icon)
	}

	setStatus(string(StateDisconnected))
	fakeTick.tick()
	second := waitStatus(t, notifier.status, StateDisconnected)
	if second.Icon != IconDisconnected {
		t.Errorf("disconnected icon = %v, want disconnected", second.Icon)
	}

	// Pull the proxy away entirely: the tray must fall back to disconnected
	// rather than keep the stale connected icon.
	server.Close()
	fakeTick.tick()
	third := waitStatus(t, notifier.status, StateUnreachable)
	if third.Icon != IconDisconnected {
		t.Errorf("unreachable icon = %v, want disconnected", third.Icon)
	}

	cancel()
	waitClosed(t, done)
}

func TestAppRetriesFailedNotifierUpdate(t *testing.T) {
	fetcher := &fakeFetcher{state: StateConnected}
	notifier := newFakeNotifier()
	notifier.failures = 1

	app, err := NewApp(fetcher, notifier, DefaultInterval, testLogger())
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	poller, err := newPoller(app.fetcher, app.interval, app.apply, app.newTicker)
	if err != nil {
		t.Fatalf("newPoller: %v", err)
	}

	ctx := context.Background()
	if update := poller.Poll(ctx); update.Err == nil {
		t.Fatal("first Poll Err = nil, want the notifier failure")
	}
	if update := poller.Poll(ctx); update.Err != nil {
		t.Fatalf("retry Poll Err = %v, want nil", update.Err)
	}
	if calls := notifier.callsCount(); calls != 2 {
		t.Fatalf("notifier calls = %d, want 2: the failed update was not retried", calls)
	}

	poller.Poll(ctx)
	if calls := notifier.callsCount(); calls != 2 {
		t.Errorf("notifier calls = %d, want 2: an unchanged status was re-sent", calls)
	}
}

func TestAppKeepsRunningAfterNotifierFailure(t *testing.T) {
	fetcher := &fakeFetcher{state: StateConnected}
	notifier := newFakeNotifier()
	notifier.failures = 1

	app, err := NewApp(fetcher, notifier, DefaultInterval, testLogger())
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	fakeTick := newFakeTicker()
	app.newTicker = func(time.Duration) ticker { return fakeTick }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if runErr := app.Run(ctx); runErr != nil {
			t.Errorf("Run: %v", runErr)
		}
	}()

	waitStatus(t, notifier.status, StateConnected)
	fakeTick.tick()
	waitStatus(t, notifier.status, StateConnected)
	cancel()
	waitClosed(t, done)

	if notifier.closedNow() {
		t.Error("App closed the notifier; cmd/tray owns its lifecycle")
	}
}
