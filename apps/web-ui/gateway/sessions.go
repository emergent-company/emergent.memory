package main

import (
	"context"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/web-ui/toolresult"
	"github.com/labstack/echo/v4"
)

// --- session trace/log viewer ---

// uiSessions renders the session list, most recent first, each linking to its
// detail view (/sessions/:id). A failed fetch renders the whole-page error
// state.
func (s *Server) uiSessions(c echo.Context) error {
	ctx := c.Request().Context()
	convs, err := s.memory.ListConversations(ctx, false)
	if err != nil {
		return s.page(c, pageTitle("Sessions"), SessionsPage(nil, nil, userFacingError(err)))
	}
	list := convs.Conversations
	// Agent appearance (icon + color) is best-effort decoration on each row
	// and can only matter when a conversation is bound to an agent definition,
	// so skip the fetch when none are.
	var agents []AgentDefinitionSummary
	if slices.ContainsFunc(list, func(c Conversation) bool { return c.AgentDefinitionID != "" }) {
		var aerr error
		agents, aerr = s.memory.ListAgentDefinitions(ctx)
		if aerr != nil {
			captureError(aerr)
			agents = nil
		}
	}
	// CreatedAt is RFC3339; string ordering is chronological for UTC timestamps.
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].CreatedAt > list[j].CreatedAt
	})
	return s.page(c, pageTitle("Sessions"), SessionsPage(agents, list, nil))
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
			return s.page(c, pageTitle("Session"), SessionPage(nil, nil, nil, nil))
		}
		return s.page(c, pageTitle("Session"), SessionPage(nil, nil, nil, userFacingError(err)))
	}
	// Agent appearance (icon + color) is best-effort decoration on the header
	// badge and the assistant avatar; it only applies to agent-bound sessions,
	// so skip the fetch otherwise.
	var agents []AgentDefinitionSummary
	if detail != nil && detail.AgentDefinitionID != "" {
		var aerr error
		agents, aerr = s.memory.ListAgentDefinitions(ctx)
		if aerr != nil {
			captureError(aerr)
			agents = nil
		}
	}
	history, err := s.memory.GetConversationHistory(ctx, id)
	if err != nil {
		if isSessionNotFound(err) {
			return s.page(c, pageTitle("Session"), SessionPage(agents, detail, nil, nil))
		}
		return s.page(c, pageTitle("Session"), SessionPage(agents, detail, nil, userFacingError(err)))
	}
	if len(history.Items) == 0 {
		history.Items = messageTimelineItems(detail.Messages)
	}
	items := parseTimeline(history.Items)
	enrichTimelineSteps(items)
	groups := groupByRun(items)
	s.enrichRunTraces(ctx, groups)
	return s.page(c, pageTitle(sessionTitle(detail)), SessionPage(agents, detail, groups, nil))
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

// --- tool-step presentation ---------------------------------------------------

// toolStep is one tool call's presentation on the recorded-session page: the same
// category, subject and view the live chat chip gets on the wire, assembled here
// because this page renders without JavaScript (design D4). Deriving it at render
// time means nothing is persisted and the page cannot show a stale reading.
type toolStep struct {
	Category toolresult.Category
	Subject  string
	// Paused marks a call that was paused for approval: the policy check returned
	// the executor's sentinel instead of running the tool, so the payload is not a
	// result and there is no view to show.
	Paused bool
	View   *toolresult.Presentation
	// Failure is the call's failure line, or "" when it succeeded. It is the
	// answer when a call failed, so it is shown once, with error styling, and the
	// outcome and body drop the sentence it repeats.
	Failure string
}

// Label is what the step says it did: the category verb, then the subject its
// arguments gave it. A paused call has not finished, so it reads in the
// continuous tense — the same rule the live chip applies.
func (s *toolStep) Label() string {
	verb := s.Category.Verb
	if s.Paused {
		verb = s.Category.RunningVerb
	}
	if s.Subject == "" {
		return verb
	}
	return verb + " · " + s.Subject
}

// Outcome is the collapsed one-line result the chat chip shows, worded once in
// the toolresult package. A paused call has no result: it says it is waiting.
func (s *toolStep) Outcome() string {
	switch {
	case s.Paused:
		return "Waiting for confirmation"
	case s.View == nil:
		return ""
	default:
		return s.View.Outcome
	}
}

// ShowOutcome reports whether the outcome line adds anything the card does not
// already say. A failure is the answer, so its error line carries it; a body of
// text that is the outcome worded once is not repeated either.
func (s *toolStep) ShowOutcome() bool {
	// A paused call says it is waiting once, in the note that also explains what
	// that means; a failed call is carried by its error line.
	if s.Paused || s.Failure != "" || s.Outcome() == "" {
		return false
	}
	if s.Text() && sameLine(s.View.Text, s.Outcome()) {
		return false
	}
	return true
}

// ShowText reports whether the text body adds anything over the failure line.
func (s *toolStep) ShowText() bool {
	if !s.Text() {
		return false
	}
	return s.Failure == "" || !sameLine(s.View.Text, s.Failure)
}

// sameLine reports whether two strings are the same sentence to a reader.
func sameLine(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

// enrichTimelineSteps attaches each tool_call item's presentation. The parse is
// bounded by the package's caps, so a long run costs a bounded pass per item.
func enrichTimelineSteps(items []TimelineItem) {
	for i := range items {
		if items[i].Kind != "tool_call" {
			continue
		}
		step := &toolStep{
			Category: toolresult.Classify(items[i].ToolName),
			Subject:  toolresult.Subject(items[i].ToolInput),
		}
		step.Failure = toolError(items[i].ToolStatus, string(items[i].ToolOutput))
		if toolAwaitingConfirmation(items[i].ToolOutput) {
			step.Paused = true
		} else if view := toolresult.Parse(string(items[i].ToolOutput)); view != nil {
			step.View = view
		}
		items[i].ToolStep = step
	}
}

// Records, Fields and Text say which readable body the result has. They are
// predicates rather than templ comparisons so the template needs no import.
func (s *toolStep) Records() bool {
	return s.View != nil && s.View.Kind == toolresult.KindRecords
}

func (s *toolStep) Fields() bool {
	return s.View != nil && s.View.Kind == toolresult.KindFields
}

func (s *toolStep) Text() bool {
	return s.View != nil && s.View.Kind == toolresult.KindText
}

// ShowRawText reports a payload with no readable shape: its bounded text is the
// whole answer, so it is shown in the open rather than behind a second tap — the
// same rule the chat chip applies.
func (s *toolStep) ShowRawText() bool {
	return s.View != nil && !s.Records() && !s.Fields() && !s.Text() && s.View.Raw != ""
}

// ShowRawDisclosure reports whether the wire name and the raw blocks go behind
// the disclosure. A paused call has no result to disclose, and a payload shown in
// the open is already in front of the reader.
func (s *toolStep) ShowRawDisclosure() bool {
	return !s.Paused && !s.ShowRawText()
}

// BodyRemainder states what the body caps hid, so a shortened body cannot pass
// for the whole result.
func (s *toolStep) BodyRemainder() string {
	if s.View == nil {
		return ""
	}
	var parts []string
	if s.View.MoreRecords > 0 {
		parts = append(parts, moreCounted(s.View.MoreRecords, "record"))
	}
	if s.View.MoreFields > 0 {
		parts = append(parts, moreCounted(s.View.MoreFields, "field"))
	}
	if s.View.MoreLines > 0 {
		parts = append(parts, moreCounted(s.View.MoreLines, "line"))
	}
	if s.View.MoreCharacters > 0 {
		parts = append(parts, strconv.Itoa(s.View.MoreCharacters)+" characters cut")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// RawRemainder states what the raw cap hid.
func (s *toolStep) RawRemainder() string {
	if s.View == nil {
		return ""
	}
	var parts []string
	if s.View.RawMoreLines > 0 {
		parts = append(parts, moreCounted(s.View.RawMoreLines, "line"))
	}
	if s.View.RawMoreCharacters > 0 {
		parts = append(parts, strconv.Itoa(s.View.RawMoreCharacters)+" characters cut")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// moreCounted words a hidden remainder the way the chat chip does ("1 more
// record", "2 more records"), so the two surfaces phrase a cap the same way.
func moreCounted(n int, noun string) string {
	if n == 1 {
		return "1 more " + noun
	}
	return strconv.Itoa(n) + " more " + noun + "s"
}

// linkHost is the domain a record's link points at, for the quiet suffix the chat
// chip shows instead of a full URL.
func linkHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}
