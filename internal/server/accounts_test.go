package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func testAccounts() []AccountStatus {
	return []AccountStatus{
		{Account: "discord-ipc-0", State: "connected"},
		{Account: "discord-ipc-1", State: "disconnected"},
	}
}

func TestAccountsEndpoint(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t, WithListAccounts(testAccounts))
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/accounts")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body struct {
		Accounts []AccountStatus `json:"accounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := testAccounts()
	if len(body.Accounts) != len(want) {
		t.Fatalf("accounts = %+v, want %+v", body.Accounts, want)
	}
	for i := range want {
		if body.Accounts[i] != want[i] {
			t.Fatalf("accounts = %+v, want %+v", body.Accounts, want)
		}
	}
}

func TestAccountsEndpointEmptyWithoutProvider(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/accounts")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body struct {
		Accounts []AccountStatus `json:"accounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Accounts) != 0 {
		t.Errorf("accounts = %+v, want an empty list", body.Accounts)
	}
}

func testAccountPresence(account string) (types.PresenceSnapshot, bool) {
	details := map[string]string{
		"discord-ipc-0": "Primary Game",
		"discord-ipc-1": "Secondary Game",
	}
	value, ok := details[account]
	if !ok {
		return types.PresenceSnapshot{}, false
	}
	return testSnapshot(types.Activity{Details: value, Type: types.ActivityPlaying}), true
}

func TestPresenceEndpointSelectsAccount(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t,
		WithGetCurrentPresence(func() types.PresenceSnapshot {
			snapshot, _ := testAccountPresence("discord-ipc-0")
			return snapshot
		}),
		WithGetAccountPresence(testAccountPresence),
	)
	defer cleanup()

	cases := []struct {
		name    string
		query   string
		details string
	}{
		{name: "primary without a query", query: "", details: "Primary Game"},
		{name: "explicit primary", query: "?account=discord-ipc-0", details: "Primary Game"},
		{name: "explicit secondary", query: "?account=discord-ipc-1", details: "Secondary Game"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/api/presence" + tc.query)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			activity := testDecodeActivity(t, mustReadBody(t, resp), "presence")
			if activity.Details != tc.details {
				t.Errorf("details = %q, want %q", activity.Details, tc.details)
			}
		})
	}
}

func TestPresenceEndpointUnknownAccount(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t,
		WithGetCurrentPresence(func() types.PresenceSnapshot {
			snapshot, _ := testAccountPresence("discord-ipc-0")
			return snapshot
		}),
		WithGetAccountPresence(testAccountPresence),
	)
	defer cleanup()

	for _, query := range []string{"?account=discord-ipc-9", "?account=missing"} {
		t.Run(query, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/api/presence" + query)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
			}

			var body APIError
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Code != ErrCodeAccountNotFound {
				t.Errorf("code = %q, want %q", body.Code, ErrCodeAccountNotFound)
			}
		})
	}
}

func TestPresenceEndpointAccountWithoutProviderIsNotFound(t *testing.T) {
	activity := types.Activity{Details: "Primary Game"}
	_, ts, _, cleanup := newTestHTTPServer(t,
		WithGetCurrentPresence(func() types.PresenceSnapshot { return testSnapshot(activity) }),
	)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/presence?account=discord-ipc-0")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (an account query must never fall back to the primary account)",
			resp.StatusCode, http.StatusNotFound)
	}
}

func TestStateEndpointIncludesAccounts(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t,
		WithGetIPCState(func() string { return string(StateConnected) }),
		WithListAccounts(testAccounts),
	)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Status   string          `json:"status"`
		Accounts []AccountStatus `json:"accounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != string(StateConnected) {
		t.Errorf("status = %q, want %q", body.Status, StateConnected)
	}

	want := testAccounts()
	if len(body.Accounts) != len(want) {
		t.Fatalf("accounts = %+v, want %+v", body.Accounts, want)
	}
	for i := range want {
		if body.Accounts[i] != want[i] {
			t.Fatalf("accounts = %+v, want %+v", body.Accounts, want)
		}
	}
}

func TestStateEndpointOmitsAccountsWithoutProvider(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t,
		WithGetIPCState(func() string { return string(StateConnected) }),
	)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["accounts"]; ok {
		t.Errorf("body = %v, want no accounts field without an account provider", body)
	}
}

// A single-account deployment must keep the exact pre-multi-account body, so a
// client that decodes {"status": "..."} sees no new field.
func TestStateEndpointSingleAccountOmitsAccounts(t *testing.T) {
	_, ts, _, cleanup := newTestHTTPServer(t,
		WithGetIPCState(func() string { return string(StateConnected) }),
		WithListAccounts(func() []AccountStatus {
			return []AccountStatus{{Account: "discord-ipc-0", State: "connected"}}
		}),
	)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["accounts"]; ok {
		t.Errorf("body = %v, want no accounts field with a single account", body)
	}
	if body["status"] != string(StateConnected) {
		t.Errorf("status = %v, want %q", body["status"], StateConnected)
	}
}

func TestWSGetCurrentForAccount(t *testing.T) {
	hub, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.PresenceSnapshot {
			snapshot, _ := testAccountPresence("discord-ipc-0")
			return snapshot
		}),
		WithGetAccountPresence(testAccountPresence),
	)
	hub.PrimaryAccount = "discord-ipc-0"

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	testWrite(t, conn, ClientMessage{Type: MsgTypeGetCurrent, Account: "discord-ipc-1"})

	msg := testRead(t, conn)
	if msg.Type != MsgTypeCurrent {
		t.Fatalf("type = %q, want %q", msg.Type, MsgTypeCurrent)
	}
	if msg.Account != "discord-ipc-1" {
		t.Errorf("account = %q, want %q", msg.Account, "discord-ipc-1")
	}
	activity := testDecodeActivity(t, msg.Payload, "current")
	if activity.Details != "Secondary Game" {
		t.Errorf("details = %q, want %q", activity.Details, "Secondary Game")
	}
}

func TestWSGetCurrentPrimaryAccountIsTagged(t *testing.T) {
	hub, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.PresenceSnapshot {
			snapshot, _ := testAccountPresence("discord-ipc-0")
			return snapshot
		}),
		WithGetAccountPresence(testAccountPresence),
	)
	hub.PrimaryAccount = "discord-ipc-0"

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	testRequestCurrent(t, conn)

	msg := testRead(t, conn)
	if msg.Type != MsgTypeCurrent {
		t.Fatalf("type = %q, want %q", msg.Type, MsgTypeCurrent)
	}
	if msg.Account != "discord-ipc-0" {
		t.Errorf("account = %q, want the primary account %q", msg.Account, "discord-ipc-0")
	}
}

func TestWSGetCurrentUnknownAccount(t *testing.T) {
	hub, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.PresenceSnapshot { return types.PresenceSnapshot{Source: types.SourceNone} }),
		WithGetAccountPresence(testAccountPresence),
	)

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	testWrite(t, conn, ClientMessage{Type: MsgTypeGetCurrent, Account: "discord-ipc-9"})

	msg := testRead(t, conn)
	if msg.Type != MsgTypeError {
		t.Fatalf("type = %q, want %q", msg.Type, MsgTypeError)
	}
	var payload ErrorPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Code != ErrCodeInvalidMessage {
		t.Errorf("code = %q, want %q", payload.Code, ErrCodeInvalidMessage)
	}
}

// A single-account proxy must keep answering get_current and state frames
// without an account field, so existing clients see the same bytes as before.
func TestWSSingleAccountFramesOmitAccount(t *testing.T) {
	activity := types.Activity{Details: "Solo Game", Type: types.ActivityPlaying}
	hub, _, ts := newTestServer(t,
		WithGetCurrentPresence(func() types.PresenceSnapshot { return testSnapshot(activity) }),
		WithGetIPCState(func() string { return string(StateConnected) }),
	)

	conn := testDial(t, ts)
	defer conn.Close()
	waitForClientCount(t, hub, 1)

	subMsg, err := NewSubscribeMessage([]string{MsgTypeState})
	if err != nil {
		t.Fatalf("NewSubscribeMessage: %v", err)
	}
	testWrite(t, conn, subMsg)
	testRequestCurrent(t, conn)

	snapshot := testReadRaw(t, conn)
	if snapshot["type"] != MsgTypeState {
		t.Fatalf("first message type = %v, want %q", snapshot["type"], MsgTypeState)
	}
	if _, ok := snapshot["account"]; ok {
		t.Error("state snapshot carries an account field with a single account")
	}

	current := testReadRaw(t, conn)
	if current["type"] != MsgTypeCurrent {
		t.Fatalf("second message type = %v, want %q", current["type"], MsgTypeCurrent)
	}
	if _, ok := current["account"]; ok {
		t.Error("current frame carries an account field with a single account")
	}
}

func TestWSStateSnapshotCarriesPrimaryAccount(t *testing.T) {
	hub, _, ts := newTestServer(t, WithGetIPCState(func() string { return string(StateConnected) }))
	hub.PrimaryAccount = "discord-ipc-0"
	hub.GetCurrentPresence = func() types.PresenceSnapshot { return types.PresenceSnapshot{Source: types.SourceNone} }

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
	if snapshot.Account != "discord-ipc-0" {
		t.Errorf("state snapshot account = %q, want %q", snapshot.Account, "discord-ipc-0")
	}
}

func mustReadBody(t *testing.T, resp *http.Response) json.RawMessage {
	t.Helper()

	var body json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func testReadRaw(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	return raw
}
