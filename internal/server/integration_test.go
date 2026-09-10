package server

import (
	"encoding/json"
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

func newIntegrationServer(t *testing.T, opts ...ServerOption) (*Hub, *httptest.Server) {
	t.Helper()
	hub, cancel := newTestHub(t)
	t.Cleanup(cancel)
	srv := NewServer(hub, zap.NewNop(), opts...)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return hub, ts
}

func integrationDial(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + ts.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func integrationSubscribe(t *testing.T, conn *websocket.Conn, events []string) {
	t.Helper()
	subMsg, err := NewSubscribeMessage(events)
	if err != nil {
		t.Fatalf("NewSubscribeMessage: %v", err)
	}
	data, _ := json.Marshal(subMsg)
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("subscribe write: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
}

func integrationRead(t *testing.T, conn *websocket.Conn) ServerMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return msg
}

func TestFullWSFlow(t *testing.T) {
	activity := types.Activity{Details: "Test Game", State: "In Match", Type: types.ActivityPlaying}

	hub, ts := newIntegrationServer(t,
		WithGetCurrentPresence(func() types.Activity { return activity }),
	)

	conn := integrationDial(t, ts)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1", hub.ClientCount())
	}

	integrationSubscribe(t, conn, []string{MsgTypePresence})

	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	msg := integrationRead(t, conn)
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
	hub, ts := newIntegrationServer(t)

	conn := integrationDial(t, ts)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	integrationSubscribe(t, conn, []string{MsgTypePresence})

	hub.Broadcast(NewStateMessage(StateConnected))

	activity := types.Activity{Details: "Game", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	msg := integrationRead(t, conn)
	if msg.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q (should skip state broadcast)", msg.Type, MsgTypePresence)
	}
}

func TestHubBroadcastToMultipleClients(t *testing.T) {
	hub, ts := newIntegrationServer(t)

	const numClients = 10
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = integrationDial(t, ts)
		defer conns[i].Close()
	}

	time.Sleep(200 * time.Millisecond)
	if got := hub.ClientCount(); got != numClients {
		t.Fatalf("ClientCount = %d, want %d", got, numClients)
	}

	for _, conn := range conns {
		integrationSubscribe(t, conn, []string{MsgTypePresence})
	}

	activity := types.Activity{Details: "Broadcast", Type: types.ActivityPlaying}
	msg, _ := NewPresenceMessage(activity)
	hub.Broadcast(msg)

	for i, conn := range conns {
		received := integrationRead(t, conn)
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

			wsURL := "ws" + ts.URL[4:]
			req, _ := http.NewRequest("GET", wsURL, nil)
			if tt.name == "wrong bearer token rejected" {
				req.Header.Set("Authorization", "Bearer wrong-token")
			}

			dialer := websocket.Dialer{}
			conn, _, err := dialer.Dial(wsURL, req.Header)

			if tt.wantWS {
				if err != nil {
					t.Fatalf("expected WS success, got error: %v", err)
				}
				conn.Close()
				time.Sleep(50 * time.Millisecond)
				if hub.ClientCount() != 1 {
					t.Errorf("ClientCount = %d, want 1", hub.ClientCount())
				}
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
	}

	_, ts := newIntegrationServer(t,
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

	t.Run("CORS preflight returns 204", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/presence", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
		}
	})

	t.Run("unknown route returns 404", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/does-not-exist")
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
		}
	})

	t.Run("CORS headers on all endpoints", func(t *testing.T) {
		for _, ep := range []string{"/health", "/api/presence", "/api/state"} {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("request %s: %v", ep, err)
			}
			resp.Body.Close()

			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("%s: ACAO = %q, want %q", ep, got, "*")
			}
			if got := resp.Header.Get("Access-Control-Allow-Methods"); got != "GET, OPTIONS" {
				t.Errorf("%s: ACAM = %q, want %q", ep, got, "GET, OPTIONS")
			}
		}
	})
}

func TestDashboardServingIntegration(t *testing.T) {
	_, ts := newIntegrationServer(t)

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
	body.ReadFrom(resp.Body)
	content := body.String()

	required := []string{"Discord Proxy RPC", "presence-card", "connection-card", "noscript"}
	for _, s := range required {
		if !strings.Contains(content, s) {
			t.Errorf("dashboard missing %q", s)
		}
	}
}

func TestConcurrentWSConnections(t *testing.T) {
	hub, ts := newIntegrationServer(t)

	const numClients = 20
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = integrationDial(t, ts)
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
			integrationSubscribe(t, c, []string{MsgTypePresence, MsgTypeState})
		}(conn)
	}
	subWg.Wait()

	time.Sleep(100 * time.Millisecond)

	activity := types.Activity{Details: "Concurrent", Type: types.ActivityPlaying}
	hub.Broadcast(NewPresenceMessage(activity))
	hub.Broadcast(NewStateMessage(StateConnected))

	for i, conn := range conns {
		presence := integrationRead(t, conn)
		if presence.Type != MsgTypePresence {
			t.Errorf("client %d first = %q, want %q", i, presence.Type, MsgTypePresence)
		}
		state := integrationRead(t, conn)
		if state.Type != MsgTypeState {
			t.Errorf("client %d second = %q, want %q", i, state.Type, MsgTypeState)
		}
	}
}

func TestGracefulDisconnect(t *testing.T) {
	hub, ts := newIntegrationServer(t)

	conn := integrationDial(t, ts)
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
	hub, ts := newIntegrationServer(t)

	const numClients = 5
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = integrationDial(t, ts)
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

	hub, ts := newIntegrationServer(t,
		WithGetCurrentPresence(func() types.Activity { return currentActivity }),
	)

	conn := integrationDial(t, ts)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	getMsg := NewGetCurrentMessage()
	getData, _ := json.Marshal(getMsg)
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, getData); err != nil {
		t.Fatalf("write get_current: %v", err)
	}

	resp := integrationRead(t, conn)
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

	integrationSubscribe(t, conn, []string{MsgTypePresence})

	updatedActivity := types.Activity{Details: "Flow Game", State: "In Match", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(updatedActivity)
	hub.Broadcast(presenceMsg)

	update := integrationRead(t, conn)
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

	hub, ts := newIntegrationServer(t,
		WithGetCurrentPresence(func() types.Activity { return activity }),
	)

	conn := integrationDial(t, ts)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(ts.URL + "/api/presence")
	if err != nil {
		t.Fatalf("REST request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("REST status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var restActivity types.Activity
	json.NewDecoder(resp.Body).Decode(&restActivity)

	integrationSubscribe(t, conn, []string{MsgTypePresence})

	msg, _ := NewPresenceMessage(activity)
	hub.Broadcast(msg)

	wsMsg := integrationRead(t, conn)
	if wsMsg.Type != MsgTypePresence {
		t.Errorf("WS type = %q, want %q", wsMsg.Type, MsgTypePresence)
	}
}
