// Package mcprelay implements the MCP Relay Server — an outbound WebSocket bridge
// that lets remote MCP tool providers (e.g. Diane instances behind NAT) register
// and receive tool-call requests from the agentic runner.
//
// Star topology:
//
//	Diane Slave (laptop) ──outbound WS──┐
//	Diane Master (server) ─outbound WS──┤
//	                                    ▼
//	                        MCP Relay (memory.emergent-company.ai)
//	                                    │
//	                                    ▼
//	                        Agentic Runner → calls tools on any instance
package mcprelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ErrSessionNotFound is returned when a relay session is not in the active set.
// The relay was connected at some point (so tools were registered) but is currently
// disconnected (e.g. laptop went to sleep, network dropped). Callers handling this
// error should return a user-friendly message suggesting retry later.
var ErrSessionNotFound = errors.New("relay session not found or disconnected")

// -----------------------------------------------------------------------------
// Wire protocol frames
// -----------------------------------------------------------------------------

// FrameType identifies the kind of relay frame.
type FrameType string

const (
	FrameRegister FrameType = "register"
	FrameRequest  FrameType = "request"
	FrameResponse FrameType = "response"
	FramePing     FrameType = "ping"
	FramePong     FrameType = "pong"
	FrameError    FrameType = "error"
)

// RegisterFrame is the first frame sent by the remote MCP provider after connect.
type RegisterFrame struct {
	Type       FrameType      `json:"type"`
	InstanceID string         `json:"instance_id"`
	Version    string         `json:"version,omitempty"`
	Tools      map[string]any `json:"tools,omitempty"` // result of MCP tools/list
}

// RequestFrame is sent by the relay → remote provider (tool call).
type RequestFrame struct {
	Type    FrameType      `json:"type"`
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"` // MCP JSON-RPC payload
}

// ResponseFrame is sent by remote provider → relay (tool result).
// ID is json.RawMessage because the MCP/JSON-RPC 2.0 spec allows the id field
// to be a string, number, or null. Using json.RawMessage avoids an unmarshal
// error when the remote provider sends a numeric id (e.g. {"id": 1, ...}).
type ResponseFrame struct {
	Type    FrameType       `json:"type"`
	ID      json.RawMessage `json:"id"`
	Payload map[string]any  `json:"payload"`
	Error   string          `json:"error,omitempty"`
}

// pendingKey returns a normalised string suitable for use as a map key.
// It strips surrounding quotes from JSON string values so that "1" and 1
// both map to the same key "1".
func (r ResponseFrame) pendingKey() string {
	if len(r.ID) == 0 {
		return ""
	}
	// JSON string — strip the enclosing quotes.
	var s string
	if json.Unmarshal(r.ID, &s) == nil {
		return s
	}
	// JSON number, bool, or null — use the raw bytes as the key.
	return string(r.ID)
}

// PingFrame / PongFrame for keepalive.
type PingFrame struct {
	Type FrameType `json:"type"`
}

// -----------------------------------------------------------------------------
// Session — one connected MCP provider instance
// -----------------------------------------------------------------------------

const (
	idleTimeout    = 90 * time.Second
	writeTimeout   = 10 * time.Second
	maxPendingReqs = 64
)

// pendingCall holds a channel waiting for the response to an in-flight tool call.
type pendingCall struct {
	ch chan ResponseFrame
}

// Session represents one live WebSocket connection from a remote MCP provider.
type Session struct {
	ProjectID   string
	InstanceID  string
	Version     string
	Tools       map[string]any
	ConnectedAt time.Time

	conn    *websocket.Conn
	mu      sync.Mutex // guards pending map
	writeMu sync.Mutex // serializes all WebSocket writes (gorilla requires single writer)
	pending map[string]*pendingCall
	done    chan struct{}
}

func newSession(projectID, instanceID, version string, tools map[string]any, conn *websocket.Conn) *Session {
	return &Session{
		ProjectID:   projectID,
		InstanceID:  instanceID,
		Version:     version,
		Tools:       tools,
		ConnectedAt: time.Now().UTC(),
		conn:        conn,
		pending:     make(map[string]*pendingCall),
		done:        make(chan struct{}),
	}
}

// writeMessage serializes all WebSocket writes so gorilla's single-writer
// constraint is satisfied across the ping goroutine, SendRequest, and the
// app-level pong handler.
func (s *Session) writeMessage(msgType int, data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return s.conn.WriteMessage(msgType, data)
}

// SendRequest sends a tool-call RequestFrame to the remote provider and waits for
// the matching ResponseFrame.  Times out after writeTimeout + idleTimeout.
func (s *Session) SendRequest(ctx context.Context, id string, payload map[string]any) (map[string]any, error) {
	ch := make(chan ResponseFrame, 1)

	s.mu.Lock()
	if len(s.pending) >= maxPendingReqs {
		s.mu.Unlock()
		return nil, fmt.Errorf("too many in-flight requests")
	}
	s.pending[id] = &pendingCall{ch: ch}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	frame := RequestFrame{Type: FrameRequest, ID: id, Payload: payload}
	data, _ := json.Marshal(frame)

	if err := s.writeMessage(websocket.TextMessage, data); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != "" {
			return nil, fmt.Errorf("remote error: %s", resp.Error)
		}
		return resp.Payload, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("session disconnected")
	}
}

// deliverResponse routes an incoming response frame to the waiting caller.
func (s *Session) deliverResponse(resp ResponseFrame) {
	s.mu.Lock()
	pc, ok := s.pending[resp.pendingKey()]
	s.mu.Unlock()
	if ok {
		select {
		case pc.ch <- resp:
		default:
		}
	}
}

// close signals the session is done.
func (s *Session) close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// -----------------------------------------------------------------------------
// Service — session registry
// -----------------------------------------------------------------------------

// RelayChangeCallback is notified when a relay session is registered or unregistered.
type RelayChangeCallback func(projectID, instanceID string, isRegister bool)

// Service manages all active relay sessions, keyed by (projectID, instanceID).
type Service struct {
	log              *slog.Logger
	mu               sync.RWMutex
	sessions         map[string]*Session // key: projectID+"/"+instanceID
	onChangeCallbacks []RelayChangeCallback
}

// NewService creates a new relay service.
func NewService(log *slog.Logger) *Service {
	return &Service{
		log:      log,
		sessions: make(map[string]*Session),
	}
}

// OnChange registers a callback that is invoked whenever a relay session
// is registered or unregistered.  Callbacks are called with the projectID,
// instanceID, and a boolean (true = registered, false = unregistered).
func (s *Service) OnChange(cb RelayChangeCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChangeCallbacks = append(s.onChangeCallbacks, cb)
}

func sessionKey(projectID, instanceID string) string {
	return projectID + "/" + instanceID
}

// Register adds a new session, replacing any prior session for the same key.
func (s *Service) Register(sess *Session) {
	key := sessionKey(sess.ProjectID, sess.InstanceID)
	s.mu.Lock()
	if old, ok := s.sessions[key]; ok {
		old.close()
	}
	s.sessions[key] = sess
	callbacks := append([]RelayChangeCallback(nil), s.onChangeCallbacks...)
	s.mu.Unlock()
	s.log.Info("mcprelay: instance registered",
		slog.String("project_id", sess.ProjectID),
		slog.String("instance_id", sess.InstanceID),
		slog.Int("tools", len(sess.Tools)),
	)
	for _, cb := range callbacks {
		cb(sess.ProjectID, sess.InstanceID, true)
	}
}

// Unregister removes the session.
func (s *Service) Unregister(projectID, instanceID string) {
	key := sessionKey(projectID, instanceID)
	s.mu.Lock()
	sess, ok := s.sessions[key]
	if ok {
		delete(s.sessions, key)
	}
	callbacks := append([]RelayChangeCallback(nil), s.onChangeCallbacks...)
	s.mu.Unlock()
	if ok {
		sess.close()
		s.log.Info("mcprelay: instance unregistered",
			slog.String("project_id", projectID),
			slog.String("instance_id", instanceID),
		)
	}
	for _, cb := range callbacks {
		cb(projectID, instanceID, false)
	}
}

// Get returns the session for a (projectID, instanceID) pair.
func (s *Service) Get(projectID, instanceID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sessionKey(projectID, instanceID)]
	return sess, ok
}

// ListByProject returns all sessions for a project.
func (s *Service) ListByProject(projectID string) []*Session {
	prefix := projectID + "/"
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Session
	for k, sess := range s.sessions {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, sess)
		}
	}
	return out
}

// CallTool forwards an MCP tool call to a connected relay instance.
// The instanceID is the bare instance ID; the toolName is the bare MCP tool name
// (without any prefix). Returns the raw MCP tool result as a map.
func (s *Service) CallTool(ctx context.Context, projectID, instanceID, toolName string, args map[string]any) (map[string]any, error) {
	sess, ok := s.Get(projectID, instanceID)
	if !ok {
		return nil, fmt.Errorf("%w: instance %q", ErrSessionNotFound, instanceID)
	}

	reqID := uuid.New().String()
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}

	return sess.SendRequest(ctx, reqID, payload)
}
