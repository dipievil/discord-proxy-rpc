package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func newTestHTTPServer(t *testing.T, opts ...ServerOption) (*Server, *httptest.Server, *Hub, func()) {
	t.Helper()
	hub, cancel := newTestHub(t)
	srv := NewServer(hub, zap.NewNop(), opts...)
	ts := httptest.NewServer(srv)
	return srv, ts, hub, func() {
		ts.Close()
		cancel()
	}
}

func TestHealthEndpoint(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestPresenceEndpoint(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	activity := types.Activity{
		Details: "Test Game",
		State:   "In Match",
		Type:    types.ActivityPlaying,
	}

	srv := NewServer(hub, zap.NewNop(),
		WithGetCurrentPresence(func() types.Activity { return activity }),
	)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/presence")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got types.Activity
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Details != "Test Game" {
		t.Errorf("Details = %q, want %q", got.Details, "Test Game")
	}
	if got.State != "In Match" {
		t.Errorf("State = %q, want %q", got.State, "In Match")
	}
}

func TestPresenceEndpointDefault(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/presence")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var got types.Activity
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.IsEmpty() {
		t.Errorf("expected empty activity by default, got %+v", got)
	}
}

func TestStateEndpoint(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	srv := NewServer(hub, zap.NewNop(),
		WithGetIPCState(func() string { return string(StateConnected) }),
	)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != string(StateConnected) {
		t.Errorf("status = %q, want %q", body["status"], StateConnected)
	}
}

func TestStateEndpointDefault(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != string(StateDisconnected) {
		t.Errorf("status = %q, want %q", body["status"], StateDisconnected)
	}
}

func TestDashboardServing(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

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
}

func TestNoWildcardCORSHeaders(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty (no wildcard CORS)", got)
	}
}

func TestNoCORSHeadersOnAllEndpoints(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	endpoints := []string{"/health", "/api/presence", "/api/state", "/"}
	for _, ep := range endpoints {
		resp, err := http.Get(ts.URL + ep)
		if err != nil {
			t.Fatalf("request %s: %v", ep, err)
		}
		resp.Body.Close()

		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want empty (no wildcard CORS)", ep, got)
		}
	}
}

func TestWSUpgradeViaServer(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	srv := NewServer(hub, zap.NewNop())
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount = %d, want 1", count)
	}
}

func TestWSAuthViaServer(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	authFunc := func(r *http.Request) bool {
		return false
	}

	srv := NewServer(hub, zap.NewNop(), WithAuth(authFunc))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	_, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial error with auth rejection")
	}

	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount = %d, want 0 (auth rejected)", count)
	}
}

func TestWSGetCurrentViaServer(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	activity := types.Activity{Details: "Server Wired Game", Type: types.ActivityPlaying}
	srv := NewServer(hub, zap.NewNop(),
		WithGetCurrentPresence(func() types.Activity { return activity }),
	)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	getMsg := NewGetCurrentMessage()
	data, _ := json.Marshal(getMsg)
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var resp ServerMessage
	if err := json.Unmarshal(received, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Type != MsgTypeCurrent {
		t.Errorf("type = %q, want %q", resp.Type, MsgTypeCurrent)
	}

	var got types.Activity
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if got.Details != "Server Wired Game" {
		t.Errorf("Details = %q, want %q", got.Details, "Server Wired Game")
	}
}

func TestWSCustomPathViaServer(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	srv := NewServer(hub, zap.NewNop(), WithWSPath("/custom-ws"))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/custom-ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial custom ws path: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount = %d, want 1", count)
	}

	// Default /ws path should no longer be served.
	_, _, err = websocket.DefaultDialer.Dial("ws"+ts.URL[4:]+"/ws", nil)
	if err == nil {
		t.Error("expected default /ws path to be disabled when custom path configured")
	}
}

func TestWSRejectsCrossOriginViaServer(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	srv := NewServer(hub, zap.NewNop())
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	header := http.Header{}
	header.Set("Origin", "http://evil.example.com")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		conn.Close()
		t.Fatal("expected cross-origin WebSocket upgrade to be rejected")
	}

	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount = %d, want 0 (cross-origin rejected)", count)
	}
}

func Test404ForUnknownRoutes(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestServerImplementsHandler(t *testing.T) {
	var _ http.Handler = (*Server)(nil)
}
