package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func railTime(offset time.Duration) string {
	return time.Now().UTC().Add(offset).Format(time.RFC3339)
}

// TestGroupConversations covers the parent/child fold used by the rail: roots
// order by subtree recency, descendants keep spawn order, an orphan is promoted
// to a root, and the active child opens its branch.
func TestGroupConversations(t *testing.T) {
	convs := []Conversation{
		{ID: "parent", Title: "Parent", UpdatedAt: railTime(-3 * time.Hour), CreatedAt: railTime(-3 * time.Hour)},
		{ID: "child-a", Title: "Child A", ParentConversationID: "parent", UpdatedAt: railTime(-2 * time.Hour), CreatedAt: railTime(-2 * time.Hour)},
		{ID: "child-b", Title: "Child B", ParentConversationID: "parent", UpdatedAt: railTime(-1 * time.Hour), CreatedAt: railTime(-1 * time.Hour)},
		{ID: "other", Title: "Other", UpdatedAt: railTime(-30 * time.Minute), CreatedAt: railTime(-30 * time.Minute)},
		{ID: "orphan", Title: "Orphan", ParentConversationID: "missing", UpdatedAt: railTime(-4 * time.Hour), CreatedAt: railTime(-4 * time.Hour)},
	}

	groups := groupConversations(convs, "")
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3 (parent, other, orphan): %+v", len(groups), groups)
	}
	// The parent subtree's newest row (child-b, -1h) beats other (-30m)? No:
	// -30m is newer than -1h, so other leads.
	if groups[0].Root.ID != "other" {
		t.Errorf("first group = %q, want other (newest subtree)", groups[0].Root.ID)
	}
	if groups[1].Root.ID != "parent" {
		t.Errorf("second group = %q, want parent", groups[1].Root.ID)
	}
	if groups[2].Root.ID != "orphan" {
		t.Errorf("orphan must be promoted to a root, got %q", groups[2].Root.ID)
	}

	parent := groups[1]
	if len(parent.Descendants) != 2 {
		t.Fatalf("parent descendants = %d, want 2", len(parent.Descendants))
	}
	if parent.Descendants[0].ID != "child-a" || parent.Descendants[1].ID != "child-b" {
		t.Errorf("descendants must be in spawn (created_at) order, got %q, %q",
			parent.Descendants[0].ID, parent.Descendants[1].ID)
	}
	for _, d := range parent.Descendants {
		if d.Depth != 1 {
			t.Errorf("depth(%s) = %d, want 1", d.ID, d.Depth)
		}
	}
	if parent.Expanded {
		t.Error("group must start collapsed when nothing is active")
	}

	// Opening a child reveals its branch.
	for _, g := range groupConversations(convs, "child-b") {
		if g.Root.ID == "parent" && !g.Expanded {
			t.Error("active child must expand its group")
		}
	}
}

// TestGroupConversationsDeepChainAndCycle keeps the walk safe: a grandchild is
// folded into the root group with its depth, and a corrupt parent cycle ends
// instead of looping.
func TestGroupConversationsDeepChainAndCycle(t *testing.T) {
	convs := []Conversation{
		{ID: "root", UpdatedAt: railTime(-3 * time.Hour), CreatedAt: railTime(-3 * time.Hour)},
		{ID: "mid", ParentConversationID: "root", UpdatedAt: railTime(-2 * time.Hour), CreatedAt: railTime(-2 * time.Hour)},
		{ID: "leaf", ParentConversationID: "mid", UpdatedAt: railTime(-1 * time.Hour), CreatedAt: railTime(-1 * time.Hour)},
	}
	groups := groupConversations(convs, "")
	if len(groups) != 1 || len(groups[0].Descendants) != 2 {
		t.Fatalf("deep chain must fold into one group: %+v", groups)
	}
	var leafDepth int
	for _, d := range groups[0].Descendants {
		if d.ID == "leaf" {
			leafDepth = d.Depth
		}
	}
	if leafDepth != 2 {
		t.Errorf("leaf depth = %d, want 2", leafDepth)
	}

	cycle := []Conversation{
		{ID: "a", ParentConversationID: "b", UpdatedAt: railTime(0)},
		{ID: "b", ParentConversationID: "a", UpdatedAt: railTime(0)},
	}
	got := groupConversations(cycle, "")
	if len(got) != 1 {
		t.Fatalf("cycle must canonicalise to one group, got %d", len(got))
	}
	if got[0].Root.ID != "a" || len(got[0].Descendants) != 1 || got[0].Descendants[0].ID != "b" {
		t.Fatalf("cycle group = root %q descendants %v, want a -> [b]", got[0].Root.ID, got[0].Descendants)
	}
}

// TestChatRailRendersSubchatGroup pins the template contract chat.js depends on:
// the parent carries the toggle/count, nested rows carry data-parent-id, a
// collapsed group renders its children hidden, and the active child opens it.
func TestChatRailRendersSubchatGroup(t *testing.T) {
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "parent", Title: "Parent", AgentDefinitionID: "agent", UpdatedAt: railTime(-3 * time.Hour)},
		{ID: "child-a", Title: "Child A", AgentDefinitionID: "internal", ParentConversationID: "parent", SpawnSource: "search-knowledge", UpdatedAt: railTime(-2 * time.Hour)},
		{ID: "child-b", Title: "Child B", ParentConversationID: "parent", UpdatedAt: railTime(-1 * time.Hour)},
	}}

	collapsed := renderHTML(t, chatRailList(convs, map[string]agentAppearance{"agent": {Name: "Memory"}}, nil, nil, nil, ""))
	for _, want := range []string{
		`data-subchat-count="2"`,
		`data-action="toggle-subchats"`,
		`aria-expanded="false"`,
		`data-subchat-group="parent"`,
		`data-parent-id="parent"`,
		`search-knowledge`,
	} {
		if !strings.Contains(collapsed, want) {
			t.Errorf("collapsed rail missing %q", want)
		}
	}
	if strings.Count(collapsed, "data-parent-id=\"parent\"") != 2 {
		t.Errorf("both children must carry data-parent-id")
	}
	// The filter pass reads the group id from the parent row; the toggle needs
	// it too. A fixture that invents the row attribute masked a missing one.
	if strings.Count(collapsed, `data-subchat-group="parent"`) != 2 {
		t.Errorf("parent row and toggle must both carry data-subchat-group")
	}
	parentTag := ""
	for _, tag := range strings.Split(collapsed, "<li") {
		if strings.Contains(tag, `data-subchat-count="2"`) {
			parentTag = tag
			break
		}
	}
	if !strings.Contains(parentTag, `data-subchat-group="parent"`) {
		t.Errorf("the parent row carrying data-subchat-count must also carry data-subchat-group")
	}
	if !strings.Contains(collapsed, `class="list-row session-subchat hidden"`) {
		t.Error("collapsed group must render its children with hidden")
	}

	expanded := renderHTML(t, chatRailList(convs, nil, nil, nil, nil, "child-b"))
	if !strings.Contains(expanded, `aria-expanded="true"`) {
		t.Error("active child must open the group toggle")
	}
	if strings.Contains(expanded, `session-subchat hidden`) {
		t.Error("expanded group must not hide its children")
	}
	if !strings.Contains(expanded, `data-active="true"`) {
		t.Error("active child must remain marked active")
	}
}

// TestMemoryClientListConversationsWithAncestors pins the rail-only request:
// the ordinary list stays exact, the rail variant opts into ancestor expansion.
func TestMemoryClientListConversationsWithAncestors(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"conversations":[],"total":0}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.ListConversations(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if gotQuery.Has("includeAncestors") {
		t.Errorf("ordinary list must not send includeAncestors, got %v", gotQuery)
	}

	if _, err := m.ListConversationsWithAncestors(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := gotQuery.Get("includeAncestors"); got != "true" {
		t.Errorf("includeAncestors = %q, want true", got)
	}
	if got := gotQuery.Get("includeArchived"); got != "true" {
		t.Errorf("includeArchived = %q, want true", got)
	}
}

// TestSessionRailSubchatToggleSitsInGrowColumn pins #51: the sub-chat count
// indicator lives on the row's second line (inside the grow column, next to the
// subtitle) instead of the trailing action cluster, where it stole width from
// the session title.
func TestSessionRailSubchatToggleSitsInGrowColumn(t *testing.T) {
	c := Conversation{ID: "parent", Title: "Parent", UpdatedAt: railTime(-time.Hour), CreatedAt: railTime(-time.Hour)}
	html := renderHTML(t, sessionRailGroupParent(c, agentAppearance{}, false, 3, false))

	grow := strings.Index(html, `class="list-col-grow"`)
	actions := strings.Index(html, `<div class="flex items-center gap-1">`)
	toggle := strings.Index(html, `data-testid="session-subchats-toggle"`)
	if grow < 0 || actions < 0 || toggle < 0 {
		t.Fatalf("row markup missing grow column / actions / toggle (grow=%d actions=%d toggle=%d)", grow, actions, toggle)
	}
	if toggle < grow || toggle > actions {
		t.Errorf("sub-chat toggle must render inside the grow column before the trailing actions (grow=%d toggle=%d actions=%d)", grow, toggle, actions)
	}
	if !strings.Contains(html, ">3<") {
		t.Error("sub-chat toggle should show the count")
	}
}
