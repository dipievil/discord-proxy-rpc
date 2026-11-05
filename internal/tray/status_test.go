package tray

import "testing"

func TestNormalizeState(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ProxyState
	}{
		{name: "connected", raw: "connected", want: StateConnected},
		{name: "reconnecting", raw: "reconnecting", want: StateReconnecting},
		{name: "disconnected", raw: "disconnected", want: StateDisconnected},
		{name: "case and spaces", raw: "  CONNECTED\n", want: StateConnected},
		{name: "unknown", raw: "starting", want: StateUnreachable},
		{name: "empty", raw: "", want: StateUnreachable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeState(test.raw); got != test.want {
				t.Errorf("NormalizeState(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestStatusFor(t *testing.T) {
	tests := []struct {
		state       ProxyState
		wantIcon    Icon
		wantTooltip string
	}{
		{StateConnected, IconConnected, "Discord Proxy RPC — Discord IPC connected"},
		{StateReconnecting, IconReconnecting, "Discord Proxy RPC — Discord IPC reconnecting"},
		{StateDisconnected, IconDisconnected, "Discord Proxy RPC — Discord IPC disconnected"},
		{StateUnreachable, IconDisconnected, "Discord Proxy RPC — proxy unreachable"},
	}

	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			got := StatusFor(test.state)
			if got.State != test.state {
				t.Errorf("State = %q, want %q", got.State, test.state)
			}
			if got.Icon != test.wantIcon {
				t.Errorf("Icon = %v, want %v", got.Icon, test.wantIcon)
			}
			if got.Tooltip != test.wantTooltip {
				t.Errorf("Tooltip = %q, want %q", got.Tooltip, test.wantTooltip)
			}
		})
	}
}

func TestStatusForDistinctTooltips(t *testing.T) {
	states := []ProxyState{StateConnected, StateReconnecting, StateDisconnected, StateUnreachable}
	seen := make(map[string]ProxyState, len(states))
	for _, state := range states {
		status := StatusFor(state)
		if other, ok := seen[status.Tooltip]; ok {
			t.Errorf("%q and %q share tooltip %q", other, state, status.Tooltip)
		}
		seen[status.Tooltip] = state
	}
}

func TestIconString(t *testing.T) {
	tests := map[Icon]string{
		IconConnected:    "connected",
		IconReconnecting: "reconnecting",
		IconDisconnected: "disconnected",
		Icon(99):         "disconnected",
	}
	for icon, want := range tests {
		if got := icon.String(); got != want {
			t.Errorf("Icon(%d).String() = %q, want %q", int(icon), got, want)
		}
	}
}
