package server

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

const (
	writeWait         = 10 * time.Second
	pongWait          = 60 * time.Second
	defaultPingPeriod = 30 * time.Second
	maxMessageSize    = 64 * 1024
)

type Client struct {
	id         string
	conn       *websocket.Conn
	send       chan []byte
	events     map[string]bool
	hub        *Hub
	mu         sync.Mutex
	pingPeriod time.Duration
}

type Hub struct {
	clients            map[string]*Client
	mu                 sync.RWMutex
	register           chan *Client
	unregister         chan *Client
	broadcast          chan ServerMessage
	logger             *zap.Logger
	done               chan struct{}
	ClientID           string
	GetCurrentPresence func() types.Activity
}

func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan ServerMessage, 256),
		logger:     logger,
		done:       make(chan struct{}),
	}
}

func (h *Hub) Run(ctx context.Context) {
	defer func() {
		select {
		case <-h.done:
		default:
			close(h.done)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			h.closeAllClients()
			return
		case <-h.done:
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			h.mu.Unlock()
			h.logger.Info("client registered", zap.String("id", client.id), zap.Int("total", h.ClientCount()))
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Info("client unregistered", zap.String("id", client.id), zap.Int("total", h.ClientCount()))
		case msg := <-h.broadcast:
			if msg.Type == MsgTypeState && msg.ClientID == "" {
				msg.ClientID = h.ClientID
			}
			data, err := json.Marshal(msg)
			if err != nil {
				h.logger.Error("failed to marshal broadcast message", zap.Error(err))
				continue
			}
			h.mu.RLock()
			for _, client := range h.clients {
				client.mu.Lock()
				subscribed := client.events[msg.Type]
				client.mu.Unlock()
				if subscribed {
					select {
					case client.send <- data:
					default:
						h.logger.Warn("client send channel full, dropping message", zap.String("id", client.id))
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Register(client *Client) {
	select {
	case h.register <- client:
	case <-h.done:
	}
}

func (h *Hub) Unregister(client *Client) {
	select {
	case h.unregister <- client:
	case <-h.done:
	}
}

func (h *Hub) Broadcast(msg ServerMessage) {
	select {
	case h.broadcast <- msg:
	default:
		h.logger.Warn("broadcast channel full, dropping message")
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) Close() {
	h.closeAllClients()
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

func (h *Hub) closeAllClients() {
	h.mu.Lock()
	for id, client := range h.clients {
		close(client.send)
		delete(h.clients, id)
		client.conn.Close()
	}
	h.mu.Unlock()
}

func NewClient(conn *websocket.Conn, hub *Hub) *Client {
	return &Client{
		id:         uuid.New().String(),
		conn:       conn,
		send:       make(chan []byte, 256),
		events:     make(map[string]bool),
		hub:        hub,
		pingPeriod: defaultPingPeriod,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.hub.logger.Warn("unexpected close", zap.Error(err), zap.String("client", c.id))
			}
			return
		}

		msg, err := ParseClientMessage(data)
		if err != nil {
			c.hub.logger.Warn("invalid client message", zap.Error(err), zap.String("client", c.id))
			continue
		}

		switch msg.Type {
		case MsgTypeSubscribe:
			c.mu.Lock()
			for _, event := range msg.SubscribeEvents {
				c.events[event] = true
			}
			c.mu.Unlock()
			c.hub.logger.Debug("client subscribed", zap.String("client", c.id), zap.Strings("events", msg.SubscribeEvents))
		case MsgTypeGetCurrent:
			if c.hub.GetCurrentPresence != nil {
				activity := c.hub.GetCurrentPresence()
				resp, err := NewCurrentMessage(activity)
				if err != nil {
					c.hub.logger.Error("failed to create current message", zap.Error(err))
					continue
				}
				respData, err := json.Marshal(resp)
				if err != nil {
					c.hub.logger.Error("failed to marshal current message", zap.Error(err))
					continue
				}
				c.SendMessage(respData)
			}
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(c.pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) SendMessage(data []byte) {
	select {
	case c.send <- data:
	default:
		c.hub.logger.Warn("client send channel full, dropping message", zap.String("id", c.id))
	}
}

func (c *Client) subscribe(events ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range events {
		c.events[e] = true
	}
}
