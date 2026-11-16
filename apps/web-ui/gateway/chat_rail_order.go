package main

import (
	"sort"
	"time"
)

// --- session rail ordering ---

// railEntryKind names which producer owns one rail row. The rail is a single
// activity-ordered list, but its rows come from four places: conversations
// (memory), runs of scheduled agents, object-driven (board/extraction) runs,
// and sessions an anonymous end user started through a share link.
type railEntryKind int

const (
	railEntryConversation railEntryKind = iota
	railEntryScheduledRun
	railEntryWorkRun
	railEntrySharedSession

	// railEntryKindCount bounds the enum so a test can walk every kind and
	// assert it has a row renderer (see railEntryRows). It is not a real kind.
	railEntryKindCount
)

// railEntry is one rail row — or one conversation group (a root plus the
// sub-chats spawned beneath it) — together with the instant of its last
// activity. at is both the sort key and, for every visible row, the time the
// row's subtitle shows: a run row renders runActivityAt, a shared row renders
// ActivityRelative, and a conversation group's root row renders the subtree's
// newest updated_at even when it is collapsed (so a group never reads older
// than the row beneath it).
type railEntry struct {
	kind   railEntryKind
	at     time.Time
	group  conversationGroup
	sched  scheduledRunRow
	work   workRunRow
	shared sharedSessionRow
}

// orderRailEntries merges every rail producer into one list ordered by last
// activity, most recent first.
//
// The rail used to render each producer as its own block — every conversation,
// then every scheduled run, then every board/extraction run, then every shared
// session — so a run that failed four minutes ago sat below a chat last touched
// two days ago. Ordering is per row now, and each row's activity instant is the
// same value its subtitle renders:
//
//   - conversation group: the newest updated_at in the subtree, so a sub-chat
//     that ran later lifts its whole group (the rule groupConversations already
//     uses to order groups among themselves), and the group's root row displays
//     that same instant;
//   - scheduled/board/extraction run: completed_at once the run finished,
//     started_at while it is still running;
//   - shared session: last_activity_at, falling back to created_at.
//
// Rows without a usable timestamp sort last, and ties break by kind then id so
// the order is deterministic across renders: the rail is re-fetched by
// /partial/chat-rail, and a reshuffle would move rows under the pointer.
func orderRailEntries(convs []Conversation, convID string, schedRuns []scheduledRunRow, workRuns []workRunRow, shared []sharedSessionRow) []railEntry {
	entries := make([]railEntry, 0, len(convs)+len(schedRuns)+len(workRuns)+len(shared))

	for _, g := range groupConversations(convs, convID) {
		entries = append(entries, railEntry{kind: railEntryConversation, at: g.latest, group: g})
	}
	for _, r := range schedRuns {
		entries = append(entries, railEntry{
			kind:  railEntryScheduledRun,
			at:    parseRailTime(runActivityAt(r.StartedAt, r.CompletedAt)),
			sched: r,
		})
	}
	for _, r := range workRuns {
		entries = append(entries, railEntry{
			kind: railEntryWorkRun,
			at:   parseRailTime(runActivityAt(r.StartedAt, r.CompletedAt)),
			work: r,
		})
	}
	for _, sh := range shared {
		entries = append(entries, railEntry{
			kind:   railEntrySharedSession,
			at:     parseRailTime(sh.ActivityAt),
			shared: sh,
		})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if !a.at.Equal(b.at) {
			// A missing timestamp is the oldest possible activity.
			if a.at.IsZero() {
				return false
			}
			if b.at.IsZero() {
				return true
			}
			return a.at.After(b.at)
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return railEntryID(a) < railEntryID(b)
	})
	return entries
}

// railEntryID is the row's stable identity, used only to break timestamp ties.
func railEntryID(e railEntry) string {
	switch e.kind {
	case railEntryConversation:
		return e.group.Root.ID
	case railEntryScheduledRun:
		return e.sched.ID
	case railEntryWorkRun:
		return e.work.RunID + "|" + e.work.ID
	default:
		return e.shared.ID
	}
}

// runActivityAt is the instant a run row's last activity happened — and the
// value its subtitle shows: the completion time once the run finished, else its
// start. A running row therefore reads "running · 2m ago" and moves up as it
// progresses, while a finished row is anchored where it finished.
func runActivityAt(startedAt, completedAt string) string {
	if completedAt != "" {
		return completedAt
	}
	return startedAt
}
