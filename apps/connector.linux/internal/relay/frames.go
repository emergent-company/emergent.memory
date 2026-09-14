// Package relay implements the client side of the Memory MCP relay wire
// protocol. The hub (mcprelay) accepts one outbound WebSocket per connector
// instance: the client registers its tools and serves relayed tools/call
// requests. Wire shapes follow the mcprelay handler/service contract.
package relay

import (
	"encoding/json"
	"errors"
	"fmt"
)

// FrameType identifies the kind of relay frame. Values match mcprelay's
// FrameType constants.
type FrameType string

const (
	FrameRegister   FrameType = "register"
	FrameRequest    FrameType = "request"
	FrameResponse   FrameType = "response"
	FramePing       FrameType = "ping"
	FramePong       FrameType = "pong"
	FrameError      FrameType = "error"
	FrameRegistered FrameType = "registered"
)

// RegisterFrame is the first frame a connector sends after dialing. Tools
// carries the nested MCP tools/list result shape {"tools":[...]} the hub
// re-serves.
type RegisterFrame struct {
	Type       FrameType      `json:"type"`
	InstanceID string         `json:"instance_id"`
	Version    string         `json:"version,omitempty"`
	Tools      map[string]any `json:"tools,omitempty"`
}

// RequestFrame is sent by the hub to the connector: a relayed tool call.
// ID correlates with the response; Payload is a JSON-RPC 2.0 tools/call
// request.
type RequestFrame struct {
	Type    FrameType      `json:"type"`
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
}

// ResponseFrame is sent by the connector to the hub with the tool result. ID
// echoes the RequestFrame ID. Error, when set, signals a failed call to the
// hub (surfaced as a remote error on its side).
type ResponseFrame struct {
	Type    FrameType       `json:"type"`
	ID      json.RawMessage `json:"id"`
	Payload map[string]any  `json:"payload"`
	Error   string          `json:"error,omitempty"`
}

// PingFrame is an application-level keepalive frame.
type PingFrame struct {
	Type FrameType `json:"type"`
}

// PongFrame is the application-level reply to a PingFrame.
type PongFrame struct {
	Type FrameType `json:"type"`
}

// ErrorFrame reports a protocol-level problem from the hub (for example
// "first frame must be register").
type ErrorFrame struct {
	Type    FrameType `json:"type"`
	Message string    `json:"message"`
}

// RegisteredFrame acknowledges a registration.
type RegisteredFrame struct {
	Type       FrameType `json:"type"`
	InstanceID string    `json:"instance_id"`
}

// Errors returned by Parse for frames that cannot be decoded.
var (
	ErrMalformedJSON = errors.New("malformed frame JSON")
	ErrUnknownFrame  = errors.New("unknown frame type")
)

// Parse decodes a text frame into its concrete type. Malformed JSON returns
// an error wrapping ErrMalformedJSON; well-formed JSON with an unknown type
// returns an error wrapping ErrUnknownFrame (callers log and continue).
func Parse(data []byte) (any, error) {
	var base struct {
		Type FrameType `json:"type"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedJSON, err)
	}
	if base.Type == "" {
		return nil, fmt.Errorf("%w: empty type", ErrMalformedJSON)
	}

	var frame any
	switch base.Type {
	case FrameRegister:
		frame = &RegisterFrame{}
	case FrameRequest:
		frame = &RequestFrame{}
	case FrameResponse:
		frame = &ResponseFrame{}
	case FramePing:
		frame = &PingFrame{}
	case FramePong:
		frame = &PongFrame{}
	case FrameError:
		frame = &ErrorFrame{}
	case FrameRegistered:
		frame = &RegisteredFrame{}
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownFrame, base.Type)
	}
	if err := json.Unmarshal(data, frame); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedJSON, err)
	}
	return frame, nil
}

// Marshal encodes a frame struct. It fails when v is not a recognized relay
// frame shape.
func Marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal frame: %w", err)
	}
	if _, err := Parse(data); err != nil {
		return nil, fmt.Errorf("marshal frame: %w", err)
	}
	return data, nil
}

// RequestFrame.ID raw JSON string for echoing in a ResponseFrame.
func rawID(id string) json.RawMessage {
	raw, _ := json.Marshal(id)
	return raw
}
