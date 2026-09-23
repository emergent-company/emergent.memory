package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- voiceBindingStore ---

func TestVoiceBindingStoreSetConsume(t *testing.T) {
	s := newVoiceBindingStore()
	s.Set("room-1", "agent-a", voiceBinding{ProjectID: "p1", Token: "emt-x"})
	b, ok := s.Consume("room-1", "agent-a")
	if !ok || b.ProjectID != "p1" || b.Token != "emt-x" {
		t.Fatalf("consume = %+v ok=%v", b, ok)
	}
	if _, ok := s.Consume("room-1", "agent-a"); ok {
		t.Fatal("second consume must be empty (one-time)")
	}
}

func TestVoiceBindingStoreExpiry(t *testing.T) {
	s := newVoiceBindingStore()
	s.Set("room-e", "agent-a", voiceBinding{Token: "emt-e"})
	// Force expiry by rewinding the entry's deadline.
	s.mu.Lock()
	e := s.entries[voiceBindingKey{"room-e", "agent-a"}]
	e.expiresAt = time.Now().Add(-time.Second)
	s.entries[voiceBindingKey{"room-e", "agent-a"}] = e
	s.mu.Unlock()
	if _, ok := s.Consume("room-e", "agent-a"); ok {
		t.Fatal("expired binding must not be consumed")
	}
}

func TestVoiceBindingStoreUnknown(t *testing.T) {
	s := newVoiceBindingStore()
	if _, ok := s.Consume("nope", "agent-a"); ok {
		t.Fatal("unknown room must not consume")
	}
}

// TestVoiceBindingStoreCrossWorkerRejected is the security regression guard for
// issue #821: a binding minted for one agent must not be consumable by another,
// and the rejected attempt must NOT consume it (the rightful worker still gets
// it later).
func TestVoiceBindingStoreCrossWorkerRejected(t *testing.T) {
	s := newVoiceBindingStore()
	s.Set("room-x", "agent-a", voiceBinding{ProjectID: "p1", Token: "emt-x"})

	if _, ok := s.Consume("room-x", "agent-b"); ok {
		t.Fatal("agent-b must not consume agent-a's binding")
	}
	b, ok := s.Consume("room-x", "agent-a")
	if !ok || b.Token != "emt-x" {
		t.Fatalf("binding not preserved for the rightful agent: %+v ok=%v", b, ok)
	}
}

// TestVoiceBindingStoreSetDoesNotOverwriteOtherAgent guards against a silent
// cross-agent overwrite: minting a binding for a second agent on the same room
// must not clobber the first agent's unexpired binding. Each agent keeps its
// own binding for the room.
func TestVoiceBindingStoreSetDoesNotOverwriteOtherAgent(t *testing.T) {
	s := newVoiceBindingStore()
	s.Set("room-y", "agent-a", voiceBinding{Token: "emt-a"})
	s.Set("room-y", "agent-b", voiceBinding{Token: "emt-b"})

	b, ok := s.Consume("room-y", "agent-a")
	if !ok || b.Token != "emt-a" {
		t.Fatalf("agent-a binding clobbered by agent-b mint: %+v ok=%v", b, ok)
	}
	b2, ok2 := s.Consume("room-y", "agent-b")
	if !ok2 || b2.Token != "emt-b" {
		t.Fatalf("agent-b binding missing after its own mint: %+v ok=%v", b2, ok2)
	}
}

// TestVoiceBindingStoreSetSameAgentRemint verifies a legitimate re-mint by the
// same agent refreshes its binding (e.g. a renewed session minting the room
// again), rather than leaving a stale or duplicate entry.
func TestVoiceBindingStoreSetSameAgentRemint(t *testing.T) {
	s := newVoiceBindingStore()
	s.Set("room-z", "agent-a", voiceBinding{Token: "emt-1"})
	s.Set("room-z", "agent-a", voiceBinding{Token: "emt-2"})

	b, ok := s.Consume("room-z", "agent-a")
	if !ok || b.Token != "emt-2" {
		t.Fatalf("same-agent re-mint did not refresh binding: %+v ok=%v", b, ok)
	}
}

// TestVoiceBindingStoreConcurrentSingleWinner asserts the consume is atomic: a
// concurrent race for the same binding yields exactly one winner.
func TestVoiceBindingStoreConcurrentSingleWinner(t *testing.T) {
	s := newVoiceBindingStore()
	s.Set("room-c", "agent-a", voiceBinding{Token: "emt-c"})

	const n = 64
	wins := make(chan bool, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, ok := s.Consume("room-c", "agent-a")
			wins <- ok
		})
	}
	wg.Wait()
	close(wins)

	total := 0
	for ok := range wins {
		if ok {
			total++
		}
	}
	if total != 1 {
		t.Fatalf("concurrent consume winners = %d, want exactly 1", total)
	}
}

// --- voiceBindingHandler ---

func newBindingEcho(cfg Config) (*Server, *echo.Echo, *workerRegistry) {
	creds := newWorkerRegistry()
	s := &Server{cfg: cfg, bindings: newVoiceBindingStore(), workerCreds: creds}
	e := echo.New()
	e.GET("/internal/voice-binding", s.voiceBindingHandler)
	return s, e, creds
}

func TestVoiceBindingHandlerAuthorized(t *testing.T) {
	s, e, creds := newBindingEcho(Config{})
	credA := creds.issue("agent-a")
	s.bindings.Set("room-1", "agent-a", voiceBinding{ProjectID: "p1", AgentDefinitionID: "a1", Token: "emt-1"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/voice-binding?room=room-1", nil)
	req.Header.Set("X-Worker-Key", credA)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got voiceBinding
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "p1" || got.Token != "emt-1" || got.AgentDefinitionID != "a1" {
		t.Fatalf("binding = %+v", got)
	}
	// One-time consumption: a second fetch returns 404.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/internal/voice-binding?room=room-1", nil)
	req2.Header.Set("X-Worker-Key", credA)
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("second fetch status = %d, want 404", rec2.Code)
	}
}

func TestVoiceBindingHandlerUnauthorized(t *testing.T) {
	_, e, creds := newBindingEcho(Config{})
	creds.issue("agent-a")
	for name, key := range map[string]string{"missing": "", "forged": "nope"} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/internal/voice-binding?room=room-1", nil)
			if key != "" {
				req.Header.Set("X-Worker-Key", key)
			}
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestVoiceBindingHandlerUnknownRoom(t *testing.T) {
	_, e, creds := newBindingEcho(Config{})
	credA := creds.issue("agent-a")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/voice-binding?room=ghost", nil)
	req.Header.Set("X-Worker-Key", credA)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestVoiceBindingHandlerCrossWorkerRejected guards the endpoint against the
// issue #821 theft: a worker authenticated as agent-b must not be handed
// agent-a's binding for a room, and the rejection must leave the binding in
// place for the rightful worker.
func TestVoiceBindingHandlerCrossWorkerRejected(t *testing.T) {
	s, e, creds := newBindingEcho(Config{})
	credA := creds.issue("agent-a")
	credB := creds.issue("agent-b")
	s.bindings.Set("room-1", "agent-a", voiceBinding{ProjectID: "p1", Token: "emt-1"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/voice-binding?room=room-1", nil)
	req.Header.Set("X-Worker-Key", credB)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-worker fetch status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/internal/voice-binding?room=room-1", nil)
	req2.Header.Set("X-Worker-Key", credA)
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("rightful worker fetch status = %d, want 200; body=%s", rec2.Code, rec2.Body.String())
	}
}

// --- mintToken session-aware binding ---

func newBindingTokenEcho(f MemoryBackend, cfg Config) (*Server, *echo.Echo) {
	s := &Server{cfg: cfg, memory: f, bindings: newVoiceBindingStore()}
	e := echo.New()
	e.POST("/api/token", s.mintToken)
	return s, e
}

func voiceAgentBackend() *fakeMemory {
	return &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "memory", Enabled: true}},
		defs:   map[string]*AgentDefinition{"a1": {ID: "a1", Name: "memory", Config: map[string]any{"language": "pl"}}},
	}
}

func TestMintTokenSessionModeStoresBinding(t *testing.T) {
	f := voiceAgentBackend()
	s, e := newBindingTokenEcho(f, tokenTestConfig())

	req := httptest.NewRequest(http.MethodPost, "/api/token",
		strings.NewReader(`{"identity":"u1","agent":"memory"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{
		Token: "sess-token", ProjectID: "proj-1", OrgID: "org-1",
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		ParticipantToken string `json:"participant_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	room := decodeRoomGrant(t, got.ParticipantToken, "lksecret")

	b, ok := s.bindings.Consume(room, "memory")
	if !ok {
		t.Fatal("binding not stored for room " + room)
	}
	if b.ProjectID != "proj-1" || b.OrgID != "org-1" {
		t.Fatalf("binding project/org = %q/%q, want proj-1/org-1", b.ProjectID, b.OrgID)
	}
	if b.AgentDefinitionID != "a1" || b.Language != "pl" {
		t.Fatalf("binding agent/lang = %q/%q, want a1/pl", b.AgentDefinitionID, b.Language)
	}
	if b.Token == "" || b.Token == "sess-token" {
		t.Fatalf("binding token = %q, want a freshly minted project token", b.Token)
	}
	// Security: the minted token must never appear in the client's join JWT.
	if strings.Contains(got.ParticipantToken, b.Token) {
		t.Fatal("minted token leaked into the client join JWT")
	}
}

// TestMintTokenWithoutSessionRejected guards that voice now requires a
// session: the old key/device-mode static-token fallback is gone, so a
// session-less mint must fail closed rather than hand out a binding.
func TestMintTokenWithoutSessionRejected(t *testing.T) {
	f := voiceAgentBackend()
	cfg := tokenTestConfig()
	cfg.MemoryProjectID = "static-proj"
	s, e := newBindingTokenEcho(f, cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/token",
		strings.NewReader(`{"identity":"u1","agent":"memory"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session-less mint status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if len(s.bindings.entries) != 0 {
		t.Fatal("session-less mint must not store a binding")
	}
}

// TestMintTokenDisabledAgentRejected asserts a disabled agent definition is
// refused before a binding or worker is created, and any warm worker for it is
// stopped (the removed background reconcile applied the same gate).
func TestMintTokenDisabledAgentRejected(t *testing.T) {
	f := voiceAgentBackend()
	f.agents[0].Enabled = false
	s, e := newBindingTokenEcho(f, tokenTestConfig())

	bin, err := exec.LookPath("sleep")
	if err == nil {
		sup := NewSupervisor(bin, []string{"60"}, "", 0, 0, nil, "u")
		sup.EnsureWorker("memory")
		s.supervisor = sup
		t.Cleanup(func() { sup.stopAll() })
	}

	req := httptest.NewRequest(http.MethodPost, "/api/token",
		strings.NewReader(`{"identity":"u1","agent":"memory"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{Token: "sess", ProjectID: "proj-1"}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled-agent mint status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if len(s.bindings.entries) != 0 {
		t.Fatal("disabled-agent mint must not store a binding")
	}
	if s.supervisor != nil {
		s.supervisor.mu.Lock()
		_, running := s.supervisor.workers["memory"]
		s.supervisor.mu.Unlock()
		if running {
			t.Fatal("existing worker for a disabled agent must be stopped")
		}
	}
}

func TestMintTokenSessionModeAgentNotFound(t *testing.T) {
	f := voiceAgentBackend() // only "memory" exists
	s, e := newBindingTokenEcho(f, tokenTestConfig())

	req := httptest.NewRequest(http.MethodPost, "/api/token",
		strings.NewReader(`{"identity":"u1","agent":"ghost"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{Token: "sess", ProjectID: "proj-1"}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// roomAllowed already rejects non-matching room prefixes with 403, but for an
	// explicit room the agent check must still fail closed.
	if rec.Code == http.StatusOK {
		t.Fatal("agent not in project must not mint a token")
	}
	_ = s
}

// --- per-project agent resolution ---

// projectAwareFake varies ListAgentDefinitions by the session's project.
type projectAwareFake struct {
	*fakeMemory
	byProject map[string][]AgentDefinitionSummary
}

func (f *projectAwareFake) ListAgentDefinitions(ctx context.Context) ([]AgentDefinitionSummary, error) {
	if sc, ok := sessionContextFrom(ctx); ok {
		if a, ok := f.byProject[sc.ProjectID]; ok {
			return a, nil
		}
	}
	return f.fakeMemory.ListAgentDefinitions(ctx)
}

func TestResolveVoiceAgentPerProject(t *testing.T) {
	f := &projectAwareFake{
		fakeMemory: &fakeMemory{defs: map[string]*AgentDefinition{
			"a1": {ID: "a1", Name: "memory", Config: map[string]any{"language": "en"}},
			"b2": {ID: "b2", Name: "memory", Config: map[string]any{"language": "pl"}},
		}},
		byProject: map[string][]AgentDefinitionSummary{
			"proj-a": {{ID: "a1", Name: "memory"}},
			"proj-b": {{ID: "b2", Name: "memory"}},
		},
	}
	s := &Server{memory: f, cfg: Config{}}

	idA, langA, _, okA := s.resolveVoiceAgent(withSessionContext(context.Background(), &sessionContext{ProjectID: "proj-a"}), "memory")
	if !okA || idA != "a1" || langA != "en" {
		t.Fatalf("proj-a: id=%q lang=%q ok=%v", idA, langA, okA)
	}
	idB, langB, _, okB := s.resolveVoiceAgent(withSessionContext(context.Background(), &sessionContext{ProjectID: "proj-b"}), "memory")
	if !okB || idB != "b2" || langB != "pl" {
		t.Fatalf("proj-b: id=%q lang=%q ok=%v", idB, langB, okB)
	}
}
