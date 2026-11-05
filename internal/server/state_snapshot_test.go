package server

import "testing"

// A state subscription is answered with the proxy's current IPC state before
// any broadcast, so a client never has to infer connectivity from an open
// WebSocket.
func TestStateSnapshotOnStateSubscribe(t *testing.T) {
	hub, _, ts := newTestServer(t, WithGetIPCState(func() string { return string(StateConnected) }))
	hub.ClientID = "snapshot-client-id"

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	subMsg, err := NewSubscribeMessage([]string{MsgTypeState})
	if err != nil {
		t.Fatalf("NewSubscribeMessage: %v", err)
	}
	testWrite(t, conn, subMsg)
	testRequestCurrent(t, conn)

	snapshot := testRead(t, conn)
	if snapshot.Type != MsgTypeState {
		t.Fatalf("first message type = %q, want %q", snapshot.Type, MsgTypeState)
	}
	if snapshot.Status != StateConnected {
		t.Errorf("snapshot status = %q, want %q", snapshot.Status, StateConnected)
	}
	if snapshot.ClientID != "snapshot-client-id" {
		t.Errorf("snapshot ClientID = %q, want %q", snapshot.ClientID, "snapshot-client-id")
	}

	// ReadPump handles messages in order, so the get_current answer following
	// the snapshot proves the snapshot was sent for the subscription itself.
	if ack := testRead(t, conn); ack.Type != MsgTypeCurrent {
		t.Fatalf("second message type = %q, want %q", ack.Type, MsgTypeCurrent)
	}
}

func TestStateSnapshotOnlyForStateSubscribers(t *testing.T) {
	hub, _, ts := newTestServer(t, WithGetIPCState(func() string { return string(StateConnected) }))
	hub.ClientID = "snapshot-client-id"

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	subMsg, err := NewSubscribeMessage([]string{MsgTypePresence})
	if err != nil {
		t.Fatalf("NewSubscribeMessage: %v", err)
	}
	testWrite(t, conn, subMsg)
	testRequestCurrent(t, conn)

	if msg := testRead(t, conn); msg.Type != MsgTypeCurrent {
		t.Fatalf("first message type = %q, want %q (no state snapshot expected)", msg.Type, MsgTypeCurrent)
	}
}

func TestConnectionStateFromIPC(t *testing.T) {
	tests := []struct {
		name string
		ipc  string
		want ConnectionState
	}{
		{name: "connected", ipc: "connected", want: StateConnected},
		{name: "reconnecting", ipc: "reconnecting", want: StateReconnecting},
		{name: "connecting dial in progress", ipc: "connecting", want: StateReconnecting},
		{name: "disconnected", ipc: "disconnected", want: StateDisconnected},
		{name: "empty", ipc: "", want: StateDisconnected},
		{name: "unknown", ipc: "bogus", want: StateDisconnected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := connectionStateFromIPC(tt.ipc); got != tt.want {
				t.Errorf("connectionStateFromIPC(%q) = %q, want %q", tt.ipc, got, tt.want)
			}
		})
	}
}
