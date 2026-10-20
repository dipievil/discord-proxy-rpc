package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

// newTestServer starts a hub plus an HTTP test server. It returns the hub, the
// hub's cancel func (callers that exercise shutdown may invoke it early) and
// the test server.
func newTestServer(t *testing.T, opts ...ServerOption) (*Hub, context.CancelFunc, *httptest.Server) {
	t.Helper()
	hub, cancel := newTestHub(t)
	srv := NewServer(hub, zap.NewNop(), opts...)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		cancel()
	})
	return hub, cancel, ts
}

func testDial(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(testWSURL(ts), nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

func testWSURL(ts *httptest.Server) string {
	return "ws" + ts.URL[4:] + "/ws"
}

func testWrite(t *testing.T, conn *websocket.Conn, msg any) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal client message: %v", err)
	}
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write client message: %v", err)
	}
}

// testSubscribe subscribes conn to the given events and returns only once the
// server has processed the message. ReadPump handles client messages in order,
// so a get_current round-trip after the subscribe is a deterministic ack —
// sleeping for a fixed interval would be racy under -race.
func testSubscribe(t *testing.T, conn *websocket.Conn, events []string) {
	t.Helper()
	subMsg, err := NewSubscribeMessage(events)
	if err != nil {
		t.Fatalf("NewSubscribeMessage: %v", err)
	}
	testWrite(t, conn, subMsg)
	testRequestCurrent(t, conn)
	if ack := testRead(t, conn); ack.Type != MsgTypeCurrent {
		t.Fatalf("subscribe ack: type = %q, want %q", ack.Type, MsgTypeCurrent)
	}
}

// waitForClientCount blocks until the hub reports want registered clients. The
// hub registers from the handler goroutine, so poll instead of sleeping.
func waitForClientCount(t *testing.T, hub *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ClientCount = %d, want %d", hub.ClientCount(), want)
}

func testRead(t *testing.T, conn *websocket.Conn) ServerMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return msg
}

func testRequestCurrent(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	testWrite(t, conn, NewGetCurrentMessage())
}

// testSnapshot builds a presence snapshot carrying an IPC-sourced activity, the
// shape the production wiring emits.
func testSnapshot(activity types.Activity) types.PresenceSnapshot {
	return types.PresenceSnapshot{Source: types.SourceIPC, Activity: &activity}
}

// testDecodeSnapshot unmarshals a presence or current payload into the snapshot
// envelope. Checking the error keeps a malformed payload from surfacing as a
// confusing mismatch.
func testDecodeSnapshot(t *testing.T, payload json.RawMessage, what string) types.PresenceSnapshot {
	t.Helper()
	var snapshot types.PresenceSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("decode %s payload: %v", what, err)
	}
	return snapshot
}

// testDecodeActivity returns the activity carried by a presence or current
// payload, failing the test if the snapshot has no activity.
func testDecodeActivity(t *testing.T, payload json.RawMessage, what string) types.Activity {
	t.Helper()
	return testActivityFromSnapshot(t, testDecodeSnapshot(t, payload, what), what)
}

// testDecodeSnapshotBody decodes a presence snapshot straight from an HTTP
// response body.
func testDecodeSnapshotBody(t *testing.T, r io.Reader, what string) types.PresenceSnapshot {
	t.Helper()
	var snapshot types.PresenceSnapshot
	if err := json.NewDecoder(r).Decode(&snapshot); err != nil {
		t.Fatalf("decode %s body: %v", what, err)
	}
	return snapshot
}

// testActivityFromSnapshot returns the activity carried by a snapshot, failing
// the test if the snapshot has no activity.
func testActivityFromSnapshot(t *testing.T, snapshot types.PresenceSnapshot, what string) types.Activity {
	t.Helper()
	if snapshot.Activity == nil {
		t.Fatalf("%s snapshot has no activity (source=%q)", what, snapshot.Source)
	}
	return *snapshot.Activity
}
