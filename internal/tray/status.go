// Package tray shows the proxy's Discord IPC state in the desktop system tray.
//
// The Linux backend is a D-Bus StatusNotifierItem (KDE interface), so it needs
// no cgo, GTK or icon theme: the status icons are generated pixmaps carried in
// the D-Bus properties themselves. Everything except the raw D-Bus export is
// platform independent and unit tested: the /api/state client, the URL/token
// resolution, the state-to-icon mapping and the poll loop.
package tray

import "strings"

// ProxyState is the Discord IPC state as reported by GET /api/state. The proxy
// serves "connected", "reconnecting", "disconnected" or "connecting";
// StateUnreachable is a client-side value for a proxy that answered with
// something else or not at all.
type ProxyState string

const (
	StateConnected    ProxyState = "connected"
	StateReconnecting ProxyState = "reconnecting"
	StateDisconnected ProxyState = "disconnected"
	StateUnreachable  ProxyState = "unreachable"

	// stateConnecting is the proxy's own value for a Discord dial in progress
	// (internal/ipc/client.go). The server reports it as reconnecting as well
	// (internal/server/http.go, connectionStateFromIPC), and so does the tray.
	stateConnecting ProxyState = "connecting"
)

// Icon identifies the tray icon to render. The Linux StatusNotifierItem
// backend maps it to embedded pixmaps; keeping the mapping logical lets the
// state logic stay platform independent and testable without D-Bus.
type Icon int

const (
	IconConnected Icon = iota
	IconReconnecting
	IconDisconnected
)

func (i Icon) String() string {
	switch i {
	case IconConnected:
		return "connected"
	case IconReconnecting:
		return "reconnecting"
	default:
		return "disconnected"
	}
}

// Status is everything the tray shows for one proxy state.
type Status struct {
	State   ProxyState
	Icon    Icon
	Tooltip string
}

// trayTitle is the human-readable product name shown in tooltips.
const trayTitle = "Discord Proxy RPC"

// NormalizeState maps a raw /api/state payload to a known state. A dial in
// progress ("connecting") is a reconnect, matching the server's WebSocket
// mapping. An empty or unrecognized value means the proxy cannot be trusted to
// report health, so it is treated as unreachable rather than guessing.
func NormalizeState(raw string) ProxyState {
	switch ProxyState(strings.ToLower(strings.TrimSpace(raw))) {
	case StateConnected:
		return StateConnected
	case StateReconnecting, stateConnecting:
		return StateReconnecting
	case StateDisconnected:
		return StateDisconnected
	default:
		return StateUnreachable
	}
}

// StatusFor maps a proxy state to the icon and tooltip the tray renders.
// "unreachable" (the proxy could not be polled) falls back to the disconnected
// icon, with a tooltip that says the proxy, not Discord, is down.
func StatusFor(state ProxyState) Status {
	switch state {
	case StateConnected:
		return Status{State: StateConnected, Icon: IconConnected, Tooltip: trayTitle + " — Discord IPC connected"}
	case StateReconnecting:
		return Status{State: StateReconnecting, Icon: IconReconnecting, Tooltip: trayTitle + " — Discord IPC reconnecting"}
	case StateDisconnected:
		return Status{State: StateDisconnected, Icon: IconDisconnected, Tooltip: trayTitle + " — Discord IPC disconnected"}
	default:
		return Status{State: StateUnreachable, Icon: IconDisconnected, Tooltip: trayTitle + " — proxy unreachable"}
	}
}
