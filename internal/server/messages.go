package server

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

const (
	MsgTypePresence   = "presence"
	MsgTypeState      = "state"
	MsgTypeCurrent    = "current"
	MsgTypeSubscribe  = "subscribe"
	MsgTypeGetCurrent = "get_current"
)

type ConnectionState string

const (
	StateConnected    ConnectionState = "connected"
	StateDisconnected ConnectionState = "disconnected"
	StateReconnecting ConnectionState = "reconnecting"
)

type ServerMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Status  ConnectionState `json:"status,omitempty"`
}

func (m *ServerMessage) UnmarshalJSON(data []byte) error {
	type Alias ServerMessage
	aux := &struct {
		*Alias
	}{Alias: (*Alias)(m)}

	if err := json.Unmarshal(data, aux); err != nil {
		return fmt.Errorf("invalid message JSON: %w", err)
	}

	switch m.Type {
	case MsgTypePresence, MsgTypeCurrent:
		if m.Payload == nil {
			return fmt.Errorf("message type %q requires payload", m.Type)
		}
	case MsgTypeState:
		if m.Status == "" {
			return errors.New("message type \"state\" requires status")
		}
	case "":
		return errors.New("message type is required")
	default:
		return fmt.Errorf("unknown message type: %q", m.Type)
	}
	return nil
}

type SubscribeEvents []string

func (e SubscribeEvents) MarshalJSON() ([]byte, error) {
	if len(e) == 0 {
		return json.Marshal([]string{})
	}
	return json.Marshal([]string(e))
}

type ClientMessage struct {
	Type           string          `json:"type"`
	SubscribeEvents SubscribeEvents `json:"events,omitempty"`
}

func (m *ClientMessage) UnmarshalJSON(data []byte) error {
	type Alias ClientMessage
	aux := &struct {
		*Alias
	}{Alias: (*Alias)(m)}

	if err := json.Unmarshal(data, aux); err != nil {
		return fmt.Errorf("invalid message JSON: %w", err)
	}

	switch m.Type {
	case MsgTypeSubscribe:
		if len(m.SubscribeEvents) == 0 {
			return errors.New("message type \"subscribe\" requires events")
		}
	case MsgTypeGetCurrent:
	case "":
		return errors.New("message type is required")
	default:
		return fmt.Errorf("unknown message type: %q", m.Type)
	}
	return nil
}

func NewPresenceMessage(activity types.Activity) (ServerMessage, error) {
	payload, err := json.Marshal(activity)
	if err != nil {
		return ServerMessage{}, fmt.Errorf("marshalling presence payload: %w", err)
	}
	return ServerMessage{Type: MsgTypePresence, Payload: payload}, nil
}

func NewStateMessage(status ConnectionState) ServerMessage {
	return ServerMessage{Type: MsgTypeState, Status: status}
}

func NewCurrentMessage(activity types.Activity) (ServerMessage, error) {
	payload, err := json.Marshal(activity)
	if err != nil {
		return ServerMessage{}, fmt.Errorf("marshalling current payload: %w", err)
	}
	return ServerMessage{Type: MsgTypeCurrent, Payload: payload}, nil
}

func NewSubscribeMessage(events []string) ClientMessage {
	return ClientMessage{Type: MsgTypeSubscribe, SubscribeEvents: events}
}

func NewGetCurrentMessage() ClientMessage {
	return ClientMessage{Type: MsgTypeGetCurrent}
}

func ParseServerMessage(data []byte) (*ServerMessage, error) {
	var msg ServerMessage
	if err := msg.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return &msg, nil
}

func ParseClientMessage(data []byte) (*ClientMessage, error) {
	var msg ClientMessage
	if err := msg.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return &msg, nil
}
