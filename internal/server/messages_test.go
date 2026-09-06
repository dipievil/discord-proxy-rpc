package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func TestPresenceMessageRoundTrip(t *testing.T) {
	activity := types.Activity{
		Details: "In a match",
		State:   "Playing",
		Type:    types.ActivityPlaying,
	}

	original, err := NewPresenceMessage(activity)
	if err != nil {
		t.Fatalf("NewPresenceMessage: %v", err)
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ServerMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != MsgTypePresence {
		t.Errorf("Type = %q, want %q", decoded.Type, MsgTypePresence)
	}
	if decoded.Payload == nil {
		t.Fatal("Payload is nil")
	}

	var got types.Activity
	if err := json.Unmarshal(decoded.Payload, &got); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if got.Details != activity.Details {
		t.Errorf("Details = %q, want %q", got.Details, activity.Details)
	}
	if got.State != activity.State {
		t.Errorf("State = %q, want %q", got.State, activity.State)
	}
}

func TestStateMessageRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		status ConnectionState
	}{
		{"connected", StateConnected},
		{"disconnected", StateDisconnected},
		{"reconnecting", StateReconnecting},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := NewStateMessage(tt.status)

			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var decoded ServerMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if decoded.Type != MsgTypeState {
				t.Errorf("Type = %q, want %q", decoded.Type, MsgTypeState)
			}
			if decoded.Status != tt.status {
				t.Errorf("Status = %q, want %q", decoded.Status, tt.status)
			}
		})
	}
}

func TestCurrentMessageRoundTrip(t *testing.T) {
	activity := types.Activity{
		Details: "Listening to Spotify",
		Type:    types.ActivityListening,
	}

	original, err := NewCurrentMessage(activity)
	if err != nil {
		t.Fatalf("NewCurrentMessage: %v", err)
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ServerMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != MsgTypeCurrent {
		t.Errorf("Type = %q, want %q", decoded.Type, MsgTypeCurrent)
	}
	if decoded.Payload == nil {
		t.Fatal("Payload is nil")
	}

	var got types.Activity
	if err := json.Unmarshal(decoded.Payload, &got); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if got.Details != activity.Details {
		t.Errorf("Details = %q, want %q", got.Details, activity.Details)
	}
}

func TestSubscribeMessageRoundTrip(t *testing.T) {
	events := []string{MsgTypePresence, MsgTypeState}

	original := NewSubscribeMessage(events)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ClientMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != MsgTypeSubscribe {
		t.Errorf("Type = %q, want %q", decoded.Type, MsgTypeSubscribe)
	}
	if len(decoded.SubscribeEvents) != len(events) {
		t.Fatalf("Events len = %d, want %d", len(decoded.SubscribeEvents), len(events))
	}
	for i, e := range decoded.SubscribeEvents {
		if e != events[i] {
			t.Errorf("Events[%d] = %q, want %q", i, e, events[i])
		}
	}
}

func TestSubscribeMessageEmptyEvents(t *testing.T) {
	original := ClientMessage{Type: MsgTypeSubscribe, SubscribeEvents: SubscribeEvents{}}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ClientMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != MsgTypeSubscribe {
		t.Errorf("Type = %q, want %q", decoded.Type, MsgTypeSubscribe)
	}
	if len(decoded.SubscribeEvents) != 0 {
		t.Errorf("Events len = %d, want 0", len(decoded.SubscribeEvents))
	}
}

func TestGetCurrentMessageRoundTrip(t *testing.T) {
	original := NewGetCurrentMessage()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ClientMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != MsgTypeGetCurrent {
		t.Errorf("Type = %q, want %q", decoded.Type, MsgTypeGetCurrent)
	}
}

func TestInvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"server malformed", `{"type": "presence", "payload": {broken}`},
		{"client malformed", `{"type": "subscribe", "events": [broken]}`},
		{"server empty", ``},
		{"client empty", ``},
		{"server not json", `not json`},
		{"client not json", `not json`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.HasPrefix(tt.name, "server") {
				var msg ServerMessage
				err := msg.UnmarshalJSON([]byte(tt.data))
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				var msg ClientMessage
				err := msg.UnmarshalJSON([]byte(tt.data))
				if err == nil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestUnknownMessageType(t *testing.T) {
	t.Run("server unknown type", func(t *testing.T) {
		data := []byte(`{"type": "unknown", "payload": {}}`)
		var msg ServerMessage
		err := msg.UnmarshalJSON(data)
		if err == nil {
			t.Error("expected error for unknown server message type")
		}
	})

	t.Run("client unknown type", func(t *testing.T) {
		data := []byte(`{"type": "unknown"}`)
		var msg ClientMessage
		err := msg.UnmarshalJSON(data)
		if err == nil {
			t.Error("expected error for unknown client message type")
		}
	})

	t.Run("server missing type", func(t *testing.T) {
		data := []byte(`{"payload": {}}`)
		var msg ServerMessage
		err := msg.UnmarshalJSON(data)
		if err == nil {
			t.Error("expected error for missing type")
		}
	})

	t.Run("client missing type", func(t *testing.T) {
		data := []byte(`{"events": ["presence"]}`)
		var msg ClientMessage
		err := msg.UnmarshalJSON(data)
		if err == nil {
			t.Error("expected error for missing type")
		}
	})
}

func TestActivityFieldPreservation(t *testing.T) {
	activity := types.Activity{
		Details: "Playing ranked",
		State:   "Diamond III",
		Timestamps: &types.Timestamps{
			Start: 1234567890,
			End:   1234567999,
		},
		Assets: &types.Assets{
			LargeImage: "large.png",
			LargeText:  "Map",
			SmallImage: "small.png",
			SmallText:  "Role",
		},
		Party: &types.Party{
			ID:   "party-123",
			Size: [2]int{3, 5},
		},
		Buttons: []types.Button{
			{Label: "Join", URL: "https://example.com/join"},
			{Label: "Spectate", URL: "https://example.com/spec"},
		},
		Type: types.ActivityPlaying,
	}

	msg, err := NewPresenceMessage(activity)
	if err != nil {
		t.Fatalf("NewPresenceMessage: %v", err)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ServerMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var got types.Activity
	if err := json.Unmarshal(decoded.Payload, &got); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}

	if got.Details != activity.Details {
		t.Errorf("Details = %q, want %q", got.Details, activity.Details)
	}
	if got.State != activity.State {
		t.Errorf("State = %q, want %q", got.State, activity.State)
	}
	if got.Timestamps.Start != activity.Timestamps.Start {
		t.Errorf("Timestamps.Start = %d, want %d", got.Timestamps.Start, activity.Timestamps.Start)
	}
	if got.Timestamps.End != activity.Timestamps.End {
		t.Errorf("Timestamps.End = %d, want %d", got.Timestamps.End, activity.Timestamps.End)
	}
	if got.Assets.LargeImage != activity.Assets.LargeImage {
		t.Errorf("Assets.LargeImage = %q, want %q", got.Assets.LargeImage, activity.Assets.LargeImage)
	}
	if got.Assets.LargeText != activity.Assets.LargeText {
		t.Errorf("Assets.LargeText = %q, want %q", got.Assets.LargeText, activity.Assets.LargeText)
	}
	if got.Assets.SmallImage != activity.Assets.SmallImage {
		t.Errorf("Assets.SmallImage = %q, want %q", got.Assets.SmallImage, activity.Assets.SmallImage)
	}
	if got.Assets.SmallText != activity.Assets.SmallText {
		t.Errorf("Assets.SmallText = %q, want %q", got.Assets.SmallText, activity.Assets.SmallText)
	}
	if got.Party.ID != activity.Party.ID {
		t.Errorf("Party.ID = %q, want %q", got.Party.ID, activity.Party.ID)
	}
	if got.Party.Size != activity.Party.Size {
		t.Errorf("Party.Size = %v, want %v", got.Party.Size, activity.Party.Size)
	}
	if len(got.Buttons) != len(activity.Buttons) {
		t.Fatalf("Buttons len = %d, want %d", len(got.Buttons), len(activity.Buttons))
	}
	for i, b := range got.Buttons {
		if b.Label != activity.Buttons[i].Label {
			t.Errorf("Buttons[%d].Label = %q, want %q", i, b.Label, activity.Buttons[i].Label)
		}
		if b.URL != activity.Buttons[i].URL {
			t.Errorf("Buttons[%d].URL = %q, want %q", i, b.URL, activity.Buttons[i].URL)
		}
	}
	if got.Type != activity.Type {
		t.Errorf("Type = %d, want %d", got.Type, activity.Type)
	}
}

func TestStateMessageOmitsPayload(t *testing.T) {
	msg := NewStateMessage(StateConnected)

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	jsonStr := string(data)
	if strings.Contains(jsonStr, "payload") {
		t.Errorf("state message should not contain payload, got: %s", jsonStr)
	}
}

func TestPresenceMessageOmitsStatus(t *testing.T) {
	msg, err := NewPresenceMessage(types.Activity{Details: "test"})
	if err != nil {
		t.Fatalf("NewPresenceMessage: %v", err)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	jsonStr := string(data)
	if strings.Contains(jsonStr, "status") {
		t.Errorf("presence message should not contain status, got: %s", jsonStr)
	}
}

func TestSubscribeEventsMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		events   SubscribeEvents
		expected string
	}{
		{"nil events", nil, `[]`},
		{"empty events", SubscribeEvents{}, `[]`},
		{"with events", SubscribeEvents{"presence", "state"}, `["presence","state"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.events)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(data) != tt.expected {
				t.Errorf("got %s, want %s", string(data), tt.expected)
			}
		})
	}
}
