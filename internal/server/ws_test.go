package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func wsURL(server *httptest.Server) string {
	return "ws" + server.URL[4:]
}

func TestWSUpgrade(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())

	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount = %d, want 1", count)
	}
}

func TestWSCleanDisconnect(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Fatalf("ClientCount = %d, want 1", count)
	}

	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	conn.Close()

	time.Sleep(200 * time.Millisecond)

	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount after disconnect = %d, want 0", count)
	}
}

func TestWSAuthReject(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	authFunc := func(r *http.Request) bool {
		return false
	}

	handler := NewWSHandler(hub, zap.NewNop()).WithAuth(authFunc)
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	time.Sleep(50 * time.Millisecond)
	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount = %d, want 0 (auth rejected)", count)
	}
}

func TestWSAuthAllow(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	authFunc := func(r *http.Request) bool {
		return true
	}

	handler := NewWSHandler(hub, zap.NewNop()).WithAuth(authFunc)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount = %d, want 1", count)
	}
}

func TestWSSubscribeViaHandler(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	subMsg, err := NewSubscribeMessage([]string{MsgTypePresence})
	if err != nil {
		t.Fatalf("NewSubscribeMessage: %v", err)
	}
	data, _ := json.Marshal(subMsg)

	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	activity := types.Activity{Details: "Test Game", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var resp ServerMessage
	if err := json.Unmarshal(received, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q", resp.Type, MsgTypePresence)
	}
}

func TestWSGetCurrentViaHandler(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	activity := types.Activity{Details: "Current Game", Type: types.ActivityPlaying}
	hub.GetCurrentPresence = func() types.Activity {
		return activity
	}

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	getMsg := NewGetCurrentMessage()
	data, _ := json.Marshal(getMsg)

	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var resp ServerMessage
	if err := json.Unmarshal(received, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Type != MsgTypeCurrent {
		t.Errorf("type = %q, want %q", resp.Type, MsgTypeCurrent)
	}

	var got types.Activity
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if got.Details != "Current Game" {
		t.Errorf("Details = %q, want %q", got.Details, "Current Game")
	}
}

func TestWSPresencePushViaHandler(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	subMsg, _ := NewSubscribeMessage([]string{MsgTypePresence, MsgTypeState})
	subData, _ := json.Marshal(subMsg)
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	conn.WriteMessage(websocket.TextMessage, subData)

	time.Sleep(100 * time.Millisecond)

	activity := types.Activity{Details: "Pushed Game", State: "Rank 1", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	stateMsg := NewStateMessage(StateConnected)
	hub.Broadcast(stateMsg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data1, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage presence: %v", err)
	}
	var resp1 ServerMessage
	json.Unmarshal(data1, &resp1)
	if resp1.Type != MsgTypePresence {
		t.Errorf("first message type = %q, want %q", resp1.Type, MsgTypePresence)
	}

	_, data2, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage state: %v", err)
	}
	var resp2 ServerMessage
	json.Unmarshal(data2, &resp2)
	if resp2.Type != MsgTypeState {
		t.Errorf("second message type = %q, want %q", resp2.Type, MsgTypeState)
	}
}

func TestWSMultipleClients(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	var conns []*websocket.Conn
	for i := 0; i < 3; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns = append(conns, conn)
	}

	time.Sleep(100 * time.Millisecond)
	if count := hub.ClientCount(); count != 3 {
		t.Fatalf("ClientCount = %d, want 3", count)
	}

	for _, conn := range conns {
		subMsg, _ := NewSubscribeMessage([]string{MsgTypePresence})
		subData, _ := json.Marshal(subMsg)
		conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		conn.WriteMessage(websocket.TextMessage, subData)
	}

	time.Sleep(100 * time.Millisecond)

	activity := types.Activity{Details: "Multi Client", Type: types.ActivityPlaying}
	msg, _ := NewPresenceMessage(activity)
	hub.Broadcast(msg)

	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client %d ReadMessage: %v", i, err)
		}
		var resp ServerMessage
		json.Unmarshal(data, &resp)
		if resp.Type != MsgTypePresence {
			t.Errorf("client %d type = %q, want %q", i, resp.Type, MsgTypePresence)
		}
	}

	for _, conn := range conns {
		conn.Close()
	}
}

func TestWSNoAuthWithoutMiddleware(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount = %d, want 1", count)
	}
}

func TestWSUpgradeRejectsNonWebSocket(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Error("non-WebSocket request should not get upgrade response")
	}
}

func TestWSCustomUpgrader(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	customUpgrader := &websocket.Upgrader{
		ReadBufferSize:  2048,
		WriteBufferSize: 2048,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	handler := NewWSHandler(hub, zap.NewNop()).WithUpgrader(customUpgrader)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount = %d, want 1", count)
	}
}

func TestWSDefaultRejectsCrossOrigin(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	h := http.Header{}
	h.Set("Origin", "http://malicious.example.com")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), h)
	if err == nil {
		conn.Close()
		t.Fatal("expected cross-origin request to be rejected")
	}

	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount = %d, want 0 (cross-origin rejected)", count)
	}
}

func TestWSDefaultAcceptsSameOrigin(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	handler := NewWSHandler(hub, zap.NewNop())
	server := httptest.NewServer(handler)
	defer server.Close()

	h := http.Header{}
	h.Set("Origin", server.URL)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), h)
	if err != nil {
		t.Fatalf("dial same-origin: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount = %d, want 1", count)
	}
}

func TestWSInvalidUpgradeReturnsError(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	strictUpgrader := &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return false },
	}

	handler := NewWSHandler(hub, zap.NewNop()).WithUpgrader(strictUpgrader)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err == nil {
		conn.Close()
		t.Fatal("expected dial error with strict origin check")
	}

	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount = %d, want 0 (upgrade rejected)", count)
	}
}

func TestWSAuthFuncWithBearerToken(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	validToken := "test-secret-token"

	authFunc := func(r *http.Request) bool {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			return false
		}
		if !strings.HasPrefix(auth, "Bearer ") {
			return false
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		return token == validToken
	}

	handler := NewWSHandler(hub, zap.NewNop()).WithAuth(authFunc)
	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("no token rejected", func(t *testing.T) {
		resp, err := http.Get(server.URL)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL, nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("valid token accepted", func(t *testing.T) {
		req, _ := http.NewRequest("GET", wsURL(server), nil)
		req.Header.Set("Authorization", "Bearer "+validToken)

		dialer := websocket.Dialer{}
		conn, _, err := dialer.Dial(wsURL(server), req.Header)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		time.Sleep(50 * time.Millisecond)
		if count := hub.ClientCount(); count != 1 {
			t.Errorf("ClientCount = %d, want 1", count)
		}
	})
}
