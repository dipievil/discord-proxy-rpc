package server

import (
	"net/http"
	"runtime/debug"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var defaultUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type WSHandler struct {
	hub         *Hub
	logger      *zap.Logger
	auth        func(r *http.Request) bool
	upgrader    *websocket.Upgrader
	connTracker *ConnectionTracker
}

func NewWSHandler(hub *Hub, logger *zap.Logger) *WSHandler {
	return &WSHandler{
		hub:      hub,
		logger:   logger,
		upgrader: &defaultUpgrader,
	}
}

func (h *WSHandler) WithAuth(authFunc func(r *http.Request) bool) *WSHandler {
	h.auth = authFunc
	return h
}

func (h *WSHandler) WithUpgrader(u *websocket.Upgrader) *WSHandler {
	h.upgrader = u
	return h
}

func (h *WSHandler) WithConnectionTracker(ct *ConnectionTracker) *WSHandler {
	h.connTracker = ct
	return h
}

func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			h.logger.Error("recovered from panic in WebSocket handler",
				zap.Any("error", rec),
				zap.String("stack", string(debug.Stack())),
				zap.String("remote", r.RemoteAddr),
			)
			writeAPIError(w, h.logger, http.StatusInternalServerError, ErrCodeInternal, "internal server error")
		}
	}()

	if h.auth != nil && !h.auth(r) {
		writeAPIError(w, h.logger, http.StatusUnauthorized, ErrCodeUnauthorized, "unauthorized")
		return
	}

	if h.connTracker != nil && !h.connTracker.CanConnect() {
		h.logger.Warn("WebSocket connection rejected: max connections reached",
			zap.Int("max", h.connTracker.maxConn))
		writeAPIError(w, h.logger, http.StatusServiceUnavailable,
			ErrCodeTooManyConns, "max connections reached")
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	if h.connTracker != nil {
		h.connTracker.Add()
	}

	client := NewClient(conn, h.hub)
	h.hub.Register(client)

	h.logger.Info("client connected", zap.String("id", client.id))

	go h.trackDisconnect(client)
	go client.WritePump()
	go client.ReadPump()
}

func (h *WSHandler) trackDisconnect(client *Client) {
	<-client.Done
	if h.connTracker != nil {
		h.connTracker.Remove()
	}
}
