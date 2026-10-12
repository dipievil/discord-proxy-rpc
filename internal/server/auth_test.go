package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func newTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func TestAuthMiddleware_Disabled(t *testing.T) {
	cfg := config.AuthConfig{Enabled: false, Token: "secret"}
	handler := AuthMiddleware(cfg, zap.NewNop())(newTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(cfg, zap.NewNop())(newTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertUnauthorized(t, rec)
}

func TestAuthMiddleware_EmptyToken(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(cfg, zap.NewNop())(newTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertUnauthorized(t, rec)
}

func TestAuthMiddleware_WrongToken(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(cfg, zap.NewNop())(newTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertUnauthorized(t, rec)
}

func TestAuthMiddleware_CorrectToken(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(cfg, zap.NewNop())(newTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}
}

func TestAuthMiddleware_CaseInsensitiveBearer(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(cfg, zap.NewNop())(newTestHandler())

	tests := []struct {
		name   string
		prefix string
	}{
		{"lowercase bearer", "bearer"},
		{"mixed case Bearer", "Bearer"},
		{"uppercase BEARER", "BEARER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.prefix+" my-secret-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_NoBearerPrefix(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(cfg, zap.NewNop())(newTestHandler())

	tests := []struct {
		name  string
		value string
	}{
		{"Basic scheme", "Basic dXNlcjpwYXNz"},
		{"No scheme", "my-secret-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.value)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertUnauthorized(t, rec)
		})
	}
}

func assertUnauthorized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", rec.Header().Get("Content-Type"))
	}
	var apiErr APIError
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if apiErr.Code != ErrCodeUnauthorized {
		t.Errorf("error code = %q, want %q", apiErr.Code, ErrCodeUnauthorized)
	}
	if apiErr.Message != "unauthorized" {
		t.Errorf("error message = %q, want %q", apiErr.Message, "unauthorized")
	}
}

func TestBearerAuthFunc(t *testing.T) {
	tests := []struct {
		name           string
		authEnabled    bool
		configuredToken string
		authHeader     string
		wantAllow      bool
	}{
		{
			name:            "auth disabled - no header",
			authEnabled:     false,
			configuredToken: "secret",
			authHeader:      "",
			wantAllow:       true,
		},
		{
			name:            "auth disabled - with header",
			authEnabled:     false,
			configuredToken: "secret",
			authHeader:      "Bearer anything",
			wantAllow:       true,
		},
		{
			name:            "missing header",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "",
			wantAllow:       false,
		},
		{
			name:            "empty token (Bearer with space)",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "Bearer ",
			wantAllow:       false,
		},
		{
			name:            "wrong token",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "Bearer wrong-token",
			wantAllow:       false,
		},
		{
			name:            "correct token - lowercase bearer",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "bearer my-secret-token",
			wantAllow:       true,
		},
		{
			name:            "correct token - mixed case Bearer",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "Bearer my-secret-token",
			wantAllow:       true,
		},
		{
			name:            "correct token - uppercase BEARER",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "BEARER my-secret-token",
			wantAllow:       true,
		},
		{
			name:            "extra whitespace around token",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "Bearer  my-secret-token  ",
			wantAllow:       true,
		},
		{
			name:            "missing scheme (just token)",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "my-secret-token",
			wantAllow:       false,
		},
		{
			name:            "non-bearer scheme (Basic)",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "Basic dXNlcjpwYXNz",
			wantAllow:       false,
		},
		{
			name:            "non-bearer scheme (Token)",
			authEnabled:     true,
			configuredToken: "my-secret-token",
			authHeader:      "Token my-secret-token",
			wantAllow:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.AuthConfig{
				Enabled: tt.authEnabled,
				Token:   tt.configuredToken,
			}
			authFn := BearerAuthFunc(cfg, zap.NewNop())

			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			got := authFn(req)
			if got != tt.wantAllow {
				t.Errorf("BearerAuthFunc() = %v, want %v", got, tt.wantAllow)
			}
		})
	}
}
