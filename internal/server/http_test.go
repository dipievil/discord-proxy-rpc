package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func newTestHTTPServer(t *testing.T, opts ...ServerOption) (*Server, *httptest.Server, *Hub, func()) {
	t.Helper()
	hub, cancel := newTestHub(t)
	srv := NewServer(hub, zap.NewNop(), opts...)
	ts := httptest.NewServer(srv)
	return srv, ts, hub, func() {
		ts.Close()
		srv.Close()
		cancel()
	}
}

func TestHealthEndpoint(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	srv := NewServer(hub, zap.NewNop(),
		WithGetIPCState(func() string { return string(StateConnected) }),
	)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	start := time.Now()
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("response time = %v, want <10ms", elapsed)
	}

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
	if body["ipc"] != string(StateConnected) {
		t.Errorf("ipc = %q, want %q", body["ipc"], StateConnected)
	}
}

func TestHealthEndpointDisconnected(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ipc"] != string(StateDisconnected) {
		t.Errorf("ipc = %q, want %q", body["ipc"], StateDisconnected)
	}
}

func TestHealthzEndpoint(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	start := time.Now()
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("response time = %v, want <10ms", elapsed)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestReadyzEndpointConnected(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	srv := NewServer(hub, zap.NewNop(),
		WithGetIPCState(func() string { return string(StateConnected) }),
	)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	start := time.Now()
	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("response time = %v, want <10ms", elapsed)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
	if body["ipc"] != "connected" {
		t.Errorf("ipc = %q, want %q", body["ipc"], "connected")
	}
}

func TestReadyzEndpointDisconnected(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "not ready" {
		t.Errorf("status = %q, want %q", body["status"], "not ready")
	}
	if body["ipc"] != "disconnected" {
		t.Errorf("ipc = %q, want %q", body["ipc"], "disconnected")
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
		WithGetCurrentPresence(func() types.PresenceSnapshot { return testSnapshot(activity) }),
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

	snapshot := testDecodeSnapshotBody(t, resp.Body, "presence")
	if snapshot.Source != types.SourceIPC {
		t.Errorf("Source = %q, want %q", snapshot.Source, types.SourceIPC)
	}
	got := testActivityFromSnapshot(t, snapshot, "presence")
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

	snapshot := testDecodeSnapshotBody(t, resp.Body, "presence")
	if snapshot.Source != types.SourceNone {
		t.Errorf("Source = %q, want %q", snapshot.Source, types.SourceNone)
	}
	if snapshot.Activity != nil {
		t.Errorf("Activity = %+v, want nil when there is no source", snapshot.Activity)
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

func TestDashboardInjectsWSPath(t *testing.T) {
	tests := []struct {
		name string
		opts []ServerOption
		want string
	}{
		{name: "default path", opts: nil, want: `content="/ws"`},
		{name: "custom path", opts: []ServerOption{WithWSPath("/custom-ws")}, want: `content="/custom-ws"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ts, _, cleanup := newTestHTTPServer(t, tt.opts...)
			defer cleanup()

			resp, err := http.Get(ts.URL + "/")
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}

			if !strings.Contains(string(body), tt.want) {
				t.Errorf("dashboard index missing %q", tt.want)
			}
			if strings.Contains(string(body), wsPathPlaceholder) {
				t.Errorf("dashboard index still contains placeholder %q", wsPathPlaceholder)
			}
		})
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

	endpoints := []string{"/health", "/healthz", "/readyz", "/api/presence", "/api/state", "/"}
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
	hub.ClientID = "app-123"
	srv := NewServer(hub, zap.NewNop(),
		WithGetCurrentPresence(func() types.PresenceSnapshot { return testSnapshot(activity) }),
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
	if resp.ClientID != "app-123" {
		t.Errorf("client_id = %q, want %q", resp.ClientID, "app-123")
	}

	got := testDecodeActivity(t, resp.Payload, "current")
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

func TestRecoveryMiddlewareCatchesPanic(t *testing.T) {
	logger := zap.NewNop()
	panickingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	handler := RecoveryMiddleware(logger, panickingHandler)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/panic")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.Code != ErrCodeInternal {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeInternal)
	}
	if apiErr.Message != "internal server error" {
		t.Errorf("error message = %q, want %q", apiErr.Message, "internal server error")
	}
}

func TestRecoveryMiddlewareLetsNormalRequestsThrough(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	srv := NewServer(hub, zap.NewNop())
	ts := httptest.NewServer(RecoveryMiddleware(zap.NewNop(), srv))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestServerRecoveryMiddlewareIntegration(t *testing.T) {
	// This test verifies that RecoveryMiddleware is wired into Server.ServeHTTP
	// by registering a test route that panics and confirming the middleware
	// catches it and returns the standard INT_001 error response.
	hub, cancel := newTestHub(t)
	defer cancel()

	// Register a test route on the server's mux that panics.
	// We access the unexported mux via a test-only helper pattern:
	// create a new server, then use reflection or a test hook.
	// Since mux is unexported, we test via the existing Server by
	// adding a handler that panics through a custom ServerOption.
	panicked := false
	testSrv := NewServer(hub, zap.NewNop(),
		WithGetCurrentPresence(func() types.PresenceSnapshot {
			if !panicked {
				panicked = true
				panic("test panic in presence handler")
			}
			return types.PresenceSnapshot{Source: types.SourceNone}
		}),
	)

	ts := httptest.NewServer(testSrv)
	defer ts.Close()

	// First request triggers the panic
	resp, err := http.Get(ts.URL + "/api/presence")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.Code != ErrCodeInternal {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeInternal)
	}
	if apiErr.Message != "internal server error" {
		t.Errorf("error message = %q, want %q", apiErr.Message, "internal server error")
	}
}

func TestAPIErrorCodesInAuthResponse(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	cfg := config.AuthConfig{Enabled: true, Token: "secret"}
	srv := NewServer(hub, zap.NewNop())
	authedHandler := AuthMiddleware(cfg, zap.NewNop())(srv)
	ts := httptest.NewServer(authedHandler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/presence")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.Code != ErrCodeUnauthorized {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeUnauthorized)
	}
}

// waitFor polls cond until it holds or the timeout elapses, so tests do not
// depend on goroutine scheduling with fixed sleeps.
func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, desc)
}

func wsTestURL(ts *httptest.Server) string {
	return "ws" + ts.URL[4:] + "/ws"
}

func TestRateLimitWSUpgrade(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	_, ts, _, cleanup := newTestHTTPServer(t, WithRateLimiter(rl))
	defer cleanup()

	wsURL := wsTestURL(ts)
	for i := 0; i < 2; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i+1, err)
		}
		conn.Close()
	}

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected rate limit rejection for third connection from same IP")
	}
	if resp == nil {
		t.Fatal("expected an HTTP response on rate limit rejection")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.Code != ErrCodeRateLimited {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeRateLimited)
	}
}

func TestConnectionTrackerRejects(t *testing.T) {
	ct := NewConnectionTracker(1)
	_, ts, _, cleanup := newTestHTTPServer(t, WithConnectionTracker(ct))
	defer cleanup()

	wsURL := wsTestURL(ts)
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer conn1.Close()

	waitFor(t, 2*time.Second, "first connection tracked", func() bool {
		return ct.Count() == 1
	})

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected rejection when max connections reached")
	}
	if resp == nil {
		t.Fatal("expected an HTTP response when max connections reached")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.Code != ErrCodeTooManyConns {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeTooManyConns)
	}
}

func TestConnectionTrackerAllowsAfterRemove(t *testing.T) {
	ct := NewConnectionTracker(1)
	_, ts, _, cleanup := newTestHTTPServer(t, WithConnectionTracker(ct))
	defer cleanup()

	wsURL := wsTestURL(ts)
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}

	waitFor(t, 2*time.Second, "first connection tracked", func() bool {
		return ct.Count() == 1
	})

	conn1.Close()
	waitFor(t, 2*time.Second, "disconnect to decrement tracker", func() bool {
		return ct.Count() == 0
	})

	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("second dial after first closed: %v", err)
	}
	defer conn2.Close()

	waitFor(t, 2*time.Second, "second connection tracked", func() bool {
		return ct.Count() == 1
	})
}

func TestHTTPBodySizeLimitRejectsOversizedBody(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	body := strings.NewReader(strings.Repeat("x", 2*1024*1024))
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/presence", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.Code != ErrCodePayloadTooLarge {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodePayloadTooLarge)
	}
}

func TestHTTPBodySizeLimitAcceptsWithinLimit(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t,
		WithGetIPCState(func() string { return string(StateConnected) }),
	)
	defer cleanup()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/presence",
		strings.NewReader(`{"details":"hello"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHTTPBodySizeLimitAllowsNormalGET(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	for _, path := range []string{"/health", "/healthz", "/readyz", "/api/presence", "/api/state"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("GET %s status = %d, want 200 or 503", path, resp.StatusCode)
		}
	}
}
