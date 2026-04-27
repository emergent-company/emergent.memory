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
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

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
type ResponseFrame struct {
	Type    FrameType      `json:"type"`
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
	Error   string         `json:"error,omitempty"`
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
	pc, ok := s.pending[resp.ID]
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

// Service manages all active relay sessions, keyed by (projectID, instanceID).
type Service struct {
	log      *slog.Logger
	mu       sync.RWMutex
	sessions map[string]*Session // key: projectID+"/"+instanceID
}

// NewService creates a new relay service.
func NewService(log *slog.Logger) *Service {
	return &Service{
		log:      log,
		sessions: make(map[string]*Session),
	}
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
	s.mu.Unlock()
	s.log.Info("mcprelay: instance registered",
		slog.String("project_id", sess.ProjectID),
		slog.String("instance_id", sess.InstanceID),
		slog.Int("tools", len(sess.Tools)),
	)
}

// Unregister removes the session.
func (s *Service) Unregister(projectID, instanceID string) {
	key := sessionKey(projectID, instanceID)
	s.mu.Lock()
	sess, ok := s.sessions[key]
	if ok {
		delete(s.sessions, key)
	}
	s.mu.Unlock()
	if ok {
		sess.close()
		s.log.Info("mcprelay: instance unregistered",
			slog.String("project_id", projectID),
			slog.String("instance_id", instanceID),
		)
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
