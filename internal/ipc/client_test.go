package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	gipc "github.com/dragsbruh/gopresence/ipc"
	"go.uber.org/zap"
)

type ipcFrame struct {
	opcode  int32
	payload []byte
}

func (f ipcFrame) ClientID() string {
	var hs struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(f.payload, &hs); err != nil {
		return ""
	}
	return hs.ClientID
}

type mockDiscord struct {
	t        *testing.T
	path     string
	ln       net.Listener
	accepted chan struct{}
	recv     chan ipcFrame

	mu           sync.Mutex
	conn         *gipc.Client
	handshake    ipcFrame
	handshakeErr bool
	readyUser    string
	readyID      string
}

func newMockDiscord(t *testing.T) *mockDiscord {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix socket mock not supported on windows")
	}

	path := filepath.Join(t.TempDir(), "discord-ipc-0")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listening mock socket: %v", err)
	}

	m := &mockDiscord{
		t:         t,
		path:      path,
		ln:        ln,
		accepted:  make(chan struct{}),
		recv:      make(chan ipcFrame, 8),
		readyUser: "MockUser",
		readyID:   "123456789",
	}

	go m.serve()

	t.Cleanup(func() {
		m.ln.Close()
		m.mu.Lock()
		if m.conn != nil {
			_ = m.conn.Close()
		}
		m.mu.Unlock()
	})

	return m
}

func (m *mockDiscord) serve() {
	conn, err := m.ln.Accept()
	if err != nil {
		return
	}
	c := gipc.NewFrom(conn)

	m.mu.Lock()
	m.conn = c
	m.mu.Unlock()
	close(m.accepted)

	for {
		op, data, err := c.Read()
		if err != nil {
			return
		}

		m.mu.Lock()
		firstFrame := m.handshake.opcode == 0 && m.handshake.payload == nil
		if firstFrame {
			m.handshake = ipcFrame{opcode: op, payload: data}
		}
		reject := m.handshakeErr
		m.mu.Unlock()

		if firstFrame {
			if reject {
				_ = c.Write(OpClose, []byte(`{"code":4000,"message":"mock handshake rejected"}`))
				return
			}
			if err := c.Write(OpFrame, m.readyPayload()); err != nil {
				return
			}
			continue
		}

		select {
		case m.recv <- ipcFrame{opcode: op, payload: data}:
		default:
		}
	}
}

func (m *mockDiscord) readyPayload() []byte {
	frame := map[string]any{
		"cmd": "DISPATCH",
		"evt": "READY",
		"data": map[string]any{
			"v":      1,
			"config": map[string]any{},
			"user": map[string]any{
				"id":          m.readyID,
				"username":    "mockuser",
				"global_name": m.readyUser,
			},
		},
	}
	b, err := json.Marshal(frame)
	if err != nil {
		m.t.Fatalf("marshalling ready payload: %v", err)
	}
	return b
}

func (m *mockDiscord) handshakeFrame() ipcFrame {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handshake
}

func (m *mockDiscord) send(op int32, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn == nil {
		return errors.New("mock: no connection")
	}
	return m.conn.Write(op, payload)
}

func (m *mockDiscord) closeConnection() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		_ = m.conn.Close()
	}
}

func newTestClient(t *testing.T, m *mockDiscord) *Client {
	t.Helper()
	c := New("test-client-id", zap.NewNop())
	c.paths = []string{m.path}
	c.timeout = 2 * time.Second
	if err := c.Connect(); err != nil {
		t.Fatalf("connecting to mock discord: %v", err)
	}
	return c
}

func waitAccept(t *testing.T, m *mockDiscord) {
	t.Helper()
	select {
	case <-m.accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("mock socket never accepted connection")
	}
}

func recvEvent(t *testing.T, ch <-chan Event) (Event, bool) {
	t.Helper()
	select {
	case evt, ok := <-ch:
		return evt, ok
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ipc event")
		return Event{}, false
	}
}

func TestStateString(t *testing.T) {
	cases := []struct {
		state State
		want  string
	}{
		{StateDisconnected, "disconnected"},
		{StateConnecting, "connecting"},
		{StateConnected, "connected"},
		{StateReconnecting, "reconnecting"},
		{State(99), "disconnected"},
	}
	for _, tc := range cases {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestConnectHandshake(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	if got := c.State(); got != StateConnected {
		t.Errorf("state = %v, want connected", got)
	}
	if got := c.User(); got != "MockUser" {
		t.Errorf("user = %q, want %q", got, "MockUser")
	}

	hs := m.handshakeFrame()
	if hs.opcode != OpHandshake {
		t.Errorf("handshake opcode = %d, want %d", hs.opcode, OpHandshake)
	}
	if got := hs.ClientID(); got != "test-client-id" {
		t.Errorf("handshake client_id = %q, want %q", got, "test-client-id")
	}
}

func TestConnectFallsBackToNextSocket(t *testing.T) {
	stale := newMockDiscord(t)
	stale.handshakeErr = true
	good := newMockDiscord(t)

	c := New("test-client-id", zap.NewNop())
	c.paths = []string{stale.path, good.path}
	c.timeout = 2 * time.Second

	if err := c.Connect(); err != nil {
		t.Fatalf("connect should fall back to working socket: %v", err)
	}
	if c.State() != StateConnected {
		t.Errorf("state = %v, want connected", c.State())
	}
	if c.User() != "MockUser" {
		t.Errorf("user = %q, want %q", c.User(), "MockUser")
	}
	if stale.handshakeFrame().opcode != OpHandshake {
		t.Error("stale socket should have received a handshake attempt")
	}
}

func TestConnectNoSocketAvailable(t *testing.T) {
	c := New("test-client-id", zap.NewNop())
	c.paths = []string{"/nonexistent/discord-ipc-0"}
	c.timeout = 200 * time.Millisecond

	if err := c.Connect(); err == nil {
		t.Fatal("expected connect error, got nil")
	}
	if c.State() != StateDisconnected {
		t.Errorf("state = %v, want disconnected", c.State())
	}
}

func TestRunBeforeConnect(t *testing.T) {
	c := New("test-client-id", zap.NewNop())
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected error running before connect")
	}
}

func TestForwardsDispatchFrames(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	dispatch := []byte(`{"cmd":"DISPATCH","evt":"ACTIVITY_JOIN","data":{"secret":"shared-secret"}}`)
	if err := m.send(OpFrame, dispatch); err != nil {
		t.Fatalf("sending dispatch frame: %v", err)
	}

	evt, ok := recvEvent(t, c.Events())
	if !ok {
		t.Fatal("events channel closed unexpectedly")
	}
	if evt.Opcode != OpFrame {
		t.Errorf("opcode = %d, want %d", evt.Opcode, OpFrame)
	}
	if evt.Frame == nil {
		t.Fatal("expected parsed frame")
	}
	if evt.Frame.Event != "ACTIVITY_JOIN" {
		t.Errorf("event = %q, want %q", evt.Frame.Event, "ACTIVITY_JOIN")
	}
}

func TestRunTerminatesOnPeerClose(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

	waitAccept(t, m)
	m.closeConnection()

	err := <-errCh
	if err == nil {
		t.Fatal("expected run to return an error on peer close")
	}

	found := false
	for evt := range c.Events() {
		if evt.Err != nil {
			found = true
		}
	}
	if !found {
		t.Error("expected a terminal event with error")
	}
}

func TestSendRawWritesFrame(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	if err := c.SendRaw(OpPing, []byte(`{}`)); err != nil {
		t.Fatalf("send raw ping: %v", err)
	}

	select {
	case f := <-m.recv:
		if f.opcode != OpPing {
			t.Errorf("recv opcode = %d, want %d", f.opcode, OpPing)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mock never received ping")
	}
}

func TestCloseSetsDisconnected(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if c.State() != StateDisconnected {
		t.Errorf("state = %v, want disconnected", c.State())
	}
}
