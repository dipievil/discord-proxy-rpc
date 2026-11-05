package server

import (
	"encoding/json"
	"html"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/web"
)

// wsPathPlaceholder is replaced in the served dashboard with the configured
// WebSocket route so the page connects to the right path.
const wsPathPlaceholder = "__WS_PATH__"

type Server struct {
	mux                *http.ServeMux
	hub                *Hub
	logger             *zap.Logger
	wsHandler          *WSHandler
	auth               func(*http.Request) bool
	wsPath             string
	getCurrentPresence func() types.PresenceSnapshot
	getIPCState        func() string
	rateLimiter        *RateLimiter
	connTracker        *ConnectionTracker
	trustedProxies     []*net.IPNet
}

type ServerOption func(*Server)

func WithGetCurrentPresence(fn func() types.PresenceSnapshot) ServerOption {
	return func(s *Server) {
		s.getCurrentPresence = fn
	}
}

func WithGetIPCState(fn func() string) ServerOption {
	return func(s *Server) {
		s.getIPCState = fn
	}
}

func WithAuth(authFunc func(r *http.Request) bool) ServerOption {
	return func(s *Server) {
		s.auth = authFunc
		s.wsHandler = s.wsHandler.WithAuth(authFunc)
	}
}

func WithWSPath(path string) ServerOption {
	return func(s *Server) {
		if path != "" {
			s.wsPath = path
		}
	}
}

func WithRateLimiter(rl *RateLimiter) ServerOption {
	return func(s *Server) {
		s.rateLimiter = rl
	}
}

func WithConnectionTracker(ct *ConnectionTracker) ServerOption {
	return func(s *Server) {
		s.connTracker = ct
	}
}

// WithTrustedProxies lists the CIDR blocks whose X-Forwarded-For header is
// honoured when resolving the rate-limit key. With no entry the TCP peer is
// always the client, so a client cannot forge its identity; entries are
// validated by the config loader and invalid ones are ignored here.
func WithTrustedProxies(entries []string) ServerOption {
	return func(s *Server) {
		s.trustedProxies = parseTrustedProxies(entries)
	}
}

func parseTrustedProxies(entries []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(entries))
	for _, entry := range entries {
		_, network, err := net.ParseCIDR(strings.TrimSpace(entry))
		if err != nil {
			continue
		}
		networks = append(networks, network)
	}
	return networks
}

func NewServer(hub *Hub, logger *zap.Logger, opts ...ServerOption) *Server {
	s := &Server{
		mux:                http.NewServeMux(),
		hub:                hub,
		logger:             logger,
		wsPath:             "/ws",
		getCurrentPresence: func() types.PresenceSnapshot { return types.PresenceSnapshot{Source: types.SourceNone} },
		getIPCState:        func() string { return string(StateDisconnected) },
	}

	s.wsHandler = NewWSHandler(hub, logger)

	for _, opt := range opts {
		opt(s)
	}

	if s.connTracker != nil {
		s.wsHandler = s.wsHandler.WithConnectionTracker(s.connTracker)
	}

	s.hub.GetCurrentPresence = s.getCurrentPresence
	s.hub.GetCurrentState = func() ConnectionState { return connectionStateFromIPC(s.getIPCState()) }

	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.rateLimit(s.requireAuth(s.handleDashboard, false), false))
	s.mux.HandleFunc(s.wsPath, s.rateLimit(s.wsHandler.ServeHTTP, true))
	s.mux.HandleFunc("/api/presence", s.rateLimit(s.requireAuth(s.handlePresence, true), true))
	s.mux.HandleFunc("/api/state", s.rateLimit(s.requireAuth(s.handleState, true), true))
	// Health endpoints stay open on purpose: they are consumed by container
	// and Kubernetes probes, which cannot carry the bearer token. /metrics is
	// exempt for Prometheus scrapes that may run without credentials. Both are
	// never subject to the per-IP HTTP rate limit.
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/readyz", s.handleReadyz)
}

// requireAuth gates a data route behind the configured bearer token when auth
// is enabled; with auth disabled it returns the handler untouched. API routes
// answer 401 with the standard JSON error envelope, while dashboard routes
// answer a plain-text 401 because the browser renders the body directly.
func (s *Server) requireAuth(next http.HandlerFunc, apiRoute bool) http.HandlerFunc {
	if s.auth == nil {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth(r) {
			if apiRoute {
				writeAPIError(w, s.logger, http.StatusUnauthorized, ErrCodeUnauthorized, "unauthorized")
			} else {
				http.Error(w, "unauthorized: open with ?token=<token> or send Authorization: Bearer <token>",
					http.StatusUnauthorized)
			}
			return
		}

		// A token presented in the URL authenticated this request; remember it
		// so the static assets the page loads next authenticate as well.
		if token := extractQueryToken(r); token != "" {
			setAuthCookie(w, r, token)
		}

		next.ServeHTTP(w, r)
	}
}

// maxRequestBodyBytes caps the request body accepted on any route, so a
// malicious client cannot exhaust memory by streaming an unbounded payload.
const maxRequestBodyBytes = 1 << 20

// ServeHTTP enforces the request body size limit before dispatching to the
// mux. Declared oversized bodies are rejected up front with 413; bodies of
// unknown length are truncated by MaxBytesReader so a read past the limit
// fails instead of buffering indefinitely.
// RecoveryMiddleware wraps the mux to catch panics from any HTTP handler
// (including the dashboard, REST endpoints, and health checks). The WebSocket
// handler has its own inner recover; it catches panics first so the outer
// middleware never sees them — no problematic double-wrap.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxRequestBodyBytes {
		s.logger.Warn("request body too large",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int64("content_length", r.ContentLength),
		)
		writeAPIError(w, s.logger, http.StatusRequestEntityTooLarge,
			ErrCodePayloadTooLarge, "request body too large")
		return
	}

	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	}

	RecoveryMiddleware(s.logger, s.mux).ServeHTTP(w, r)
}

// Close releases resources owned by the server, currently the rate limiter's
// background cleanup goroutine. It is idempotent.
func (s *Server) Close() {
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
}

// rateLimit applies the shared per-IP rate limiter to a data route: the
// dashboard (and its static assets), the REST endpoints and the WebSocket
// upgrade all draw from one budget per client, so switching surfaces never
// bypasses the limit. It wraps authentication rather than the reverse, so an
// unauthenticated flood is rejected before the token check; because the budget
// is keyed by IP, a 401/429 distinction never reveals whether a token is valid.
// apiRoute selects the rejection body: the standard JSON envelope for the REST
// and WebSocket surface, plain text for the dashboard, which the browser
// renders directly.
//
// Health probes and /metrics are registered without this middleware so they
// keep answering while a client is rate limited.
func (s *Server) rateLimit(next http.HandlerFunc, apiRoute bool) http.HandlerFunc {
	if !s.rateLimiter.Enabled() {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ip := s.clientIP(r)
		if !s.rateLimiter.Allow(ip) {
			retryAfter := retryAfterSeconds(s.rateLimiter.RetryAfter(ip))
			s.logRateLimited(ip, r.URL.Path, retryAfter)

			w.Header().Set("Retry-After", retryAfter)
			// A 429 for a static asset must not be replayed from a cache
			// after the window has elapsed.
			w.Header().Set("Cache-Control", "no-store")

			if apiRoute {
				writeAPIError(w, s.logger, http.StatusTooManyRequests,
					ErrCodeRateLimited, "rate limit exceeded")
				return
			}
			http.Error(w, "rate limit exceeded, retry in "+retryAfter+"s",
				http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// logRateLimited warns once per IP per window and logs further rejections at
// debug level, so a reconnecting dashboard cannot flood the log; the rejection
// metric carries the exact volume.
func (s *Server) logRateLimited(ip, path, retryAfter string) {
	fields := []zap.Field{
		zap.String("ip", ip),
		zap.String("path", path),
		zap.String("retry_after", retryAfter+"s"),
	}
	if s.rateLimiter.MarkWarned(ip) {
		s.logger.Warn("request rate limited", fields...)
		return
	}
	s.logger.Debug("request rate limited", fields...)
}

// clientIP resolves the rate-limit key for a request. Without a trusted proxy
// the TCP peer is the client. Behind a configured trusted proxy the last
// X-Forwarded-For hop that is not itself trusted becomes the client, so a
// single ingress does not collapse every client into one shared budget while
// a client still cannot forge its identity by sending the header itself.
func (s *Server) clientIP(r *http.Request) string {
	peer := extractIP(r.RemoteAddr)
	peerIP := net.ParseIP(peer)
	if !s.isTrustedProxy(peerIP) {
		return peer
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}

	hops := strings.Split(xff, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			break
		}
		if !s.isTrustedProxy(ip) {
			return ip.String()
		}
	}
	return peer
}

func (s *Server) isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, network := range s.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// retryAfterSeconds renders a Retry-After delay in whole seconds, rounding up
// and never below one so a client always waits at least a second.
func retryAfterSeconds(d time.Duration) string {
	seconds := int(math.Ceil(d.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		s.serveDashboardIndex(w)
		return
	}
	http.FileServerFS(web.FS).ServeHTTP(w, r)
}

// serveDashboardIndex serves the dashboard shell with the configured
// WebSocket path injected, so a custom server.ws_path works without a client
// change.
func (s *Server) serveDashboardIndex(w http.ResponseWriter) {
	page, err := fs.ReadFile(web.FS, "index.html")
	if err != nil {
		s.logger.Error("failed to read dashboard index", zap.Error(err))
		writeAPIError(w, s.logger, http.StatusInternalServerError, ErrCodeInternal, "internal server error")
		return
	}

	body := strings.Replace(string(page), wsPathPlaceholder, html.EscapeString(s.wsPath), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if _, err := io.WriteString(w, body); err != nil {
		s.logger.Error("failed to write dashboard index", zap.Error(err))
	}
}

func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	snapshot := s.getCurrentPresence()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		s.logger.Error("failed to encode presence response", zap.Error(err))
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	state := s.getIPCState()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": state}); err != nil {
		s.logger.Error("failed to encode state response", zap.Error(err))
	}
}

// connectionStateFromIPC maps the IPC client's state string (/api/state) onto
// the connection states the WebSocket protocol carries. A dial in progress has
// no WebSocket equivalent yet and is reported as reconnecting.
func connectionStateFromIPC(state string) ConnectionState {
	switch state {
	case string(StateConnected):
		return StateConnected
	case string(StateReconnecting), "connecting":
		return StateReconnecting
	default:
		return StateDisconnected
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok", "ipc": s.getIPCState()}); err != nil {
		s.logger.Error("failed to encode health response", zap.Error(err))
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logger.Error("failed to encode healthz response", zap.Error(err))
	}
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.getIPCState() == string(StateConnected) {
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok", "ipc": "connected"}); err != nil {
			s.logger.Error("failed to encode readyz response", zap.Error(err))
		}
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "ipc": "disconnected"}); err != nil {
		s.logger.Error("failed to encode readyz response", zap.Error(err))
	}
}
