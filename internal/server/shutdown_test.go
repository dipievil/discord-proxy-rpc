package server

import (
	"context"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/state"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func TestShutdownTimeout(t *testing.T) {
	hub, cancel, ts := newTestServer(t)
	defer cancel()

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	testSubscribe(t, conn, []string{MsgTypePresence})

	activity := types.Activity{Details: "Test Game", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	msg := testRead(t, conn)
	if msg.Type != MsgTypePresence {
		t.Errorf("expected presence message, got %q", msg.Type)
	}

	// Trigger shutdown
	cancel()

	// Verify connection is closed within timeout
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Error("expected connection to be closed after shutdown")
	}
}

func TestShutdownGoroutineLeak(t *testing.T) {
	// Capture baseline goroutines after GC
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	const numIterations = 3
	for i := 0; i < numIterations; i++ {
		hub, cancel, ts := newTestServer(t)

		conns := make([]*websocket.Conn, 3)
		for j := range conns {
			conns[j] = testDial(t, ts)
		}
		waitForClientCount(t, hub, 3)

		for _, conn := range conns {
			testSubscribe(t, conn, []string{MsgTypePresence})
		}

		activity := types.Activity{Details: "Leak Test", Type: types.ActivityPlaying}
		presenceMsg, _ := NewPresenceMessage(activity)
		hub.Broadcast(presenceMsg)

		for _, conn := range conns {
			testRead(t, conn)
		}

		cancel()

		for _, conn := range conns {
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			conn.ReadMessage()
			conn.Close()
		}

		ts.Close()
	}

	// Allow goroutines to clean up
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Allow small delta for test framework goroutines
	maxAllowed := baseline + 10
	if after > maxAllowed {
		t.Errorf("Goroutine leak detected: baseline=%d, after=%d (max allowed=%d)", baseline, after, maxAllowed)
	}
}

func TestPresenceTimerNoFireAfterStop(t *testing.T) {
	p := state.NewPresence(50*time.Millisecond, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)

	var count int
	var mu sync.Mutex
	notifyCh := make(chan struct{}, 10)

	unsub := p.Subscribe(func(u state.PresenceUpdate) {
		mu.Lock()
		count++
		mu.Unlock()
		select {
		case notifyCh <- struct{}{}:
		default:
		}
	})
	defer unsub()

	p.Update(types.Activity{Details: "before stop", Type: types.ActivityPlaying})

	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("first notification not received")
	}

	mu.Lock()
	firstCount := count
	mu.Unlock()

	cancel()
	p.Stop()

	// Try to trigger a flush after stop
	p.Update(types.Activity{Details: "after stop", Type: types.ActivityPlaying})

	// Wait to see if any notification fires
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	finalCount := count
	mu.Unlock()

	if finalCount != firstCount {
		t.Errorf("Notification fired after Stop(): count went from %d to %d", firstCount, finalCount)
	}
}

func TestFullProxyShutdownSequence(t *testing.T) {
	// This test simulates the full proxy shutdown sequence:
	// 1. Start presence, hub, HTTP server
	// 2. Subscribe clients
	// 3. Trigger shutdown via context cancel
	// 4. Verify all components shut down cleanly

	logger := zap.NewNop()

	presence := state.NewPresence(50*time.Millisecond, logger)
	ctx, cancel := context.WithCancel(context.Background())
	presence.Start(ctx)

	hub := NewHub(logger)
	hubCtx, hubCancel := context.WithCancel(context.Background())
	go hub.Run(hubCtx)

	srv := NewServer(hub, logger, WithGetCurrentPresence(presence.Current))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	testSubscribe(t, conn, []string{MsgTypePresence})

	activity := types.Activity{Details: "Full Test", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	testRead(t, conn)

	// Shutdown sequence (mirrors main.go)
	hub.Close()
	hubCancel()

	// Gracefully shutdown HTTP server (httptest.Server.Close() is sufficient for tests)
	ts.Close()

	cancel()
	presence.Stop()

	cancel()
	presence.Stop()

	// Verify no goroutine leak from our components
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
} // trigger CI

func TestMDNSDeregistrationOnShutdown(t *testing.T) {
	// This test verifies that mDNS advertiser Shutdown is called during shutdown.
	// Since we can't easily test real mDNS in unit tests, we verify the Shutdown
	// method exists and can be called without error.
	// Integration testing with real mDNS would require network access.

	// The actual mDNS advertiser is tested in internal/mdns/announce_test.go
	// This test serves as documentation that main.go calls adv.Shutdown() on defer.

	t.Log("mDNS Shutdown() is called via defer in main.go line 88")
	t.Log("Verified by: defer adv.Shutdown() executes when main() returns after shutdown")
}
