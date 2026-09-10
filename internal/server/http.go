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
	getCurrentPresence func() types.Activity
	getIPCState        func() string
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

func NewServer(hub *Hub, logger *zap.Logger, opts ...ServerOption) *Server {
	s := &Server{
		mux:                http.NewServeMux(),
		hub:                hub,
		logger:             logger,
		getCurrentPresence: func() types.Activity { return types.Activity{} },
		getIPCState:        func() string { return string(StateDisconnected) },
	}

	s.wsHandler = NewWSHandler(hub, logger)

	for _, opt := range opts {
		opt(s)
	}

	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/ws", s.wsHandler.ServeHTTP)
	s.mux.HandleFunc("/api/presence", s.handlePresence)
	s.mux.HandleFunc("/api/state", s.handleState)
	s.mux.HandleFunc("/health", s.handleHealth)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	http.FileServerFS(web.FS).ServeHTTP(w, r)
}

func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	activity := s.getCurrentPresence()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(activity)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	state := s.getIPCState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": state})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
