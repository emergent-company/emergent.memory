package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- MemoryClient skill methods (bare JSON, no {success,data} envelope) ---

func TestListSkills(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"skills":[
			{"id":"s1","name":"summarize-email","description":"Condenses threads","content":"You summarize…","metadata":{"source":"github","license":"MIT","version":"1.0.0"},"hasEmbedding":true,"projectId":"proj-1","scope":"project","createdAt":"2026-08-26T10:00:00Z","updatedAt":"2026-08-26T11:00:00Z"},
			{"id":"s2","name":"recall-memory","description":"Remembers things","content":"You recall…","scope":"global","createdAt":"2026-08-26T09:00:00Z","updatedAt":"2026-08-26T09:00:00Z"}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	skills, err := m.ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/skills" {
		t.Errorf("path = %q", gotPath)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2", len(skills))
	}
	s1 := skills[0]
	if s1.ID != "s1" || s1.Name != "summarize-email" || s1.Scope != "project" || !s1.HasEmbedding {
		t.Errorf("s1 = %+v", s1)
	}
	if s1.Metadata == nil || s1.Metadata.Source != "github" || s1.Metadata.License != "MIT" || s1.Metadata.Version != "1.0.0" {
		t.Errorf("s1 metadata = %+v", s1.Metadata)
	}
	if s2 := skills[1]; s2.ID != "s2" || s2.Scope != "global" || s2.Metadata != nil {
		t.Errorf("s2 = %+v", s2)
	}
}

func TestListSkillsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"down"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.ListSkills(context.Background()); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestGetSkill(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"s1","name":"summarize-email","description":"Condenses","content":"You summarize…","scope":"project","updatedAt":"2026-08-26T11:00:00Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	sk, err := m.GetSkill(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	// Get is global, not project-scoped.
	if gotPath != "/api/skills/s1" {
		t.Errorf("path = %q", gotPath)
	}
	if sk.Name != "summarize-email" || sk.Scope != "project" {
		t.Errorf("skill = %+v", sk)
	}
}

func TestCreateSkill(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"new-s1","name":"summarize-email","description":"Condenses","content":"You summarize…","scope":"project"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	sk, err := m.CreateSkill(context.Background(), "summarize-email", "Condenses", "You summarize…")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/skills" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["name"] != "summarize-email" || gotBody["description"] != "Condenses" || gotBody["content"] != "You summarize…" {
		t.Errorf("body = %v", gotBody)
	}
	if _, ok := gotBody["metadata"]; ok {
		t.Errorf("create body should not send metadata: %v", gotBody)
	}
	if sk.ID != "new-s1" || sk.Name != "summarize-email" {
		t.Errorf("skill = %+v", sk)
	}
}

func TestUpdateSkill(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"s1","name":"summarize-email","description":"New desc","content":"New content","scope":"project"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	sk, err := m.UpdateSkill(context.Background(), "s1", "New desc", "New content")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/projects/proj-1/skills/s1" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["description"] != "New desc" || gotBody["content"] != "New content" {
		t.Errorf("body = %v", gotBody)
	}
	// name and scope are NOT updatable — they must never be sent.
	if _, ok := gotBody["name"]; ok {
		t.Errorf("update body must not send name: %v", gotBody)
	}
	if _, ok := gotBody["scope"]; ok {
		t.Errorf("update body must not send scope: %v", gotBody)
	}
	if sk.Description != "New desc" || sk.Content != "New content" {
		t.Errorf("skill = %+v", sk)
	}
}

func TestDeleteSkill(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if err := m.DeleteSkill(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/projects/proj-1/skills/s1" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
}

func TestRestoreSkill(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"s1","name":"agent.triage","description":"d","content":"c","metadata":{"lifecycle_state":"active","pinned":true,"author":"diane"},"scope":"project"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	sk, err := m.RestoreSkill(context.Background(), "s1", true)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/skills/s1/restore" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["pin"] != true {
		t.Errorf("body = %v, want pin=true", gotBody)
	}
	if sk == nil || !sk.Metadata.Pinned || sk.LifecycleState() != "active" {
		t.Errorf("skill = %+v", sk)
	}
}

func TestRestoreSkillError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"skill not found"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj-1")
	if _, err := m.RestoreSkill(context.Background(), "missing", false); err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestSkillLifecycleHelpers pins the defaults the UI relies on: a skill with no
// metadata is active, and only an explicit archived state hides it from agents.
func TestSkillLifecycleHelpers(t *testing.T) {
	if got := (Skill{}).LifecycleState(); got != "active" {
		t.Errorf("default lifecycle = %q, want active", got)
	}
	if (Skill{}).Archived() {
		t.Error("a skill without metadata must not read as archived")
	}
	skill := Skill{Metadata: &SkillMetadata{LifecycleState: "archived"}}
	if !skill.Archived() {
		t.Error("archived state not detected")
	}
	if skill.IsPinned() {
		t.Error("archived skill must not read as pinned")
	}
	if got := skillLifecycleLabel("archived"); got != "Archived" {
		t.Errorf("label = %q", got)
	}
}

// --- skill proposals (gateway client) ---

func TestListSkillCaptureProposals(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"proposals":[{"id":"p1","projectId":"proj-1","agentId":"a1","agentName":"diane","kind":"create","name":"diane.triage","description":"Sorts mail","content":"You triage…","confidence":0.82,"source":"capture","status":"pending","createdAt":"2026-08-26T10:00:00Z","findings":[{"severity":"warning","code":"missing_heading","message":"start the skill with a Markdown heading"}]}],"count":1}`)
	}))
	defer srv.Close()

	got, err := NewMemoryClient(srv.URL, "proj-1").ListSkillCaptureProposals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/skills/proposals" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "status=pending" {
		t.Errorf("query = %q, want status=pending", gotQuery)
	}
	if len(got) != 1 {
		t.Fatalf("got %d proposals, want 1", len(got))
	}
	p := got[0]
	if p.ID != "p1" || p.Kind != "create" || p.Name != "diane.triage" || p.AgentName != "diane" || p.Status != "pending" {
		t.Errorf("proposal = %+v", p)
	}
	if p.Confidence != 0.82 {
		t.Errorf("confidence = %v, want 0.82", p.Confidence)
	}
	if len(p.Findings) != 1 || p.Findings[0].Code != "missing_heading" || p.Findings[0].Severity != "warning" {
		t.Errorf("findings = %+v", p.Findings)
	}
}

func TestGetSkillCaptureProposal(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"p9","kind":"patch","name":"diane.triage","status":"pending"}`)
	}))
	defer srv.Close()

	p, err := NewMemoryClient(srv.URL, "proj-1").GetSkillCaptureProposal(context.Background(), "p9")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/skills/proposals/p9" {
		t.Errorf("path = %q", gotPath)
	}
	if p.ID != "p9" || p.Kind != "patch" {
		t.Errorf("proposal = %+v", p)
	}
}

func TestApproveRejectSkillCaptureProposal(t *testing.T) {
	type call struct{ method, path, contentType string }
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.Method, r.URL.Path, r.Header.Get("Content-Type")})
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if err := m.ApproveSkillCaptureProposal(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	if err := m.RejectSkillCaptureProposal(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	want := []call{
		{http.MethodPost, "/api/projects/proj-1/skills/proposals/p1/approve", "application/json"},
		{http.MethodPost, "/api/projects/proj-1/skills/proposals/p1/reject", "application/json"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %+v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d = %+v, want %+v", i, calls[i], want[i])
		}
	}
}

// TestSkillCaptureProposalPathEscaping pins that the proposal id is escaped on
// every route, so a malformed id cannot smuggle an extra path segment.
func TestSkillCaptureProposalPathEscaping(t *testing.T) {
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.RequestURI)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	_, _ = m.GetSkillCaptureProposal(context.Background(), "p/1")
	_ = m.ApproveSkillCaptureProposal(context.Background(), "p/1")
	_ = m.RejectSkillCaptureProposal(context.Background(), "p/1")

	want := []string{
		"/api/projects/proj-1/skills/proposals/p%2F1",
		"/api/projects/proj-1/skills/proposals/p%2F1/approve",
		"/api/projects/proj-1/skills/proposals/p%2F1/reject",
	}
	if len(uris) != len(want) {
		t.Fatalf("uris = %v", uris)
	}
	for i := range want {
		if uris[i] != want[i] {
			t.Errorf("uri %d = %q, want %q", i, uris[i], want[i])
		}
	}
}

func TestSkillCaptureProposalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"down"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj-1")
	if _, err := m.ListSkillCaptureProposals(context.Background()); err == nil {
		t.Error("list: want error, got nil")
	}
	if _, err := m.GetSkillCaptureProposal(context.Background(), "p1"); err == nil {
		t.Error("get: want error, got nil")
	}
	if err := m.ApproveSkillCaptureProposal(context.Background(), "p1"); err == nil {
		t.Error("approve: want error, got nil")
	}
	if err := m.RejectSkillCaptureProposal(context.Background(), "p1"); err == nil {
		t.Error("reject: want error, got nil")
	}
}

// --- UI render ---

func TestRenderSkillsPage(t *testing.T) {
	skills := []Skill{
		{ID: "s1", Name: "summarize-email", Description: "Condenses threads", Scope: "project",
			Metadata:  &SkillMetadata{Source: "github", License: "MIT", Version: "1.0.0"},
			UpdatedAt: "2026-08-26T11:00:00Z"},
		{ID: "s2", Name: "recall-memory", Description: "Remembers things", Scope: "global",
			UpdatedAt: "2026-08-26T09:00:00Z"},
	}
	html := renderHTML(t, SkillsPage(skills, nil, nil, nil, "", nil, "", map[string]int{"summarize-email": 2, "recall-memory": 0}))
	for _, want := range []string{
		"Skills", "summarize-email", "Condenses threads", "recall-memory", "Remembers things",
		"Used by", "2 agents", "Unused",
		`href="/skills/s1"`, `href="/skills/s2"`,
		"Add skill", `href="/skills/new"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("skills page missing %q", want)
		}
	}
	for _, notWant := range []string{`action="/skills"`, `href="#new-skill"`, `name="name"`, "Create skill"} {
		if strings.Contains(html, notWant) {
			t.Errorf("skills page should not contain %q (create form moved to /skills/new)", notWant)
		}
	}
	if strings.Contains(html, "Add with assistant") {
		t.Error("assistant button rendered with no assistant configured")
	}

	// With an assistant agent configured the "Add with assistant" button shows.
	htmlWithAssistant := renderHTML(t, SkillsPage(skills, nil, nil, nil, "", nil, "agent-1", nil))
	if !strings.Contains(htmlWithAssistant, "Add with assistant") {
		t.Error("assistant button missing when assistant configured")
	}

	htmlEmpty := renderHTML(t, SkillsPage(nil, nil, nil, nil, "", nil, "", nil))
	if !strings.Contains(htmlEmpty, "No skills yet") {
		t.Error("empty state missing")
	}

	htmlErr := renderHTML(t, SkillsPage(nil, errTest, nil, nil, "", nil, "", nil))
	if !strings.Contains(htmlErr, "Failed to load skills") {
		t.Error("error state missing")
	}

	htmlFlash := renderHTML(t, SkillsPage(nil, nil, nil, nil, "Skill created.", nil, "", nil))
	if !strings.Contains(htmlFlash, "Skill created.") {
		t.Error("flash message missing")
	}
}

// TestRenderSkillNewPage covers the standalone create form page.
func TestRenderSkillNewPage(t *testing.T) {
	html := renderHTML(t, SkillNewPage(nil))
	for _, want := range []string{
		"New skill", "A name, what it does, and the instructions that make it work.",
		`action="/skills"`, `name="name"`, `name="description"`, `name="content"`,
		"Create skill", `href="/skills"`, "Skills", "Lowercase letters, numbers, and dashes",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("new-skill page missing %q", want)
		}
	}

	htmlErr := renderHTML(t, SkillNewPage(errTest))
	if !strings.Contains(htmlErr, "backend unreachable") {
		t.Error("flash error missing on new-skill page")
	}
}

// TestRenderSkillProposalsPanel pins the review inbox: the pending row's
// identifying data, its live findings, both actions, and the distinct empty and
// failed-load states.
func TestRenderSkillProposalsPanel(t *testing.T) {
	html := renderHTML(t, skillProposalsPanel([]SkillCaptureProposal{
		{
			ID: "p1", Kind: "create", Name: "diane.triage", AgentName: "diane",
			Description: "Sorts mail", Confidence: 0.82, Status: "pending",
			Findings: []SkillCaptureFinding{{Severity: "error", Code: "github_token", Message: "content looks like it contains a GitHub token"}},
		},
		{ID: "p2", Kind: "delete", Name: "old-skill", AgentID: "a2", Confidence: 0.5, Status: "pending"},
	}, nil))
	for _, want := range []string{
		"Proposed skills", "diane.triage", "Create", "diane", "82% confidence",
		"Pending", "github_token", "content looks like it contains a GitHub token",
		"Approve", "Reject",
		`action="/skills/proposals/p1/approve"`, `action="/skills/proposals/p1/reject"`,
		"old-skill", "Delete", "a2", "No findings",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("proposals panel missing %q", want)
		}
	}

	empty := renderHTML(t, skillProposalsPanel(nil, nil))
	if !strings.Contains(empty, "No skill proposals are waiting for review") {
		t.Error("empty inbox state missing")
	}

	failed := renderHTML(t, skillProposalsPanel(nil, errTest))
	if !strings.Contains(failed, "Pending proposals could not be loaded") {
		t.Error("failed-load state missing")
	}
	if strings.Contains(failed, "No skill proposals are waiting") {
		t.Error("a failed load must not render as an empty inbox")
	}
}

func TestRenderSkillDetailPage(t *testing.T) {
	sk := &Skill{
		ID:           "s1",
		Name:         "summarize-email",
		Description:  "Condenses threads",
		Content:      "You summarize email threads.\nRead every message.",
		Scope:        "org",
		HasEmbedding: true,
		Metadata:     &SkillMetadata{Source: "github", License: "MIT", Version: "1.0.0"},
		UpdatedAt:    "2026-08-26T11:00:00Z",
	}
	html := renderHTML(t, SkillDetailPage(sk, nil, "", nil))
	for _, want := range []string{
		"summarize-email", "Condenses threads",
		"You summarize email threads.", "Read every message.",
		"org", "github", "MIT", "v1.0.0", "embedded",
		`action="/skills/s1/update"`, `name="description"`, `name="content"`,
		"can't be changed", "Save changes",
		`action="/skills/s1/delete"`, "Delete skill?",
		`href="/skills"`, "Skills", "breadcrumbs text-sm",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	htmlErr := renderHTML(t, SkillDetailPage(nil, errTest, "", nil))
	if !strings.Contains(htmlErr, "Skill unavailable") {
		t.Error("error state missing")
	}

	htmlFlash := renderHTML(t, SkillDetailPage(sk, nil, "Skill updated.", nil))
	if !strings.Contains(htmlFlash, "Skill updated.") {
		t.Error("flash message missing")
	}
}

// TestScopeIntent covers the badge mapping for known and unknown scopes.
func TestScopeIntent(t *testing.T) {
	if scopeIntent("global") == scopeIntent("project") {
		t.Error("global and project must map to different intents")
	}
	if scopeIntent("org") == scopeIntent("project") {
		t.Error("org and project must map to different intents")
	}
	if scopeIntent("mystery") == scopeIntent("global") {
		t.Error("unknown scope should not map to global")
	}
	if scopeLabel("") != "project" {
		t.Errorf("empty scope should display as project, got %q", scopeLabel(""))
	}
}

func TestProvenanceLabel(t *testing.T) {
	if got := provenanceLabel(nil); got != "" {
		t.Errorf("nil metadata should be empty, got %q", got)
	}
	if got := provenanceLabel(&SkillMetadata{}); got != "" {
		t.Errorf("empty metadata should be empty, got %q", got)
	}
	got := provenanceLabel(&SkillMetadata{Source: "github", License: "MIT", Version: "1.0.0"})
	if got != "github · MIT · v1.0.0" {
		t.Errorf("provenance = %q", got)
	}
	// version prefix is normalized (v1.0.0 input → v1.0.0 output)
	got = provenanceLabel(&SkillMetadata{Version: "v2.0.0"})
	if got != "v2.0.0" {
		t.Errorf("version-only provenance = %q", got)
	}
}

// --- UI routes ---

// TestUIRouteSkills exercises the /skills page routes against the fake
// backend: list, detail, and the create/update/delete PRG redirects.
func TestUIRouteSkills(t *testing.T) {
	f := &fakeMemory{
		skills: []Skill{
			{ID: "s1", Name: "meeting-notes", Description: "Condenses threads", Content: "You summarize…", Scope: "project", UpdatedAt: "2026-08-26T11:00:00Z"},
		},
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "memory", Skills: []string{"meeting-notes"}}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/skills", s.uiSkills)
	e.GET("/skills/new", s.uiNewSkill)
	e.GET("/skills/:id", s.uiSkill)
	e.POST("/skills", s.uiCreateSkill)
	e.POST("/skills/:id/update", s.uiUpdateSkill)
	e.POST("/skills/:id/delete", s.uiDeleteSkill)

	// list page
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "meeting-notes") {
		t.Fatalf("list page: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "1 agent") {
		t.Error("usage count missing on list page (meeting-notes used by one agent)")
	}

	// new-skill page: create form, no list
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("new-skill status = %d", rec.Code)
	}
	newBody := rec.Body.String()
	for _, want := range []string{`action="/skills"`, `name="name"`, "Create skill", `href="/skills"`} {
		if !strings.Contains(newBody, want) {
			t.Errorf("new-skill page missing %q", want)
		}
	}
	if strings.Contains(newBody, "meeting-notes") {
		t.Error("new-skill page should not list skills")
	}

	// detail page: content + edit form + delete affordance
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/s1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"You summarize…", `action="/skills/s1/update"`, `action="/skills/s1/delete"`, "Save changes"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	// unknown skill id → whole-page error, not a broken render
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/nope", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Skill unavailable") {
		t.Fatalf("unknown skill: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// create redirect + forwarded fields
	form := url.Values{"name": {"triage-inbox"}, "description": {"Sorts the inbox"}, "content": {"You sort messages by priority."}}
	rec = httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/skills", strings.NewReader(form.Encode()))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, createReq)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/skills?created=1" {
		t.Errorf("create location = %q", loc)
	}
	if len(f.createdSkills) != 1 {
		t.Fatalf("created = %+v", f.createdSkills)
	}
	if c := f.createdSkills[0]; c.Name != "triage-inbox" || c.Description != "Sorts the inbox" || c.Content != "You sort messages by priority." {
		t.Errorf("created skill = %+v", c)
	}

	// create failure → ?err=1
	f2 := &fakeMemory{skillErr: errTest}
	s2 := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f2}
	e2 := echo.New()
	e2.POST("/skills", s2.uiCreateSkill)
	rec = httptest.NewRecorder()
	errReq := httptest.NewRequest(http.MethodPost, "/skills", strings.NewReader(form.Encode()))
	errReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e2.ServeHTTP(rec, errReq)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || loc != "/skills/new?err=backend+unreachable" {
		t.Fatalf("create failure: status=%d location=%q", rec.Code, loc)
	}

	// update redirect (name is not sent by the form, only description/content)
	form = url.Values{"description": {"New desc"}, "content": {"New content"}}
	rec = httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPost, "/skills/s1/update", strings.NewReader(form.Encode()))
	updateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, updateReq)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/skills/s1?updated=1" {
		t.Errorf("update location = %q", loc)
	}
	if f.updatedSkillID != "s1" {
		t.Errorf("updated id = %q, want s1", f.updatedSkillID)
	}

	// updated flash renders on the detail page
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/s1?updated=1", nil))
	if !strings.Contains(rec.Body.String(), "Skill updated.") {
		t.Error("updated flash missing on detail page")
	}

	// delete redirect
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/s1/delete", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/skills?deleted=1" {
		t.Errorf("delete location = %q", loc)
	}
	if f.deletedSkillID != "s1" {
		t.Errorf("deleted id = %q, want s1", f.deletedSkillID)
	}

	// deleted flash renders on the list page
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills?deleted=1", nil))
	if !strings.Contains(rec.Body.String(), "Skill deleted.") {
		t.Error("deleted flash missing on list page")
	}
}

// TestUIRouteSkillRestore pins the un-archive surface: an archived skill shows
// its lifecycle state and both restore actions, the POST forwards the pin flag
// and redirects with a flash, and an active skill shows no restore affordance.
func TestUIRouteSkillRestore(t *testing.T) {
	f := &fakeMemory{skills: []Skill{{
		ID: "s1", Name: "agent.triage", Description: "aged out", Content: "body", Scope: "project",
		UpdatedAt: "2026-08-26T11:00:00Z",
		Metadata:  &SkillMetadata{Source: "agent", LifecycleState: "archived", Version: "1.0.0"},
	}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/skills/:id", s.uiSkill)
	e.POST("/skills/:id/restore", s.uiRestoreSkill)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/s1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Archived", "This skill is archived", `action="/skills/s1/restore"`, `name="pin"`} {
		if !strings.Contains(body, want) {
			t.Errorf("archived detail page missing %q", want)
		}
	}

	// Restore & pin: the form flag reaches memory, then PRG back to the detail page.
	form := url.Values{"pin": {"1"}}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/skills/s1/restore", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("restore redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/skills/s1?restored=1" {
		t.Errorf("restore location = %q", loc)
	}
	if f.restoredSkillID != "s1" || !f.restoredPinned {
		t.Errorf("restore call = id %q pin %v, want s1/true", f.restoredSkillID, f.restoredPinned)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/s1?restored=1", nil))
	if !strings.Contains(rec.Body.String(), "Skill restored.") {
		t.Error("restored flash missing on the detail page")
	}

	// A plain restore sends pin=false.
	f.restoredPinned = true
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/s1/restore", nil))
	if f.restoredPinned {
		t.Error("restore without the pin flag must not pin")
	}

	// Restore failure: the detail page returns with the backend error, and the
	// skill is not reported as restored.
	bad := &fakeMemory{skillErr: errTest}
	sbad := &Server{cfg: Config{DefaultAgent: "memory"}, memory: bad}
	ebad := echo.New()
	ebad.POST("/skills/:id/restore", sbad.uiRestoreSkill)
	rec = httptest.NewRecorder()
	ebad.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/s1/restore", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("failed restore status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/skills/s1?err=backend+unreachable" {
		t.Errorf("failed restore location = %q", loc)
	}

	// An active skill has no un-archive affordance.
	active := &fakeMemory{skills: []Skill{{ID: "s2", Name: "manual", Description: "d", Content: "c", Scope: "project"}}}
	sactive := &Server{cfg: Config{DefaultAgent: "memory"}, memory: active}
	eactive := echo.New()
	eactive.GET("/skills/:id", sactive.uiSkill)
	rec = httptest.NewRecorder()
	eactive.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/s2", nil))
	if strings.Contains(rec.Body.String(), "This skill is archived") {
		t.Error("an active skill must not show the restore section")
	}
}

// TestUIRouteSkillProposalReview exercises the review actions end to end against
// the fake backend: the panel renders, each action posts the proposal id, and
// the PRG flash confirms the outcome.
func TestUIRouteSkillProposalReview(t *testing.T) {
	f := &fakeMemory{
		skills:         []Skill{{ID: "s1", Name: "meeting-notes", Scope: "project"}},
		skillProposals: []SkillCaptureProposal{{ID: "p1", Kind: "create", Name: "diane.triage", AgentName: "diane", Status: "pending"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/skills", s.uiSkills)
	e.POST("/skills/proposals/:id/approve", s.uiApproveSkillCaptureProposal)
	e.POST("/skills/proposals/:id/reject", s.uiRejectSkillCaptureProposal)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "diane.triage") {
		t.Fatalf("list page: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `action="/skills/proposals/p1/approve"`) {
		t.Error("approve action missing on the list page")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/proposals/p1/approve", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/skills?proposalApproved=1" {
		t.Fatalf("approve: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.approvedProposalID != "p1" {
		t.Errorf("approved id = %q, want p1", f.approvedProposalID)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills?proposalApproved=1", nil))
	if !strings.Contains(rec.Body.String(), "Proposal approved and applied.") {
		t.Error("approved flash missing on the list page")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/proposals/p1/reject", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/skills?proposalRejected=1" {
		t.Fatalf("reject: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.rejectedProposalID != "p1" {
		t.Errorf("rejected id = %q, want p1", f.rejectedProposalID)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills?proposalRejected=1", nil))
	if !strings.Contains(rec.Body.String(), "Proposal rejected.") {
		t.Error("rejected flash missing on the list page")
	}
}

// TestSkillProposalApproveFailureStaysPending pins the failure contract: a
// failed approve (the security scan can still block it server-side) redirects
// with an error and the re-rendered panel still shows the proposal as pending —
// never as approved.
func TestSkillProposalApproveFailureStaysPending(t *testing.T) {
	f := &fakeMemory{
		skillProposals:     []SkillCaptureProposal{{ID: "p1", Kind: "create", Name: "diane.triage", Status: "pending"}},
		approveProposalErr: errTest,
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/skills", s.uiSkills)
	e.POST("/skills/proposals/:id/approve", s.uiApproveSkillCaptureProposal)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/proposals/p1/approve", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/skills?err=backend+unreachable" {
		t.Fatalf("location = %q, want the error redirect", loc)
	}
	if strings.Contains(loc, "proposalApproved") {
		t.Error("a failed approve must not redirect to the approved flash")
	}
	if f.approvedProposalID != "" {
		t.Errorf("approved id = %q, want none", f.approvedProposalID)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, loc, nil))
	body := rec.Body.String()
	if !strings.Contains(body, "diane.triage") || !strings.Contains(body, "Pending") {
		t.Error("failed approve: proposal must still render as pending")
	}
	if !strings.Contains(body, "backend unreachable") {
		t.Error("failed approve: the error must be surfaced")
	}
	if strings.Contains(body, "Proposal approved") {
		t.Error("failed approve: the page must not claim the proposal was approved")
	}
}
