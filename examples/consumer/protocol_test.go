package main

import (
	"strings"
	"testing"
)

func TestParseServerMessage(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
		want    serverMessage
	}{
		{
			name: "presence with payload",
			data: `{"type":"presence","payload":{"source":"ipc","activity":{"details":"In a match","type":0}}}`,
			want: serverMessage{Type: "presence", Payload: []byte(`{"source":"ipc","activity":{"details":"In a match","type":0}}`)},
		},
		{
			name: "state with status and client id",
			data: `{"type":"state","status":"connected","client_id":"123"}`,
			want: serverMessage{Type: "state", Status: "connected", ClientID: "123"},
		},
		{
			name: "error with payload",
			data: `{"type":"error","payload":{"code":"invalid_message","message":"boom"}}`,
			want: serverMessage{Type: "error", Payload: []byte(`{"code":"invalid_message","message":"boom"}`)},
		},
		{name: "invalid JSON", data: `{"type":`, wantErr: true},
		{name: "missing type", data: `{"status":"connected"}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseServerMessage([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseServerMessage(%s) error = %v, wantErr %v", tt.data, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.Status != tt.want.Status {
				t.Errorf("Status = %q, want %q", got.Status, tt.want.Status)
			}
			if got.ClientID != tt.want.ClientID {
				t.Errorf("ClientID = %q, want %q", got.ClientID, tt.want.ClientID)
			}
			if string(got.Payload) != string(tt.want.Payload) {
				t.Errorf("Payload = %s, want %s", got.Payload, tt.want.Payload)
			}
		})
	}
}

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "presence with details state party and buttons",
			data: `{"type":"presence","payload":{"source":"ipc","activity":{"details":"In a match","state":"Winning 3-1","party":{"id":"p1","size":[3,5]},"buttons":[{"label":"Join","url":"https://example.com"}],"type":0}}}`,
			want: `presence: In a match | Winning 3-1 [Playing] party 3/5 button "Join" -> https://example.com (source: ipc)`,
		},
		{
			name: "presence with streaming type",
			data: `{"type":"presence","payload":{"source":"ipc","activity":{"details":"Speedrun","type":1}}}`,
			want: `presence: Speedrun [Streaming] (source: ipc)`,
		},
		{
			name: "presence with no activity source",
			data: `{"type":"presence","payload":{"source":"none","activity":null}}`,
			want: `presence: no activity source (none)`,
		},
		{
			name: "presence cleared",
			data: `{"type":"presence","payload":{"source":"ipc","activity":{}}}`,
			want: `presence: cleared (ipc)`,
		},
		{
			name: "state connected",
			data: `{"type":"state","status":"connected","client_id":"123456789012345678"}`,
			want: `state: discord connected (client 123456789012345678)`,
		},
		{
			name: "state without client id",
			data: `{"type":"state","status":"reconnecting"}`,
			want: `state: discord reconnecting`,
		},
		{
			name: "current snapshot",
			data: `{"type":"current","payload":{"source":"ipc","activity":{"details":"Listening to music","type":2}}}`,
			want: `current: Listening to music [Listening] (source: ipc)`,
		},
		{
			name: "current with no activity source",
			data: `{"type":"current","payload":{"source":"none","activity":null}}`,
			want: `current: no activity source (none)`,
		},
		{
			name: "error",
			data: `{"type":"error","payload":{"code":"invalid_message","message":"unknown message type \"bogus\""}}`,
			want: `error: invalid_message: unknown message type "bogus"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := parseServerMessage([]byte(tt.data))
			if err != nil {
				t.Fatalf("parseServerMessage(%s): %v", tt.data, err)
			}
			got, err := formatMessage(msg)
			if err != nil {
				t.Fatalf("formatMessage: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatMessageRejectsUndecodablePayloads(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "presence payload not an object", data: `{"type":"presence","payload":5}`},
		{name: "presence activity not an object", data: `{"type":"presence","payload":{"source":"ipc","activity":5}}`},
		{name: "current payload not an object", data: `{"type":"current","payload":"x"}`},
		{name: "error payload not an object", data: `{"type":"error","payload":[1]}`},
		{name: "unknown type", data: `{"type":"bogus"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := parseServerMessage([]byte(tt.data))
			if err != nil {
				t.Fatalf("parseServerMessage(%s): %v", tt.data, err)
			}
			if _, err := formatMessage(msg); err == nil {
				t.Errorf("formatMessage(%s) = nil error, want error", tt.data)
			} else if !strings.Contains(err.Error(), "payload") && tt.name != "unknown type" {
				t.Errorf("error %q does not mention payload", err)
			}
		})
	}
}
