package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/labstack/echo/v4"
)

// --- run transcript as chat timeline ---
//
// Scheduled-agent runs live in agent_runs, not conversations: there is no
// /api/chat history for them. runTimeline synthesizes a run's stored
// transcript (messages + tool calls + agent questions) into the same item
// shape the chat workspace renders conversation history with, so a scheduled
// run can be opened / continued as a normal chat session.

// runMessageItem is one synthesized chat timeline message item for a run
// message. Only the extracted text is carried (function_call/response noise is
// dropped — tool calls render as their own chips below); role + created_at +
// step_number mirror memory's /api/chat history items so the shared client
// renderer and sortTimeline handle them identically.
type runMessageItem struct {
	Kind       string         `json:"kind"`
	RunID      string         `json:"run_id,omitempty"`
	StepNumber int            `json:"step_number"`
	CreatedAt  string         `json:"created_at,omitempty"`
	Role       string         `json:"role"`
	Content    map[string]any `json:"content"`
}

// runToolItem is one synthesized chat timeline tool_call item: a run tool
// invocation (rendered as a chip), or an ask_user invocation carrying the
// agent's question so the client renders the interactive question card.
type runToolItem struct {
	Kind       string         `json:"kind"`
	RunID      string         `json:"run_id,omitempty"`
	StepNumber int            `json:"step_number"`
	CreatedAt  string         `json:"created_at,omitempty"`
	ToolName   string         `json:"tool_name"`
	ToolStatus string         `json:"tool_status,omitempty"`
	ToolInput  map[string]any `json:"tool_input,omitempty"`
	ToolOutput map[string]any `json:"tool_output,omitempty"`
}

// runTimelineItems synthesizes the run's transcript as timeline items in
// chronological order. answered keeps a question's stored response so the
// builder can annotate ask_user items directly (per-run, precise).
//
//   - messages with extractable text become "message" items (role + content),
//   - non-ask_user tool calls become "tool_call" chips,
//   - ask_user tool calls with a question id become "tool_call" items whose
//     tool_input carries the question (the chat renderer shows the card) and
//     whose tool_output.question_id lets the client answer it,
//   - agent questions with no matching ask_user tool call are appended as
//     synthetic ask_user items so a paused run never loses its prompt,
//   - answered ask_user items are annotated with tool_output.response so the
//     card renders in its static "Answered" state.
func runTimelineItems(full *AgentRunFull, questions []AgentQuestionItem) []json.RawMessage {
	if full == nil {
		return nil
	}
	run := full.Run
	runID := ""
	fallbackCreated := ""
	if run != nil {
		runID = run.ID
		fallbackCreated = run.StartedAt
	}
	answered := make(map[string]string, len(questions))
	byID := make(map[string]AgentQuestionItem, len(questions))
	for _, q := range questions {
		byID[q.ID] = q
		if q.Response != nil && *q.Response != "" {
			answered[q.ID] = *q.Response
		}
	}

	entries := make([]timelineEntry, 0, len(full.Messages)+len(full.ToolCalls)+len(questions))
	covered := make(map[string]bool, len(questions)) // question ids with an ask_user item

	// 1. ask_user tool calls first-class, other tool calls as chips.
	for _, tc := range full.ToolCalls {
		if tc == nil {
			continue
		}
		out := tc.Output
		if tc.ToolName == "ask_user" {
			qid, _ := out["question_id"].(string)
			if qid == "" {
				if !toolFinished(tc.Status) {
					// Still pending (or the recorder dropped the id): the
					// stored agent-question row below is the source of truth
					// for the prompt — a half-started ask_user chip would just
					// spin forever.
					continue
				}
				// ask_user finished without a resumable prompt (e.g. a
				// validation error on bad options): surface it as a plain chip
				// so the failure is visible.
				entries = append(entries, timelineEntry{
					created: keyTime(tc.CreatedAt, fallbackCreated),
					step:    tc.StepNumber,
					idx:     len(entries),
					raw:     mustJSON(runToolItem{Kind: "tool_call", RunID: runID, StepNumber: tc.StepNumber, CreatedAt: orZero(tc.CreatedAt, fallbackCreated), ToolName: tc.ToolName, ToolStatus: toolStatus(tc.Status), ToolInput: tc.Input, ToolOutput: out}),
				})
				continue
			}
			covered[qid] = true
			out = cloneMap(out)
			if ans, ok := answered[qid]; ok {
				out["response"] = ans
			}
			in := tc.Input
			if questionText(in) == "" {
				// The recorded tool args lack the question (or are nil) — fall
				// back to the stored agent-question row so the card still has
				// something to ask.
				if q, ok := byID[qid]; ok {
					in = questionToolInput(q)
				}
			}
			entries = append(entries, timelineEntry{
				created: keyTime(tc.CreatedAt, fallbackCreated),
				step:    tc.StepNumber,
				idx:     len(entries),
				raw:     mustJSON(runToolItem{Kind: "tool_call", RunID: runID, StepNumber: tc.StepNumber, CreatedAt: orZero(tc.CreatedAt, fallbackCreated), ToolName: "ask_user", ToolStatus: "completed", ToolInput: in, ToolOutput: out}),
			})
			continue
		}
		entries = append(entries, timelineEntry{
			created: keyTime(tc.CreatedAt, fallbackCreated),
			step:    tc.StepNumber,
			idx:     len(entries),
			raw:     mustJSON(runToolItem{Kind: "tool_call", RunID: runID, StepNumber: tc.StepNumber, CreatedAt: orZero(tc.CreatedAt, fallbackCreated), ToolName: tc.ToolName, ToolStatus: toolStatus(tc.Status), ToolInput: tc.Input, ToolOutput: out}),
		})
	}

	// 2. messages with extractable text (assistant thinking/tool calls inside
	// content are already represented by their tool-call chips above).
	for _, m := range full.Messages {
		if m == nil {
			continue
		}
		text := runMessageText(m.Content)
		if text == "" || m.Role == "tool" {
			continue
		}
		content := map[string]any{"text": text}
		entries = append(entries, timelineEntry{
			created: keyTime(m.CreatedAt, fallbackCreated),
			step:    m.StepNumber,
			idx:     len(entries),
			raw:     mustJSON(runMessageItem{Kind: "message", RunID: runID, StepNumber: m.StepNumber, CreatedAt: orZero(m.CreatedAt, fallbackCreated), Role: m.Role, Content: content}),
		})
	}

	// 3. questions with no matching ask_user tool call: append as ask_user
	// items at the end of the recorded activity (a paused run has no newer
	// events, so the tail is the right position).
	lastStep := 0
	latest := timeKey(fallbackCreated)
	for _, e := range entries {
		if e.step > lastStep {
			lastStep = e.step
		}
		if e.created > latest {
			latest = e.created
		}
	}
	for _, q := range questions {
		if covered[q.ID] {
			continue
		}
		out := map[string]any{"question_id": q.ID}
		if ans, ok := answered[q.ID]; ok {
			out["response"] = ans
		}
		lastStep++
		entries = append(entries, timelineEntry{
			created: latest,
			step:    lastStep,
			idx:     len(entries),
			raw:     mustJSON(runToolItem{Kind: "tool_call", RunID: runID, StepNumber: lastStep, CreatedAt: string(latest), ToolName: "ask_user", ToolStatus: "completed", ToolInput: questionToolInput(q), ToolOutput: out}),
		})
		covered[q.ID] = true
	}

	// 4. chronological order (created_at, then step_number, preserving append
	// order on exact ties so tool call and its result message stay adjacent).
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].created != entries[j].created {
			return entries[i].created < entries[j].created
		}
		if entries[i].step != entries[j].step {
			return entries[i].step < entries[j].step
		}
		return entries[i].idx < entries[j].idx
	})
	items := make([]json.RawMessage, 0, len(entries))
	for _, e := range entries {
		items = append(items, e.raw)
	}
	return items
}

// runTimeline builds the chat-shaped history for one run id: its stored full
// transcript plus its agent questions. A questions fetch failure is
// best-effort (the transcript still renders; only the Continue affordance for
// an ask_user without a recorded tool call would be missing).
func (s *Server) runTimeline(ctx context.Context, runID string) (*ConversationHistory, error) {
	full, err := s.memory.GetRunFull(ctx, runID)
	if err != nil {
		return nil, err
	}
	questions, err := s.memory.GetRunQuestions(ctx, runID)
	if err != nil {
		questions = nil
	}
	return &ConversationHistory{ConversationID: runID, Items: runTimelineItems(full, questions)}, nil
}

// getRunHistory implements GET /api/runs/:runId/history — the run transcript
// as chat timeline items, mirroring getConversationHistory (markdown-render
// assistant messages, then surface pending tool approvals scoped to the run).
func (s *Server) getRunHistory(c echo.Context) error {
	ctx := c.Request().Context()
	runID := c.Param("runId")
	out, err := s.runTimeline(ctx, runID)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	out.Items = renderHistoryHTML(out.Items)
	// Surface pending tool approvals for this run (paused awaiting a decision)
	// so the transcript shows the interactive Approve/Reject/Cancel card.
	// Non-fatal: on failure the history still renders without approval cards.
	if approvals, aerr := s.memory.ListToolApprovals(ctx); aerr == nil {
		for _, a := range approvals {
			if a.RunID == runID && a.Decision == "pending" {
				out.PendingApprovals = append(out.PendingApprovals, PendingApprovalItem{
					QuestionID: a.QuestionID,
					Tool:       a.ToolName,
					Input:      a.ArgsSummary,
				})
			}
		}
	}
	data, err := marshalNoEscape(out)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, data)
}

// --- helpers ---

// timelineEntry is one candidate run timeline item with its sort keys.
type timelineEntry struct {
	created timeKey
	step    int
	idx     int
	raw     json.RawMessage
}

// timeKey is a sortable timestamp: RFC3339 or a comparable fallback.
type timeKey string

// keyTime resolves a sortable timestamp, falling back to the run's start time
// when an item carries none.
func keyTime(t, fallback string) timeKey {
	if t == "" {
		t = fallback
	}
	return timeKey(t)
}

func orZero(t, fallback string) string {
	if t == "" {
		return fallback
	}
	return t
}

// toolStatus normalizes a recorded tool status to the chat chip vocabulary
// ("completed"/"running"/"error"); an empty status means the call finished.
func toolStatus(status string) string {
	if status == "" {
		return "completed"
	}
	return status
}

// toolFinished reports whether an ask_user invocation has stopped running
// (empty / in-flight statuses are treated as unfinished: a paused question's
// prompt lives in the agent-question row, not in a spinning chip).
func toolFinished(status string) bool {
	switch status {
	case "", "running", "started", "in_progress", "pending", "queued":
		return false
	default:
		return true
	}
}

// questionText returns the ask text of a tool-input map, or "".
func questionText(in map[string]any) string {
	if in == nil {
		return ""
	}
	q, _ := in["question"].(string)
	return q
}

// questionToolInput maps a stored agent question onto the ask_user tool args
// shape the chat question card renders (snake_case keys). The interaction
// type comes from the question row (buttons / select / multi_select / text)
// when set; otherwise it defaults to buttons when options exist and free text
// when not.
func questionToolInput(q AgentQuestionItem) map[string]any {
	in := map[string]any{"question": q.Question}
	interactionType := "text"
	if len(q.Options) > 0 {
		opts := make([]any, 0, len(q.Options))
		for _, o := range q.Options {
			opt := map[string]any{"label": o.Label, "value": o.Value}
			if o.Description != "" {
				opt["description"] = o.Description
			}
			opts = append(opts, opt)
		}
		in["options"] = opts
		interactionType = "buttons"
	} else if q.Placeholder != "" {
		in["placeholder"] = q.Placeholder
	}
	if q.InteractionType != "" {
		interactionType = q.InteractionType
	}
	in["interaction_type"] = interactionType
	return in
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
