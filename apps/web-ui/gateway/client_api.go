package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// --- project configuration (device surface) ---

// projectConfig reports the slice of project configuration a device needs
// before it decides what to offer. It is read-only and deliberately minimal:
// voice enablement only, never secrets, endpoints or provider settings.
//
// Voice defaults to off for a fresh project (the same default as the settings
// page), so an absent setting means disabled. A read failure is a 502 rather
// than a silent "off": the device keeps its last known value instead of hiding
// a working call affordance because the backend blipped.
func (s *Server) projectConfig(c echo.Context) error {
	if s.cfg.VoiceDisabled {
		return c.JSON(http.StatusOK, map[string]bool{"voiceEnabled": false})
	}
	ctx := c.Request().Context()
	ps, err := s.memory.GetProjectSetting(ctx, voiceCategory, voiceKeyEnabled)
	if err != nil {
		log.Printf("project config: memory service unavailable: %v", err)
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	enabled := false
	if ps != nil {
		if on, ok := ps.Value["value"].(bool); ok {
			enabled = on
		}
	}
	return c.JSON(http.StatusOK, map[string]bool{"voiceEnabled": enabled})
}

// --- session log (sourced from Memory conversations/history) ---

// sessionSummary mirrors the iOS SessionSummary model (snake_case field names).
// Origin records how the session started: "manual" for on-demand conversations
// (voice/text), "scheduled" for runs of scheduled agents.
type sessionSummary struct {
	Room              string  `json:"room"`
	Origin            string  `json:"origin,omitempty"`
	StartedAt         *string `json:"started_at"`
	EndedAt           *string `json:"ended_at"`
	Turns             int     `json:"turns"`
	ToolCalls         int     `json:"tool_calls"`
	Preview           *string `json:"preview"`
	AgentDefinitionID string  `json:"agent_definition_id,omitempty"`
	Title             string  `json:"title,omitempty"`
	IsArchived        bool    `json:"is_archived,omitempty"`
	// Sub-chat parentage and the rail badge inputs. Additive and optional: an
	// older app ignores them, and a caller that does not care gets the same
	// list as before.
	ParentConversationID string `json:"parent_conversation_id,omitempty"`
	Bucket               string `json:"bucket,omitempty"`
	PendingApprovals     int    `json:"pending_approvals,omitempty"`
	PendingQuestions     int    `json:"pending_questions,omitempty"`
}

// listSessions maps every memory conversation onto a SessionSummary, most
// recently active first, then merges in runs of scheduled agents (origin
// "scheduled"). A conversation whose history cannot be fetched is still
// included with zeroed counts rather than dropped. The optional ?origin=
// query param filters the merged list to one origin; ?agentId= filters it to
// sessions whose agent definition id matches.
func (s *Server) listSessions(c echo.Context) error {
	ctx := c.Request().Context()
	includeArchived := c.QueryParam("includeArchived") == "true"
	list, err := s.memory.ListConversations(ctx, includeArchived)
	if err != nil {
		log.Printf("session list: memory service unavailable: %v", err)
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	// Project-wide decision snapshots, fetched once for the whole list exactly
	// like the rail (chatRailData) so every row's badge derives from the same
	// state. Best-effort: a failed snapshot leaves the rows at "done".
	approvals, aErr := s.memory.ListToolApprovals(ctx)
	if aErr != nil {
		captureError(aErr)
		approvals = nil
	}
	questions, qErr := s.memory.ListAgentQuestions(ctx)
	if qErr != nil {
		captureError(qErr)
		questions = nil
	}
	// A child is only nested while its parent is in this same list. An orphan
	// promoted to a root keeps its own run state, mirroring the rail.
	visible := make(map[string]bool, len(list.Conversations))
	for _, conv := range list.Conversations {
		visible[conv.ID] = true
	}
	summaries := make([]sessionSummary, len(list.Conversations))
	runConcurrently(len(list.Conversations), func(i int) {
		conv := list.Conversations[i]
		sm := sessionSummary{
			Room:                 conv.ID,
			Origin:               "manual",
			AgentDefinitionID:    conv.AgentDefinitionID,
			Title:                conv.Title,
			IsArchived:           conv.IsArchived,
			ParentConversationID: conv.ParentConversationID,
			Bucket:               runBucketDone,
		}
		nested := conv.ParentConversationID != "" && visible[conv.ParentConversationID]
		if conv.CreatedAt != "" {
			sm.StartedAt = &conv.CreatedAt
		}
		if conv.UpdatedAt != "" {
			sm.EndedAt = &conv.UpdatedAt
		}
		if hist, err := s.conversationTimeline(ctx, conv.ID); err == nil {
			items := parseTimeline(hist.Items)
			for _, it := range items {
				switch it.Kind {
				case "message":
					sm.Turns++
				case "tool_call":
					sm.ToolCalls++
				}
			}
			if p := sessionPreview(items); p != "" {
				sm.Preview = &p
			}
			if nested {
				// The rail never loads a nested sub-chat's transcript for its
				// badge (ui.go); pending decisions alone decide the state.
				for _, a := range approvals {
					if a.ConversationID == conv.ID && a.Decision == "pending" {
						sm.PendingApprovals++
					}
				}
				for _, q := range questions {
					if q.ConversationID == conv.ID && (q.Response == nil || *q.Response == "") {
						sm.PendingQuestions++
					}
				}
				if sm.PendingApprovals > 0 || sm.PendingQuestions > 0 {
					sm.Bucket = runBucketNeedsInput
				}
			} else {
				st := runStateFromTimeline(items, approvals, questions, func(a ToolApprovalItem) bool {
					return a.ConversationID == conv.ID
				})
				sm.Bucket = st.bucket
				sm.PendingApprovals = len(st.pendingApprovals)
				sm.PendingQuestions = len(st.pendingQuestions)
			}
		}
		// history fetch failure: keep zeros, nil preview and the "done" bucket
		summaries[i] = sm
	})
	// Merge scheduled-agent runs (best-effort: a failure here leaves the list
	// with just the conversations rather than failing the whole request).
	summaries = append(summaries, s.scheduledSessionSummaries(ctx)...)
	summaries = append(summaries, s.boardSessionSummaries(ctx)...)
	summaries = append(summaries, s.extractionSessionSummaries(ctx)...)
	if origin := c.QueryParam("origin"); origin != "" {
		filtered := summaries[:0]
		for _, sm := range summaries {
			if sm.Origin == origin {
				filtered = append(filtered, sm)
			}
		}
		summaries = filtered
	}
	if agentID := c.QueryParam("agentId"); agentID != "" {
		filtered := summaries[:0]
		for _, sm := range summaries {
			if sm.AgentDefinitionID == agentID {
				filtered = append(filtered, sm)
			}
		}
		summaries = filtered
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		a, b := summaries[i], summaries[j]
		if at, bt := convTime(a.EndedAt), convTime(b.EndedAt); !at.Equal(bt) {
			return at.After(bt)
		}
		return convTime(a.StartedAt).After(convTime(b.StartedAt))
	})
	return c.JSON(http.StatusOK, map[string]any{"sessions": summaries})
}

// scheduledSessionSummaries maps runs of schedule-triggered agents onto
// session summaries with origin "scheduled". Only agents whose triggerType is
// "schedule" are merged (their runs are the reliable scheduled-origin signal —
// agent_runs.trigger_source is null for cron runs). Each run becomes one
// summary keyed by its run id.
func (s *Server) scheduledSessionSummaries(ctx context.Context) []sessionSummary {
	agents, err := s.memory.ListScheduledAgents(ctx)
	if err != nil {
		return nil
	}
	scheduled := make([]ScheduledAgent, 0, len(agents))
	for _, agent := range agents {
		if agent.TriggerType == "schedule" {
			scheduled = append(scheduled, agent)
		}
	}
	// Fetch each scheduled agent's runs in bounded parallel (was one sequential
	// round trip per agent). Best-effort: a failed fetch contributes no runs for
	// that agent, matching the previous per-agent `continue`.
	runsByAgent := make([][]ScheduledAgentRun, len(scheduled))
	runConcurrently(len(scheduled), func(i int) {
		runs, err := s.memory.ListScheduledAgentRuns(ctx, scheduled[i].ID)
		if err != nil {
			return
		}
		runsByAgent[i] = runs
	})
	var out []sessionSummary
	for i, agent := range scheduled {
		for _, run := range runsByAgent[i] {
			sm := sessionSummary{Room: run.ID, Origin: "scheduled"}
			if agent.AgentDefinitionID != nil {
				sm.AgentDefinitionID = *agent.AgentDefinitionID
			}
			if run.StartedAt != "" {
				sm.StartedAt = &run.StartedAt
			}
			sm.EndedAt = run.CompletedAt
			if p := scheduledRunPreview(run, agent.Name); p != "" {
				sm.Preview = &p
			}
			out = append(out, sm)
		}
	}
	return out
}

// boardSessionSummaries maps the project's object-driven (board/work) runs onto
// session summaries with origin "board". Each run becomes one summary keyed by
// its run id. The work-item board projection is fetched once and joined in Go by
// subjectObjectID == workItem.CanonicalID so each row gets a useful title without
// an N+1 per-run fetch. Best-effort: a fetch failure contributes no rows.
func (s *Server) boardSessionSummaries(ctx context.Context) []sessionSummary {
	runs, err := s.memory.ListBoardRuns(ctx, 50)
	if err != nil {
		return nil
	}
	items, _ := s.memory.ListWorkItems(ctx, "", "", 1000)
	bySubject := make(map[string]WorkItem, len(items))
	for _, it := range items {
		if it.CanonicalID != "" {
			bySubject[it.CanonicalID] = it
		}
	}
	out := make([]sessionSummary, 0, len(runs))
	for _, run := range runs {
		sm := sessionSummary{Room: run.ID, Origin: "board"}
		if run.StartedAt != "" {
			sm.StartedAt = &run.StartedAt
		}
		sm.EndedAt = run.CompletedAt
		if p := boardRunPreview(run, bySubject); p != "" {
			sm.Preview = &p
		}
		out = append(out, sm)
	}
	return out
}

// extractionSessionSummaries maps extraction jobs that recorded a transcript onto
// session summaries with origin "extraction". An extraction is a job, not a
// conversation, so it only becomes a session when the worker recorded its pipeline
// as an agent run (job.RunID); a run-less job stays a document-history row and is
// omitted here rather than being presented as something resumable. The run id is
// the session room, matching every other run-backed origin.
func (s *Server) extractionSessionSummaries(ctx context.Context) []sessionSummary {
	rows := s.extractionRunRows(ctx)
	out := make([]sessionSummary, 0, len(rows))
	for _, row := range rows {
		if row.RunID == "" {
			continue
		}
		sm := sessionSummary{Room: row.RunID, Origin: "extraction"}
		if row.StartedAt != "" {
			started := row.StartedAt
			sm.StartedAt = &started
		}
		if row.CompletedAt != "" {
			completed := row.CompletedAt
			sm.EndedAt = &completed
		}
		if row.Title != "" {
			preview := truncateRunes(row.Title, 160)
			sm.Preview = &preview
		}
		out = append(out, sm)
	}
	return out
}

// boardRunPreview picks the matched work item's key (when the run's subject
// object matches a board work item), falling back to the run summary text and
// then the agent name, truncated to 160 runes.
func boardRunPreview(run BoardRun, items map[string]WorkItem) string {
	if run.SubjectObjectID != nil {
		if it, ok := items[*run.SubjectObjectID]; ok {
			if t := strings.TrimSpace(it.Key); t != "" {
				return truncateRunes(t, 160)
			}
		}
	}
	for _, k := range []string{"summary", "result", "output", "message", "text"} {
		if v, ok := run.Summary[k].(string); ok {
			if t := strings.TrimSpace(v); t != "" {
				return truncateRunes(t, 160)
			}
		}
	}
	for _, v := range run.Summary {
		if s, ok := v.(string); ok {
			if t := strings.TrimSpace(s); t != "" {
				return truncateRunes(t, 160)
			}
		}
	}
	if run.AgentName != "" {
		return truncateRunes("Board run — "+run.AgentName, 160)
	}
	return ""
}

// scheduledRunPreview picks the first non-empty summary text of a scheduled
// run (falling back to the agent name), truncated to 160 runes.
func scheduledRunPreview(run ScheduledAgentRun, agentName string) string {
	for _, k := range []string{"summary", "result", "output", "message", "text"} {
		if v, ok := run.Summary[k].(string); ok {
			if t := strings.TrimSpace(v); t != "" {
				return truncateRunes(t, 160)
			}
		}
	}
	for _, v := range run.Summary {
		if s, ok := v.(string); ok {
			if t := strings.TrimSpace(s); t != "" {
				return truncateRunes(t, 160)
			}
		}
	}
	if agentName != "" {
		return truncateRunes("Scheduled run — "+agentName, 160)
	}
	return ""
}

// getSession maps one conversation's timeline onto iOS SessionRecords.
// Unknown rooms yield an empty timeline (not an error); other memory failures
// are 502. Conversation metadata and pending tool approvals are best-effort:
// a lookup failure leaves those fields out rather than failing the response.
func (s *Server) getSession(c echo.Context) error {
	room := c.QueryParam("room")
	if room == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "room query param required"})
	}
	ctx := c.Request().Context()
	hist, err := s.conversationTimeline(ctx, room)
	if err != nil {
		msg := err.Error()
		if isMemoryNotFound(err) || strings.Contains(msg, "invalid conversation id") {
			return c.JSON(http.StatusOK, s.sessionResponse(ctx, room, []sessionRecord{}))
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": msg})
	}
	records := make([]sessionRecord, 0, len(hist.Items))
	for _, it := range parseTimeline(hist.Items) {
		switch it.Kind {
		case "message":
			records = append(records, messageRecord(it))
		case "tool_call":
			records = append(records, toolRecord(it))
		}
	}
	return c.JSON(http.StatusOK, s.sessionResponse(ctx, room, records))
}

// sessionResponse builds the /api/session payload. Agent metadata is fetched
// best-effort from the conversation (a scheduled-run room id is not a UUID and
// simply yields no metadata); pending approvals are filtered from the project
// audit trail to this room. Both failures degrade to an absent field and never
// fail the session response.
func (s *Server) sessionResponse(ctx context.Context, room string, records []sessionRecord) map[string]any {
	out := map[string]any{"room": room, "records": records}
	if detail, err := s.memory.GetConversation(ctx, room); err == nil && detail != nil {
		if detail.AgentDefinitionID != "" {
			out["agent_definition_id"] = detail.AgentDefinitionID
		}
		if detail.Title != "" {
			out["title"] = detail.Title
		}
	}
	if approvals, err := s.memory.ListToolApprovals(ctx); err == nil {
		var pending []PendingApprovalItem
		for _, a := range approvals {
			if a.Decision != "pending" || a.ConversationID != room {
				continue
			}
			pending = append(pending, PendingApprovalItem{
				QuestionID: a.QuestionID,
				Tool:       a.ToolName,
				Input:      a.ArgsSummary,
			})
		}
		if len(pending) > 0 {
			out["pending_approvals"] = pending
		}
	}
	return out
}

// sessionRecord mirrors the iOS SessionRecord model.
type sessionRecord struct {
	Kind    string           `json:"kind"`
	Role    *string          `json:"role"`
	Text    *string          `json:"text"`
	Calls   []toolCallRecord `json:"calls"`
	Outputs []string         `json:"outputs"`
	IsError *bool            `json:"is_error"`
	// Citations are an assistant turn's grounded references ({kind, id, type,
	// label, url, key}), passed through from the timeline item so the iOS
	// chat can draw the same Sources footer the web chat does. Omitted when
	// the turn cited nothing.
	Citations json.RawMessage `json:"citations,omitempty"`
}

// toolCallRecord is one element of a tools_executed record's calls array.
type toolCallRecord struct {
	Name    string  `json:"name"`
	Args    *string `json:"args"`
	Result  *string `json:"result"`
	IsError bool    `json:"is_error"`
}

func messageRecord(it TimelineItem) sessionRecord {
	role := it.Role
	if role == "" {
		role = "assistant"
	}
	text := ""
	if it.Content != nil {
		text = it.Content.Text
		if text == "" && len(it.Content.FunctionCalls) > 0 {
			var names []string
			for _, fc := range it.Content.FunctionCalls {
				names = append(names, fc.Name)
			}
			text = "(calls: " + strings.Join(names, ", ") + ")"
		}
	}
	rec := sessionRecord{Kind: "turn", Role: &role, Text: &text}
	// The assistant side is any role but "user" (history records the agent's
	// name as the role), matching how the iOS SessionRecord reads it.
	if role != "user" && len(decodeCitations(it.Citations)) > 0 {
		rec.Citations = it.Citations
	}
	return rec
}

func toolRecord(it TimelineItem) sessionRecord {
	args := compactJSON(it.ToolInput)
	result := compactJSON(it.ToolOutput)
	isErr := toolError(it.ToolStatus, string(it.ToolOutput)) != ""
	rec := sessionRecord{
		Kind:    "tools_executed",
		Calls:   []toolCallRecord{{Name: it.ToolName, Args: args, Result: result, IsError: isErr}},
		IsError: &isErr,
	}
	if result != nil {
		rec.Outputs = []string{*result}
	}
	return rec
}

// compactJSON renders raw JSON as a compact string, or nil when null/empty.
func compactJSON(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil
	}
	s := buf.String()
	return &s
}

// sessionPreview picks the first user message text (falling back to any first
// message text), truncated to 160 runes with a "…" suffix.
func sessionPreview(items []TimelineItem) string {
	var first string
	for _, it := range items {
		if it.Kind != "message" || it.Content == nil {
			continue
		}
		if first == "" {
			first = it.Content.Text
		}
		if it.Role == "user" {
			return truncateRunes(it.Content.Text, 160)
		}
	}
	return truncateRunes(first, 160)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func convTime(s *string) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return time.Time{}
	}
	return t
}
