package main

import (
	"log"
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
	agent     string
	expiresAt time.Time
}

// voiceConsumption is the audit record of a one-time binding consumption: which
// authenticated agent took it and when.
type voiceConsumption struct {
	agent string
	at    time.Time
}

// voiceBindingStore is an in-memory, one-time-consume store of voice bindings
// keyed by (LiveKit room, agent). Bindings are short-lived and deleted on a
// successful consume, so a room's credential is handed out exactly once — and
// only to the worker whose authenticated agent matches the binding's agent.
type voiceBindingStore struct {
	mu       sync.Mutex
	entries  map[string]voiceBindingEntry
	consumed map[string]voiceConsumption
}

func newVoiceBindingStore() *voiceBindingStore {
	return &voiceBindingStore{
		entries:  map[string]voiceBindingEntry{},
		consumed: map[string]voiceConsumption{},
	}
}

// Set stores a binding for (room, agent) with a short expiry.
func (s *voiceBindingStore) Set(room, agent string, b voiceBinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[room] = voiceBindingEntry{binding: b, agent: agent, expiresAt: time.Now().Add(voiceBindingTTL)}
}

// Consume atomically returns and removes the binding for room, but only when
// the authenticated agent matches the agent the binding was minted for. The
// bool is false when the room is unknown, expired, or bound to a different
// agent. A mismatched agent does NOT consume the binding — the rightful worker
// can still take it later. A successful consume records the agent for audit.
func (s *voiceBindingStore) Consume(room, agent string) (voiceBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[room]
	if !ok {
		return voiceBinding{}, false
	}
	if time.Now().After(e.expiresAt) {
		delete(s.entries, room)
		return voiceBinding{}, false
	}
	if e.agent != agent {
		return voiceBinding{}, false
	}
	delete(s.entries, room)
	s.consumed[room] = voiceConsumption{agent: agent, at: time.Now()}
	return e.binding, true
}

// LastConsumption reports the authenticated agent that consumed room, if any.
// This is the "attributable" half of consumption: after the fact the gateway
// can say which worker took a room's binding.
func (s *voiceBindingStore) LastConsumption(room string) (voiceConsumption, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.consumed[room]
	return c, ok
}

// workerIdentity is the authenticated identity behind a per-worker credential.
type workerIdentity struct {
	agent string
}

// workerRegistry maps per-worker credentials to the worker's authenticated
// identity (agent name). The supervisor mints a fresh 256-bit credential for
// every spawned worker and revokes it on exit, so a worker proves it is the
// specific worker the gateway started for a given agent — not merely a holder
// of some shared key.
type workerRegistry struct {
	mu      sync.Mutex
	workers map[string]workerIdentity
}

func newWorkerRegistry() *workerRegistry {
	return &workerRegistry{workers: map[string]workerIdentity{}}
}

// issue mints a fresh credential bound to agent and registers it, returning the
// credential to inject into that worker only.
func (r *workerRegistry) issue(agent string) string {
	cred := randomHex(32)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workers[cred] = workerIdentity{agent: agent}
	return cred
}

// revoke removes a credential (worker exited or was stopped).
func (r *workerRegistry) revoke(cred string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.workers, cred)
}

// authenticate resolves a presented credential to its authenticated agent. ok
// is false when the credential is unknown (forged, revoked, or expired).
func (r *workerRegistry) authenticate(cred string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.workers[cred]
	return id.agent, ok
}

// voiceBindingHandler serves GET /internal/voice-binding?room=… to bridge
// workers. Auth is the per-worker credential in X-Worker-Key, resolved against
// the worker registry — not a shared key, and not the web session. The binding
// is consumed only if the authenticated agent matches the binding's agent.
func (s *Server) voiceBindingHandler(c echo.Context) error {
	cred := c.Request().Header.Get("X-Worker-Key")
	agent, ok := s.authenticateWorker(cred)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	room := c.QueryParam("room")
	if room == "" {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "missing room"})
	}
	b, ok := s.bindings.Consume(room, agent)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "unknown or expired room"})
	}
	log.Printf("voice-binding: room=%s consumed by agent=%s", room, agent)
	return c.JSON(http.StatusOK, b)
}

// authenticateWorker resolves a per-worker credential to its authenticated
// agent. A nil registry (bare test server / no worker spawn path) rejects
// everything.
func (s *Server) authenticateWorker(cred string) (string, bool) {
	if s.workerCreds == nil {
		return "", false
	}
	return s.workerCreds.authenticate(cred)
}
