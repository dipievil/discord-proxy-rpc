package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func e2eServer(t *testing.T, opts ...ServerOption) (*Hub, context.CancelFunc, *httptest.Server) {
	t.Helper()
	hub, cancel := newTestHub(t)
	srv := NewServer(hub, zap.NewNop(), opts...)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		cancel()
	})
	return hub, cancel, ts
}

func e2eDial(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + ts.URL[4:] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

func e2eSubscribe(t *testing.T, conn *websocket.Conn, events []string) {
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

func e2eRead(t *testing.T, conn *websocket.Conn) ServerMessage {
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

func TestE2EHealthEndpoint(t *testing.T) {
	_, _, ts := e2eServer(t,
		WithGetIPCState(func() string { return string(StateConnected) }),
	)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
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
}

func TestE2EStateEndpoint(t *testing.T) {
	_, _, ts := e2eServer(t,
		WithGetIPCState(func() string { return string(StateConnected) }),
	)

	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("GET /api/state: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != string(StateConnected) {
		t.Errorf("status = %q, want %q", body["status"], StateConnected)
	}
}

func TestE2EPresenceEndpoint(t *testing.T) {
	mockPresence := types.Activity{
		Details: "E2E Test Game",
		State:   "Ranked Match",
		Type:    types.ActivityPlaying,
		Assets: &types.Assets{
			LargeImage: "game-icon",
			LargeText:  "E2E Test Game",
		},
	}

	_, _, ts := e2eServer(t,
		WithGetCurrentPresence(func() types.Activity { return mockPresence }),
	)

	resp, err := http.Get(ts.URL + "/api/presence")
	if err != nil {
		t.Fatalf("GET /api/presence: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got types.Activity
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Details != "E2E Test Game" {
		t.Errorf("Details = %q, want %q", got.Details, "E2E Test Game")
	}
	if got.State != "Ranked Match" {
		t.Errorf("State = %q, want %q", got.State, "Ranked Match")
	}
	if got.Assets == nil || got.Assets.LargeImage != "game-icon" {
		t.Errorf("Assets.LargeImage = %v, want %q", got.Assets, "game-icon")
	}
}

func TestE2EDashboardHTML(t *testing.T) {
	_, _, ts := e2eServer(t)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html*", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	content := string(body)
	for _, want := range []string{"Discord Proxy RPC", "presence-card", "connection-card", "noscript"} {
		if !strings.Contains(content, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestE2EWSReceivePresenceBroadcast(t *testing.T) {
	mockPresence := types.Activity{
		Details: "E2E Test Game",
		State:   "Ranked Match",
		Type:    types.ActivityPlaying,
	}

	hub, _, ts := e2eServer(t,
		WithGetCurrentPresence(func() types.Activity { return mockPresence }),
	)

	conn := e2eDial(t, ts)
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1", hub.ClientCount())
	}

	e2eSubscribe(t, conn, []string{MsgTypePresence})

	presenceMsg, _ := NewPresenceMessage(mockPresence)
	hub.Broadcast(presenceMsg)

	msg := e2eRead(t, conn)
	if msg.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q", msg.Type, MsgTypePresence)
	}
	var got types.Activity
	json.Unmarshal(msg.Payload, &got)
	if got.Details != "E2E Test Game" {
		t.Errorf("Details = %q, want %q", got.Details, "E2E Test Game")
	}
	if got.State != "Ranked Match" {
		t.Errorf("State = %q, want %q", got.State, "Ranked Match")
	}
}

func TestE2EWSGetCurrent(t *testing.T) {
	mockPresence := types.Activity{
		Details: "Current Test",
		State:   "In Lobby",
		Type:    types.ActivityPlaying,
	}

	_, _, ts := e2eServer(t,
		WithGetCurrentPresence(func() types.Activity { return mockPresence }),
	)

	conn := e2eDial(t, ts)
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)

	getMsg := NewGetCurrentMessage()
	data, _ := json.Marshal(getMsg)
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write get_current: %v", err)
	}

	resp := e2eRead(t, conn)
	if resp.Type != MsgTypeCurrent {
		t.Errorf("type = %q, want %q", resp.Type, MsgTypeCurrent)
	}
	var got types.Activity
	json.Unmarshal(resp.Payload, &got)
	if got.Details != "Current Test" {
		t.Errorf("Details = %q, want %q", got.Details, "Current Test")
	}
	if got.State != "In Lobby" {
		t.Errorf("State = %q, want %q", got.State, "In Lobby")
	}
}

func TestE2EWSStateBroadcast(t *testing.T) {
	hub, _, ts := e2eServer(t)

	conn := e2eDial(t, ts)
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)

	e2eSubscribe(t, conn, []string{MsgTypeState})

	hub.Broadcast(NewStateMessage(StateConnected))

	msg := e2eRead(t, conn)
	if msg.Type != MsgTypeState {
		t.Errorf("type = %q, want %q", msg.Type, MsgTypeState)
	}
	if msg.Status != StateConnected {
		t.Errorf("status = %q, want %q", msg.Status, StateConnected)
	}
}

func TestE2EMultipleClientsReceiveBroadcast(t *testing.T) {
	mockPresence := types.Activity{
		Details: "Multi Client",
		Type:    types.ActivityPlaying,
	}

	hub, _, ts := e2eServer(t,
		WithGetCurrentPresence(func() types.Activity { return mockPresence }),
	)

	const numClients = 5
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = e2eDial(t, ts)
		defer conns[i].Close()
	}
	time.Sleep(100 * time.Millisecond)

	if hub.ClientCount() != numClients {
		t.Fatalf("ClientCount = %d, want %d", hub.ClientCount(), numClients)
	}

	for _, conn := range conns {
		e2eSubscribe(t, conn, []string{MsgTypePresence})
	}

	presenceMsg, _ := NewPresenceMessage(mockPresence)
	hub.Broadcast(presenceMsg)

	for i, conn := range conns {
		msg := e2eRead(t, conn)
		if msg.Type != MsgTypePresence {
			t.Errorf("client %d type = %q, want %q", i, msg.Type, MsgTypePresence)
		}
		var got types.Activity
		json.Unmarshal(msg.Payload, &got)
		if got.Details != "Multi Client" {
			t.Errorf("client %d Details = %q, want %q", i, got.Details, "Multi Client")
		}
	}
}

func TestE2EConcurrentBroadcast(t *testing.T) {
	hub, _, ts := e2eServer(t)

	const numClients = 10
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conns[i] = e2eDial(t, ts)
		defer conns[i].Close()
	}
	time.Sleep(200 * time.Millisecond)

	for _, conn := range conns {
		e2eSubscribe(t, conn, []string{MsgTypePresence, MsgTypeState})
	}
	time.Sleep(100 * time.Millisecond)

	activity := types.Activity{Details: "Concurrent Game", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)
	hub.Broadcast(NewStateMessage(StateConnected))

	for i, conn := range conns {
		p := e2eRead(t, conn)
		if p.Type != MsgTypePresence {
			t.Errorf("client %d first = %q, want %q", i, p.Type, MsgTypePresence)
		}
		s := e2eRead(t, conn)
		if s.Type != MsgTypeState {
			t.Errorf("client %d second = %q, want %q", i, s.Type, MsgTypeState)
		}
	}
}

func TestE2EPresenceUpdateLifecycle(t *testing.T) {
	initialActivity := types.Activity{Details: "Game A", State: "Lobby", Type: types.ActivityPlaying}
	updatedActivity := types.Activity{Details: "Game A", State: "In Match", Type: types.ActivityPlaying}

	current := initialActivity

	hub, _, ts := e2eServer(t,
		WithGetCurrentPresence(func() types.Activity { return current }),
	)

	conn := e2eDial(t, ts)
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)

	e2eSubscribe(t, conn, []string{MsgTypePresence})

	presenceMsg, _ := NewPresenceMessage(initialActivity)
	hub.Broadcast(presenceMsg)

	msg := e2eRead(t, conn)
	if msg.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q", msg.Type, MsgTypePresence)
	}
	var got types.Activity
	json.Unmarshal(msg.Payload, &got)
	if got.State != "Lobby" {
		t.Errorf("State = %q, want %q", got.State, "Lobby")
	}

	current = updatedActivity
	presenceMsg2, _ := NewPresenceMessage(updatedActivity)
	hub.Broadcast(presenceMsg2)

	msg2 := e2eRead(t, conn)
	var got2 types.Activity
	json.Unmarshal(msg2.Payload, &got2)
	if got2.State != "In Match" {
		t.Errorf("State = %q, want %q", got2.State, "In Match")
	}

	getMsg := NewGetCurrentMessage()
	getData, _ := json.Marshal(getMsg)
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	conn.WriteMessage(websocket.TextMessage, getData)

	resp := e2eRead(t, conn)
	var got3 types.Activity
	json.Unmarshal(resp.Payload, &got3)
	if got3.State != "In Match" {
		t.Errorf("get_current State = %q, want %q", got3.State, "In Match")
	}
}

func TestE2ENoWildcardCORS(t *testing.T) {
	_, _, ts := e2eServer(t)

	endpoints := []string{"/health", "/api/presence", "/api/state", "/"}
	for _, ep := range endpoints {
		resp, err := http.Get(ts.URL + ep)
		if err != nil {
			t.Fatalf("GET %s: %v", ep, err)
		}
		resp.Body.Close()

		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s: ACAO = %q, want empty", ep, got)
		}
	}
}

func TestE2EUnknownRoutesReturn404(t *testing.T) {
	_, _, ts := e2eServer(t)

	unknownRoutes := []string{"/does-not-exist", "/api/unknown", "/foo/bar"}
	for _, route := range unknownRoutes {
		resp, err := http.Get(ts.URL + route)
		if err != nil {
			t.Fatalf("GET %s: %v", route, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want %d", route, resp.StatusCode, http.StatusNotFound)
		}
	}
}

func TestE2EAuthRejectsUnauthorized(t *testing.T) {
	validToken := "e2e-test-token"

	hub, _, ts := e2eServer(t,
		WithAuth(func(r *http.Request) bool {
			return r.Header.Get("Authorization") == "Bearer "+validToken
		}),
	)

	t.Run("ws without token rejected", func(t *testing.T) {
		wsURL := "ws" + ts.URL[4:] + "/ws"
		_, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err == nil {
			t.Error("expected WS connection without token to be rejected")
		}
	})

	t.Run("ws with wrong token rejected", func(t *testing.T) {
		wsURL := "ws" + ts.URL[4:] + "/ws"
		header := http.Header{}
		header.Set("Authorization", "Bearer wrong-token")
		_, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err == nil {
			t.Error("expected WS connection with wrong token to be rejected")
		}
	})

	t.Run("ws with valid token accepted", func(t *testing.T) {
		wsURL := "ws" + ts.URL[4:] + "/ws"
		header := http.Header{}
		header.Set("Authorization", "Bearer "+validToken)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			t.Fatalf("expected WS connection with valid token to succeed: %v", err)
		}
		defer conn.Close()
		time.Sleep(50 * time.Millisecond)

		if hub.ClientCount() != 1 {
			t.Errorf("ClientCount = %d, want 1", hub.ClientCount())
		}
	})

	t.Run("rest endpoints remain accessible without token", func(t *testing.T) {
		for _, ep := range []string{"/health", "/api/presence", "/api/state"} {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("GET %s: %v", ep, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s: status = %d, want %d (auth only applies to WebSocket)", ep, resp.StatusCode, http.StatusOK)
			}
		}
	})
}

func TestE2EGracefulDisconnect(t *testing.T) {
	hub, _, ts := e2eServer(t)

	conn := e2eDial(t, ts)
	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1", hub.ClientCount())
	}

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	conn.Close()
	time.Sleep(200 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Errorf("ClientCount after disconnect = %d, want 0", hub.ClientCount())
	}
}

func TestE2EServerShutdownCleansUp(t *testing.T) {
	hub, cancel, ts := e2eServer(t)

	conns := make([]*websocket.Conn, 3)
	for i := 0; i < 3; i++ {
		conns[i] = e2eDial(t, ts)
	}
	time.Sleep(100 * time.Millisecond)

	if hub.ClientCount() != 3 {
		t.Fatalf("ClientCount = %d, want 3", hub.ClientCount())
	}

	cancel()
	time.Sleep(200 * time.Millisecond)

	for i, conn := range conns {
		_, _, err := conn.ReadMessage()
		if err == nil {
			t.Errorf("client %d: expected connection to be closed after server shutdown", i)
		}
	}
}
