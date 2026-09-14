package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// --- session log (sourced from Memory conversations/history) ---

// sessionSummary mirrors the iOS SessionSummary model (snake_case field names).
// Origin records how the session started: "manual" for on-demand conversations
// (voice/text), "scheduled" for runs of scheduled agents.
type sessionSummary struct {
	Room      string  `json:"room"`
	Origin    string  `json:"origin,omitempty"`
	StartedAt *string `json:"started_at"`
	EndedAt   *string `json:"ended_at"`
	Turns     int     `json:"turns"`
	ToolCalls int     `json:"tool_calls"`
	Preview   *string `json:"preview"`
}

// listSessions maps every memory conversation onto a SessionSummary, most
// recently active first, then merges in runs of scheduled agents (origin
// "scheduled"). A conversation whose history cannot be fetched is still
// included with zeroed counts rather than dropped. The optional ?origin=
// query param filters the merged list to one origin.
func (s *Server) listSessions(c echo.Context) error {
	ctx := c.Request().Context()
	list, err := s.memory.ListConversations(ctx)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	summaries := make([]sessionSummary, len(list.Conversations))
	runConcurrently(len(list.Conversations), func(i int) {
		conv := list.Conversations[i]
		sm := sessionSummary{Room: conv.ID, Origin: "manual"}
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
		}
		// history fetch failure: keep zeros + nil preview
		summaries[i] = sm
	})
	// Merge scheduled-agent runs (best-effort: a failure here leaves the list
	// with just the conversations rather than failing the whole request).
	summaries = append(summaries, s.scheduledSessionSummaries(ctx)...)
	if origin := c.QueryParam("origin"); origin != "" {
		filtered := summaries[:0]
		for _, sm := range summaries {
			if sm.Origin == origin {
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
// are 502.
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
			return c.JSON(http.StatusOK, map[string]any{"room": room, "records": []sessionRecord{}})
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
	return c.JSON(http.StatusOK, map[string]any{"room": room, "records": records})
}

// sessionRecord mirrors the iOS SessionRecord model.
type sessionRecord struct {
	Kind    string           `json:"kind"`
	Role    *string          `json:"role"`
	Text    *string          `json:"text"`
	Calls   []toolCallRecord `json:"calls"`
	Outputs []string         `json:"outputs"`
	IsError *bool            `json:"is_error"`
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
	return sessionRecord{Kind: "turn", Role: &role, Text: &text}
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

// --- memory proxy ---

// agentDefinitionByName resolves an agent by name. Returns (nil, nil) when the
// name is unknown.
func (s *Server) agentDefinitionByName(ctx context.Context, name string) (*AgentDefinition, error) {
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		if a.Name == name {
			return s.memory.GetAgentDefinition(ctx, a.ID)
		}
	}
	return nil, nil
}

// memoryToolNames is the tool namespace exposed by the MCP server named
// "memory" (memory's built-in graph/search/remember/forget toolset, from
// memory's agentcompat internalToolNames + the remember/forget tools). An
// agent references the memory server iff its Tools whitelist names one of
// these (exact match or glob).
//
// Assumption: AgentDefinition.Tools is a flat whitelist of MCP tool names (not
// server IDs), so a tool-name match is the reference relationship. If the
// "memory" server isn't registered at all we fail closed (no agent can
// reference it).
var memoryToolNames = []string{
	"remember", "forget",
	"entity-create", "entity-update", "entity-delete", "entity-query",
	"entity-search", "entity-restore", "entity-history", "entity-edges-get",
	"entity-type-list",
	"relationship-create", "relationship-list", "relationship-update", "relationship-delete",
	"search-hybrid", "search-semantic", "search-similar", "search-knowledge",
	"graph-traverse", "tag-list",
}

// agentHasMemory reports whether def references the MCP server named "memory".
func (s *Server) agentHasMemory(ctx context.Context, def *AgentDefinition) (bool, error) {
	servers, err := s.memory.ListMCPServers(ctx)
	if err != nil {
		return false, err
	}
	hasMemoryServer := false
	for _, srv := range servers {
		if srv.Name == "memory" {
			hasMemoryServer = true
			break
		}
	}
	if !hasMemoryServer {
		return false, nil
	}
	for _, t := range def.Tools {
		for _, name := range memoryToolNames {
			if t == name {
				return true, nil
			}
			if strings.ContainsAny(t, "*?[") {
				if ok, _ := path.Match(t, name); ok {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// memoryCapability implements GET /api/memories/capability?agent=<name>.
func (s *Server) memoryCapability(c echo.Context) error {
	agent := c.QueryParam("agent")
	if agent == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "agent required"})
	}
	ctx := c.Request().Context()
	def, err := s.agentDefinitionByName(ctx, agent)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	if def == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "unknown agent"})
	}
	has, err := s.agentHasMemory(ctx, def)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]any{"agent": agent, "hasMemory": has})
}

// memoryItem is the client-facing memory shape (iOS Memory model): snake/camel
// field names matching the legacy /api/memories wire contract.
type memoryItem struct {
	ID         string  `json:"id"`
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

// listMemories implements GET /api/memories?agent=<name>&query=<text>.
func (s *Server) listMemories(c echo.Context) error {
	agent := c.QueryParam("agent")
	if agent == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "agent required"})
	}
	ctx := c.Request().Context()
	def, err := s.agentDefinitionByName(ctx, agent)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	if def == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "unknown agent"})
	}
	has, err := s.agentHasMemory(ctx, def)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	if !has {
		return c.JSON(http.StatusOK, map[string]any{"memories": []memoryItem{}})
	}

	var memories []Memory
	if query := c.QueryParam("query"); query != "" {
		// Search: unified search endpoint (legacy admin.py _memory_search).
		memories, err = s.memory.SearchMemories(ctx, query)
	} else {
		// No query = browse-all. The unified search endpoint rejects empty
		// queries, so this mirrors legacy admin.py _memory_list via the
		// entity-query MCP tool.
		memories, err = s.memory.ListMemories(ctx)
	}
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	items := make([]memoryItem, 0, len(memories))
	for _, m := range memories {
		items = append(items, memoryItem{ID: m.ID, Content: m.Content, Category: m.Category, Confidence: m.Confidence})
	}
	return c.JSON(http.StatusOK, map[string]any{"memories": items})
}
