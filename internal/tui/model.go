package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/server"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

const (
	reconnectBase = time.Second
	reconnectMax  = 30 * time.Second
	tickInterval  = time.Second
)

type wsStatus string

const (
	statusConnecting   wsStatus = "connecting"
	statusConnected    wsStatus = "connected"
	statusReconnecting wsStatus = "reconnecting"
)

type connectedMsg struct{}

type connectFailedMsg struct {
	err error
}

type wsEventMsg struct {
	ev wsMsg
}

type reconnectMsg struct{}

type tickMsg time.Time

// Model is the bubbletea model rendering the proxy presence state. All I/O
// happens in commands; Update is deterministic and unit-testable.
type Model struct {
	url    string
	client Backend

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	ws       wsStatus
	ipc      server.ConnectionState
	clientID string

	snapshot types.PresenceSnapshot
	lastErr  string

	backoff time.Duration
	now     time.Time

	width  int
	height int

	quitting bool
}

// New builds a Model connected to the given WebSocket endpoint, using the
// same protocol as the web dashboard.
func New(url, token string) Model {
	return newModel(url, NewWSBackend(url, token))
}

func newModel(url string, backend Backend) Model {
	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		url:    url,
		client: backend,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		ws:     statusConnecting,
		ipc:    server.StateDisconnected,
		now:    time.Now(),
	}
}

func (m Model) Init() tea.Cmd {
	return m.connectCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m.quit()
		}
		return m, nil

	case connectedMsg:
		m.ws = statusConnected
		m.backoff = 0
		m.lastErr = ""
		return m, m.readCmd()

	case connectFailedMsg:
		return m.scheduleReconnect(msg.err)

	case reconnectMsg:
		return m, m.connectCmd()

	case wsEventMsg:
		return m.handleWsEvent(msg.ev)

	case tickMsg:
		m.now = time.Time(msg)
		if m.needsTick() {
			return m, m.tickCmd()
		}
		return m, nil
	}
	return m, nil
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	close(m.done)
	m.cancel()
	if err := m.client.Close(); err != nil {
		m.lastErr = err.Error()
	}
	return m, tea.Quit
}

func (m Model) handleWsEvent(ev wsMsg) (tea.Model, tea.Cmd) {
	if ev.err != nil {
		m.lastErr = ev.err.Error()
		if ev.fatal {
			return m.scheduleReconnect(ev.err)
		}
		return m, m.readCmd()
	}

	m.applyServerMessage(ev.msg)

	cmds := []tea.Cmd{m.readCmd()}
	if m.needsTick() {
		cmds = append(cmds, m.tickCmd())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) scheduleReconnect(err error) (tea.Model, tea.Cmd) {
	m.ws = statusReconnecting
	m.lastErr = err.Error()
	m.backoff = nextBackoff(m.backoff, reconnectBase, reconnectMax)
	return m, m.reconnectCmd(m.backoff)
}

func (m *Model) applyServerMessage(msg *server.ServerMessage) {
	if msg == nil {
		return
	}
	switch msg.Type {
	case server.MsgTypePresence, server.MsgTypeCurrent:
		var snapshot types.PresenceSnapshot
		if len(msg.Payload) > 0 {
			if err := json.Unmarshal(msg.Payload, &snapshot); err != nil {
				m.lastErr = fmt.Sprintf("invalid presence payload: %v", err)
				return
			}
		}
		m.snapshot = snapshot
		m.lastErr = ""
	case server.MsgTypeState:
		m.ipc = msg.Status
		m.lastErr = ""
	case server.MsgTypeError:
		var payload server.ErrorPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			m.lastErr = "server error"
			return
		}
		m.lastErr = fmt.Sprintf("server error %s: %s", payload.Code, payload.Message)
	}
	if msg.ClientID != "" {
		m.clientID = msg.ClientID
	}
}

func (m Model) needsTick() bool {
	activity := m.snapshot.Activity
	if activity == nil || activity.Timestamps == nil {
		return false
	}
	ts := activity.Timestamps
	return ts.Start != 0 || ts.End != 0
}

func (m Model) connectCmd() tea.Cmd {
	return func() tea.Msg {
		if err := m.client.Dial(m.ctx); err != nil {
			return connectFailedMsg{err: err}
		}
		return connectedMsg{}
	}
}

func (m Model) readCmd() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-m.client.Messages():
			return wsEventMsg{ev: ev}
		case <-m.done:
			return nil
		}
	}
}

func (m Model) reconnectCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return reconnectMsg{}
	})
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func nextBackoff(current, base, max time.Duration) time.Duration {
	if current <= 0 {
		return base
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	sectionHead = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Width(12)
	footStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

var statusStyles = map[wsStatus]lipgloss.Style{
	statusConnecting:   lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
	statusConnected:    lipgloss.NewStyle().Foreground(lipgloss.Color("48")),
	statusReconnecting: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
}

var ipcStatusStyles = map[server.ConnectionState]lipgloss.Style{
	server.StateConnected:    lipgloss.NewStyle().Foreground(lipgloss.Color("48")),
	server.StateDisconnected: lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	server.StateReconnecting: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("discord-proxy-rpc") + "\n\n")

	b.WriteString(m.field("Proxy", m.url) + "\n")
	b.WriteString(m.field("WebSocket", m.renderWSStatus()) + "\n")
	b.WriteString(m.field("Discord IPC", m.renderIPCStatus()) + "\n\n")

	b.WriteString(sectionHead.Render("Activity") + "\n")
	if m.snapshot.Activity == nil || m.snapshot.Activity.IsEmpty() {
		b.WriteString(noticeStyle.Render(m.sourceNotice()) + "\n")
	} else {
		b.WriteString(m.field("Source", string(m.snapshot.Source)) + "\n")
		b.WriteString(m.renderActivity() + "\n")
	}

	if m.lastErr != "" {
		b.WriteString("\n" + errStyle.Render("Error: "+m.lastErr) + "\n")
	}

	b.WriteString("\n" + footStyle.Render("q / ctrl+c to quit"))
	return b.String()
}

func (m Model) field(label, value string) string {
	return labelStyle.Render(label) + value
}

func (m Model) renderWSStatus() string {
	style, ok := statusStyles[m.ws]
	if !ok {
		style = lipgloss.NewStyle()
	}
	return style.Render("● " + strings.ToUpper(string(m.ws)[:1]) + string(m.ws)[1:])
}

func (m Model) renderIPCStatus() string {
	text := string(m.ipc)
	if text == "" {
		text = string(server.StateDisconnected)
	}
	style, ok := ipcStatusStyles[m.ipc]
	if !ok {
		style = lipgloss.NewStyle()
	}
	rendered := style.Render("● " + strings.ToUpper(text[:1]) + text[1:])
	if m.clientID != "" {
		rendered += " (client " + m.clientID + ")"
	}
	return rendered
}

func (m Model) renderActivity() string {
	activity := m.snapshot.Activity
	var b strings.Builder

	b.WriteString(m.field("Type", activityTypeLabel(activity.Type)) + "\n")
	b.WriteString(m.field("Details", valueOrDash(activity.Details)) + "\n")
	b.WriteString(m.field("State", valueOrDash(activity.State)) + "\n")

	elapsed, remaining := formatTimestamps(activity.Timestamps, m.now.UnixMilli())
	b.WriteString(m.field("Elapsed", elapsed) + "\n")
	b.WriteString(m.field("Remaining", remaining) + "\n")

	large, small := formatAssets(activity.Assets)
	b.WriteString(m.field("Large asset", large) + "\n")
	b.WriteString(m.field("Small asset", small) + "\n")
	b.WriteString(m.field("Party", formatParty(activity.Party)) + "\n")
	b.WriteString(m.field("Buttons", formatButtons(activity.Buttons)))
	return b.String()
}

func valueOrDash(value string) string {
	if value == "" {
		return dash
	}
	return value
}

// sourceNotice mirrors the web dashboard's wording so the TUI states the
// activity-source limitation honestly instead of rendering a null activity
// as an empty feed.
func (m Model) sourceNotice() string {
	if m.snapshot.Source == types.SourceIPC {
		return "No activity reported by the source right now."
	}
	return "No activity source: Discord IPC exposes no command to read another app\u2019s Rich Presence, so this proxy cannot observe activity yet."
}
