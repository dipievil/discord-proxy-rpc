package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func AuthMiddleware(next http.Handler, cfg config.AuthConfig, logger *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		token := extractBearerToken(r)
		if token == "" {
			logger.Warn("auth: missing or malformed Authorization header",
				zap.String("remote", r.RemoteAddr),
			)
			writeUnauthorized(w)
			return
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
			logger.Warn("auth: invalid token",
				zap.String("remote", r.RemoteAddr),
			)
			writeUnauthorized(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}

	token := strings.TrimSpace(auth[len(prefix):])
	if token == "" {
		return ""
	}

	return token
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthorized"}`))
}
