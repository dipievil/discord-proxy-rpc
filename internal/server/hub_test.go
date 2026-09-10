package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func newTestHub(t *testing.T) (*Hub, context.CancelFunc) {
	t.Helper()
	logger := zap.NewNop()
	hub := NewHub(logger)
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	return hub, cancel
}

func newMockConn(t *testing.T) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			mt, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(mt, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestHubRegisterUnregister(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	conn := newMockConn(t)
	client := NewClient(conn, hub)

	hub.Register(client)
	time.Sleep(50 * time.Millisecond)

	if count := hub.ClientCount(); count != 1 {
		t.Fatalf("ClientCount = %d, want 1", count)
	}

	hub.Unregister(client)
	time.Sleep(50 * time.Millisecond)

	if count := hub.ClientCount(); count != 0 {
		t.Fatalf("ClientCount after unregister = %d, want 0", count)
	}
}

func TestHubBroadcast(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	conn1 := newMockConn(t)
	conn2 := newMockConn(t)

	client1 := NewClient(conn1, hub)
	client1.subscribe(MsgTypePresence)
	hub.Register(client1)
	go client1.WritePump()

	client2 := NewClient(conn2, hub)
	client2.subscribe(MsgTypePresence)
	hub.Register(client2)
	go client2.WritePump()

	time.Sleep(50 * time.Millisecond)

	if count := hub.ClientCount(); count != 2 {
		t.Fatalf("ClientCount = %d, want 2", count)
	}

	activity := types.Activity{Details: "Playing", State: "In match", Type: types.ActivityPlaying}
	msg, err := NewPresenceMessage(activity)
	if err != nil {
		t.Fatalf("NewPresenceMessage: %v", err)
	}
	hub.Broadcast(msg)
	time.Sleep(100 * time.Millisecond)

	for i, conn := range []*websocket.Conn{conn1, conn2} {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client %d ReadMessage: %v", i+1, err)
		}
		var received ServerMessage
		if err := json.Unmarshal(data, &received); err != nil {
			t.Fatalf("client %d Unmarshal: %v", i+1, err)
		}
		if received.Type != MsgTypePresence {
			t.Errorf("client %d type = %q, want %q", i+1, received.Type, MsgTypePresence)
		}
	}
}

func TestHubBroadcastFiltered(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	conn := newMockConn(t)
	client := NewClient(conn, hub)
	client.subscribe(MsgTypePresence)
	hub.Register(client)
	go client.WritePump()

	time.Sleep(50 * time.Millisecond)

	activity := types.Activity{Details: "Test", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected subscribed message but got error: %v", err)
	}
	var received ServerMessage
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if received.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q", received.Type, MsgTypePresence)
	}

	stateMsg := NewStateMessage(StateDisconnected)
	hub.Broadcast(stateMsg)

	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected no message for unsubscribed event, but got one")
	}
}

func TestHubBroadcastStateInjectsClientID(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	hub.ClientID = "test-client-id"

	conn := newMockConn(t)
	client := NewClient(conn, hub)
	client.subscribe(MsgTypeState)
	hub.Register(client)
	go client.WritePump()

	time.Sleep(50 * time.Millisecond)

	stateMsg := NewStateMessage(StateConnected)
	hub.Broadcast(stateMsg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var received ServerMessage
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if received.Type != MsgTypeState {
		t.Errorf("type = %q, want %q", received.Type, MsgTypeState)
	}
	if received.ClientID != "test-client-id" {
		t.Errorf("ClientID = %q, want %q", received.ClientID, "test-client-id")
	}
}

func TestHubBroadcastStateKeepsExplicitClientID(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	hub.ClientID = "hub-client-id"

	conn := newMockConn(t)
	client := NewClient(conn, hub)
	client.subscribe(MsgTypeState)
	hub.Register(client)
	go client.WritePump()

	time.Sleep(50 * time.Millisecond)

	stateMsg := NewStateMessage(StateConnected, "explicit-client-id")
	hub.Broadcast(stateMsg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var received ServerMessage
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if received.ClientID != "explicit-client-id" {
		t.Errorf("ClientID = %q, want %q", received.ClientID, "explicit-client-id")
	}
}

func TestHubClientCount(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	for i := 0; i < 5; i++ {
		conn := newMockConn(t)
		client := NewClient(conn, hub)
		hub.Register(client)
		go client.WritePump()
	}

	time.Sleep(100 * time.Millisecond)

	if count := hub.ClientCount(); count != 5 {
		t.Fatalf("ClientCount = %d, want 5", count)
	}
}

func TestHubConcurrent(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	type entry struct {
		conn   *websocket.Conn
		client *Client
	}
	entries := make([]entry, 10)
	for i := 0; i < 10; i++ {
		conn := newMockConn(t)
		client := NewClient(conn, hub)
		entries[i] = entry{conn: conn, client: client}
	}

	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(e entry) {
			defer wg.Done()
			hub.Register(e.client)
			go e.client.WritePump()
			time.Sleep(20 * time.Millisecond)
			hub.Unregister(e.client)
		}(e)
	}

	wg.Wait()
	time.Sleep(50 * time.Millisecond)

	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount = %d, want 0", count)
		return
	}

	for i := 0; i < 5; i++ {
		activity := types.Activity{Details: "test", Type: types.ActivityPlaying}
		msg, _ := NewPresenceMessage(activity)
		hub.Broadcast(msg)
	}
	time.Sleep(100 * time.Millisecond)
}

func TestClientSendMessage(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	conn := newMockConn(t)
	client := NewClient(conn, hub)

	data := []byte(`{"type":"presence","payload":{}}`)
	client.SendMessage(data)

	select {
	case received := <-client.send:
		if string(received) != string(data) {
			t.Errorf("SendMessage data = %q, want %q", received, data)
		}
	case <-time.After(time.Second):
		t.Fatal("SendMessage did not put data in send channel")
	}
}

func TestHubGetCurrentPresence(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	activity := types.Activity{Details: "Current Game", Type: types.ActivityPlaying}
	hub.GetCurrentPresence = func() types.Activity {
		return activity
	}

	var serverConn *websocket.Conn
	serverReady := make(chan struct{})
	inbox := make(chan []byte, 16)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConn = c
		close(serverReady)
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			inbox <- data
		}
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	client := NewClient(conn, hub)
	hub.Register(client)

	go client.WritePump()
	go client.ReadPump()

	<-serverReady
	time.Sleep(50 * time.Millisecond)

	getCurrentMsg := NewGetCurrentMessage()
	data, err := json.Marshal(getCurrentMsg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	serverConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := serverConn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	var received []byte
	select {
	case received = <-inbox:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for current message")
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

func TestHubMultipleEventTypes(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	conn := newMockConn(t)
	client := NewClient(conn, hub)
	client.subscribe(MsgTypePresence, MsgTypeState)
	hub.Register(client)
	go client.WritePump()

	time.Sleep(50 * time.Millisecond)

	activity := types.Activity{Details: "Game", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage for presence: %v", err)
	}
	var received ServerMessage
	json.Unmarshal(data, &received)
	if received.Type != MsgTypePresence {
		t.Errorf("first message type = %q, want %q", received.Type, MsgTypePresence)
	}

	stateMsg := NewStateMessage(StateConnected)
	hub.Broadcast(stateMsg)

	_, data, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage for state: %v", err)
	}
	json.Unmarshal(data, &received)
	if received.Type != MsgTypeState {
		t.Errorf("second message type = %q, want %q", received.Type, MsgTypeState)
	}
}

func TestClientReadPumpSubscribe(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	var serverConn *websocket.Conn
	serverReady := make(chan struct{})
	inbox := make(chan []byte, 16)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConn = c
		close(serverReady)
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			inbox <- data
		}
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	client := NewClient(conn, hub)
	hub.Register(client)
	go client.WritePump()
	go client.ReadPump()

	<-serverReady
	time.Sleep(50 * time.Millisecond)

	subMsg, err := NewSubscribeMessage([]string{MsgTypePresence, MsgTypeState})
	if err != nil {
		t.Fatalf("NewSubscribeMessage: %v", err)
	}
	data, _ := json.Marshal(subMsg)

	serverConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := serverConn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	activity := types.Activity{Details: "Game", Type: types.ActivityPlaying}
	presenceMsg, _ := NewPresenceMessage(activity)
	hub.Broadcast(presenceMsg)

	var received []byte
	select {
	case received = <-inbox:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for presence message")
	}

	var resp ServerMessage
	json.Unmarshal(received, &resp)
	if resp.Type != MsgTypePresence {
		t.Errorf("type = %q, want %q", resp.Type, MsgTypePresence)
	}
}

func TestClientWritePumpPing(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	pongReceived := make(chan struct{})
	conn.SetPongHandler(func(string) error {
		select {
		case <-pongReceived:
		default:
			close(pongReceived)
		}
		return nil
	})
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	client := NewClient(conn, hub)
	client.pingPeriod = 100 * time.Millisecond
	go client.WritePump()

	select {
	case <-pongReceived:
	case <-time.After(3 * time.Second):
		t.Fatal("expected pong reply to ping, got none")
	}
}

func TestHubClose(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()

	conn1 := newMockConn(t)
	conn2 := newMockConn(t)
	client1 := NewClient(conn1, hub)
	client2 := NewClient(conn2, hub)
	hub.Register(client1)
	hub.Register(client2)

	time.Sleep(50 * time.Millisecond)
	hub.Close()

	time.Sleep(100 * time.Millisecond)

	if count := hub.ClientCount(); count != 0 {
		t.Fatalf("ClientCount after Close = %d, want 0", count)
	}
}
