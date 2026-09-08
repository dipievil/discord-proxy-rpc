package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func TestAuthMiddlewareDisabledPassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	cfg := config.AuthConfig{Enabled: false, Token: "secret"}
	handler := AuthMiddleware(inner, cfg, zap.NewNop())

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

func TestAuthMiddlewareValidTokenPasses(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(inner, cfg, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareMissingTokenReturns401(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(inner, cfg, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertUnauthorized(t, rec)
}

func TestAuthMiddlewareInvalidTokenReturns401(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(inner, cfg, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertUnauthorized(t, rec)
}

func TestAuthMiddlewareMalformedHeaderReturns401(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(inner, cfg, zap.NewNop())

	tests := []struct {
		name  string
		value string
	}{
		{"Basic scheme", "Basic dXNlcjpwYXNz"},
		{"No scheme", "my-secret-token"},
		{"Empty Bearer", "Bearer "},
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

func TestAuthMiddlewareEmptyBearerTokenReturns401(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cfg := config.AuthConfig{Enabled: true, Token: "my-secret-token"}
	handler := AuthMiddleware(inner, cfg, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertUnauthorized(t, rec)
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid", "Bearer abc123", "abc123"},
		{"empty header", "", ""},
		{"no prefix", "abc123", ""},
		{"basic auth", "Basic dXNlcjpwYXNz", ""},
		{"bearer with spaces", "Bearer   token  ", "token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got := extractBearerToken(req)
			if got != tt.want {
				t.Errorf("extractBearerToken() = %q, want %q", got, tt.want)
			}
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
