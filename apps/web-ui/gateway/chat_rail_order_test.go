package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- session rail ordering ---
//
// The rail is one session list. These tests pin that every producer's rows
// (conversations, scheduled runs, board/extraction runs, shared sessions) share
// a single order — most recent activity first — instead of being rendered as
// consecutive blocks, which put a run that failed minutes ago below a chat last
// touched days ago.

// before reports whether marker a appears before marker b in html, failing the
// test when either is missing.
func before(t *testing.T, html, a, b string) {
	t.Helper()
	ia, ib := strings.Index(html, a), strings.Index(html, b)
	if ia < 0 {
		t.Fatalf("rail missing %q\n%s", a, html)
	}
	if ib < 0 {
		t.Fatalf("rail missing %q\n%s", b, html)
	}
	if ia > ib {
		t.Errorf("rail order wrong: %q should render above %q\n%s", a, b, html)
	}
}

// TestChatRailOrdersRunsAboveOlderConversations is the reported bug: a run that
// finished minutes ago must not sit below a chat whose last activity was days
// ago. The rail renders conversations, scheduled runs, board runs and shared
// sessions in one activity order.
func TestChatRailOrdersRunsAboveOlderConversations(t *testing.T) {
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "old-chat", Title: "Two days ago", UpdatedAt: "2026-11-13T10:00:00Z"},
		{ID: "stale-chat", Title: "Last month", UpdatedAt: "2026-10-08T10:00:00Z"},
	}}
	workRuns := []workRunRow{{
		ID: "run-failed", RunID: "run-failed", Origin: "board",
		Title:  "github:357590e8:emergent-company/emergent.memory#811",
		Status: "failed", StartedAt: "2026-11-15T20:26:00Z", CompletedAt: "2026-11-15T20:28:00Z",
	}}
	schedRuns := []scheduledRunRow{{
		ID: "run-sched", AgentName: "Daily briefing", Status: "completed",
		StartedAt: "2026-11-15T08:00:00Z", CompletedAt: "2026-11-15T08:02:00Z",
	}}
	shared := []sharedSessionRow{{
		ID: "share-1", Title: "Shared chat", ActivityAt: "2026-11-14T12:00:00Z", ActivityRelative: "1d ago",
	}}

	html := renderHTML(t, chatRailList(convs, nil, schedRuns, shared, workRuns, ""))

	before(t, html, "run-failed", "run-sched")
	before(t, html, "run-sched", "share-1")
	before(t, html, "share-1", "old-chat")
	before(t, html, "old-chat", "stale-chat")
}

// TestChatRailOrdersNewestConversationFirst proves the merge did not cost the
// conversation case: a chat that ran just now stays above an older run.
func TestChatRailOrdersNewestConversationFirst(t *testing.T) {
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "live-chat", Title: "Just now", UpdatedAt: "2026-11-15T20:34:00Z"},
	}}
	workRuns := []workRunRow{{
		ID: "run-old", RunID: "run-old", Origin: "board", Title: "Older run",
		Status: "completed", StartedAt: "2026-11-15T18:00:00Z", CompletedAt: "2026-11-15T18:01:00Z",
	}}

	html := renderHTML(t, chatRailList(convs, nil, nil, nil, workRuns, ""))
	before(t, html, "live-chat", "run-old")
}

// TestChatRailGroupStaysContiguous pins the group rule: a root and its sub-chats
// render together, at the position of the group's newest activity, so an
// interleaved run can never split a root from the sub-chat that outlived it.
func TestChatRailGroupStaysContiguous(t *testing.T) {
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "root", Title: "Root chat", UpdatedAt: "2026-11-10T10:00:00Z"},
		{ID: "child", Title: "Tool session: search-knowledge", ParentConversationID: "root", SpawnSource: "search-knowledge", UpdatedAt: "2026-11-15T19:00:00Z"},
	}}
	workRuns := []workRunRow{
		{ID: "run-mid", RunID: "run-mid", Origin: "board", Title: "Mid run", Status: "completed", StartedAt: "2026-11-15T18:00:00Z"},
		{ID: "run-oldest", RunID: "run-oldest", Origin: "board", Title: "Oldest run", Status: "completed", StartedAt: "2026-11-12T18:00:00Z"},
	}

	html := renderHTML(t, chatRailList(convs, nil, nil, nil, workRuns, ""))

	// The group rises on the child's activity (19:00), above the 18:00 run.
	before(t, html, `data-id="root"`, "run-mid")
	// Root row, then its sub-chat, then the older run — never split.
	before(t, html, `data-id="root"`, `data-id="child"`)
	before(t, html, `data-id="child"`, "run-oldest")
}

// TestRunRailSubtitleShowsActivityTime pins that a run row displays the same
// instant the rail sorts it by: completion once the run finished, start while it
// is still running.
func TestRunRailSubtitleShowsActivityTime(t *testing.T) {
	done := scheduledRunRow{
		AgentName:   "Daily briefing",
		Status:      "completed",
		StartedAt:   "2026-11-15T08:00:00Z",
		CompletedAt: "2026-11-15T08:05:00Z",
	}
	if got := railRunSubtitle(done); !strings.Contains(got, relTime("2026-11-15T08:05:00Z")) {
		t.Errorf("completed run subtitle = %q, want the completion time", got)
	}
	running := scheduledRunRow{
		AgentName: "Daily briefing",
		Status:    "running",
		StartedAt: "2026-11-15T08:00:00Z",
	}
	if got := railRunSubtitle(running); !strings.Contains(got, relTime("2026-11-15T08:00:00Z")) {
		t.Errorf("running run subtitle = %q, want the start time", got)
	}
}

// TestOrderRailEntriesTieBreakIsDeterministic keeps two rows with the same
// instant from swapping places between renders: /partial/chat-rail re-fetches the
// list while the rail is on screen, so an unstable order moves rows under the
// pointer.
func TestOrderRailEntriesTieBreakIsDeterministic(t *testing.T) {
	at := "2026-11-15T12:00:00Z"
	convs := []Conversation{{ID: "c1", Title: "Chat", UpdatedAt: at}}
	workRuns := []workRunRow{
		{ID: "run-b", RunID: "run-b", Origin: "board", Title: "B", Status: "completed", StartedAt: at},
		{ID: "run-a", RunID: "run-a", Origin: "board", Title: "A", Status: "completed", StartedAt: at},
	}
	first := orderRailEntries(convs, "", nil, workRuns, nil)
	last := orderRailEntries(convs, "", nil, workRuns, nil)
	if len(first) != 3 {
		t.Fatalf("entries = %d, want 3", len(first))
	}
	for i := range first {
		if railEntryID(first[i]) != railEntryID(last[i]) {
			t.Fatalf("order is not stable at %d: %q vs %q", i, railEntryID(first[i]), railEntryID(last[i]))
		}
	}
	if railEntryID(first[0]) != "c1" {
		t.Errorf("conversation should win a tie with runs, got %q", railEntryID(first[0]))
	}
	if railEntryID(first[1]) != "run-a|run-a" || railEntryID(first[2]) != "run-b|run-b" {
		t.Errorf("equal-instant runs must order by id, got %q then %q", railEntryID(first[1]), railEntryID(first[2]))
	}
}

// TestOrderRailEntriesRowsWithoutTimestampSortLast keeps a producer that omits
// its time (a degraded row) from jumping to the top of the rail.
func TestOrderRailEntriesRowsWithoutTimestampSortLast(t *testing.T) {
	entries := orderRailEntries(
		[]Conversation{{ID: "c1", UpdatedAt: "2026-11-15T12:00:00Z"}},
		"",
		nil,
		[]workRunRow{{ID: "run-untimed", RunID: "run-untimed", Origin: "board", Title: "No time"}},
		nil,
	)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if railEntryID(entries[0]) != "c1" {
		t.Errorf("timed row must render above an untimed row, got %q", railEntryID(entries[0]))
	}
}

// TestChatRailRouteOrdersByLastActivity drives the real rail route with a recent
// board run and an older conversation — the producer data, not a hand-built row,
// so the test fails if a producer stops supplying its activity time.
func TestChatRailRouteOrdersByLastActivity(t *testing.T) {
	f := &fakeMemory{
		convs: []Conversation{
			{ID: "old-chat", Title: "Two days ago", UpdatedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)},
		},
		boardRuns: []BoardRun{{
			ID:        "run-recent",
			AgentName: "Reviewer",
			Status:    "completed",
			StartedAt: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
			CompletedAt: func() *string {
				s := time.Now().Add(-4 * time.Minute).UTC().Format(time.RFC3339)
				return &s
			}(),
		}},
	}
	_, e := newChatRailEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-rail", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	before(t, body, `data-run-id="run-recent"`, `data-id="old-chat"`)
}
