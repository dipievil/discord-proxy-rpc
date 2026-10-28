// Package main implements a minimal example consumer for the
// discord-proxy-rpc WebSocket API.
//
// The wire-protocol structs below mirror the server's message format
// (documented in the repository README) but are duplicated on purpose:
// an example should be copy-pasteable into any Go project without
// importing this module's internal packages.
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

const (
	msgTypePresence   = "presence"
	msgTypeState      = "state"
	msgTypeCurrent    = "current"
	msgTypeSubscribe  = "subscribe"
	msgTypeGetCurrent = "get_current"
	msgTypeError      = "error"
)

type serverMessage struct {
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Status   string          `json:"status,omitempty"`
	ClientID string          `json:"client_id,omitempty"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func parseServerMessage(data []byte) (serverMessage, error) {
	var msg serverMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return serverMessage{}, fmt.Errorf("invalid message JSON: %w", err)
	}
	if msg.Type == "" {
		return serverMessage{}, fmt.Errorf("message type is required")
	}
	return msg, nil
}

func formatMessage(msg serverMessage) (string, error) {
	switch msg.Type {
	case msgTypePresence, msgTypeCurrent:
		var snapshot types.PresenceSnapshot
		if err := json.Unmarshal(msg.Payload, &snapshot); err != nil {
			return "", fmt.Errorf("decoding %s payload: %w", msg.Type, err)
		}
		if snapshot.Activity == nil {
			return msg.Type + ": no activity source (" + string(snapshot.Source) + ")", nil
		}
		if snapshot.Activity.IsEmpty() {
			return msg.Type + ": cleared (" + string(snapshot.Source) + ")", nil
		}
		return msg.Type + ": " + formatActivity(*snapshot.Activity) + " (source: " + string(snapshot.Source) + ")", nil
	case msgTypeState:
		line := "state: discord " + msg.Status
		if msg.ClientID != "" {
			line += " (client " + msg.ClientID + ")"
		}
		return line, nil
	case msgTypeError:
		var ep errorPayload
		if err := json.Unmarshal(msg.Payload, &ep); err != nil {
			return "", fmt.Errorf("decoding error payload: %w", err)
		}
		return fmt.Sprintf("error: %s: %s", ep.Code, ep.Message), nil
	default:
		return "", fmt.Errorf("unknown message type %q", msg.Type)
	}
}

func formatActivity(a types.Activity) string {
	var parts []string
	if a.Details != "" {
		parts = append(parts, a.Details)
	}
	if a.State != "" {
		parts = append(parts, a.State)
	}
	line := strings.Join(parts, " | ")
	if line == "" {
		line = "(no details)"
	}

	line += " [" + activityTypeName(a.Type) + "]"

	if a.Party != nil && a.Party.Size[1] > 0 {
		line += fmt.Sprintf(" party %d/%d", a.Party.Size[0], a.Party.Size[1])
	}
	for _, b := range a.Buttons {
		line += fmt.Sprintf(" button %q -> %s", b.Label, b.URL)
	}
	return line
}

func activityTypeName(t types.ActivityType) string {
	switch t {
	case types.ActivityPlaying:
		return "Playing"
	case types.ActivityStreaming:
		return "Streaming"
	case types.ActivityListening:
		return "Listening"
	case types.ActivityWatching:
		return "Watching"
	case types.ActivityCustom:
		return "Custom"
	case types.ActivityCompeting:
		return "Competing"
	default:
		return fmt.Sprintf("Unknown(%d)", int(t))
	}
}
