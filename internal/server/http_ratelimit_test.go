package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
)

// testGetFromIP issues one GET against srv with a fixed client IP, so a test
// can give each simulated client its own rate-limit budget without binding
// sockets to many addresses.
func testGetFromIP(t *testing.T, srv *Server, ip, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = ip + ":54321"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// testDecodeAPIError decodes the standard error envelope from rec, failing the
// test if the body is not a well-formed envelope.
func testDecodeAPIError(t *testing.T, rec *httptest.ResponseRecorder) APIError {
	t.Helper()
	var apiErr APIError
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	return apiErr
}

func TestHTTPRateLimitRejectsOverLimit(t *testing.T) {
	for _, path := range []string{"/", "/api/presence", "/api/state"} {
		t.Run(path, func(t *testing.T) {
			rl := newTestRateLimiter(t, 2, time.Minute)
			srv := NewServer(NewHub(zap.NewNop()), zap.NewNop(), WithRateLimiter(rl))

			for i := 1; i <= 2; i++ {
				if rec := testGetFromIP(t, srv, "10.1.0.1", path); rec.Code != http.StatusOK {
					t.Fatalf("request %d = %d, want %d", i, rec.Code, http.StatusOK)
				}
			}

			rec := testGetFromIP(t, srv, "10.1.0.1", path)
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request over limit = %d, want %d", rec.Code, http.StatusTooManyRequests)
			}
			if code := testDecodeAPIError(t, rec).Code; code != ErrCodeRateLimited {
				t.Errorf("error code = %q, want %q", code, ErrCodeRateLimited)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			retry := rec.Header().Get("Retry-After")
			seconds, err := strconv.Atoi(retry)
			if err != nil {
				t.Fatalf("Retry-After = %q, want whole seconds", retry)
			}
			if seconds < 1 || seconds > 60 {
				t.Errorf("Retry-After = %d, want 1..60 seconds of a one-minute window", seconds)
			}

			if got := rl.Rejections(); got != 1 {
				t.Errorf("Rejections = %d, want 1 (HTTP rejections feed the metric)", got)
			}
		})
	}
}

func TestRateLimitExemptsProbesAndMetrics(t *testing.T) {
	hub := NewHub(zap.NewNop())
	rl := newTestRateLimiter(t, 1, time.Minute)
	ipcState := string(StateConnected)
	srv := NewServer(hub, zap.NewNop(),
		WithRateLimiter(rl),
		WithGetIPCState(func() string { return ipcState }),
		WithMetrics(NewMetrics(hub, func() string { return ipcState }, rl)),
	)

	if rec := testGetFromIP(t, srv, "10.2.0.1", "/api/presence"); rec.Code != http.StatusOK {
		t.Fatalf("first data request = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := testGetFromIP(t, srv, "10.2.0.1", "/api/presence"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("data request over limit = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	for _, path := range []string{"/health", "/healthz", "/readyz"} {
		if rec := testGetFromIP(t, srv, "10.2.0.1", path); rec.Code != http.StatusOK {
			t.Errorf("%s while rate limited = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}

	metrics := testGetFromIP(t, srv, "10.2.0.1", "/metrics")
	if metrics.Code != http.StatusOK {
		t.Fatalf("/metrics while rate limited = %d, want %d", metrics.Code, http.StatusOK)
	}
	if body := metrics.Body.String(); !strings.Contains(body, "discord_proxy_rate_limit_rejections_total 1") {
		t.Errorf("/metrics body does not report the HTTP rejection:\n%s", body)
	}

	if got := rl.Rejections(); got != 1 {
		t.Errorf("Rejections = %d, want 1 (exempt routes must not be counted)", got)
	}
}

func TestRateLimitBudgetsArePerIP(t *testing.T) {
	rl := newTestRateLimiter(t, 1, time.Minute)
	srv := NewServer(NewHub(zap.NewNop()), zap.NewNop(), WithRateLimiter(rl))

	if rec := testGetFromIP(t, srv, "10.3.0.1", "/api/state"); rec.Code != http.StatusOK {
		t.Fatalf("first IP first request = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := testGetFromIP(t, srv, "10.3.0.1", "/api/presence"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("first IP second request = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec := testGetFromIP(t, srv, "10.3.0.2", "/api/presence"); rec.Code != http.StatusOK {
		t.Errorf("second IP first request = %d, want %d (separate budget)", rec.Code, http.StatusOK)
	}
}

func TestRateLimitSharedBetweenHTTPAndWS(t *testing.T) {
	rl := newTestRateLimiter(t, 1, time.Minute)
	_, ts, _, cleanup := newTestHTTPServer(t, WithRateLimiter(rl))
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/presence")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first HTTP request = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	conn, wsResp, err := websocket.DefaultDialer.Dial(wsTestURL(ts), nil)
	if err == nil {
		conn.Close()
		t.Fatal("expected the WS upgrade to share the HTTP budget and be limited")
	}
	if wsResp == nil {
		t.Fatal("expected an HTTP response on rate limit rejection")
	}
	defer wsResp.Body.Close()

	if wsResp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("WS upgrade over limit = %d, want %d", wsResp.StatusCode, http.StatusTooManyRequests)
	}
	var apiErr APIError
	if err := json.NewDecoder(wsResp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if apiErr.Code != ErrCodeRateLimited {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeRateLimited)
	}
}

func TestRateLimitDisabledNeverRejects(t *testing.T) {
	tests := []struct {
		name string
		rl   func(t *testing.T) *RateLimiter
	}{
		{"nil limiter", func(*testing.T) *RateLimiter { return nil }},
		{"zero rate", func(t *testing.T) *RateLimiter { return newTestRateLimiter(t, 0, time.Minute) }},
		{"zero window", func(t *testing.T) *RateLimiter { return newTestRateLimiter(t, 1, 0) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(NewHub(zap.NewNop()), zap.NewNop(), WithRateLimiter(tt.rl(t)))

			for i := 1; i <= 20; i++ {
				if rec := testGetFromIP(t, srv, "10.4.0.1", "/api/presence"); rec.Code != http.StatusOK {
					t.Fatalf("request %d = %d, want %d (limiter disabled)", i, rec.Code, http.StatusOK)
				}
			}
		})
	}
}

func TestRateLimitPrecedesAuth(t *testing.T) {
	rl := newTestRateLimiter(t, 1, time.Minute)
	srv := NewServer(NewHub(zap.NewNop()), zap.NewNop(),
		WithRateLimiter(rl),
		WithAuth(func(*http.Request) bool { return false }),
	)

	first := testGetFromIP(t, srv, "10.5.0.1", "/api/presence")
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request = %d, want %d", first.Code, http.StatusUnauthorized)
	}
	if code := testDecodeAPIError(t, first).Code; code != ErrCodeUnauthorized {
		t.Errorf("error code = %q, want %q", code, ErrCodeUnauthorized)
	}

	// The limiter runs before auth, so a flood is cut off before the token
	// check — the second request answers 429, not another 401.
	second := testGetFromIP(t, srv, "10.5.0.1", "/api/presence")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("unauthenticated flood = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if code := testDecodeAPIError(t, second).Code; code != ErrCodeRateLimited {
		t.Errorf("error code = %q, want %q", code, ErrCodeRateLimited)
	}

	// A different IP still reaches auth, so the 429 was per-IP rather than a
	// global lockout.
	other := testGetFromIP(t, srv, "10.5.0.2", "/api/presence")
	if other.Code != http.StatusUnauthorized {
		t.Errorf("request from other IP = %d, want %d", other.Code, http.StatusUnauthorized)
	}
}

func TestRateLimitValidTokenStillLimited(t *testing.T) {
	rl := newTestRateLimiter(t, 1, time.Minute)
	token := "secret"
	srv := NewServer(NewHub(zap.NewNop()), zap.NewNop(),
		WithRateLimiter(rl),
		WithAuth(BearerAuthFunc(config.AuthConfig{Enabled: true, Token: token}, zap.NewNop())),
	)

	authed := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "10.6.0.1:54321"
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	if rec := authed("/api/presence"); rec.Code != http.StatusOK {
		t.Fatalf("first authed request = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := authed("/api/state"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("second authed request = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}
