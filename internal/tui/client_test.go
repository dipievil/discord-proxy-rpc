package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/server"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func wsTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestBackendSendsBearerToken(t *testing.T) {
	authCh := make(chan string, 1)
	upgrader := websocket.Upgrader{}
	srv := wsTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		authCh <- r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	backend := NewWSBackend(wsURL(srv), "secret-token")
	if err := backend.Dial(context.Background()); err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer backend.Close()

	select {
	case got := <-authCh:
		if got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q, want %q", got, "Bearer secret-token")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handshake")
	}
}

func TestBackendSubscribesThenRequestsCurrent(t *testing.T) {
	upgrader := websocket.Upgrader{}
	clientMsgs := make(chan server.ClientMessage, 4)
	srv := wsTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for i := 0; i < 2; i++ {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg, err := server.ParseClientMessage(data)
			if err != nil {
				return
			}
			clientMsgs <- *msg
		}

		current, err := server.NewCurrentMessage(types.PresenceSnapshot{
			Source:   types.SourceIPC,
			Activity: &types.Activity{Details: "from server"},
		}, "client-9")
		if err != nil {
			return
		}
		if err := conn.WriteJSON(current); err != nil {
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	backend := NewWSBackend(wsURL(srv), "")
	if err := backend.Dial(context.Background()); err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer backend.Close()

	var ev wsMsg
	select {
	case ev = <-backend.Messages():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for current message")
	}
	if ev.err != nil {
		t.Fatalf("received error event: %v", ev.err)
	}
	if ev.msg.Type != server.MsgTypeCurrent {
		t.Fatalf("message type = %q, want %q", ev.msg.Type, server.MsgTypeCurrent)
	}

	var snapshot types.PresenceSnapshot
	if err := json.Unmarshal(ev.msg.Payload, &snapshot); err != nil {
		t.Fatalf("unmarshalling payload: %v", err)
	}
	if snapshot.Source != types.SourceIPC || snapshot.Activity == nil || snapshot.Activity.Details != "from server" {
		t.Fatalf("snapshot = %+v, want ipc activity %q", snapshot, "from server")
	}
	if ev.msg.ClientID != "client-9" {
		t.Fatalf("client id = %q, want %q", ev.msg.ClientID, "client-9")
	}

	var subscribe server.ClientMessage
	select {
	case subscribe = <-clientMsgs:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribe")
	}
	if subscribe.Type != server.MsgTypeSubscribe {
		t.Fatalf("first client message = %q, want %q", subscribe.Type, server.MsgTypeSubscribe)
	}
	want := map[string]bool{server.MsgTypePresence: true, server.MsgTypeState: true}
	for _, event := range subscribe.SubscribeEvents {
		if !want[event] {
			t.Fatalf("subscribed to unexpected event %q", event)
		}
		delete(want, event)
	}
	if len(want) != 0 {
		t.Fatalf("missing subscribed events: %v", want)
	}

	var getCurrent server.ClientMessage
	select {
	case getCurrent = <-clientMsgs:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for get_current")
	}
	if getCurrent.Type != server.MsgTypeGetCurrent {
		t.Fatalf("second client message = %q, want %q", getCurrent.Type, server.MsgTypeGetCurrent)
	}
}

func TestBackendDeliversFatalErrorOnClosedConnection(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := wsTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for i := 0; i < 2; i++ {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	backend := NewWSBackend(wsURL(srv), "")
	if err := backend.Dial(context.Background()); err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer backend.Close()

	select {
	case ev := <-backend.Messages():
		if ev.err == nil || !ev.fatal {
			t.Fatalf("event = %+v, want fatal error", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fatal error event")
	}
}

func TestBackendDialRejectsBadEndpoint(t *testing.T) {
	srv := wsTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	})

	backend := NewWSBackend(wsURL(srv), "")
	if err := backend.Dial(context.Background()); err == nil {
		backend.Close()
		t.Fatal("Dial against rejected handshake should fail")
	}
}
