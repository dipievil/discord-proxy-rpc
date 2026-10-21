package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/server"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

type fakeBackend struct {
	out       chan wsMsg
	dialErr   error
	dialCalls int
	closes    int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{out: make(chan wsMsg, 8)}
}

func (f *fakeBackend) Messages() <-chan wsMsg { return f.out }

func (f *fakeBackend) Dial(context.Context) error {
	f.dialCalls++
	return f.dialErr
}

func (f *fakeBackend) Close() error {
	f.closes++
	return nil
}

func newTestModel(t *testing.T) (Model, *fakeBackend) {
	t.Helper()
	fake := newFakeBackend()
	return newModel("ws://127.0.0.1:8765/ws", fake), fake
}

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func presenceEvent(t *testing.T, snapshot types.PresenceSnapshot) wsMsg {
	t.Helper()
	msg, err := server.NewPresenceMessage(snapshot)
	if err != nil {
		t.Fatalf("NewPresenceMessage: %v", err)
	}
	return wsMsg{msg: &msg}
}

func TestInitDialsBackend(t *testing.T) {
	m, fake := newTestModel(t)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(connectedMsg); !ok {
		t.Fatalf("Init cmd returned %T, want connectedMsg", msg)
	}
	if fake.dialCalls != 1 {
		t.Fatalf("Dial called %d times, want 1", fake.dialCalls)
	}
}

func TestConnectedMsgSetsStatusAndChainsRead(t *testing.T) {
	m, _ := newTestModel(t)

	m, cmd := update(t, m, connectedMsg{})
	if m.ws != statusConnected {
		t.Fatalf("ws status = %q, want %q", m.ws, statusConnected)
	}
	if m.backoff != 0 {
		t.Fatalf("backoff = %v, want reset to 0", m.backoff)
	}
	if cmd == nil {
		t.Fatal("expected a read cmd after connect")
	}
}

func TestConnectFailureSchedulesBackoff(t *testing.T) {
	m, _ := newTestModel(t)

	m, cmd := update(t, m, connectFailedMsg{err: errors.New("dial refused")})
	if m.ws != statusReconnecting {
		t.Fatalf("ws status = %q, want %q", m.ws, statusReconnecting)
	}
	if m.backoff != reconnectBase {
		t.Fatalf("backoff = %v, want %v", m.backoff, reconnectBase)
	}
	if !strings.Contains(m.lastErr, "dial refused") {
		t.Fatalf("lastErr = %q, want to contain the dial error", m.lastErr)
	}
	if cmd == nil {
		t.Fatal("expected a reconnect cmd")
	}

	m, _ = update(t, m, connectFailedMsg{err: errors.New("dial refused")})
	if m.backoff != reconnectBase*2 {
		t.Fatalf("backoff = %v, want %v", m.backoff, reconnectBase*2)
	}
}

func TestReconnectMsgRedials(t *testing.T) {
	m, fake := newTestModel(t)

	_, cmd := update(t, m, reconnectMsg{})
	msg := cmd()
	if _, ok := msg.(connectedMsg); !ok {
		t.Fatalf("reconnect cmd returned %T, want connectedMsg", msg)
	}
	if fake.dialCalls != 1 {
		t.Fatalf("Dial called %d times, want 1", fake.dialCalls)
	}
}

func TestPresenceMessageUpdatesSnapshot(t *testing.T) {
	m, _ := newTestModel(t)

	activity := types.Activity{
		Details: "Building TUI",
		State:   "in a worktree",
		Type:    types.ActivityPlaying,
		Assets:  &types.Assets{LargeText: "Go"},
	}
	m, cmd := update(t, m, wsEventMsg{ev: presenceEvent(t, types.PresenceSnapshot{
		Source:   types.SourceIPC,
		Activity: &activity,
	})})

	if m.snapshot.Source != types.SourceIPC {
		t.Fatalf("snapshot source = %q, want %q", m.snapshot.Source, types.SourceIPC)
	}
	if m.snapshot.Activity == nil || m.snapshot.Activity.Details != "Building TUI" {
		t.Fatalf("snapshot activity = %+v, want Details %q", m.snapshot.Activity, "Building TUI")
	}
	if cmd == nil {
		t.Fatal("expected a follow-up read cmd")
	}
}

func TestSourceNoneRendersHonestNotice(t *testing.T) {
	m, _ := newTestModel(t)

	m, _ = update(t, m, wsEventMsg{ev: presenceEvent(t, types.PresenceSnapshot{
		Source:   types.SourceNone,
		Activity: nil,
	})})

	view := m.View()
	if !strings.Contains(view, "No activity source") {
		t.Fatalf("view missing no-source notice:\n%s", view)
	}
	if strings.Contains(view, "No active presence") {
		t.Fatalf("view should not show stale empty-activity wording:\n%s", view)
	}
}

func TestCurrentMessageTracksClientID(t *testing.T) {
	m, _ := newTestModel(t)

	msg, err := server.NewCurrentMessage(types.PresenceSnapshot{Source: types.SourceNone}, "client-42")
	if err != nil {
		t.Fatalf("NewCurrentMessage: %v", err)
	}
	m, _ = update(t, m, wsEventMsg{ev: wsMsg{msg: &msg}})

	if m.clientID != "client-42" {
		t.Fatalf("clientID = %q, want %q", m.clientID, "client-42")
	}
}

func TestStateMessageUpdatesIPCStatus(t *testing.T) {
	m, _ := newTestModel(t)

	msg := server.NewStateMessage(server.StateConnected, "client-7")
	m, _ = update(t, m, wsEventMsg{ev: wsMsg{msg: &msg}})
	if m.ipc != server.StateConnected {
		t.Fatalf("ipc = %q, want %q", m.ipc, server.StateConnected)
	}

	view := m.View()
	if !strings.Contains(view, "Connected") || !strings.Contains(view, "client-7") {
		t.Fatalf("view missing IPC status/client id:\n%s", view)
	}
}

func TestFatalReadErrorTriggersReconnect(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = update(t, m, connectedMsg{})

	m, cmd := update(t, m, wsEventMsg{ev: wsMsg{err: errors.New("connection lost: EOF"), fatal: true}})
	if m.ws != statusReconnecting {
		t.Fatalf("ws status = %q, want %q", m.ws, statusReconnecting)
	}
	if m.backoff != reconnectBase {
		t.Fatalf("backoff = %v, want %v", m.backoff, reconnectBase)
	}
	if cmd == nil {
		t.Fatal("expected a reconnect cmd")
	}
}

func TestNonFatalErrorKeepsConnection(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = update(t, m, connectedMsg{})

	m, cmd := update(t, m, wsEventMsg{ev: wsMsg{err: errors.New("invalid server message: boom")}})
	if m.ws != statusConnected {
		t.Fatalf("ws status = %q, want %q", m.ws, statusConnected)
	}
	if !strings.Contains(m.lastErr, "invalid server message") {
		t.Fatalf("lastErr = %q, want the parse error", m.lastErr)
	}
	if cmd == nil {
		t.Fatal("expected the read chain to continue")
	}
}

func TestBadPresencePayloadSetsErrorAndKeepsSnapshot(t *testing.T) {
	m, _ := newTestModel(t)
	msg, err := server.NewPresenceMessage(types.PresenceSnapshot{Source: types.SourceIPC})
	if err != nil {
		t.Fatalf("NewPresenceMessage: %v", err)
	}
	corrupt := msg
	corrupt.Payload = json.RawMessage("{not json")

	m, _ = update(t, m, wsEventMsg{ev: presenceEvent(t, types.PresenceSnapshot{Source: types.SourceNone})})
	m, _ = update(t, m, wsEventMsg{ev: wsMsg{msg: &corrupt}})

	if m.snapshot.Source != types.SourceNone {
		t.Fatalf("snapshot = %+v, want previous snapshot kept on corrupt payload", m.snapshot)
	}
	if !strings.Contains(m.lastErr, "invalid presence payload") {
		t.Fatalf("lastErr = %q, want payload error", m.lastErr)
	}
}

func TestQuitClosesBackendAndStopsProgram(t *testing.T) {
	m, fake := newTestModel(t)

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !m.quitting {
		t.Fatal("model should be quitting after q")
	}
	if fake.closes != 1 {
		t.Fatalf("backend closed %d times, want 1", fake.closes)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after q")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected tea.Quit after q")
	}
	if m.View() != "" {
		t.Fatalf("view after quit = %q, want empty", m.View())
	}

	// ctrl+c path shares the same quit handling (fresh model).
	m2, fake2 := newTestModel(t)
	m2, cmd2 := update(t, m2, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m2.quitting || fake2.closes != 1 {
		t.Fatal("ctrl+c should quit cleanly")
	}
	if _, ok := cmd2().(tea.QuitMsg); !ok {
		t.Fatal("expected tea.Quit after ctrl+c")
	}
}

func TestReadCmdReturnsAfterQuit(t *testing.T) {
	m, _ := newTestModel(t)

	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if msg := m.readCmd()(); msg != nil {
		t.Fatalf("read cmd after quit returned %T, want nil", msg)
	}
}

func TestTickRefreshesNowOnlyWithTimestamps(t *testing.T) {
	m, _ := newTestModel(t)

	m, cmd := update(t, m, wsEventMsg{ev: presenceEvent(t, types.PresenceSnapshot{
		Source:   types.SourceIPC,
		Activity: &types.Activity{Details: "no timestamps"},
	})})
	if cmd == nil {
		t.Fatal("expected a read cmd")
	}

	future := time.Unix(1_700_000_100, 0)
	m, tick := update(t, m, tickMsg(future))
	if !m.now.Equal(future) {
		t.Fatalf("now = %v, want %v", m.now, future)
	}
	if tick != nil {
		t.Fatal("no ticking expected without timestamps")
	}

	start := future.Add(-90 * time.Second).UnixMilli()
	m, _ = update(t, m, wsEventMsg{ev: presenceEvent(t, types.PresenceSnapshot{
		Source:   types.SourceIPC,
		Activity: &types.Activity{Timestamps: &types.Timestamps{Start: start}},
	})})
	m, tick = update(t, m, tickMsg(future))
	if tick == nil {
		t.Fatal("expected ticking while timestamps are shown")
	}

	view := m.View()
	if !strings.Contains(view, "1m 30s") {
		t.Fatalf("view missing elapsed 1m 30s:\n%s", view)
	}
}

func TestNextBackoff(t *testing.T) {
	cases := []struct {
		current, want time.Duration
	}{
		{0, reconnectBase},
		{reconnectBase, reconnectBase * 2},
		{reconnectBase * 2, reconnectBase * 4},
		{reconnectMax, reconnectMax},
		{reconnectMax + time.Second, reconnectMax},
	}
	for _, tc := range cases {
		if got := nextBackoff(tc.current, reconnectBase, reconnectMax); got != tc.want {
			t.Errorf("nextBackoff(%v) = %v, want %v", tc.current, got, tc.want)
		}
	}
}
