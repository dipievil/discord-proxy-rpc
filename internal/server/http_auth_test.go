package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

const testHTTPAuthToken = "s3cret-token"

// newAuthedTestServer serves the real server with Bearer auth enabled, so the
// route gating is exercised through the same path the binary uses.
func newAuthedTestServer(t *testing.T, token string) (*Server, *httptest.Server, *Hub, func()) {
	t.Helper()

	cfg := config.AuthConfig{Enabled: true, Token: token}
	return newTestHTTPServer(t, WithAuth(BearerAuthFunc(cfg, zap.NewNop())))
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	routes := []struct {
		name   string
		path   string
		api    bool
		okBody string
	}{
		{name: "dashboard index", path: "/", okBody: "Discord Proxy RPC"},
		{name: "explicit index", path: "/index.html", okBody: "Discord Proxy RPC"},
		{name: "static js asset", path: "/app.js"},
		{name: "static css asset", path: "/style.css"},
		{name: "presence API", path: "/api/presence", api: true},
		{name: "state API", path: "/api/state", api: true},
	}

	credentials := []struct {
		name      string
		bearer    string
		query     string
		cookie    string
		wantAllow bool
	}{
		{name: "no credentials"},
		{name: "valid bearer header", bearer: testHTTPAuthToken, wantAllow: true},
		{name: "wrong bearer header", bearer: "wrong-token"},
		{name: "valid query token", query: testHTTPAuthToken, wantAllow: true},
		{name: "wrong query token", query: "wrong-token"},
		{name: "valid cookie", cookie: encodeAuthCookieValue(testHTTPAuthToken), wantAllow: true},
		{name: "wrong cookie", cookie: encodeAuthCookieValue("wrong-token")},
		{name: "malformed cookie", cookie: "!!!not-base64!!!"},
	}

	_, ts, _, cleanup := newAuthedTestServer(t, testHTTPAuthToken)
	defer cleanup()

	for _, route := range routes {
		for _, cred := range credentials {
			t.Run(route.name+"/"+cred.name, func(t *testing.T) {
				req, err := http.NewRequest(http.MethodGet, ts.URL+route.path, nil)
				if err != nil {
					t.Fatalf("NewRequest: %v", err)
				}
				if cred.bearer != "" {
					req.Header.Set("Authorization", "Bearer "+cred.bearer)
				}
				if cred.query != "" {
					req.URL.RawQuery = "token=" + cred.query
				}
				if cred.cookie != "" {
					req.Header.Set("Cookie", authCookieName+"="+cred.cookie)
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				defer resp.Body.Close()

				if !cred.wantAllow {
					if route.api {
						assertJSONUnauthorized(t, resp)
					} else {
						assertPlainUnauthorized(t, resp)
					}
					return
				}

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
				}
				if route.okBody != "" {
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatalf("read body: %v", err)
					}
					if !strings.Contains(string(body), route.okBody) {
						t.Errorf("body does not contain %q", route.okBody)
					}
				}
			})
		}
	}
}

func TestHealthEndpointsBypassAuth(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	cfg := config.AuthConfig{Enabled: true, Token: testHTTPAuthToken}
	srv := NewServer(hub, zap.NewNop(),
		WithAuth(BearerAuthFunc(cfg, zap.NewNop())),
		WithGetIPCState(func() string { return string(StateConnected) }),
	)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	defer srv.Close()

	for _, ep := range []string{"/health", "/healthz", "/readyz"} {
		resp, err := http.Get(ts.URL + ep)
		if err != nil {
			t.Fatalf("GET %s: %v", ep, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, want %d (probes stay unauthenticated)", ep, resp.StatusCode, http.StatusOK)
		}
	}
}

func TestRoutesStayOpenWhenAuthDisabled(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	for _, ep := range []string{"/", "/index.html", "/app.js", "/api/presence", "/api/state"} {
		resp, err := http.Get(ts.URL + ep)
		if err != nil {
			t.Fatalf("GET %s: %v", ep, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, want %d (auth disabled)", ep, resp.StatusCode, http.StatusOK)
		}
	}
}

func TestDashboardQueryTokenSetsCookie(t *testing.T) {
	_, ts, _, cleanup := newAuthedTestServer(t, testHTTPAuthToken)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/?token=" + testHTTPAuthToken)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != authCookieName {
		t.Errorf("cookie name = %q, want %q", cookie.Name, authCookieName)
	}
	if cookie.Value != encodeAuthCookieValue(testHTTPAuthToken) {
		t.Errorf("cookie value = %q, want the base64url-encoded token", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want %v", cookie.SameSite, http.SameSiteStrictMode)
	}
	if cookie.Path != "/" {
		t.Errorf("cookie Path = %q, want %q", cookie.Path, "/")
	}
	if cookie.MaxAge <= 0 {
		t.Errorf("cookie MaxAge = %d, want > 0 (short-lived)", cookie.MaxAge)
	}

	// The cookie must carry the session for the assets the page loads next.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/app.js", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)

	assetResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("asset request: %v", err)
	}
	defer assetResp.Body.Close()

	if assetResp.StatusCode != http.StatusOK {
		t.Errorf("asset status = %d, want %d", assetResp.StatusCode, http.StatusOK)
	}
}

func TestNoAuthCookieWithoutQueryToken(t *testing.T) {
	_, ts, _, cleanup := newAuthedTestServer(t, testHTTPAuthToken)
	defer cleanup()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testHTTPAuthToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if cookies := resp.Cookies(); len(cookies) != 0 {
		t.Errorf("got %d cookies, want 0 (no ?token= in the request)", len(cookies))
	}
}

func TestInvalidQueryTokenSetsNoCookie(t *testing.T) {
	_, ts, _, cleanup := newAuthedTestServer(t, testHTTPAuthToken)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/?token=wrong-token")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	assertPlainUnauthorized(t, resp)
	if cookies := resp.Cookies(); len(cookies) != 0 {
		t.Errorf("got %d cookies, want 0 for an invalid token", len(cookies))
	}
}

// TestAuthCookieRoundTripsArbitraryToken pins the reason the cookie value is
// base64url-encoded: net/http silently drops bytes such as spaces, commas and
// semicolons from a raw cookie value, which would turn a valid token into a
// mismatch.
func TestAuthCookieRoundTripsArbitraryToken(t *testing.T) {
	tokens := []string{
		"plain-token",
		"token with spaces",
		"token,with,commas",
		"token;with;semicolons",
		"token=with=equals",
	}

	for _, token := range tokens {
		t.Run(token, func(t *testing.T) {
			if got := decodeAuthCookieValue(encodeAuthCookieValue(token)); got != token {
				t.Fatalf("decode(encode(%q)) = %q", token, got)
			}

			// The encoded form authenticates even when the raw token cannot
			// travel in a cookie or a query parameter intact.
			_, ts, _, cleanup := newAuthedTestServer(t, token)
			defer cleanup()

			encoded := encodeAuthCookieValue(token)

			req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/state", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("Cookie", authCookieName+"="+encoded)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want %d (encoded cookie must authenticate %q)", resp.StatusCode, http.StatusOK, token)
			}

			rawReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/state", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			rawReq.Header.Set("Cookie", authCookieName+"="+token)

			rawResp, err := http.DefaultClient.Do(rawReq)
			if err != nil {
				t.Fatalf("raw request: %v", err)
			}
			defer rawResp.Body.Close()

			if rawResp.StatusCode != http.StatusUnauthorized {
				t.Errorf("raw cookie status = %d, want %d (unencoded %q is mangled)", rawResp.StatusCode, http.StatusUnauthorized, token)
			}
		})
	}
}

func TestExtractTokenPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		subproto   string
		cookie     string
		query      string
		want       string
	}{
		{
			name: "nothing presented",
			want: "",
		},
		{
			name:       "bearer header alone",
			authHeader: "Bearer header-token",
			want:       "header-token",
		},
		{
			name:     "subprotocol alone",
			subproto: subprotocolTokenPrefix + encodeAuthCookieValue("subprotocol-token"),
			want:     "subprotocol-token",
		},
		{
			name:   "cookie alone",
			cookie: encodeAuthCookieValue("cookie-token"),
			want:   "cookie-token",
		},
		{
			name:  "query alone",
			query: "query-token",
			want:  "query-token",
		},
		{
			name:       "header beats every other source",
			authHeader: "Bearer header-token",
			subproto:   subprotocolTokenPrefix + encodeAuthCookieValue("subprotocol-token"),
			cookie:     encodeAuthCookieValue("cookie-token"),
			query:      "query-token",
			want:       "header-token",
		},
		{
			name:     "subprotocol beats cookie and query",
			subproto: subprotocolTokenPrefix + encodeAuthCookieValue("subprotocol-token"),
			cookie:   encodeAuthCookieValue("cookie-token"),
			query:    "query-token",
			want:     "subprotocol-token",
		},
		{
			name:   "cookie beats query",
			cookie: encodeAuthCookieValue("cookie-token"),
			query:  "query-token",
			want:   "cookie-token",
		},
		{
			name:   "malformed cookie falls through to query",
			cookie: "!!!not-base64!!!",
			query:  "query-token",
			want:   "query-token",
		},
		{
			name:       "blank query is ignored",
			authHeader: "Bearer header-token",
			query:      "   ",
			want:       "header-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.subproto != "" {
				req.Header.Set("Sec-WebSocket-Protocol", tt.subproto)
			}
			if tt.cookie != "" {
				req.Header.Set("Cookie", authCookieName+"="+tt.cookie)
			}
			if tt.query != "" {
				req.URL.RawQuery = "token=" + tt.query
			}

			if got := extractToken(req); got != tt.want {
				t.Errorf("extractToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func assertJSONUnauthorized(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if apiErr.Code != ErrCodeUnauthorized {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeUnauthorized)
	}
}

func assertPlainUnauthorized(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain* (dashboard 401 is not JSON)", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
		t.Errorf("body = %q, want plain text, not JSON", string(body))
	}
	if !strings.Contains(string(body), "unauthorized") {
		t.Errorf("body = %q, want an unauthorized message", string(body))
	}
}
