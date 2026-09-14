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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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
	m := NewMemoryClient(srv.URL, "tok", "proj")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	if err := m.DeleteSkill(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/projects/proj-1/skills/s1" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
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
	html := renderHTML(t, SkillsPage(skills, nil, "", nil, "", map[string]int{"summarize-email": 2, "recall-memory": 0}))
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
	htmlWithAssistant := renderHTML(t, SkillsPage(skills, nil, "", nil, "agent-1", nil))
	if !strings.Contains(htmlWithAssistant, "Add with assistant") {
		t.Error("assistant button missing when assistant configured")
	}

	htmlEmpty := renderHTML(t, SkillsPage(nil, nil, "", nil, "", nil))
	if !strings.Contains(htmlEmpty, "No skills yet") {
		t.Error("empty state missing")
	}

	htmlErr := renderHTML(t, SkillsPage(nil, errTest, "", nil, "", nil))
	if !strings.Contains(htmlErr, "Failed to load skills") {
		t.Error("error state missing")
	}

	htmlFlash := renderHTML(t, SkillsPage(nil, nil, "Skill created.", nil, "", nil))
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
