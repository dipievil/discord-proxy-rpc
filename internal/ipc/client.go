// Package ipc implements a read-only client for Discord's local IPC socket,
// wrapping github.com/dragsbruh/gopresence.
package ipc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dragsbruh/gopresence"
	gopresenceipc "github.com/dragsbruh/gopresence/ipc"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/platform"
)

// Discord RPC opcodes.
const (
	OpHandshake int32 = 0
	OpFrame     int32 = 1
	OpClose     int32 = 2
	OpPing      int32 = 3
	OpPong      int32 = 4
)

const (
	connectTimeout  = 2 * time.Second
	eventBufferSize = 64
)

// State describes the connection lifecycle of an IPC session.
type State int

const (
	StateDisconnected State = iota
	StateConnecting
	StateConnected
	StateReconnecting
)

func (s State) String() string {
	switch s {
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	default:
		return "disconnected"
	}
}

// Event is a single frame observed on the IPC socket.
// Frame is set for OpFrame frames; Err signals that the session has ended.
type Event struct {
	Opcode int32
	Frame  *presence.ResponseFrame
	Err    error
}

// Client is a read-only wrapper around a single Discord IPC session. It dials
// the first available discord-ipc-{0..9} socket, performs the handshake
// (v=1, client_id), and forwards every subsequent frame to subscribers.
type Client struct {
	clientID string
	logger   *zap.Logger

	paths   []string
	timeout time.Duration

	mu       sync.Mutex
	ipcConn  *gopresenceipc.Client
	presence *presence.Client
	user     string
	state    State
	events   chan Event
}

// New returns a Client for the given Discord application and logger.
func New(clientID string, logger *zap.Logger) *Client {
	return &Client{
		clientID: clientID,
		logger:   logger,
		paths:    platform.IPCPaths(),
		timeout:  connectTimeout,
		state:    StateDisconnected,
	}
}

// State reports the current connection state.
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// User reports the connected Discord user's display name ("" until READY).
func (c *Client) User() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.user
}

// Events returns the output channel for the current IPC session. The channel
// is closed when the session's read loop terminates.
func (c *Client) Events() <-chan Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.events
}

// Connect dials discord-ipc-{0..9} in order and performs the handshake. It
// returns on the first successful socket; a socket that dials but fails the
// handshake is skipped in favour of the next one.
func (c *Client) Connect() error {
	c.setState(StateConnecting)

	var lastErr error
	for _, path := range c.paths {
		conn := gopresenceipc.New()
		if err := conn.Connect(path, c.timeout); err != nil {
			lastErr = err
			c.logger.Debug("ipc dial failed", zap.String("path", path), zap.Error(err))
			continue
		}

		pres := presence.NewFrom(conn)
		ready, err := pres.Login(c.clientID)
		if err != nil {
			lastErr = err
			_ = conn.Close()
			c.logger.Debug("ipc handshake failed", zap.String("path", path), zap.Error(err))
			continue
		}

		c.mu.Lock()
		c.ipcConn = conn
		c.presence = pres
		c.user = displayName(ready)
		c.events = make(chan Event, eventBufferSize)
		c.mu.Unlock()

		c.setState(StateConnected)
		c.logger.Info("connected to Discord as "+c.User(),
			zap.String("user_id", ready.User.ID),
			zap.String("socket", path))
		return nil
	}

	c.setState(StateDisconnected)
	return fmt.Errorf("connecting to discord ipc: %w", lastErr)
}

// Run reads frames until the session ends or ctx is cancelled. Received
// events are published to Events(); the channel is closed before returning.
func (c *Client) Run(ctx context.Context) error {
	conn := c.ipcConnection()
	if conn == nil {
		return errors.New("ipc: Run called before Connect")
	}

	events := c.events
	defer close(events)

	for {
		op, data, err := conn.Read()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c.setState(StateDisconnected)
			c.emitTerminalEvent(ctx, events, Event{Opcode: op, Err: err})
			return err
		}

		switch op {
		case OpClose:
			c.setState(StateDisconnected)
			c.emitTerminalEvent(ctx, events, Event{Opcode: op, Err: errors.New("ipc connection closed by Discord")})
			return nil
		case OpFrame:
			frame, parseErr := presence.ParseFrame(data)
			if parseErr != nil {
				c.logger.Warn("failed to parse ipc frame", zap.Error(parseErr))
				c.emitEvent(ctx, events, Event{Opcode: op, Err: parseErr})
				continue
			}
			c.emitEvent(ctx, events, Event{Opcode: op, Frame: frame})
		default:
			c.emitEvent(ctx, events, Event{Opcode: op})
		}
	}
}

// SendRaw writes a raw opcode frame (e.g. OpPing for health checks).
func (c *Client) SendRaw(opcode int32, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ipcConn == nil {
		return errors.New("ipc: not connected")
	}
	return c.ipcConn.Write(opcode, data)
}

// Close terminates the current IPC session.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ipcConn == nil {
		return nil
	}
	c.state = StateDisconnected
	return c.ipcConn.Close()
}

func (c *Client) setState(s State) {
	c.mu.Lock()
	c.state = s
	c.mu.Unlock()
}

func (c *Client) ipcConnection() *gopresenceipc.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ipcConn
}

func displayName(ready *presence.ReadyEvent) string {
	if ready != nil && ready.User.GlobalName != "" {
		return ready.User.GlobalName
	}
	if ready != nil && ready.User.Username != "" {
		return ready.User.Username
	}
	return ""
}

func (c *Client) emitEvent(_ context.Context, events chan<- Event, evt Event) {
	select {
	case events <- evt:
	default:
		c.logger.Warn("ipc event dropped: subscriber too slow", zap.Int32("opcode", evt.Opcode))
	}
}

func (c *Client) emitTerminalEvent(ctx context.Context, events chan<- Event, evt Event) {
	select {
	case events <- evt:
	case <-ctx.Done():
	}
}
