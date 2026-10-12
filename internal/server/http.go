package server

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/web"
)

type Server struct {
	mux                *http.ServeMux
	hub                *Hub
	logger             *zap.Logger
	wsHandler          *WSHandler
	wsPath             string
	getCurrentPresence func() types.Activity
	getIPCState        func() string
	rateLimiter        *RateLimiter
	connTracker        *ConnectionTracker
}

type ServerOption func(*Server)

func WithGetCurrentPresence(fn func() types.Activity) ServerOption {
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

func NewServer(hub *Hub, logger *zap.Logger, opts ...ServerOption) *Server {
	s := &Server{
		mux:                http.NewServeMux(),
		hub:                hub,
		logger:             logger,
		wsPath:             "/ws",
		getCurrentPresence: func() types.Activity { return types.Activity{} },
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

	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc(s.wsPath, s.rateLimitWS(s.wsHandler.ServeHTTP))
	s.mux.HandleFunc("/api/presence", s.handlePresence)
	s.mux.HandleFunc("/api/state", s.handleState)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/readyz", s.handleReadyz)
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

func (s *Server) rateLimitWS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.rateLimiter != nil {
			ip := extractIP(r.RemoteAddr)
			if !s.rateLimiter.Allow(ip) {
				s.logger.Warn("WebSocket upgrade rate limited",
					zap.String("ip", ip),
					zap.String("remote", r.RemoteAddr),
				)
				writeAPIError(w, s.logger, http.StatusTooManyRequests,
					ErrCodeRateLimited, "rate limit exceeded")
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	http.FileServerFS(web.FS).ServeHTTP(w, r)
}

func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	activity := s.getCurrentPresence()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(activity); err != nil {
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
