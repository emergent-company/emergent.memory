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

	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
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
	FrameConfig   FrameType = "config"
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
	// AcceptsManagedConfig advertises that the device operator opted in to
	// receiving server-managed configuration. Default false: the server SHALL NOT
	// push configuration to a device that does not advertise it.
	AcceptsManagedConfig bool `json:"accepts_managed_config"`
}

// ConfigFrame is a server→device configuration push. It is distinct from
// RequestFrame (tools/call-only) so the device can unambiguously handle and ack
// it. Version is monotonic per instance; the device rejects an older-or-equal
// version as stale.
type ConfigFrame struct {
	Type    FrameType                        `json:"type"`
	ID      string                           `json:"id"`
	Version int64                            `json:"version"`
	Servers []mcpregistry.DeviceServerConfig `json:"servers"`
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

	// OwnerSubject is the authenticated subject captured at register time; it is
	// the internal user ID of the device's operator, used to authorize
	// device-targeted configuration writes.
	OwnerSubject string
	// AcceptsManagedConfig is the device's opt-in flag from the register frame.
	AcceptsManagedConfig bool

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

// SendConfig sends a server→device ConfigFrame and waits for the matching
// ResponseFrame (the device's ack). Mirrors SendRequest's pending-map and
// serialized-write discipline; a blocking send is intentional — the non-blocking
// guarantee is provided by the caller (pushConfig runs it on a bounded-context
// goroutine).
func (s *Session) SendConfig(ctx context.Context, frame ConfigFrame) (map[string]any, error) {
	ch := make(chan ResponseFrame, 1)

	s.mu.Lock()
	if len(s.pending) >= maxPendingReqs {
		s.mu.Unlock()
		return nil, fmt.Errorf("too many in-flight requests")
	}
	s.pending[frame.ID] = &pendingCall{ch: ch}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, frame.ID)
		s.mu.Unlock()
	}()

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

// RegisteredCallback is notified AFTER a session's register acknowledgement has
// been written to the wire — the post-ack hydration hook. It is deliberately
// distinct from RelayChangeCallback (which fires synchronously before the ack),
// because a device's waitForAck drops pre-ack frames.
type RegisteredCallback func(projectID, instanceID string)

// Service manages all active relay sessions, keyed by (projectID, instanceID).
type Service struct {
	log               *slog.Logger
	mu                sync.RWMutex
	sessions          map[string]*Session // key: projectID+"/"+instanceID
	onChangeCallbacks []RelayChangeCallback
	onRegisteredCbs   []RegisteredCallback
	configVersions    map[string]int64 // key: sessionKey → last assigned version
}

// NewService creates a new relay service.
func NewService(log *slog.Logger) *Service {
	return &Service{
		log:            log,
		sessions:       make(map[string]*Session),
		configVersions: make(map[string]int64),
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

// OnRegistered registers a post-ack hydration callback, invoked by
// NotifyRegistered after the register ack is written. Used by the registry to
// hydrate device configuration on (re)connect.
func (s *Service) OnRegistered(cb RegisteredCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRegisteredCbs = append(s.onRegisteredCbs, cb)
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

// -----------------------------------------------------------------------------
// Device configuration push
// -----------------------------------------------------------------------------

// pushTimeout bounds a single config push goroutine so a slow/offline device can
// never leak a goroutine.
const pushTimeout = 30 * time.Second

// PushConfigToInstance pushes resolved config to a single opted-in instance.
// Unregistered or non-opted-in sessions are skipped (no error). The push is
// non-blocking: it runs on its own goroutine with a bounded context.
func (s *Service) PushConfigToInstance(ctx context.Context, projectID, instanceID string, servers []mcpregistry.DeviceServerConfig) {
	sess, ok := s.Get(projectID, instanceID)
	if !ok || !sess.AcceptsManagedConfig {
		return
	}
	s.pushConfig(sess, servers)
}

// PushConfigToProject pushes resolved config to every opted-in instance of the
// project. When admin is false, only the requester's own devices (ownerSubject)
// are targeted; an admin targets all opted-in devices. Each target's config is
// resolved via resolve so instance-scoped rows are never leaked across devices.
// Non-blocking.
func (s *Service) PushConfigToProject(ctx context.Context, projectID, ownerSubject string, admin bool, resolve func(ctx context.Context, instanceID string) ([]mcpregistry.DeviceServerConfig, error)) {
	for _, sess := range s.ListByProject(projectID) {
		if !sess.AcceptsManagedConfig {
			continue
		}
		if !admin && (ownerSubject == "" || sess.OwnerSubject != ownerSubject) {
			continue
		}
		// Snapshot the session for the goroutine.
		sess := sess
		servers, err := resolve(ctx, sess.InstanceID)
		if err != nil {
			s.log.Warn("mcprelay: resolve device config failed",
				slog.String("instance_id", sess.InstanceID),
				slog.String("error", err.Error()),
			)
			continue
		}
		s.pushConfig(sess, servers)
	}
}

// pushConfig assigns the next monotonic version for the instance and sends the
// frame on its own goroutine (non-blocking at the call site). The ack is
// consumed best-effort; a stale/error ack is never logged with secret values.
func (s *Service) pushConfig(sess *Session, servers []mcpregistry.DeviceServerConfig) {
	version := s.nextConfigVersion(sess.ProjectID, sess.InstanceID)
	frame := ConfigFrame{
		Type:    FrameConfig,
		ID:      uuid.New().String(),
		Version: version,
		Servers: servers,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
		defer cancel()
		// Ack is deliberately ignored here: pushing is best-effort and the frame
		// must never be logged (it may contain decrypted secrets).
		_, _ = sess.SendConfig(ctx, frame)
	}()
}

// nextConfigVersion returns the next monotonic version for an instance, keyed by
// sessionKey so it stays monotonic across reconnects within this process.
//
// The value is wall-clock based (Unix nanoseconds), not a counter that resets
// with the process: the connector persists its last-applied version to disk and
// rejects any frame with version <= that value as stale. A counter that reset to
// zero on a server restart would make every post-restart push look stale and the
// device would silently never receive another config update. Strict monotonicity
// (even if the clock does not advance between two pushes, or moves backwards) is
// preserved by bumping past the previous value.
func (s *Service) nextConfigVersion(projectID, instanceID string) int64 {
	key := sessionKey(projectID, instanceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	v := time.Now().UnixNano()
	if v <= s.configVersions[key] {
		v = s.configVersions[key] + 1
	}
	s.configVersions[key] = v
	return v
}

// OwnerSubject returns the owner subject recorded for an instance's session and
// whether that instance is currently registered.
func (s *Service) OwnerSubject(projectID, instanceID string) (string, bool) {
	sess, ok := s.Get(projectID, instanceID)
	if !ok {
		return "", false
	}
	return sess.OwnerSubject, true
}

// NotifyRegistered invokes the post-ack hydration callbacks for an instance. It
// is called by the handler AFTER the `registered` ack is written, and never uses
// OnChange (which fires before the ack and would be dropped by the connector's
// waitForAck). Non-blocking: callbacks run on their own goroutines.
func (s *Service) NotifyRegistered(projectID, instanceID string) {
	s.mu.RLock()
	cbs := append([]RegisteredCallback(nil), s.onRegisteredCbs...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb := cb
		go cb(projectID, instanceID)
	}
}
