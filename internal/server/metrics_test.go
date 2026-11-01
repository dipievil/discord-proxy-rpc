package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// scrapeMetrics issues a GET /metrics against the server and returns the body,
// failing the test for any non-200 response.
func scrapeMetrics(t *testing.T, s *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want %d", rec.Code, http.StatusOK)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func assertMetric(t *testing.T, body, metric, want string) {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, metric+" ") || strings.HasPrefix(line, metric+"{") {
			if strings.HasSuffix(line, " "+want) {
				return
			}
			t.Errorf("metric %s = %q, want value %q", metric, line, want)
			return
		}
	}
	t.Errorf("metric %s not found in output:\n%s", metric, body)
}

func TestMetricsHandlerExposesRegisteredMetrics(t *testing.T) {
	hub := NewHub(zap.NewNop())
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Stop()

	ipcState := string(StateDisconnected)
	m := NewMetrics(hub, func() string { return ipcState }, rl)
	srv := NewServer(hub, zap.NewNop(), WithMetrics(m))

	m.RecordPresenceSend()
	m.RecordPresenceSend()
	if !rl.Allow("10.0.0.1") {
		t.Fatal("first Allow = false, want true")
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("second Allow = true, want false (rate=1)")
	}

	body := scrapeMetrics(t, srv)
	assertMetric(t, body, "discord_proxy_ws_connected_clients", "0")
	assertMetric(t, body, "discord_proxy_ipc_connected", "0")
	assertMetric(t, body, "discord_proxy_presence_coalesced_sends_total", "2")
	assertMetric(t, body, "discord_proxy_rate_limit_rejections_total", "1")
}

func TestMetricsIPCConnectedGaugeFollowsState(t *testing.T) {
	hub := NewHub(zap.NewNop())
	ipcState := string(StateDisconnected)
	m := NewMetrics(hub, func() string { return ipcState }, nil)
	srv := NewServer(hub, zap.NewNop(), WithMetrics(m))

	assertMetric(t, scrapeMetrics(t, srv), "discord_proxy_ipc_connected", "0")

	ipcState = string(StateConnected)
	assertMetric(t, scrapeMetrics(t, srv), "discord_proxy_ipc_connected", "1")
}

func TestMetricsDisabledByDefault(t *testing.T) {
	hub := NewHub(zap.NewNop())
	srv := NewServer(hub, zap.NewNop())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /metrics = %d, want %d (route not registered)", rec.Code, http.StatusNotFound)
	}
}

func TestMetricsNilRateLimiter(t *testing.T) {
	hub := NewHub(zap.NewNop())
	m := NewMetrics(hub, func() string { return string(StateConnected) }, nil)
	srv := NewServer(hub, zap.NewNop(), WithMetrics(m))

	body := scrapeMetrics(t, srv)
	assertMetric(t, body, "discord_proxy_rate_limit_rejections_total", "0")
	assertMetric(t, body, "discord_proxy_ipc_connected", "1")
}
