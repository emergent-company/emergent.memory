package main

import (
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/go-daisy/components/nav"
	"github.com/labstack/echo/v4"
)

// --- Run session UI ---

// runPageData is the payload for RunPage: the full run transcript plus the
// trace-derived metadata shown in the run header, or the load error that
// prevented fetching the run.
type runPageData struct {
	Run     *AgentRunFull
	Trace   runTraceMeta
	LoadErr error
}

// runTraceMeta is the trace-derived metadata shown in the run header:
// the model and token totals from the run's OTEL spans, plus the trace id
// for the "view trace" link. Zero values mean "unknown".
type runTraceMeta struct {
	TraceID      string
	Model        string
	InputTokens  int64
	OutputTokens int64
}

// runTraceMetaFromRun extracts runTraceMeta from a run's OTEL spans: the
// model is taken from the first span that names one, input/output tokens are
// summed across all spans, and the trace id comes from the run itself.
// Nil-safe: a nil run yields all-zero metadata.
func runTraceMetaFromRun(r *AgentRun) runTraceMeta {
	if r == nil {
		return runTraceMeta{}
	}
	var m runTraceMeta
	m.TraceID = r.TraceID
	for _, s := range r.Spans {
		if m.Model == "" && s.Model != "" {
			m.Model = s.Model
		}
		m.InputTokens += s.InputTokens
		m.OutputTokens += s.OutputTokens
	}
	return m
}

// runDurationSeconds returns the wall-clock duration of a run in seconds
// (CompletedAt − StartedAt, both RFC3339), or 0 when either is missing,
// unparseable, or completion precedes start.
func runDurationSeconds(run *ScheduledAgentRun) float64 {
	if run == nil || run.StartedAt == "" || run.CompletedAt == nil || *run.CompletedAt == "" {
		return 0
	}
	start, err1 := time.Parse(time.RFC3339, run.StartedAt)
	end, err2 := time.Parse(time.RFC3339, *run.CompletedAt)
	if err1 != nil || err2 != nil || !end.After(start) {
		return 0
	}
	return end.Sub(start).Seconds()
}

// uiRun renders one run's session: a server-rendered metadata header plus a
// client-rendered transcript shell (#chat-root[data-run]) that chat.js fills
// from GET /api/runs/:runId/history. A missing run or a memory failure
// renders the whole-page error state; a failed trace fetch is best-effort
// (the header simply omits model/token/trace metadata).
func (s *Server) uiRun(c echo.Context) error {
	ctx := c.Request().Context()
	runID := c.Param("runId")
	run, err := s.memory.GetRunFull(ctx, runID)
	if err != nil {
		return s.page(c, pageTitle("Run"), RunPage(runPageData{LoadErr: userFacingError(err)}))
	}
	data := runPageData{Run: run}
	if tr, err := s.memory.GetAgentRun(ctx, runID); err == nil && tr != nil {
		data.Trace = runTraceMetaFromRun(tr)
	}
	return s.page(c, pageTitle("Run"), RunPage(data))
}

// runCrumbTrail builds the run page breadcrumb: Schedules → the agent's
// schedule (linked when the run knows its agent id).
//
// The run itself is deliberately not a crumb. Like an object opened from an
// extraction run, the page title and the run metadata below the header carry
// the run's identity, and the trail stops at its parent schedule. That also
// means the trail has no active crumb: the schedule is a navigable ancestor,
// not the current page.
func runCrumbTrail(run *ScheduledAgentRun) []nav.BreadcrumbItem {
	items := []nav.BreadcrumbItem{{Label: "Schedules", Href: "/schedules"}}
	if run != nil && run.AgentName != "" {
		schedule := nav.BreadcrumbItem{Label: run.AgentName}
		if run.AgentID != "" {
			schedule.Href = "/schedules/" + url.PathEscape(run.AgentID)
		}
		items = append(items, schedule)
	}
	return items
}

// runMessageText extracts best-effort text from a run message's content map.
// ADK runs store {"text", "function_calls", "function_responses"}; some
// producers emit an ADK/ACP-style "parts" array of {text, functionCall,
// functionResponse} objects. Anything unrecognised degrades to "".
func runMessageText(content map[string]any) string {
	if content == nil {
		return ""
	}
	if t, ok := content["text"].(string); ok && strings.TrimSpace(t) != "" {
		return strings.TrimSpace(t)
	}
	if parts, ok := content["parts"].([]any); ok {
		var b strings.Builder
		for _, p := range parts {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := m["text"].(string); ok && strings.TrimSpace(t) != "" {
				b.WriteString(strings.TrimSpace(t))
				b.WriteString("\n")
			}
			if fc, ok := m["functionCall"].(map[string]any); ok {
				if name, _ := fc["name"].(string); name != "" {
					b.WriteString("called " + name + "\n")
				}
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

// legacyStageLabel recognises the label an extraction transcript used to embed
// in its message text — "**EntityExtractor** · stage output\n\n<text>" — and
// splits it into the structured provenance the transcript renders today. Rows
// written before that provenance was structured are immutable (the recorded
// text is what it is), but their label can still be lifted out so it renders as
// a label instead of literal markup, and so the chain-of-thought heuristic stops
// treating it as the message's reasoning.
//
// Only the exact phrases the recorder emitted are recognised, so a genuine
// reply that opens with a bold run is left alone.
func legacyStageLabel(text string) (stage, kind, rest string, ok bool) {
	line, remainder, found := strings.Cut(text, "\n")
	if !found {
		return "", "", text, false
	}
	line = strings.TrimSpace(line)
	remainder = strings.TrimLeft(remainder, "\n")
	if line == "**Extraction failed**" {
		return "Extraction", "error", remainder, true
	}
	if !strings.HasPrefix(line, "**") {
		return "", "", text, false
	}
	body := line[2:]
	end := strings.Index(body, "**")
	if end <= 0 {
		return "", "", text, false
	}
	tail := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body[end+2:]), "·"))
	kinds := map[string]string{
		"extraction prompt": "prompt",
		"model response":    "output",
		"stage output":      "output",
		"error":             "error",
	}
	kind, known := kinds[tail]
	if !known {
		return "", "", text, false
	}
	return strings.TrimSpace(body[:end]), kind, remainder, true
}

// runAgentName is the agent a run transcript attributes itself to. The run's
// own name is preferred; "Run" is the last-resort label for a payload that did
// not carry one, so a message is never labelled with the operator's identity.
func runAgentName(run *ScheduledAgentRun) string {
	if run != nil && strings.TrimSpace(run.AgentName) != "" {
		return run.AgentName
	}
	return "Run"
}

// legacyRunHeaderText reports whether text opens with the run header the
// extraction recorder wrote before provenance was structured:
// "Extraction of document <id> · job `<id>`". That message was recorded as a
// user turn, so without lifting it the transcript shows a "You" bubble the
// operator never typed. Deliberately narrow: it needs the "Extraction of "
// prefix and the "· job `" marker on the first line.
func legacyRunHeaderText(text string) bool {
	line, _, _ := strings.Cut(text, "\n")
	return strings.HasPrefix(line, "Extraction of ") && strings.Contains(line, " · job `")
}

// runDescription builds the header subtitle: which agent ran and when.
func runDescription(run *ScheduledAgentRun) string {
	name := run.AgentName
	if name == "" {
		name = "a scheduled agent"
	}
	s := "Run of " + name + " — started " + relTime(run.StartedAt)
	if run.CompletedAt != nil && *run.CompletedAt != "" {
		s += ", finished " + relTime(*run.CompletedAt)
	}
	return s
}
