package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- voiceBindingStore ---

func TestVoiceBindingStoreSetConsume(t *testing.T) {
	s := newVoiceBindingStore()
	s.Set("room-1", voiceBinding{ProjectID: "p1", Token: "emt-x"})
	b, ok := s.Consume("room-1")
	if !ok || b.ProjectID != "p1" || b.Token != "emt-x" {
		t.Fatalf("consume = %+v ok=%v", b, ok)
	}
	if _, ok := s.Consume("room-1"); ok {
		t.Fatal("second consume must be empty (one-time)")
	}
}

func TestVoiceBindingStoreExpiry(t *testing.T) {
	s := newVoiceBindingStore()
	s.Set("room-e", voiceBinding{Token: "emt-e"})
	// Force expiry by rewinding the entry's deadline.
	s.mu.Lock()
	e := s.entries["room-e"]
	e.expiresAt = time.Now().Add(-time.Second)
	s.entries["room-e"] = e
	s.mu.Unlock()
	if _, ok := s.Consume("room-e"); ok {
		t.Fatal("expired binding must not be consumed")
	}
}

func TestVoiceBindingStoreUnknown(t *testing.T) {
	s := newVoiceBindingStore()
	if _, ok := s.Consume("nope"); ok {
		t.Fatal("unknown room must not consume")
	}
}

// --- voiceBindingHandler ---

func newBindingEcho(cfg Config) (*Server, *echo.Echo) {
	s := &Server{cfg: cfg, bindings: newVoiceBindingStore()}
	e := echo.New()
	e.GET("/internal/voice-binding", s.voiceBindingHandler)
	return s, e
}

func TestVoiceBindingHandlerAuthorized(t *testing.T) {
	s, e := newBindingEcho(Config{WorkerInternalKey: "wk-1"})
	s.bindings.Set("room-1", voiceBinding{ProjectID: "p1", AgentDefinitionID: "a1", Token: "emt-1"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/voice-binding?room=room-1", nil)
	req.Header.Set("X-Worker-Key", "wk-1")
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
	req2.Header.Set("X-Worker-Key", "wk-1")
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("second fetch status = %d, want 404", rec2.Code)
	}
}

func TestVoiceBindingHandlerUnauthorized(t *testing.T) {
	for name, key := range map[string]string{"missing": "", "wrong": "nope"} {
		t.Run(name, func(t *testing.T) {
			_, e := newBindingEcho(Config{WorkerInternalKey: "wk-1"})
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
	_, e := newBindingEcho(Config{WorkerInternalKey: "wk-1"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/voice-binding?room=ghost", nil)
	req.Header.Set("X-Worker-Key", "wk-1")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
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

	b, ok := s.bindings.Consume(room)
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

func TestMintTokenKeyModeStoresStaticBinding(t *testing.T) {
	f := voiceAgentBackend()
	cfg := tokenTestConfig()
	cfg.MemoryProjectID = "static-proj"
	cfg.MemoryToken = "emt-static"
	s, e := newBindingTokenEcho(f, cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/token",
		strings.NewReader(`{"identity":"u1","agent":"memory"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
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
	b, ok := s.bindings.Consume(room)
	if !ok {
		t.Fatal("binding not stored")
	}
	if b.ProjectID != "static-proj" || b.Token != "emt-static" {
		t.Fatalf("binding = %+v, want static project/token", b)
	}
	if b.AgentDefinitionID != "a1" {
		t.Fatalf("agent definition id = %q, want a1", b.AgentDefinitionID)
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

	idA, langA, okA := s.resolveVoiceAgent(withSessionContext(context.Background(), &sessionContext{ProjectID: "proj-a"}), "memory")
	if !okA || idA != "a1" || langA != "en" {
		t.Fatalf("proj-a: id=%q lang=%q ok=%v", idA, langA, okA)
	}
	idB, langB, okB := s.resolveVoiceAgent(withSessionContext(context.Background(), &sessionContext{ProjectID: "proj-b"}), "memory")
	if !okB || idB != "b2" || langB != "pl" {
		t.Fatalf("proj-b: id=%q lang=%q ok=%v", idB, langB, okB)
	}
}
