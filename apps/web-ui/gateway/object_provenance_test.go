package main

import (
	"context"
	"fmt"
	"io"
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
	p := objectProvenanceOf(v, agentIdentity, nil)
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
	ghost := objectProvenanceOf(GraphObject{ActorType: "agent", ActorID: "ghost-agent-id", CredentialKind: "mcp_link"}, nil, nil)
	if !strings.HasPrefix(ghost.Actor, "Agent · ") {
		t.Errorf("unresolved agent = %q, want an Agent · <short id> label", ghost.Actor)
	}
	if ghost.ActorHref != "/agents/ghost-agent-id" {
		t.Errorf("unresolved agent href = %q, want the definition path", ghost.ActorHref)
	}

	member := objectProvenanceOf(GraphObject{ActorType: "user", ActorID: "u1", CredentialKind: "session"},
		[]objectActorOption{{Type: "user", ID: "u1", Label: "Ada Lovelace"}}, nil)
	if member.Actor != "Ada Lovelace" || member.How != "web session" || member.ActorHref != "" {
		t.Errorf("member = %q / %q / %q, want Ada Lovelace / web session / no link", member.Actor, member.How, member.ActorHref)
	}

	system := objectProvenanceOf(GraphObject{ActorType: "system", CredentialKind: "none"}, nil, nil)
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
		p := objectProvenanceOf(GraphObject{ActorType: actorType, CredentialKind: "api_key"}, nil, nil)
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
	html := renderHTML(t, objectHistoryPage(data, versions, agentIdentity, nil, nil))
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

// TestObjectProvenanceOfResolvedUserAvatar pins the identity mark: a user actor
// that resolved to a person carries initials (and a photo when the page can
// serve one), while the unresolved "<Type> · <short id>" placeholder and any
// non-user actor carry neither — the mark decorates a person, not a stub.
func TestObjectProvenanceOfResolvedUserAvatar(t *testing.T) {
	known := []objectActorOption{
		{Type: "user", ID: "u1", Label: "Maciek Dev", AvatarURL: "/api/user/avatar?v=k"},
		{Type: "agent", ID: "a1", Label: "researcher"},
	}

	p := objectProvenanceOf(GraphObject{ActorType: "user", ActorID: "u1", CredentialKind: "session"}, known, nil)
	if p.ActorInitials != "MD" {
		t.Errorf("initials = %q, want MD", p.ActorInitials)
	}
	if p.ActorAvatar != "/api/user/avatar?v=k" {
		t.Errorf("avatar = %q, want the served photo", p.ActorAvatar)
	}

	// A writer missing from the roster renders the honest fallback with no mark.
	ghost := objectProvenanceOf(GraphObject{ActorType: "user", ActorID: "u-gone", CredentialKind: "session"}, known, nil)
	if ghost.ActorInitials != "" || ghost.ActorAvatar != "" {
		t.Errorf("unresolved user actor got an identity mark: %+v", ghost)
	}
	if !strings.HasPrefix(ghost.Actor, "User · ") {
		t.Errorf("unresolved user actor = %q, want the type/id fallback", ghost.Actor)
	}

	// An agent has a name and a link, never a face.
	agent := objectProvenanceOf(GraphObject{ActorType: "agent", ActorID: "a1"}, known, nil)
	if agent.ActorInitials != "" || agent.ActorAvatar != "" {
		t.Errorf("agent actor got a user avatar: %+v", agent)
	}
}

// TestObjectActorIdentitiesPrefersOrgMembers pins which roster names a writer.
// Access to a project's objects is granted by ORGANIZATION membership (the
// server's write guard), so an org member who holds no project membership can
// still write — and must still be named. Without a session org (an API-key
// path) the project roster remains the only list, and a failed org read falls
// back to it instead of losing the user options.
func TestObjectActorIdentitiesPrefersOrgMembers(t *testing.T) {
	orgMembers := []OrgMemberDto{{ID: "u1", Email: "maciek@example.com", DisplayName: ptr("Maciek Dev")}}
	members := []ProjectMemberDto{{ID: "u2", DisplayName: "Alice"}}

	t.Run("session org", func(t *testing.T) {
		f := &fakeMemory{orgMembers: orgMembers, members: members}
		s := &Server{cfg: Config{}, memory: f}
		ctx := withSessionContext(context.Background(), &sessionContext{
			OrgID: "o1", Sub: "u1", Picture: "avatars/me.jpg",
		})
		options := s.objectActorIdentities(ctx, true, false)

		if f.listOrgMembersCalls != 1 || f.orgMembersOrgID != "o1" {
			t.Errorf("org members read = %d (org %q), want one read for o1", f.listOrgMembersCalls, f.orgMembersOrgID)
		}
		if f.listMembersCalls != 0 {
			t.Errorf("project members read %d times, want 0 when the session names its org", f.listMembersCalls)
		}
		if len(options) != 1 || options[0].Label != "Maciek Dev" || options[0].Type != "user" {
			t.Fatalf("options = %+v, want the named org member", options)
		}
		// The signed-in user's own photo is the only avatar a page can serve.
		if options[0].AvatarURL != "/api/user/avatar?v=avatars%2Fme.jpg" {
			t.Errorf("self avatar = %q, want the session-scoped proxy", options[0].AvatarURL)
		}
	})

	t.Run("no session org", func(t *testing.T) {
		f := &fakeMemory{orgMembers: orgMembers, members: members}
		s := &Server{cfg: Config{}, memory: f}
		options := s.objectActorIdentities(context.Background(), true, false)

		if f.listOrgMembersCalls != 0 || f.listMembersCalls != 1 {
			t.Errorf("reads = org %d / project %d, want the project roster only", f.listOrgMembersCalls, f.listMembersCalls)
		}
		if len(options) != 1 || options[0].Label != "Alice" {
			t.Fatalf("options = %+v, want the project member", options)
		}
	})

	t.Run("org read fails", func(t *testing.T) {
		f := &fakeMemory{
			orgMembersErr: context.DeadlineExceeded,
			members:       members,
		}
		s := &Server{cfg: Config{}, memory: f}
		ctx := withSessionContext(context.Background(), &sessionContext{OrgID: "o1", Sub: "u1"})
		options := s.objectActorIdentities(ctx, true, false)

		if len(options) != 1 || options[0].Label != "Alice" {
			t.Fatalf("options = %+v, want the project-roster fallback", options)
		}
	})

	// An autoprovisioned profile can be an empty roster row: no display name, no
	// name parts, no email. The caller's own writes still get the name from the
	// claims they signed in with, instead of resolving to a bare id.
	t.Run("nameless self profile", func(t *testing.T) {
		f := &fakeMemory{orgMembers: []OrgMemberDto{{ID: "u1"}}}
		s := &Server{cfg: Config{}, memory: f}
		ctx := withSessionContext(context.Background(), &sessionContext{
			OrgID: "o1", Sub: "u1", Name: "Maciek Dev", Picture: "avatars/me.jpg",
		})
		options := s.objectActorIdentities(ctx, true, false)

		if len(options) != 1 || options[0].Label != "Maciek Dev" {
			t.Fatalf("options = %+v, want the caller named from the claims", options)
		}
		if options[0].AvatarURL == "" {
			t.Error("the caller's own avatar must be attached to their option")
		}
	})

	// A roster that does not list the caller at all still gets them: their
	// writes are in the filter's data whether or not a membership row exists.
	t.Run("self missing from roster", func(t *testing.T) {
		f := &fakeMemory{orgMembers: orgMembers}
		s := &Server{cfg: Config{}, memory: f}
		ctx := withSessionContext(context.Background(), &sessionContext{
			OrgID: "o1", Sub: "u9", Name: "Maciek Dev",
		})
		options := s.objectActorIdentities(ctx, true, false)

		var found bool
		for _, o := range options {
			if o.ID == "u9" && o.Label == "Maciek Dev" {
				found = true
			}
		}
		if !found {
			t.Errorf("options = %+v, want the caller appended", options)
		}
	})
}

// TestObjectProvenanceAvatarRenders pins the mark on both surfaces that show a
// write stamp: the browse card's footer and the history timeline. The card and
// the timeline render the same component, so a change that adds a face to one
// and not the other would be a drift, not a decision.
func TestObjectProvenanceAvatarRenders(t *testing.T) {
	identities := []objectActorOption{{Type: "user", ID: "u1", Label: "Maciek Dev"}}
	version := GraphObject{
		ID: "o1", Version: 2, Type: "Task", Key: "boiler", ActorType: "user", ActorID: "u1",
		CredentialKind: "session", CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
	}

	card := renderHTML(t, objectsBrowserCard(
		newObjectsBrowserRow(version, nil, identities, nil, 0), false, false))
	if !strings.Contains(card, `data-testid="object-actor-avatar"`) {
		t.Errorf("card footer must carry the writer's identity mark:\n%s", card)
	}
	if !strings.Contains(card, "Maciek Dev") {
		t.Errorf("card footer must name the writer:\n%s", card)
	}

	history := renderHTML(t, objectPreviewHistory([]GraphObject{version}, identities, nil))
	if !strings.Contains(history, `data-testid="object-actor-avatar"`) {
		t.Errorf("history entry must carry the writer's identity mark:\n%s", history)
	}
	if !strings.Contains(history, "Maciek Dev") {
		t.Errorf("history entry must name the writer:\n%s", history)
	}

	// An unresolved writer has no mark: the fallback is a placeholder.
	ghost := renderHTML(t, objectPreviewHistory([]GraphObject{version}, nil, nil))
	if strings.Contains(ghost, `data-testid="object-actor-avatar"`) {
		t.Errorf("unresolved writer must not get an identity mark:\n%s", ghost)
	}
}

// TestObjectsPageNamesOrgWriter is the end-to-end version of the roster rule:
// a scoped browse request (session org set by applyScope) renders the ORG
// member who wrote the object, with the identity mark — not the
// "User · <short id>" placeholder the project roster alone produced.
func TestObjectsPageNamesOrgWriter(t *testing.T) {
	f := &fakeMemory{
		orgMembers: []OrgMemberDto{{ID: "u1", Email: "maciek@example.com", DisplayName: ptr("Maciek Dev")}},
		pageObjects: []GraphObject{{
			ID: "o1", Type: "Task", Key: "boiler", ActorType: "user", ActorID: "u1",
			CredentialKind: "session",
			CreatedAt:      time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/search", s.uiSearch)

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{
		OrgID: "o1", ProjectID: "p1", Sub: "u1", Name: "Maciek Dev", Picture: "avatars/me.jpg",
	}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	html := rec.Body.String()
	if !strings.Contains(html, "Maciek Dev") {
		t.Errorf("card footer must name the org member who wrote the object:\n%s", html)
	}
	if strings.Contains(html, "User · ") {
		t.Errorf("a resolvable writer must not render the placeholder label:\n%s", html)
	}
	if !strings.Contains(html, `data-testid="object-actor-avatar"`) {
		t.Errorf("card footer must carry the writer's identity mark:\n%s", html)
	}
}

// TestObjectProvenanceOfUnstampedVersion pins the empty case: a version written
// before the write stamp existed renders no provenance line at all rather than
// claiming a writer.
func TestObjectProvenanceOfUnstampedVersion(t *testing.T) {
	p := objectProvenanceOf(GraphObject{Version: 1, CreatedAt: "2026-01-01T00:00:00Z"}, nil, nil)
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
	html := renderHTML(t, objectPreviewHistory(versions, agentIdentity, nil))
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

	bare := renderHTML(t, objectPreviewHistory([]GraphObject{{ID: "1", Version: 1, Type: "note", Key: "k"}}, nil, nil))
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
	identities, _ := s.objectProvenanceInputs(context.Background(), versions)
	if f.listMembersCalls != 0 {
		t.Errorf("agent-only timeline fetched members %d times, want 0", f.listMembersCalls)
	}
	if len(identities) != 1 || identities[0].Label != "researcher" {
		t.Fatalf("identities = %+v, want the resolved agent", identities)
	}

	// A timeline with no stamped actor costs no identity fetch at all.
	empty := &fakeMemory{agents: []AgentDefinitionSummary{{ID: "a1", Name: "researcher"}}}
	emptySrv := &Server{cfg: Config{}, memory: empty}
	emptySrv.objectProvenanceInputs(context.Background(), []GraphObject{{Version: 1}})
	if empty.listMembersCalls != 0 || len(empty.catalogCalls) != 0 || empty.lastRunOrdererIDs != nil {
		t.Errorf("unstamped timeline fetched identities: members=%d catalog=%v orderers=%v",
			empty.listMembersCalls, empty.catalogCalls, empty.lastRunOrdererIDs)
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

// ordererIdentities is the resolved cast for the provenance-v2 tests: the agent
// that performed a write and the person who ordered the run behind it.
func ordererIdentities() []objectActorOption {
	return []objectActorOption{
		{Type: "agent", ID: "a1", Label: "researcher"},
		{Type: "user", ID: "u1", Label: "Maciek Kucharz"},
	}
}

// TestObjectProvenanceOrderedBy pins provenance v2: an agent's write names the
// person who ordered its run, the run chip links to the run's transcript, and
// nothing is invented when the run has no recorded orderer (a source trigger, a
// schedule, background work).
func TestObjectProvenanceOrderedBy(t *testing.T) {
	run := "0193f2a1-0000-4000-8000-000000000001"
	identities := ordererIdentities()
	v := GraphObject{ActorType: "agent", ActorID: "a1", CredentialKind: "mcp_link", RunID: run}

	p := objectProvenanceOf(v, identities, map[string]string{run: "u1"})
	if p.OrderedBy != "Maciek Kucharz" {
		t.Errorf("OrderedBy = %q, want the ordering person", p.OrderedBy)
	}
	if p.OrderedByInitials == "" || !strings.Contains(p.OrderedByTitle, "u1") {
		t.Errorf("orderer mark = %q / title %q, want initials and the recorded id", p.OrderedByInitials, p.OrderedByTitle)
	}
	if p.Actor != "researcher" {
		t.Errorf("Actor = %q, want the performing agent", p.Actor)
	}
	if want := "/runs/" + run; p.RunHref != want {
		t.Errorf("RunHref = %q, want %q", p.RunHref, want)
	}

	// No orderer recorded: the agent stands alone.
	plain := objectProvenanceOf(v, identities, nil)
	if plain.OrderedBy != "" {
		t.Errorf("OrderedBy = %q with no orderer, want empty", plain.OrderedBy)
	}
	if plain.RunHref == "" {
		t.Error("the run chip must still link when no orderer is known")
	}

	// An orderer recorded on a run that a *person* drove directly is not an
	// order behind someone else's work.
	direct := objectProvenanceOf(GraphObject{
		ActorType: "user", ActorID: "u1", CredentialKind: "session", RunID: run,
	}, identities, map[string]string{run: "u1"})
	if direct.OrderedBy != "" {
		t.Errorf("a user write must not render an orderer, got %q", direct.OrderedBy)
	}

	// An unresolvable orderer id is not a name: claim nothing.
	ghost := objectProvenanceOf(v, []objectActorOption{{Type: "agent", ID: "a1", Label: "researcher"}}, map[string]string{run: "u-gone"})
	if ghost.OrderedBy != "" {
		t.Errorf("unresolved orderer rendered as %q, want empty", ghost.OrderedBy)
	}
}

// TestObjectPreviewHistoryOrderedByRow pins the rendered v2 line: the orderer
// leads ("on <name>'s order"), the agent is the mechanism ("via <agent>",
// linked), and the credential is not repeated — the run and the agent already
// say how the write happened.
func TestObjectPreviewHistoryOrderedByRow(t *testing.T) {
	run := "0193f2a1-0000-4000-8000-000000000001"
	versions := []GraphObject{{
		ID: "2", Version: 2, Type: "note", Key: "k", Status: "ready",
		ActorType: "agent", ActorID: "a1", CredentialKind: "mcp_link", RunID: run,
		CreatedAt: "2026-01-02T09:30:00Z",
	}}
	html := renderHTML(t, objectPreviewHistory(versions, ordererIdentities(), map[string]string{run: "u1"}))
	for _, want := range []string{
		`data-testid="object-preview-history-provenance"`,
		"on Maciek Kucharz&#39;s order",
		"via</span>",
		`href="/agents/a1"`,
		"researcher",
		`href="/runs/` + run + `"`,
		`data-testid="object-preview-history-run"`,
		"run 0193f2a1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("ordered provenance row missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "via agent MCP link") {
		t.Errorf("the orderer's line must not repeat the credential:\n%s", html)
	}

	// Without an orderer the row is exactly what it was before v2.
	plain := renderHTML(t, objectPreviewHistory(versions, ordererIdentities(), nil))
	if !strings.Contains(plain, "via agent MCP link") {
		t.Errorf("no orderer: the credential must still be shown:\n%s", plain)
	}
	if strings.Contains(plain, "on Maciek Kucharz") {
		t.Errorf("no orderer: nothing may be claimed:\n%s", plain)
	}
}

// TestObjectPossessive pins the apostrophe rule so a surname ending in s does
// not read "Davies's".
func TestObjectPossessive(t *testing.T) {
	cases := map[string]string{
		"Maciek Kucharz": "Maciek Kucharz's",
		"Davies":         "Davies'",
		"ROS":            "ROS'",
		"":               "",
	}
	for in, want := range cases {
		if got := objectPossessive(in); got != want {
			t.Errorf("objectPossessive(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestObjectProvenanceInputsFetchesPeopleForOrderer pins the coupling rule: an
// orderer is a person who need not be an actor on any version, so a page of
// agent-written versions must still fetch the member roster — otherwise every
// "on <name>'s order" resolves to nothing.
func TestObjectProvenanceInputsFetchesPeopleForOrderer(t *testing.T) {
	run := "0193f2a1-0000-4000-8000-000000000001"
	f := &fakeMemory{
		agents:      []AgentDefinitionSummary{{ID: "a1", Name: "researcher"}},
		members:     []ProjectMemberDto{{ID: "u1", DisplayName: "Maciek Kucharz"}},
		runOrderers: map[string]string{run: "u1"},
	}
	s := &Server{cfg: Config{}, memory: f}
	identities, orderers := s.objectProvenanceInputs(context.Background(), []GraphObject{
		{Version: 1, ActorType: "agent", ActorID: "a1", RunID: run},
	})
	if f.listMembersCalls != 1 {
		t.Errorf("member roster fetched %d times, want 1 (the orderer is a person)", f.listMembersCalls)
	}
	if orderers[run] != "u1" {
		t.Errorf("orderers = %v, want the run's orderer", orderers)
	}
	if _, ok := actorOptionOf(identities, "user", "u1"); !ok {
		t.Errorf("identities = %+v, want the orderer resolved", identities)
	}
	if len(f.lastRunOrdererIDs) != 1 || f.lastRunOrdererIDs[0] != run {
		t.Errorf("run ids asked for = %v, want [%s]", f.lastRunOrdererIDs, run)
	}

	// No run at all: no orderer read, and no roster for an agent-only page.
	bare := &fakeMemory{agents: []AgentDefinitionSummary{{ID: "a1", Name: "researcher"}}}
	bareSrv := &Server{cfg: Config{}, memory: bare}
	bareSrv.objectProvenanceInputs(context.Background(), []GraphObject{{Version: 1, ActorType: "agent", ActorID: "a1"}})
	if bare.listMembersCalls != 0 || bare.lastRunOrdererIDs != nil {
		t.Errorf("an unrun agent write fetched members=%d orderers=%v, want none", bare.listMembersCalls, bare.lastRunOrdererIDs)
	}
}

// TestObjectRunOrderersOneBatchRead pins the cost rule: a page with repeated run
// ids asks for each distinct run once, never one request per entry.
func TestObjectRunOrderersOneBatchRead(t *testing.T) {
	f := &fakeMemory{runOrderers: map[string]string{"r1": "u1"}}
	s := &Server{cfg: Config{}, memory: f}
	versions := []GraphObject{
		{ActorType: "agent", RunID: "r1"},
		{ActorType: "agent", RunID: "r1"},
		{ActorType: "agent", RunID: "r2"},
		{ActorType: "user", RunID: "r3"}, // a person's own write is not ordered
		{ActorType: "agent"},             // no run
	}
	s.objectRunOrderers(context.Background(), versions)
	if len(f.lastRunOrdererIDs) != 2 || f.lastRunOrdererIDs[0] != "r1" || f.lastRunOrdererIDs[1] != "r2" {
		t.Errorf("run ids = %v, want [r1 r2] exactly once each", f.lastRunOrdererIDs)
	}
}

// TestGetRunOrderersClient pins the request shape and the parse: one
// project-scoped read that returns only runs with a recorded orderer.
func TestGetRunOrderersClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/projects/proj/agent-runs" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("ids"); got != "r1,r2" {
			t.Errorf("ids = %q, want r1,r2", got)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit = %q, want 2", got)
		}
		if r.Header.Get("Authorization") != "Bearer tok123" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"items":[
			{"id":"r1","orderedByUserId":"u1"},
			{"id":"r2"}
		],"totalCount":2,"limit":2,"offset":0}}`)
	}))
	t.Cleanup(srv.Close)
	m := NewMemoryClient(srv.URL, "proj")

	orderers, err := m.GetRunOrderers(sessProjCtx("tok123", "proj"), []string{"r1", "r2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(orderers) != 1 || orderers["r1"] != "u1" {
		t.Errorf("orderers = %v, want only r1 ordered by u1", orderers)
	}
	if _, ok := orderers["r2"]; ok {
		t.Error("a run with no recorded orderer must not map to an empty entry")
	}

	// No ids: no request at all.
	if got, err := m.GetRunOrderers(sessProjCtx("tok123", "proj"), nil); err != nil || got != nil {
		t.Errorf("empty id set = %v/%v, want nil/nil", got, err)
	}
}

// TestLoadObjectBrowsePageResolvesProvenance pins the sibling rule: the shared
// browse loader is the one place every browse surface goes through (generic
// browse, agent-scoped view, and the "Load more" partial), so an appended or
// scoped page must resolve the writer's name and the run's orderer exactly like
// the first page. Resolving only in the page handler left those rows naming a
// short id and no order.
func TestLoadObjectBrowsePageResolvesProvenance(t *testing.T) {
	run := "0193f2a1-0000-4000-8000-000000000001"
	f := &fakeMemory{
		pageObjects: []GraphObject{{
			ID: "1", Version: 1, Type: "note", Key: "k",
			ActorType: "agent", ActorID: "a1", RunID: run,
		}},
		agents:      []AgentDefinitionSummary{{ID: "a1", Name: "researcher"}},
		members:     []ProjectMemberDto{{ID: "u1", DisplayName: "Maciek Kucharz"}},
		runOrderers: map[string]string{run: "u1"},
	}
	s := &Server{cfg: Config{}, memory: f}

	data := objectsPageData{}
	if err := s.loadObjectBrowsePage(context.Background(), &data, ""); err != nil {
		t.Fatal(err)
	}
	if data.RunOrderers[run] != "u1" {
		t.Errorf("RunOrderers = %v, want the page's run ordered by u1", data.RunOrderers)
	}
	if _, ok := actorOptionOf(data.ActorOptions, "user", "u1"); !ok {
		t.Errorf("ActorOptions = %+v, want the orderer resolved for the card footer", data.ActorOptions)
	}
	rows := objectsBrowserRows(data)
	if len(rows) != 1 || rows[0].Writer.OrderedBy != "Maciek Kucharz" {
		t.Fatalf("rows = %+v, want one row naming its orderer", rows)
	}
}
