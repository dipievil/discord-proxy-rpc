package tray

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientState(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   ProxyState
	}{
		{name: "connected", status: "connected", want: StateConnected},
		{name: "reconnecting", status: "reconnecting", want: StateReconnecting},
		{name: "disconnected", status: "disconnected", want: StateDisconnected},
		{name: "unknown status", status: "starting", want: StateUnreachable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				if r.URL.Path != statePath {
					t.Errorf("path = %s, want %s", r.URL.Path, statePath)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"` + test.status + `"}`))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "", nil)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			state, err := client.State(context.Background())
			if err != nil {
				t.Fatalf("State: %v", err)
			}
			if state != test.want {
				t.Errorf("State = %q, want %q", state, test.want)
			}
		})
	}
}

func TestClientSendsBearerToken(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		wantAuth  string
		wantEmpty bool
	}{
		{name: "token configured", token: "secret-token", wantAuth: "Bearer secret-token"},
		{name: "no token", token: "", wantEmpty: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotAuth string
			var sawHeader bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				_, sawHeader = r.Header["Authorization"]
				_, _ = w.Write([]byte(`{"status":"connected"}`))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, test.token, nil)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if _, err := client.State(context.Background()); err != nil {
				t.Fatalf("State: %v", err)
			}
			if test.wantEmpty {
				if sawHeader {
					t.Errorf("Authorization header = %q, want none", gotAuth)
				}
				return
			}
			if gotAuth != test.wantAuth {
				t.Errorf("Authorization = %q, want %q", gotAuth, test.wantAuth)
			}
		})
	}
}

func TestClientFailuresReportUnreachable(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "unauthorized",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"AUTH_001","error":"unauthorized"}`))
			},
		},
		{
			name: "invalid JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"status":`))
			},
		},
		{
			name:    "empty body",
			handler: func(http.ResponseWriter, *http.Request) {},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()

			client, err := NewClient(server.URL, "", nil)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			assertUnreachable(t, client)
		})
	}
}

func TestClientUnreachableProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	client, err := NewClient(url, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	assertUnreachable(t, client)
}

func assertUnreachable(t *testing.T, client *Client) {
	t.Helper()
	state, err := client.State(context.Background())
	if err == nil {
		t.Fatal("State returned no error, want a transport/decoding error")
	}
	if state != StateUnreachable {
		t.Errorf("State = %q, want %q", state, StateUnreachable)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks the token: %v", err)
	}
}

func TestNewClient(t *testing.T) {
	if _, err := NewClient("ftp://proxy.local", "", nil); err == nil {
		t.Error("NewClient accepted an unsupported scheme")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/base"+statePath {
			t.Errorf("path = %s, want /base%s", r.URL.Path, statePath)
		}
		_, _ = w.Write([]byte(`{"status":"connected"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/base/", "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.State(context.Background()); err != nil {
		t.Fatalf("State: %v", err)
	}
}
