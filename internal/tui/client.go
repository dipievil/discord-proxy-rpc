package tui

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/server"
	"github.com/gorilla/websocket"
)

const dialTimeout = 10 * time.Second

// wsMsg carries either a parsed server message or the error that ended the
// connection. A parse error is non-fatal: the connection stays usable.
type wsMsg struct {
	msg   *server.ServerMessage
	err   error
	fatal bool
}

// Backend is the WebSocket consumer the model talks to. The real
// implementation is wsBackend; tests substitute a fake.
type Backend interface {
	// Messages returns the stream of events produced after a successful Dial.
	Messages() <-chan wsMsg
	// Dial connects, authenticates and subscribes. On success a read pump
	// starts feeding Messages.
	Dial(ctx context.Context) error
	// Close tears down the current connection, if any.
	Close() error
}

type wsBackend struct {
	url   string
	token string
	out   chan wsMsg
	conn  *websocket.Conn
}

// NewWSBackend returns a Backend that speaks the same WebSocket protocol as
// the web dashboard: bearer token in the Authorization header, then
// subscribe + get_current on open.
func NewWSBackend(url, token string) Backend {
	return &wsBackend{
		url:   url,
		token: token,
		out:   make(chan wsMsg, 16),
	}
}

func (b *wsBackend) Messages() <-chan wsMsg { return b.out }

func (b *wsBackend) Dial(ctx context.Context) error {
	header := http.Header{}
	if b.token != "" {
		header.Set("Authorization", "Bearer "+b.token)
	}

	dialer := websocket.Dialer{HandshakeTimeout: dialTimeout}
	conn, _, err := dialer.DialContext(ctx, b.url, header)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", b.url, err)
	}

	subscribe, err := server.NewSubscribeMessage([]string{server.MsgTypePresence, server.MsgTypeState})
	if err != nil {
		conn.Close()
		return fmt.Errorf("building subscribe message: %w", err)
	}
	if err := conn.WriteJSON(subscribe); err != nil {
		conn.Close()
		return fmt.Errorf("sending subscribe: %w", err)
	}

	getCurrent := server.NewGetCurrentMessage()
	if err := conn.WriteJSON(getCurrent); err != nil {
		conn.Close()
		return fmt.Errorf("sending get_current: %w", err)
	}

	b.conn = conn
	go b.readPump(ctx, conn)
	return nil
}

func (b *wsBackend) readPump(ctx context.Context, conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			b.deliver(ctx, wsMsg{err: fmt.Errorf("connection lost: %w", err), fatal: true})
			return
		}

		msg, err := server.ParseServerMessage(data)
		if err != nil {
			b.deliver(ctx, wsMsg{err: fmt.Errorf("invalid server message: %w", err)})
			continue
		}
		b.deliver(ctx, wsMsg{msg: msg})
	}
}

func (b *wsBackend) deliver(ctx context.Context, ev wsMsg) {
	select {
	case b.out <- ev:
	case <-ctx.Done():
	}
}

func (b *wsBackend) Close() error {
	if b.conn == nil {
		return nil
	}
	conn := b.conn
	b.conn = nil
	return conn.Close()
}
