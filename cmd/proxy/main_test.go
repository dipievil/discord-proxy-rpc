package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/ipc"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/server"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/state"
)

// fakeIPCSource stands in for *ipc.Client: it exposes one events channel per
// session and can end a session the way Client.Run does when the connection
// drops.
type fakeIPCSource struct {
	mu sync.Mutex
	ch chan ipc.Event
}

func newFakeIPCSource() *fakeIPCSource {
	return &fakeIPCSource{ch: make(chan ipc.Event, 8)}
}

func (f *fakeIPCSource) Events() <-chan ipc.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ch
}

// endSession ends the current session and reports that no session is open, as
// Client.Run leaves Events() once its read loop terminates.
func (f *fakeIPCSource) endSession() {
	f.mu.Lock()
	ch := f.ch
	f.ch = nil
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (f *fakeIPCSource) startSession() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ch = make(chan ipc.Event, 8)
}

// newStateObserver starts a hub plus a WebSocket client subscribed to state
// events, so the event loop's broadcasts are observable in order.
func newStateObserver(t *testing.T) (*server.Hub, *websocket.Conn) {
	t.Helper()

	hub := server.NewHub(zap.NewNop())
	hub.ClientID = "observer-client-id"

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	srv := server.NewServer(hub, zap.NewNop(), server.WithGetIPCState(func() string { return "disconnected" }))
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		srv.Close()
		cancel()
	})

	conn, _, err := websocket.DefaultDialer.Dial("ws"+ts.URL[4:]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	waitForClientCount(t, hub, 1)

	subMsg, err := server.NewSubscribeMessage([]string{server.MsgTypeState})
	if err != nil {
		t.Fatalf("NewSubscribeMessage: %v", err)
	}
	writeClientMessage(t, conn, subMsg)

	// The subscription is answered with the state snapshot; reading it proves
	// the client is subscribed before the loop broadcasts anything.
	if msg := readServerMessage(t, conn); msg.Type != server.MsgTypeState {
		t.Fatalf("subscribe answer: type = %q, want %q", msg.Type, server.MsgTypeState)
	}
	return hub, conn
}

func writeClientMessage(t *testing.T, conn *websocket.Conn, msg any) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal client message: %v", err)
	}
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write client message: %v", err)
	}
}

func readServerMessage(t *testing.T, conn *websocket.Conn) server.ServerMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read server message: %v", err)
	}
	msg, err := server.ParseServerMessage(data)
	if err != nil {
		t.Fatalf("parse server message: %v", err)
	}
	return *msg
}

func waitForClientCount(t *testing.T, hub *server.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ClientCount = %d, want %d", hub.ClientCount(), want)
}

func startEventLoop(t *testing.T, source ipcEventSource, hub *server.Hub, ipcDone <-chan struct{}) <-chan struct{} {
	t.Helper()
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		runIPCEventLoop(context.Background(), source, state.NewPresence(0, zap.NewNop()), hub, ipcDone, zap.NewNop())
	}()
	return loopDone
}

func TestRunIPCEventLoopEmitsTerminalDisconnected(t *testing.T) {
	hub, conn := newStateObserver(t)
	source := newFakeIPCSource()
	ipcDone := make(chan struct{})
	loopDone := startEventLoop(t, source, hub, ipcDone)

	if msg := readServerMessage(t, conn); msg.Status != server.StateConnected {
		t.Fatalf("first status = %q, want %q", msg.Status, server.StateConnected)
	}

	// The IPC client stops for good (discord.auto_reconnect=false).
	source.endSession()
	close(ipcDone)

	if msg := readServerMessage(t, conn); msg.Status != server.StateReconnecting {
		t.Errorf("status after session end = %q, want %q", msg.Status, server.StateReconnecting)
	}
	if msg := readServerMessage(t, conn); msg.Status != server.StateDisconnected {
		t.Errorf("terminal status = %q, want %q", msg.Status, server.StateDisconnected)
	}

	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("event loop did not return after the terminal disconnected")
	}
}

func TestRunIPCEventLoopKeepsReconnectingWhileReconnectPending(t *testing.T) {
	hub, conn := newStateObserver(t)
	source := newFakeIPCSource()
	ipcDone := make(chan struct{})
	loopDone := startEventLoop(t, source, hub, ipcDone)

	if msg := readServerMessage(t, conn); msg.Status != server.StateConnected {
		t.Fatalf("first status = %q, want %q", msg.Status, server.StateConnected)
	}

	// ipcDone stays open: another session will follow.
	source.endSession()
	if msg := readServerMessage(t, conn); msg.Status != server.StateReconnecting {
		t.Fatalf("status after session end = %q, want %q", msg.Status, server.StateReconnecting)
	}

	source.startSession()
	if msg := readServerMessage(t, conn); msg.Status != server.StateConnected {
		t.Fatalf("status after reconnect = %q, want %q", msg.Status, server.StateConnected)
	}

	// Finish the loop so it does not outlive the test.
	source.endSession()
	close(ipcDone)
	readServerMessage(t, conn)
	readServerMessage(t, conn)

	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("event loop did not return after the terminal disconnected")
	}
}
