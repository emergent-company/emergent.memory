package main

import (
	"crypto/subtle"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// voiceBinding is the per-room binding the gateway hands to a bridge worker:
// the worker's Memory credentials + agent identity for that one voice session.
// It is delivered server-side only (via the internal endpoint), never placed in
// the client's join JWT whose room config is client-decodable.
type voiceBinding struct {
	ProjectID         string `json:"project_id"`
	OrgID             string `json:"org_id,omitempty"`
	AgentDefinitionID string `json:"agent_definition_id"`
	Language          string `json:"language,omitempty"`
	Token             string `json:"token"`
}

// voiceBindingTTL is how long an unconsumed binding lives before it expires.
const voiceBindingTTL = 5 * time.Minute

type voiceBindingEntry struct {
	binding   voiceBinding
	expiresAt time.Time
}

// voiceBindingStore is an in-memory, one-time-consume store of voice bindings
// keyed by LiveKit room name. Bindings are short-lived and deleted on read, so
// a room's credential is handed to its worker exactly once.
type voiceBindingStore struct {
	mu      sync.Mutex
	entries map[string]voiceBindingEntry
}

func newVoiceBindingStore() *voiceBindingStore {
	return &voiceBindingStore{entries: map[string]voiceBindingEntry{}}
}

// Set stores a binding for room with a short expiry.
func (s *voiceBindingStore) Set(room string, b voiceBinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[room] = voiceBindingEntry{binding: b, expiresAt: time.Now().Add(voiceBindingTTL)}
}

// Consume returns and removes the binding for room. The bool is false when the
// room is unknown or the binding expired.
func (s *voiceBindingStore) Consume(room string) (voiceBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[room]
	if !ok {
		return voiceBinding{}, false
	}
	delete(s.entries, room)
	if time.Now().After(e.expiresAt) {
		return voiceBinding{}, false
	}
	return e.binding, true
}

// voiceBindingHandler serves GET /internal/voice-binding?room=… to bridge
// workers. Auth is the shared WORKER_INTERNAL_KEY header, not the web session —
// this endpoint is exempt from authDispatch and is its own trust boundary.
func (s *Server) voiceBindingHandler(c echo.Context) error {
	key := c.Request().Header.Get("X-Worker-Key")
	if s.cfg.WorkerInternalKey == "" || subtle.ConstantTimeCompare([]byte(key), []byte(s.cfg.WorkerInternalKey)) != 1 {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	room := c.QueryParam("room")
	if room == "" {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "missing room"})
	}
	b, ok := s.bindings.Consume(room)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "unknown or expired room"})
	}
	return c.JSON(http.StatusOK, b)
}
