package server

import (
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
	body := rec.Body.String()
	if body != `{"error":"unauthorized"}` {
		t.Errorf("expected JSON error body, got %q", body)
	}
}
