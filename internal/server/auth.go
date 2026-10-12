package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func AuthMiddleware(cfg config.AuthConfig, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
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
				writeAPIError(w, logger, http.StatusUnauthorized, ErrCodeUnauthorized, "unauthorized")
				return
			}

			if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
				logger.Warn("auth: invalid token",
					zap.String("remote", r.RemoteAddr),
				)
				writeAPIError(w, logger, http.StatusUnauthorized, ErrCodeUnauthorized, "unauthorized")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return ""
	}

	return token
}

// BearerAuthFunc returns a func(*http.Request) bool that validates
// the Authorization header against the configured bearer token.
// Intended for WebSocket upgrade authentication where a simple
// boolean allow/deny is needed without writing HTTP responses.
func BearerAuthFunc(cfg config.AuthConfig, logger *zap.Logger) func(*http.Request) bool {
	return func(r *http.Request) bool {
		if !cfg.Enabled {
			return true
		}

		token := extractBearerToken(r)
		if token == "" {
			logger.Debug("auth: missing or malformed Authorization header",
				zap.String("remote", r.RemoteAddr),
			)
			return false
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
			logger.Debug("auth: invalid token",
				zap.String("remote", r.RemoteAddr),
			)
			return false
		}

		return true
	}
}
