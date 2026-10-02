package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func TestFullWSFlow(t *testing.T) {
	activity := types.Activity{Details: "Test Game", State: "In Match", Type: types.ActivityPlaying}

	hub, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.Activity { return activity }),
	)

	conn := testDial(t, ts)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1", hub.ClientCount())
	}

	testSubscribe(t, conn, []string{MsgTypePresence})

	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	msg := testRead(t, conn)
	if msg.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q", msg.Type, MsgTypePresence)
	}

	var got types.Activity
	if err := json.Unmarshal(msg.Payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got.Details != "Test Game" {
		t.Errorf("Details = %q, want %q", got.Details, "Test Game")
	}
	if got.State != "In Match" {
		t.Errorf("State = %q, want %q", got.State, "In Match")
	}
}

func TestFullWSFlowFiltersUnsubscribedEvents(t *testing.T) {
	hub, _, ts := newTestServer(t)

	conn := testDial(t, ts)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	testSubscribe(t, conn, []string{MsgTypePresence})

	hub.Broadcast(NewStateMessage(StateConnected))

	activity := types.Activity{Details: "Game", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	msg := testRead(t, conn)
	if msg.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q (should skip state broadcast)", msg.Type, MsgTypePresence)
	}
}

func TestHubBroadcastToMultipleClients(t *testing.T) {
	hub, _, ts := newTestServer(t)

	const numClients = 10
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = testDial(t, ts)
		defer conns[i].Close()
	}

	time.Sleep(200 * time.Millisecond)
	if got := hub.ClientCount(); got != numClients {
		t.Fatalf("ClientCount = %d, want %d", got, numClients)
	}

	for _, conn := range conns {
		testSubscribe(t, conn, []string{MsgTypePresence})
	}

	activity := types.Activity{Details: "Broadcast", Type: types.ActivityPlaying}
	msg, _ := NewPresenceMessage(activity)
	hub.Broadcast(msg)

	for i, conn := range conns {
		received := testRead(t, conn)
		if received.Type != MsgTypePresence {
			t.Errorf("client %d type = %q, want %q", i, received.Type, MsgTypePresence)
		}
	}
}

func TestAuthMiddlewareIntegration(t *testing.T) {
	validToken := "test-secret-token"

	tests := []struct {
		name     string
		authFunc func(r *http.Request) bool
		wantWS   bool
	}{
		{
			name:     "no auth configured allows connection",
			authFunc: nil,
			wantWS:   true,
		},
		{
			name:     "always deny blocks connection",
			authFunc: func(r *http.Request) bool { return false },
			wantWS:   false,
		},
		{
			name:     "always allow permits connection",
			authFunc: func(r *http.Request) bool { return true },
			wantWS:   true,
		},
		{
			name: "valid bearer token accepted",
			authFunc: func(r *http.Request) bool {
				return r.Header.Get("Authorization") == "Bearer "+validToken
			},
			wantWS: true,
		},
		{
			name: "wrong bearer token rejected",
			authFunc: func(r *http.Request) bool {
				return r.Header.Get("Authorization") == "Bearer "+validToken
			},
			wantWS: false,
		},
		{
			name: "missing bearer token rejected",
			authFunc: func(r *http.Request) bool {
				return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "+validToken)
			},
			wantWS: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub, cancel := newTestHub(t)
			defer cancel()

			var opts []ServerOption
			if tt.authFunc != nil {
				opts = append(opts, WithAuth(tt.authFunc))
			}
			srv := NewServer(hub, zap.NewNop(), opts...)
			ts := httptest.NewServer(srv)
			defer ts.Close()

			wsURL := "ws" + ts.URL[4:] + "/ws"
			req, _ := http.NewRequest("GET", wsURL, nil)
			switch tt.name {
			case "valid bearer token accepted":
				req.Header.Set("Authorization", "Bearer "+validToken)
			case "wrong bearer token rejected":
				req.Header.Set("Authorization", "Bearer wrong-token")
			}

			dialer := websocket.Dialer{}
			conn, _, err := dialer.Dial(wsURL, req.Header)

			if tt.wantWS {
				if err != nil {
					t.Fatalf("expected WS success, got error: %v", err)
				}
				time.Sleep(50 * time.Millisecond)
				if hub.ClientCount() != 1 {
					t.Errorf("ClientCount = %d, want 1", hub.ClientCount())
				}
				conn.Close()
			} else {
				if err == nil {
					conn.Close()
					t.Fatal("expected WS failure, but succeeded")
				}
				if hub.ClientCount() != 0 {
					t.Errorf("ClientCount = %d, want 0", hub.ClientCount())
				}
			}
		})
	}
}

func TestRESTEndpointsIntegration(t *testing.T) {
	activity := types.Activity{
		Details: "REST Test",
		State:   "Playing",
		Type:    types.ActivityPlaying,
		Assets: &types.Assets{
			LargeImage: "game-icon",
			LargeText:  "REST Test",
		},
	}

	_, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.Activity { return activity }),
		WithGetIPCState(func() string { return string(StateConnected) }),
	)

	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var body map[string]string
		json.NewDecoder(resp.Body).Decode(&body)
		if body["status"] != "ok" {
			t.Errorf("status = %q, want %q", body["status"], "ok")
		}
	})

	t.Run("presence returns custom activity", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/presence")
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		var got types.Activity
		json.NewDecoder(resp.Body).Decode(&got)
		if got.Details != "REST Test" {
			t.Errorf("Details = %q, want %q", got.Details, "REST Test")
		}
		if got.State != "Playing" {
			t.Errorf("State = %q, want %q", got.State, "Playing")
		}
		if got.Assets == nil || got.Assets.LargeImage != "game-icon" {
			t.Errorf("Assets.LargeImage = %v, want %q", got.Assets, "game-icon")
		}
	})

	t.Run("state returns connection status", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/state")
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()

		var body map[string]string
		json.NewDecoder(resp.Body).Decode(&body)
		if body["status"] != string(StateConnected) {
			t.Errorf("status = %q, want %q", body["status"], StateConnected)
		}
	})

	t.Run("no wildcard CORS headers", func(t *testing.T) {
		for _, ep := range []string{"/health", "/api/presence", "/api/state", "/"} {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("request %s: %v", ep, err)
			}
			resp.Body.Close()

			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("%s: ACAO = %q, want empty (no wildcard CORS)", ep, got)
			}
		}
	})

	t.Run("unknown route returns 404", func(t *testing.T) {
		for _, route := range []string{"/does-not-exist", "/api/unknown", "/foo/bar"} {
			resp, err := http.Get(ts.URL + route)
			if err != nil {
				t.Fatalf("request %s: %v", route, err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s: status = %d, want %d", route, resp.StatusCode, http.StatusNotFound)
			}
		}
	})
}

func TestDashboardServingIntegration(t *testing.T) {
	_, _, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html*", ct)
	}

	var body strings.Builder
	if _, err := io.Copy(&body, resp.Body); err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	content := body.String()

	required := []string{"Discord Proxy RPC", "presence-card", "connection-card", "noscript"}
	for _, s := range required {
		if !strings.Contains(content, s) {
			t.Errorf("dashboard missing %q", s)
		}
	}
}

func TestConcurrentWSConnections(t *testing.T) {
	hub, _, ts := newTestServer(t)

	const numClients = 20
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = testDial(t, ts)
		defer conns[i].Close()
	}

	time.Sleep(200 * time.Millisecond)
	if got := hub.ClientCount(); got != numClients {
		t.Fatalf("ClientCount = %d, want %d", got, numClients)
	}

	var subWg sync.WaitGroup
	for _, conn := range conns {
		subWg.Add(1)
		go func(c *websocket.Conn) {
			defer subWg.Done()
			testSubscribe(t, c, []string{MsgTypePresence, MsgTypeState})
		}(conn)
	}
	subWg.Wait()

	time.Sleep(100 * time.Millisecond)

	activity := types.Activity{Details: "Concurrent", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)
	hub.Broadcast(NewStateMessage(StateConnected))

	for i, conn := range conns {
		presence := testRead(t, conn)
		if presence.Type != MsgTypePresence {
			t.Errorf("client %d first = %q, want %q", i, presence.Type, MsgTypePresence)
		}
		state := testRead(t, conn)
		if state.Type != MsgTypeState {
			t.Errorf("client %d second = %q, want %q", i, state.Type, MsgTypeState)
		}
	}
}

func TestGracefulDisconnect(t *testing.T) {
	hub, _, ts := newTestServer(t)

	conn := testDial(t, ts)
	time.Sleep(50 * time.Millisecond)
	if got := hub.ClientCount(); got != 1 {
		t.Fatalf("ClientCount after connect = %d, want 1", got)
	}

	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	conn.Close()

	time.Sleep(200 * time.Millisecond)
	if got := hub.ClientCount(); got != 0 {
		t.Fatalf("ClientCount after disconnect = %d, want 0", got)
	}
}

func TestGracefulDisconnectMultipleClients(t *testing.T) {
	hub, _, ts := newTestServer(t)

	const numClients = 5
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = testDial(t, ts)
	}

	time.Sleep(100 * time.Millisecond)
	if got := hub.ClientCount(); got != numClients {
		t.Fatalf("ClientCount = %d, want %d", got, numClients)
	}

	for i := 0; i < numClients-1; i++ {
		conns[i].WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conns[i].Close()
	}

	time.Sleep(200 * time.Millisecond)
	if got := hub.ClientCount(); got != 1 {
		t.Fatalf("ClientCount after partial disconnect = %d, want 1", got)
	}

	conns[numClients-1].WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	conns[numClients-1].Close()

	time.Sleep(200 * time.Millisecond)
	if got := hub.ClientCount(); got != 0 {
		t.Fatalf("ClientCount after full disconnect = %d, want 0", got)
	}
}

func TestPresenceUpdateFlow(t *testing.T) {
	currentActivity := types.Activity{Details: "Flow Game", State: "Queue", Type: types.ActivityPlaying}

	hub, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.Activity { return currentActivity }),
	)

	conn := testDial(t, ts)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	testRequestCurrent(t, conn)

	resp := testRead(t, conn)
	if resp.Type != MsgTypeCurrent {
		t.Errorf("type = %q, want %q", resp.Type, MsgTypeCurrent)
	}

	var got types.Activity
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got.Details != "Flow Game" {
		t.Errorf("Details = %q, want %q", got.Details, "Flow Game")
	}
	if got.State != "Queue" {
		t.Errorf("State = %q, want %q", got.State, "Queue")
	}

	testSubscribe(t, conn, []string{MsgTypePresence})

	updatedActivity := types.Activity{Details: "Flow Game", State: "In Match", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(updatedActivity)
	hub.Broadcast(presenceMsg)

	update := testRead(t, conn)
	if update.Type != MsgTypePresence {
		t.Errorf("update type = %q, want %q", update.Type, MsgTypePresence)
	}

	var updated types.Activity
	json.Unmarshal(update.Payload, &updated)
	if updated.State != "In Match" {
		t.Errorf("updated State = %q, want %q", updated.State, "In Match")
	}
}

func TestMixedRESTAndWS(t *testing.T) {
	activity := types.Activity{Details: "Mixed Test", Type: types.ActivityPlaying}

	hub, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.Activity { return activity }),
	)

	conn := testDial(t, ts)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(ts.URL + "/api/presence")
	if err != nil {
		t.Fatalf("REST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("REST status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var restActivity types.Activity
	if err := json.NewDecoder(resp.Body).Decode(&restActivity); err != nil {
		t.Fatalf("decode REST response: %v", err)
	}
	if restActivity.Details != "Mixed Test" {
		t.Errorf("REST Details = %q, want %q", restActivity.Details, "Mixed Test")
	}

	testSubscribe(t, conn, []string{MsgTypePresence})

	msg, _ := NewPresenceMessage(activity)
	hub.Broadcast(msg)

	wsMsg := testRead(t, conn)
	if wsMsg.Type != MsgTypePresence {
		t.Errorf("WS type = %q, want %q", wsMsg.Type, MsgTypePresence)
	}
}
