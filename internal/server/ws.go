package server

import (
	"net/http"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var defaultUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type WSHandler struct {
	hub      *Hub
	logger   *zap.Logger
	auth     func(r *http.Request) bool
	upgrader *websocket.Upgrader
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

func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.auth != nil && !h.auth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	client := NewClient(conn, h.hub)
	h.hub.Register(client)

	h.logger.Info("client connected", zap.String("id", client.id))

	go client.WritePump()
	go client.ReadPump()
}
