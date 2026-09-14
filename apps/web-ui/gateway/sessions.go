package main

import (
	"context"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- session trace/log viewer ---

// uiSessions renders the session list, most recent first, each linking to its
// detail view (/sessions/:id). A failed fetch renders the whole-page error
// state.
func (s *Server) uiSessions(c echo.Context) error {
	convs, err := s.memory.ListConversations(c.Request().Context())
	if err != nil {
		return s.page(c, pageTitle("Sessions"), SessionsPage(nil, err))
	}
	list := convs.Conversations
	// CreatedAt is RFC3339; string ordering is chronological for UTC timestamps.
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].CreatedAt > list[j].CreatedAt
	})
	return s.page(c, pageTitle("Sessions"), SessionsPage(list, nil))
}

// uiSession renders one session's timeline, reusing the existing conversation
// dump path: GetConversation + GetConversationHistory + parseTimeline, with the
// stored-messages fallback when a session has no run timeline. Timeline items
// are grouped by run and each run is enriched with its OpenTelemetry trace
// waterfall when one exists. Unknown sessions render an empty timeline (not an
// error); memory-down renders the error state.
func (s *Server) uiSession(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	detail, err := s.memory.GetConversation(ctx, id)
	if err != nil {
		if isSessionNotFound(err) {
			return s.page(c, pageTitle("Session"), SessionPage(nil, nil, nil))
		}
		return s.page(c, pageTitle("Session"), SessionPage(nil, nil, err))
	}
	history, err := s.memory.GetConversationHistory(ctx, id)
	if err != nil {
		if isSessionNotFound(err) {
			return s.page(c, pageTitle("Session"), SessionPage(detail, nil, nil))
		}
		return s.page(c, pageTitle("Session"), SessionPage(detail, nil, err))
	}
	if len(history.Items) == 0 {
		history.Items = messageTimelineItems(detail.Messages)
	}
	items := parseTimeline(history.Items)
	groups := groupByRun(items)
	s.enrichRunTraces(ctx, groups)
	return s.page(c, pageTitle(sessionTitle(detail)), SessionPage(detail, groups, nil))
}

// enrichRunTraces attaches each run's trace spans to its run group. Memory now
// returns the run's flattened spans inline in the agent-run DTO, so this is one
// project-scoped call per run — no separate trace endpoint. Those calls run in
// bounded parallel (see runConcurrently) instead of sequentially. Everything
// degrades to "no trace section": a missing run, empty/nil spans, or a failed
// fetch must never break the page.
func (s *Server) enrichRunTraces(ctx context.Context, groups []runGroup) {
	runConcurrently(len(groups), func(i int) {
		if groups[i].runID == "" {
			return
		}
		run, err := s.memory.GetAgentRun(ctx, groups[i].runID)
		if err != nil || run == nil || len(run.Spans) == 0 {
			return
		}
		groups[i].spans = run.Spans
	})
}

// sessionTitle is the display title for a session detail (falls back to a
// shortened id for untitled sessions, like chatTitle does for the list).
func sessionTitle(detail *ConversationDetail) string {
	if detail == nil {
		return "Session"
	}
	return chatTitle(Conversation{ID: detail.ID, Title: detail.Title})
}

// isSessionNotFound matches the "session not found" style errors memory
// returns for a missing conversation (parity with dumpError in session_dump.go).
func isSessionNotFound(err error) bool {
	msg := err.Error()
	return isMemoryNotFound(err) || strings.Contains(msg, "invalid conversation id")
}
