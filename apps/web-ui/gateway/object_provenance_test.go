package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// agentIdentity is a resolved agent actor for the timeline tests.
var agentIdentity = []objectActorOption{{Type: "agent", ID: "a1", Label: "researcher"}}

// TestObjectProvenanceOfAgent pins the agent line: the resolved definition name
// (not the id), a link to the definition page, the credential the write used,
// and a short run handle whose tooltip carries the full run and root run ids.
func TestObjectProvenanceOfAgent(t *testing.T) {
	run := "0193f2a1-0000-4000-8000-000000000001"
	root := "0193f2a1-0000-4000-8000-0000000000aa"
	v := GraphObject{
		Version:        3,
		ActorType:      "agent",
		ActorID:        "a1",
		CredentialKind: "mcp_link",
		RunID:          run,
		RootRunID:      root,
		CreatedAt:      "2026-01-02T09:30:00Z",
	}
	p := objectProvenanceOf(v, agentIdentity)
	if p.Actor != "researcher" || p.ActorKind != "agent" {
		t.Errorf("actor = %q/%q, want researcher/agent", p.Actor, p.ActorKind)
	}
	if p.ActorHref != "/agents/a1" {
		t.Errorf("actor href = %q, want /agents/a1", p.ActorHref)
	}
	if !strings.Contains(p.ActorTitle, "a1") {
		t.Errorf("actor title must carry the recorded id, got %q", p.ActorTitle)
	}
	if p.How != "agent MCP link" {
		t.Errorf("how = %q, want agent MCP link", p.How)
	}
	if p.Run != "run 0193f2a1" {
		t.Errorf("run = %q, want run 0193f2a1", p.Run)
	}
	if !strings.Contains(p.RunTitle, run) || !strings.Contains(p.RunTitle, root) {
		t.Errorf("run title must carry both run ids, got %q", p.RunTitle)
	}
	if p.When == "" || !strings.Contains(p.WhenTitle, "2026-01-02T09:30:00Z") {
		t.Errorf("when = %q / %q, want an absolute stamp titled by the raw value", p.When, p.WhenTitle)
	}
}

// TestObjectProvenanceOfUnresolvedActors pins the honest fallbacks: a writer the
// project no longer resolves still renders (type + short id, never a guessed
// name), and background work names itself rather than reading as "Any actor".
func TestObjectProvenanceOfUnresolvedActors(t *testing.T) {
	ghost := objectProvenanceOf(GraphObject{ActorType: "agent", ActorID: "ghost-agent-id", CredentialKind: "mcp_link"}, nil)
	if !strings.HasPrefix(ghost.Actor, "Agent · ") {
		t.Errorf("unresolved agent = %q, want an Agent · <short id> label", ghost.Actor)
	}
	if ghost.ActorHref != "/agents/ghost-agent-id" {
		t.Errorf("unresolved agent href = %q, want the definition path", ghost.ActorHref)
	}

	member := objectProvenanceOf(GraphObject{ActorType: "user", ActorID: "u1", CredentialKind: "session"},
		[]objectActorOption{{Type: "user", ID: "u1", Label: "Ada Lovelace"}})
	if member.Actor != "Ada Lovelace" || member.How != "web session" || member.ActorHref != "" {
		t.Errorf("member = %q / %q / %q, want Ada Lovelace / web session / no link", member.Actor, member.How, member.ActorHref)
	}

	system := objectProvenanceOf(GraphObject{ActorType: "system", CredentialKind: "none"}, nil)
	if system.Actor != "System" || system.How != "" {
		t.Errorf("system = %q / %q, want System and no credential line", system.Actor, system.How)
	}
	if !strings.Contains(system.ActorTitle, "background") {
		t.Errorf("system title = %q, want it to name background work", system.ActorTitle)
	}
}

// TestObjectProvenanceOfTypeWithoutID pins the empty-id case: a version that
// records an actor type but no id still names a writer by its type, and never
// borrows the actor filter's "Any actor" placeholder.
func TestObjectProvenanceOfTypeWithoutID(t *testing.T) {
	for _, actorType := range []string{"agent", "user"} {
		p := objectProvenanceOf(GraphObject{ActorType: actorType, CredentialKind: "api_key"}, nil)
		if p.Actor == "Any actor" || p.Actor == "" {
			t.Errorf("%s without id = %q, want the type name", actorType, p.Actor)
		}
		if p.Actor != actorTypeLabel(actorType) {
			t.Errorf("%s without id = %q, want %q", actorType, p.Actor, actorTypeLabel(actorType))
		}
		if p.ActorHref != "" {
			t.Errorf("%s without id linked to %q, want no link", actorType, p.ActorHref)
		}
		if p.ActorKind != actorType {
			t.Errorf("%s without id kind = %q, want %q", actorType, p.ActorKind, actorType)
		}
	}
}

// TestObjectHistorySubpageNamesWriter pins the second surface: the History
// subpage renders the same provenance line as the preview drawer, so the full
// timeline is not the weaker one.
func TestObjectHistorySubpageNamesWriter(t *testing.T) {
	data := objectDetailShellData{
		Obj:    &GraphObject{ID: "o1", Type: "note", Key: "k"},
		Active: "history",
	}
	versions := []GraphObject{{
		ID: "v2", Version: 2, Type: "note", Key: "k", Status: "ready",
		ActorType: "agent", ActorID: "a1", CredentialKind: "mcp_link",
		CreatedAt: "2026-01-02T09:30:00Z",
	}}
	html := renderHTML(t, objectHistoryPage(data, versions, agentIdentity, nil))
	for _, want := range []string{
		`data-testid="object-history-page"`,
		`data-testid="object-preview-history-provenance"`,
		"researcher",
		`href="/agents/a1"`,
		"via agent MCP link",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("history subpage missing %q:\n%s", want, html)
		}
	}
}

// TestObjectProvenanceOfUnstampedVersion pins the empty case: a version written
// before the write stamp existed renders no provenance line at all rather than
// claiming a writer.
func TestObjectProvenanceOfUnstampedVersion(t *testing.T) {
	p := objectProvenanceOf(GraphObject{Version: 1, CreatedAt: "2026-01-01T00:00:00Z"}, nil)
	if p.Actor != "" || p.ActorKind != "" || p.How != "" || p.Run != "" {
		t.Errorf("unstamped version invented provenance: %+v", p)
	}
	if p.When == "" {
		t.Error("unstamped version still reports when it was created")
	}
}

// TestObjectVersionWhen pins the timestamp format: absolute local time in the
// row, with the relative age and the raw server value in the tooltip.
func TestObjectVersionWhen(t *testing.T) {
	text, title := objectVersionWhen("2026-01-02T09:30:00Z")
	want := time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC).Local().Format("2 Jan 2006, 15:04")
	if text != want {
		t.Errorf("when = %q, want %q", text, want)
	}
	if !strings.Contains(title, "2026-01-02T09:30:00Z") {
		t.Errorf("when title = %q, want the raw stamp", title)
	}
	// A stamp older than a week does not repeat its own date in the tooltip.
	if !strings.HasPrefix(title, "Recorded ") {
		t.Errorf("stale stamp title = %q, want the raw stamp without a repeated date", title)
	}
	// A recent stamp keeps the relative age alongside the raw value.
	recent := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	if _, title := objectVersionWhen(recent); !strings.Contains(title, "h ago") {
		t.Errorf("recent stamp title = %q, want the relative age", title)
	}
	if text, title := objectVersionWhen(""); text != "" || title != "" {
		t.Errorf("empty stamp = %q / %q, want empty", text, title)
	}
	// An unparseable value passes through rather than rendering blank.
	if text, _ := objectVersionWhen("not-a-time"); text != "not-a-time" {
		t.Errorf("unparseable stamp = %q, want it passed through", text)
	}
}

// TestShortRunID pins the run handle: a UUID reads as its first dash group so it
// is scannable and copyable, and any other id keeps the generic shortening.
func TestShortRunID(t *testing.T) {
	if got := shortRunID("0193f2a1-0000-4000-8000-000000000001"); got != "0193f2a1" {
		t.Errorf("shortRunID(uuid) = %q, want 0193f2a1", got)
	}
	if got := shortRunID("shortrun"); got != "shortrun" {
		t.Errorf("shortRunID(short) = %q, want it unchanged", got)
	}
}

// TestObjectPreviewHistoryProvenanceRow pins the rendered timeline row: the
// writer, the credential and the run are visible without expanding the entry,
// an agent writer links to its definition page, and a version with no stamp
// renders no provenance row.
func TestObjectPreviewHistoryProvenanceRow(t *testing.T) {
	run := "0193f2a1-0000-4000-8000-000000000001"
	versions := []GraphObject{
		{
			ID: "2", Version: 2, Type: "note", Key: "k", Status: "ready",
			ActorType: "agent", ActorID: "a1", CredentialKind: "mcp_link", RunID: run,
			CreatedAt: "2026-01-02T09:30:00Z",
		},
	}
	html := renderHTML(t, objectPreviewHistory(versions, agentIdentity))
	for _, want := range []string{
		`data-testid="object-preview-history-provenance"`,
		`data-testid="object-preview-history-when"`,
		"researcher",
		`href="/agents/a1"`,
		"via agent MCP link",
		"run 0193f2a1",
		"2026-01-02T09:30:00Z",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("provenance row missing %q:\n%s", want, html)
		}
	}
	// The old relative-only time must not be the only timestamp offered.
	if !strings.Contains(html, `title="`) {
		t.Error("provenance row should carry an audit tooltip")
	}

	bare := renderHTML(t, objectPreviewHistory([]GraphObject{{ID: "1", Version: 1, Type: "note", Key: "k"}}, nil))
	if strings.Contains(bare, `data-testid="object-preview-history-provenance"`) {
		t.Errorf("unstamped version must not render a provenance row:\n%s", bare)
	}
}

// TestObjectActorIdentitiesFor pins the cost rule: a timeline names an agent, so
// the members list is never fetched; the agent definition name resolves through.
func TestObjectActorIdentitiesFor(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "researcher"}},
	}
	s := &Server{cfg: Config{}, memory: f}
	versions := []GraphObject{{Version: 1, ActorType: "agent", ActorID: "a1"}}
	identities := s.objectActorIdentitiesFor(context.Background(), versions)
	if f.listMembersCalls != 0 {
		t.Errorf("agent-only timeline fetched members %d times, want 0", f.listMembersCalls)
	}
	if len(identities) != 1 || identities[0].Label != "researcher" {
		t.Fatalf("identities = %+v, want the resolved agent", identities)
	}

	// A timeline with no stamped actor costs no identity fetch at all.
	empty := &fakeMemory{agents: []AgentDefinitionSummary{{ID: "a1", Name: "researcher"}}}
	emptySrv := &Server{cfg: Config{}, memory: empty}
	emptySrv.objectActorIdentitiesFor(context.Background(), []GraphObject{{Version: 1}})
	if empty.listMembersCalls != 0 || len(empty.catalogCalls) != 0 {
		t.Errorf("unstamped timeline fetched identities: members=%d catalog=%v", empty.listMembersCalls, empty.catalogCalls)
	}
}

// TestObjectHistoryPartialNamesWriter pins the handler path end to end: the
// partial resolves the actor id to the agent's name and renders who/how/when.
func TestObjectHistoryPartialNamesWriter(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "researcher"}},
		objectHistory: []GraphObject{{
			ID: "22222222-2222-2222-2222-222222222222", Version: 2, Type: "BoardTask", Key: "k",
			Status: "ready", ActorType: "agent", ActorID: "a1", CredentialKind: "mcp_link",
			CreatedAt: "2026-01-02T09:30:00Z",
		}},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/objects/:id/history/partial", s.uiObjectHistoryPartial)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/w1/history/partial", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"researcher", `href="/agents/a1"`, "via agent MCP link"} {
		if !strings.Contains(body, want) {
			t.Errorf("history partial missing %q:\n%s", want, body)
		}
	}
}

// TestGetObjectHistoryWriteStampDecodes pins the client decode of the write
// stamp: the history read path returns actor, credential and run per version, and
// the gateway must keep them rather than dropping them on the floor.
func TestGetObjectHistoryWriteStampDecodes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"versions":[{"id":"11111111-1111-1111-1111-111111111111","version":2,"type":"note",
			"actor_type":"agent","actor_id":"a1","credential_kind":"mcp_link",
			"run_id":"0193f2a1-0000-4000-8000-000000000001","root_run_id":"0193f2a1-0000-4000-8000-0000000000aa",
			"data_class":"synthesized","owner_source_id":null,"created_at":"2026-01-02T09:30:00Z"}]}`)
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	versions, err := m.GetObjectHistory(context.Background(), "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("GetObjectHistory: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(versions))
	}
	v := versions[0]
	if v.ActorType != "agent" || v.ActorID != "a1" {
		t.Errorf("actor = %q/%q, want agent/a1", v.ActorType, v.ActorID)
	}
	if v.CredentialKind != "mcp_link" {
		t.Errorf("credential_kind = %q, want mcp_link", v.CredentialKind)
	}
	if v.RunID == "" || v.RootRunID == "" {
		t.Errorf("run stamp dropped: run=%q root=%q", v.RunID, v.RootRunID)
	}
	if v.DataClass != "synthesized" {
		t.Errorf("data_class = %q, want synthesized", v.DataClass)
	}
}
