package server

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

// This suite covers what integration_test.go does not: full presence lifecycles,
// WS-only auth enforcement, and hub shutdown cleanup. Endpoint and broadcast
// coverage lives in the integration suite (see testhelpers_test.go for the
// shared harness).

func TestE2EWSStateBroadcast(t *testing.T) {
	hub, _, ts := newTestServer(t)

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	testSubscribe(t, conn, []string{MsgTypeState})

	hub.Broadcast(NewStateMessage(StateConnected))

	msg := testRead(t, conn)
	if msg.Type != MsgTypeState {
		t.Errorf("type = %q, want %q", msg.Type, MsgTypeState)
	}
	if msg.Status != StateConnected {
		t.Errorf("status = %q, want %q", msg.Status, StateConnected)
	}
}

func TestE2EPresenceUpdateLifecycle(t *testing.T) {
	initialActivity := types.Activity{Details: "Game A", State: "Lobby", Type: types.ActivityPlaying}
	updatedActivity := types.Activity{Details: "Game A", State: "In Match", Type: types.ActivityPlaying}

	var mu sync.Mutex
	current := initialActivity

	hub, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.Activity {
			mu.Lock()
			defer mu.Unlock()
			return current
		}),
	)

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	testSubscribe(t, conn, []string{MsgTypePresence})

	presenceMsg, _ := NewPresenceMessage(initialActivity)
	hub.Broadcast(presenceMsg)

	msg := testRead(t, conn)
	if msg.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q", msg.Type, MsgTypePresence)
	}
	if got := testDecodeActivity(t, msg.Payload, "presence"); got.State != "Lobby" {
		t.Errorf("State = %q, want %q", got.State, "Lobby")
	}

	mu.Lock()
	current = updatedActivity
	mu.Unlock()

	presenceMsg2, _ := NewPresenceMessage(updatedActivity)
	hub.Broadcast(presenceMsg2)

	msg2 := testRead(t, conn)
	if msg2.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q", msg2.Type, MsgTypePresence)
	}
	if got := testDecodeActivity(t, msg2.Payload, "presence"); got.State != "In Match" {
		t.Errorf("State = %q, want %q", got.State, "In Match")
	}

	testRequestCurrent(t, conn)

	resp := testRead(t, conn)
	if resp.Type != MsgTypeCurrent {
		t.Errorf("type = %q, want %q", resp.Type, MsgTypeCurrent)
	}
	if got := testDecodeActivity(t, resp.Payload, "current"); got.State != "In Match" {
		t.Errorf("get_current State = %q, want %q", got.State, "In Match")
	}
}

func TestE2EAuthRejectsUnauthorized(t *testing.T) {
	validToken := "e2e-test-token"

	hub, _, ts := newTestServer(t,
		WithAuth(func(r *http.Request) bool {
			return r.Header.Get("Authorization") == "Bearer "+validToken
		}),
	)

	t.Run("ws without token rejected", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(testWSURL(ts), nil)
		if err == nil {
			conn.Close()
			t.Error("expected WS connection without token to be rejected")
		}
	})

	t.Run("ws with wrong token rejected", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer wrong-token")
		conn, _, err := websocket.DefaultDialer.Dial(testWSURL(ts), header)
		if err == nil {
			conn.Close()
			t.Error("expected WS connection with wrong token to be rejected")
		}
	})

	t.Run("ws with valid token accepted", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer "+validToken)
		conn, _, err := websocket.DefaultDialer.Dial(testWSURL(ts), header)
		if err != nil {
			t.Fatalf("expected WS connection with valid token to succeed: %v", err)
		}
		defer conn.Close()
		waitForClientCount(t, hub, 1)
	})

	t.Run("rest endpoints remain accessible without token", func(t *testing.T) {
		for _, ep := range []string{"/health", "/api/presence", "/api/state"} {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("GET %s: %v", ep, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want %d (auth only applies to WebSocket)", resp.StatusCode, http.StatusOK)
			}
		}
	})
}

func TestE2EServerShutdownCleansUp(t *testing.T) {
	hub, cancel, ts := newTestServer(t)

	const numClients = 3
	conns := make([]*websocket.Conn, numClients)
	for i := range conns {
		conns[i] = testDial(t, ts)
	}
	waitForClientCount(t, hub, numClients)

	cancel()

	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := conn.ReadMessage(); err == nil {
			t.Errorf("client %d: expected connection to be closed after server shutdown", i)
		}
		conn.Close()
	}
}
