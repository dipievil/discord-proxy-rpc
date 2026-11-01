package server

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

// AuthMiddleware rejects requests without a valid bearer token. It delegates the
// decision and the rejection logging to BearerAuthFunc so that HTTP and
// WebSocket authentication share a single validation path and log severity.
func AuthMiddleware(cfg config.AuthConfig, logger *zap.Logger) func(http.Handler) http.Handler {
	allow := BearerAuthFunc(cfg, logger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allow(r) {
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

// subprotocolTokenPrefix marks the WebSocket subprotocol a browser-based client
// uses to carry its bearer token. The browser WebSocket API cannot set an
// Authorization header, so the token travels in the subprotocol list instead,
// base64url-encoded to stay within the header's token grammar.
const subprotocolTokenPrefix = "proxy-token-"

// extractSubprotocolToken returns the bearer token carried in the WebSocket
// subprotocol list, or "" when no token subprotocol is present.
func extractSubprotocolToken(r *http.Request) string {
	header := r.Header.Get("Sec-WebSocket-Protocol")
	if header == "" {
		return ""
	}

	for _, proto := range strings.Split(header, ",") {
		proto = strings.TrimSpace(proto)
		if !strings.HasPrefix(proto, subprotocolTokenPrefix) {
			continue
		}

		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(proto, subprotocolTokenPrefix))
		if err != nil || len(decoded) == 0 {
			return ""
		}
		return string(decoded)
	}

	return ""
}

// authCookieName carries an already-validated bearer token to the requests a
// browser makes after a token-authenticated page load (the static dashboard
// assets). A browser navigation cannot set an Authorization header, and
// appending ?token= to every asset URL would leak the token into access logs
// and Referer headers.
const authCookieName = "proxy-rpc-token"

// authCookieTTL bounds how long a token-authenticated dashboard keeps loading
// without the token in its URL. It only has to outlive a page load, but a
// longer window also covers a reload after the dashboard strips the token from
// the address bar. The WebSocket does not depend on it: the dashboard keeps the
// token in sessionStorage.
const authCookieTTL = 15 * time.Minute

// extractQueryToken returns the bearer token carried in the ?token= query
// parameter, which the dashboard uses because a browser navigation cannot set
// an Authorization header.
func extractQueryToken(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// encodeAuthCookieValue base64url-encodes a token for storage in a cookie. A
// raw token may contain bytes (space, comma, semicolon) that net/http silently
// strips from a cookie value, which would turn a valid token into a mismatch.
func encodeAuthCookieValue(token string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(token))
}

func decodeAuthCookieValue(value string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 {
		return ""
	}
	return string(decoded)
}

// extractCookieToken returns the bearer token stored in the auth cookie, or ""
// when the cookie is absent or malformed.
func extractCookieToken(r *http.Request) string {
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return ""
	}
	return decodeAuthCookieValue(strings.TrimSpace(cookie.Value))
}

// setAuthCookie stores a token that was just validated from the query parameter
// so the static assets the page loads next authenticate too.
func setAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    encodeAuthCookieValue(token),
		Path:     "/",
		MaxAge:   int(authCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

// extractToken returns the bearer token presented with the request, or "".
// Sources, in precedence order: Authorization header, WebSocket subprotocol,
// auth cookie, ?token= query parameter. The WebSocket subprotocol exists
// because browsers cannot set headers on a WebSocket upgrade; the last two
// exist because a browser navigation cannot set headers either.
func extractToken(r *http.Request) string {
	if token := extractBearerToken(r); token != "" {
		return token
	}
	if token := extractSubprotocolToken(r); token != "" {
		return token
	}
	if token := extractCookieToken(r); token != "" {
		return token
	}
	return extractQueryToken(r)
}

// BearerAuthFunc returns a func(*http.Request) bool that validates the bearer
// token carried by the request. Intended for WebSocket upgrade authentication
// and HTTP route gating, where a simple boolean allow/deny is needed without
// writing HTTP responses. See extractToken for the accepted sources.
func BearerAuthFunc(cfg config.AuthConfig, logger *zap.Logger) func(*http.Request) bool {
	return func(r *http.Request) bool {
		if !cfg.Enabled {
			return true
		}

		token := extractToken(r)
		if token == "" {
			logger.Warn("auth: no bearer token in Authorization header, WebSocket subprotocol, cookie or token query parameter",
				zap.String("remote", r.RemoteAddr),
			)
			return false
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
			logger.Warn("auth: invalid token",
				zap.String("remote", r.RemoteAddr),
			)
			return false
		}

		return true
	}
}
