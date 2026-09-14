package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// --- typed timeline items ---

// MessageContent is the content object carried by message timeline items. It
// may hold text and/or function_calls.
type MessageContent struct {
	Text          string             `json:"text,omitempty"`
	FunctionCalls []FunctionCallItem `json:"function_calls,omitempty"`
}

// FunctionCallItem is one entry of content.function_calls.
type FunctionCallItem struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// TimelineItem is a typed view of one raw timeline entry. Kind selects the
// item type; the remaining fields are populated per kind and omitted from
// JSON output otherwise. tool_input/tool_output are raw JSON objects (not
// strings) in the live API, so they stay as json.RawMessage.
type TimelineItem struct {
	Kind        string          `json:"kind"`
	RunID       string          `json:"run_id,omitempty"`
	StepNumber  int             `json:"step_number"`
	CreatedAt   string          `json:"created_at,omitempty"`
	RunStatus   string          `json:"run_status,omitempty"`
	RunModel    string          `json:"run_model,omitempty"`
	CompletedAt string          `json:"completed_at,omitempty"`
	Role        string          `json:"role,omitempty"`
	Content     *MessageContent `json:"content,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	ToolInput   json.RawMessage `json:"tool_input,omitempty"`
	ToolOutput  json.RawMessage `json:"tool_output,omitempty"`
	ToolStatus  string          `json:"tool_status,omitempty"`
}

// parseTimeline unmarshals raw timeline items into typed items, skipping any
// entry that cannot be decoded (best effort).
func parseTimeline(items []json.RawMessage) []TimelineItem {
	out := make([]TimelineItem, 0, len(items))
	for _, raw := range items {
		var it TimelineItem
		if err := json.Unmarshal(raw, &it); err != nil {
			continue
		}
		out = append(out, it)
	}
	return out
}

// --- error detection ---

// toolError inspects a tool result and returns a human-readable error string,
// or "" when the tool completed without an error.
func toolError(status, output string) string {
	if status != "" && status != "completed" {
		return status
	}
	if output == "" {
		return ""
	}
	var obj struct {
		Failed  *int   `json:"failed"`
		Success *bool  `json:"success"`
		Error   string `json:"error"`
		Message string `json:"message"`
		Results []struct {
			Success *bool  `json:"success"`
			Error   string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(output), &obj); err != nil {
		return ""
	}
	detected := (obj.Failed != nil && *obj.Failed > 0) || (obj.Success != nil && !*obj.Success)
	var msgs []string
	for _, r := range obj.Results {
		if (r.Success != nil && !*r.Success) || r.Error != "" {
			detected = true
			if r.Error != "" {
				msgs = append(msgs, r.Error)
			}
		}
	}
	if obj.Error != "" {
		detected = true
		msgs = append(msgs, obj.Error)
	}
	if obj.Message != "" {
		detected = true
		msgs = append(msgs, obj.Message)
	}
	if !detected {
		return ""
	}
	if len(msgs) == 0 {
		return "failed"
	}
	return strings.Join(msgs, "; ")
}

// --- text formatting ---

type runGroup struct {
	runID string
	items []TimelineItem
	// spans is the run's OpenTelemetry trace waterfall (flattened spans),
	// populated only for the session viewer when a trace exists. Text-dump and
	// JSON-dump paths leave it nil.
	spans []TraceSpan
}

// groupByRun buckets items by run_id, keeping first-seen run order and the
// original item order within each run.
func groupByRun(items []TimelineItem) []runGroup {
	var groups []runGroup
	index := make(map[string]int)
	anon := 0
	for _, it := range items {
		if it.RunID == "" {
			anon++
			groups = append(groups, runGroup{items: []TimelineItem{it}})
			continue
		}
		if i, ok := index[it.RunID]; ok {
			groups[i].items = append(groups[i].items, it)
		} else {
			index[it.RunID] = len(groups)
			groups = append(groups, runGroup{runID: it.RunID, items: []TimelineItem{it}})
		}
	}
	return groups
}

// formatTextDump renders a deterministic plaintext dump of one session.
func formatTextDump(detail *ConversationDetail, items []TimelineItem) string {
	var b strings.Builder
	b.WriteString("Session: " + detail.Title + " (" + detail.ID + ")\n")
	b.WriteString("Agent:   " + detail.AgentDefinitionID + "\n")
	b.WriteString("Created: " + detail.CreatedAt + "\n")
	b.WriteString("Updated: " + detail.UpdatedAt + "\n")
	for i, g := range groupByRun(items) {
		b.WriteString("\n")
		b.WriteString(runHeader(i+1, g))
		for _, it := range g.items {
			if t := itemText(it); t != "" {
				b.WriteString("\n\n" + t)
			}
		}
	}
	return b.String()
}

func runHeader(n int, g runGroup) string {
	var seg []string
	model, status, dur := g.runMeta()
	if model != "" {
		seg = append(seg, "model="+model)
	}
	if status != "" {
		seg = append(seg, "status="+status)
	}
	if dur != "" {
		seg = append(seg, dur)
	}
	body := "run " + strconv.Itoa(n)
	if len(seg) > 0 {
		body += " · " + strings.Join(seg, " · ")
	}
	return "━━ " + body + " ━━"
}

// runMeta extracts the model and status (from run_start, first non-empty
// otherwise) and the wall-clock duration between run_start and run_end,
// rendered as a compact human duration (e.g. "42.5s", "5m", "2h 15m",
// "1d 18h"). dur is "" when unparseable.
func (g runGroup) runMeta() (model, status, dur string) {
	var start, end TimelineItem
	for _, it := range g.items {
		if model == "" && it.RunModel != "" {
			model = it.RunModel
		}
		if status == "" && it.RunStatus != "" {
			status = it.RunStatus
		}
		switch it.Kind {
		case "run_start":
			start = it
		case "run_end":
			end = it
		}
	}
	if start.CreatedAt == "" {
		return model, status, ""
	}
	endTime := end.CompletedAt
	if endTime == "" {
		endTime = end.CreatedAt
	}
	if endTime == "" {
		return model, status, ""
	}
	s, err1 := time.Parse(time.RFC3339, start.CreatedAt)
	e, err2 := time.Parse(time.RFC3339, endTime)
	if err1 != nil || err2 != nil || !e.After(s) {
		return model, status, ""
	}
	d := e.Sub(s).Seconds()
	dur = formatRunDuration(d)
	return model, status, dur
}

// formatRunDuration renders a wall-clock duration in seconds as a compact
// human string, escalating units: "42.5s" (< 1m), "5m" (< 1h), "2h 15m"
// (< 1d), then "1d 18h". Sub-minute values keep one decimal; coarser units
// drop to whole minutes/hours.
func formatRunDuration(sec float64) string {
	total := int(math.Round(sec))
	switch {
	case total < 60:
		return strconv.FormatFloat(math.Round(sec*10)/10, 'f', 1, 64) + "s"
	case total < 3600:
		return strconv.Itoa(total/60) + "m"
	case total < 86400:
		h := total / 3600
		m := (total % 3600) / 60
		if m == 0 {
			return strconv.Itoa(h) + "h"
		}
		return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
	default:
		d := total / 86400
		h := (total % 86400) / 3600
		if h == 0 {
			return strconv.Itoa(d) + "d"
		}
		return strconv.Itoa(d) + "d " + strconv.Itoa(h) + "h"
	}
}

// itemText renders one timeline item; run_start/run_end carry no body text.
func itemText(it TimelineItem) string {
	switch it.Kind {
	case "message":
		label := it.Role
		if label == "" {
			label = "assistant"
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
		return "[" + label + "] " + text
	case "tool_call":
		var b strings.Builder
		b.WriteString("[tool] " + it.ToolName + " — " + it.ToolStatus + "\n")
		b.WriteString("  input:  " + string(it.ToolInput) + "\n")
		b.WriteString("  output: " + string(it.ToolOutput))
		if err := toolError(it.ToolStatus, string(it.ToolOutput)); err != "" {
			b.WriteString("\n  error:  " + err)
		}
		return b.String()
	default:
		return ""
	}
}

// --- handler ---

type dumpJSON struct {
	Meta     *ConversationDetail `json:"meta"`
	Timeline []TimelineItem      `json:"timeline"`
}

func (s *Server) getConversationDump(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	detail, err := s.memory.GetConversation(ctx, id)
	if err != nil {
		return dumpError(c, err)
	}
	history, err := s.memory.GetConversationHistory(ctx, id)
	if err != nil {
		return dumpError(c, err)
	}
	if len(history.Items) == 0 {
		history.Items = messageTimelineItems(detail.Messages)
	}
	items := parseTimeline(history.Items)
	if c.QueryParam("format") == "json" {
		return c.JSON(http.StatusOK, dumpJSON{Meta: detail, Timeline: items})
	}
	return c.String(http.StatusOK, formatTextDump(detail, items))
}

// dumpError maps "session not found" style memory errors to 404, everything
// else (memory down, etc.) to 502. Memory returns 404 "not_found" for a valid
// but missing id and 400 "bad_request: invalid conversation id" for a
// malformed id — both mean the client asked for a session that isn't there.
func dumpError(c echo.Context, err error) error {
	msg := err.Error()
	if isMemoryNotFound(err) || strings.Contains(msg, "invalid conversation id") {
		return c.JSON(http.StatusNotFound, map[string]string{"error": msg})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": msg})
}
