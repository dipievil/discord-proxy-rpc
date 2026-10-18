package server

import (
	"net/http"
	"runtime/debug"
	"strings"

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

	reserved := false
	if h.connTracker != nil {
		if !h.connTracker.TryAdd() {
			h.logger.Warn("WebSocket connection rejected: max connections reached",
				zap.Int("max", h.connTracker.maxConn))
			writeAPIError(w, h.logger, http.StatusServiceUnavailable,
				ErrCodeTooManyConns, "max connections reached")
			return
		}
		reserved = true
		// Release the reservation unless the connection is handed off to
		// trackDisconnect below (upgrade failure or a later panic).
		defer func() {
			if reserved {
				h.connTracker.Remove()
			}
		}()
	}

	conn, err := h.upgrader.Upgrade(w, r, subprotocolResponse(r))
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	reserved = false

	client := NewClient(conn, h.hub)
	h.hub.Register(client)

	h.logger.Info("client connected", zap.String("id", client.id))

	go h.trackDisconnect(client)
	go client.WritePump()
	go client.ReadPump()
}

// subprotocolResponse echoes back the bearer-token subprotocol the client
// offered, if any. Browsers fail the handshake when the server selects a
// subprotocol they did not offer, and gorilla/websocket only takes the selected
// subprotocol from the response header when the upgrader has no static
// subprotocol list configured.
func subprotocolResponse(r *http.Request) http.Header {
	for _, proto := range websocket.Subprotocols(r) {
		if strings.HasPrefix(proto, subprotocolTokenPrefix) {
			return http.Header{"Sec-WebSocket-Protocol": []string{proto}}
		}
	}
	return nil
}

func (h *WSHandler) trackDisconnect(client *Client) {
	<-client.Done
	if h.connTracker != nil {
		h.connTracker.Remove()
	}
}
