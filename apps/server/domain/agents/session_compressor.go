package agents

// session_compressor.go — LLM-based ADK session compression (Phase 2 of #209).
//
// When a resumed session no longer fits the model's context window, the
// executor drops the oldest events (the sliding-window trim in runPipeline).
// Compression turns that drop into a summary: the events that are about to be
// discarded are summarised by a fast LLM into a compact placeholder, and the
// retained tail is rebuilt verbatim so the continuation still sees the recent
// turns it would otherwise have seen.
//
// Design notes that are load-bearing:
//
//   - Only the DROPPED events are summarised. The caller passes the exact set
//     it is about to remove. Summarising the survivors (the previous behaviour)
//     described the wrong half of the conversation and lost the oldest history
//     — goals and original instructions — without ever summarising it.
//   - A prior [CONTEXT SUMMARY] in the dropped set is passed back as "extend
//     this". Without it, compaction N+1 re-derives the summary from the summary
//     and drifts.
//   - Tool calls are indexed, not discarded. For a graph agent the tool record
//     IS the work: which objects were created and with which ids. Each dropped
//     call contributes one bounded line of name + arguments.
//   - The rebuild is still delete + create (ADK has no event-log truncation),
//     but the act is recorded in kb.agent_session_compactions so the session's
//     ancestry is queryable. kb.agent_run_messages / kb.agent_run_tool_calls
//     keep the full transcript independently of the working session.
//
// Anti-thrash guard: if the summary alone is > 30 % of the target budget the
// compressor leaves the session unchanged and lets the plain sliding window
// handle it.
//
// Return contract: compressSession returns (nil, nil) when it left the session
// untouched; callers keep their current session in that case.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// compressSummaryMaxRatio is the anti-thrash guard: if the generated
	// summary token count exceeds this fraction of the context window the
	// compressor bails out and leaves the session unchanged.
	compressSummaryMaxRatio = 0.30

	// compressorSummaryTag marks the synthetic placeholder that carries a
	// summary into the rebuilt session. It is also how a later pass recognises
	// a prior summary to extend rather than re-derive.
	compressorSummaryTag = "[CONTEXT SUMMARY]"

	// compressorAckText is the synthetic model acknowledgement that follows
	// the summary placeholder.
	compressorAckText = "Understood, I have reviewed the prior context and will continue."

	// compressorMaxArtefactLines bounds the tool artefact index. Each line is
	// one dropped call; the cap keeps the summariser input bounded even for a
	// session that made hundreds of calls before being compressed.
	compressorMaxArtefactLines = 40

	// compressorArtefactLineMaxChars bounds one artefact line. Arguments are
	// compact JSON, so ids survive and prose is cut.
	compressorArtefactLineMaxChars = 200

	// compressorConversationPartMaxChars bounds one text part in the
	// conversation body. A tool result can arrive as text rather than as a
	// function response and be hundreds of KB; the summariser does not need more
	// than a few KB of it to say what happened.
	compressorConversationPartMaxChars = 4000

	// compressorConversationMaxBytes bounds the whole conversation body, so a
	// pathological session cannot push the summariser request past its own
	// context window — which would make compaction fail and silently keep the
	// oversized session. The body is filled oldest-first so goals and original
	// instructions survive. Elided text is not recovered elsewhere in this
	// prompt; the durable run transcript (kb.agent_run_messages /
	// kb.agent_run_tool_calls) still holds it.
	compressorConversationMaxBytes = 48 << 10
)

// compressSession summarises the dropped events and returns a rebuilt session
// with the same id that contains the summary plus the retained events verbatim.
//
// Parameters:
//   - dropped       — the events being removed from the model's history; this
//     is the only input the summariser sees.
//   - retained      — the events kept, appended to the rebuilt session as-is.
//   - sessionID     — the ADK session id (delete + create reuse it).
//   - contextWindow — max_input_tokens for the model (0 = unknown; skips the
//     anti-thrash guard because there is no budget to compare against).
//   - modelName     — used to resolve the summariser (falls back to default).
//
// A nil session with a nil error means "no change": either there was nothing
// worth summarising, the summariser failed, or the anti-thrash guard tripped.
// A non-nil error means the rebuild started and could not finish; the caller
// keeps the session it already holds.
func (ae *AgentExecutor) compressSession(
	ctx context.Context,
	dropped []*session.Event,
	retained []*session.Event,
	sessionID string,
	contextWindow int,
	modelName string,
) (session.Session, error) {
	promptText, ok := buildCompressionPrompt(dropped)
	if !ok {
		return nil, nil
	}

	// --- Call summariser LLM ---
	// Prefer the agent's own model; otherwise resolve from DB project config.
	//
	// This request is built here rather than by the agent loop, so it never
	// passes through the executor's before-model callback and is not fenced by
	// fenceModelRequestToolResults (#1142). The dropped window can hold
	// third-party tool results; fencing this input is issue #1143.
	var llm model.LLM
	var err error
	if modelName != "" {
		llm, err = ae.modelFactory.CreateModelWithName(ctx, modelName)
	} else {
		llm, err = ae.modelFactory.CreateModel(ctx)
	}
	if err != nil {
		ae.log.Warn("session compressor: failed to create summariser model, skipping compression",
			slog.String("model", modelName), logger.Error(err))
		return nil, nil
	}

	summaryReq := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(promptText, genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			MaxOutputTokens: 2048,
			Temperature:     genai.Ptr[float32](0.2),
		},
	}

	var summaryText string
	var summaryTokens int32
	for resp, respErr := range llm.GenerateContent(ctx, summaryReq, false) {
		if respErr != nil {
			ae.log.Warn("session compressor: summariser error, skipping compression",
				logger.Error(respErr))
			return nil, nil
		}
		if resp == nil {
			continue
		}
		if resp.UsageMetadata != nil {
			summaryTokens = resp.UsageMetadata.CandidatesTokenCount
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part != nil && part.Text != "" {
					summaryText += part.Text
				}
			}
		}
	}

	summaryText = strings.TrimSpace(summaryText)
	if summaryText == "" {
		ae.log.Warn("session compressor: empty summary returned, skipping compression",
			slog.String("session_id", sessionID))
		return nil, nil
	}

	// Anti-thrash guard: bail if the summary is too large to be worth it.
	if contextWindow > 0 && summaryTokens > 0 {
		maxSummaryTokens := int32(float64(contextWindow) * compressSummaryMaxRatio)
		if summaryTokens > maxSummaryTokens {
			ae.log.Warn("session compressor: summary too large, skipping compression",
				slog.String("session_id", sessionID),
				slog.Int("summary_tokens", int(summaryTokens)),
				slog.Int("max_summary_tokens", int(maxSummaryTokens)),
			)
			return nil, nil
		}
	}

	// --- Rebuild session: delete old, create new with same ID ---
	if err := ae.sessionService.Delete(ctx, &session.DeleteRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	}); err != nil {
		ae.log.Warn("session compressor: failed to delete old session, skipping compression",
			slog.String("session_id", sessionID), logger.Error(err))
		return nil, nil
	}

	createResp, err := ae.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	})
	if err != nil {
		ae.log.Error("session compressor: failed to create replacement session",
			slog.String("session_id", sessionID), logger.Error(err))
		return nil, fmt.Errorf("session compressor: create replacement session: %w", err)
	}
	newSess := createResp.Session

	// Ordering matters: bunsession reads a session's events with
	// `ORDER BY timestamp ASC`, so the rebuilt session must read
	// summary → ack → retained. Stamp the synthetic events immediately before
	// the oldest retained event so they sort first on every session service,
	// not only on the in-memory one that preserves insertion order.
	baseTime := time.Now()
	if len(retained) > 0 && retained[0] != nil && !retained[0].Timestamp.IsZero() {
		baseTime = retained[0].Timestamp.Add(-time.Millisecond)
	}

	// Append summary placeholder (user turn).
	summaryEvent := &session.Event{
		ID:        fmt.Sprintf("compress-%d-user", time.Now().UnixNano()),
		Timestamp: baseTime,
		Author:    "user",
		LLMResponse: model.LLMResponse{
			Content: genai.NewContentFromText(
				compressorSummaryTag+"\n"+summaryText,
				genai.RoleUser,
			),
		},
	}
	if err := ae.sessionService.AppendEvent(ctx, newSess, summaryEvent); err != nil {
		ae.log.Warn("session compressor: failed to append summary event",
			slog.String("session_id", sessionID), logger.Error(err))
		return nil, nil
	}

	// Append model acknowledgement.
	ackEvent := &session.Event{
		ID:        fmt.Sprintf("compress-%d-model", time.Now().UnixNano()),
		Timestamp: baseTime.Add(time.Microsecond),
		Author:    "model",
		LLMResponse: model.LLMResponse{
			Content: genai.NewContentFromText(compressorAckText, genai.RoleModel),
		},
	}
	if err := ae.sessionService.AppendEvent(ctx, newSess, ackEvent); err != nil {
		ae.log.Warn("session compressor: failed to append ack event",
			slog.String("session_id", sessionID), logger.Error(err))
		return nil, nil
	}

	// Append the retained events verbatim.
	for i, ev := range retained {
		if err := ae.sessionService.AppendEvent(ctx, newSess, ev); err != nil {
			ae.log.Warn("session compressor: failed to append retained event",
				slog.String("session_id", sessionID),
				slog.Int("event_index", i),
				logger.Error(err))
			// Best-effort: continue appending remaining events.
		}
	}

	// Record the compaction before the re-fetch so both success paths carry the
	// ancestry row. Best-effort: the session is already rebuilt, so a
	// bookkeeping failure must never fail the run.
	if ae.repo != nil {
		row := &SessionCompaction{
			SessionID:      sessionID,
			DroppedEvents:  len(dropped),
			RetainedEvents: len(retained),
			Summary:        summaryText,
			SummaryTokens:  int(summaryTokens),
			Model:          modelName,
		}
		if len(dropped) > 0 && dropped[0] != nil {
			row.FirstDroppedID = dropped[0].ID
		}
		if len(dropped) > 0 && dropped[len(dropped)-1] != nil {
			row.LastDroppedID = dropped[len(dropped)-1].ID
		}
		if recErr := ae.repo.RecordSessionCompaction(ctx, row); recErr != nil {
			ae.log.Warn("session compressor: failed to record compaction ancestry",
				slog.String("session_id", sessionID), logger.Error(recErr))
		}
	}

	// Re-fetch the rebuilt session so callers get a fully hydrated object.
	getResp, err := ae.sessionService.Get(ctx, &session.GetRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	})
	if err != nil || getResp == nil || getResp.Session == nil {
		ae.log.Warn("session compressor: failed to re-fetch compressed session, using in-memory session",
			slog.String("session_id", sessionID), logger.Error(err))
		return newSess, nil
	}

	ae.log.Info("session compressed",
		slog.String("session_id", sessionID),
		slog.String("model", modelName),
		slog.Int("dropped_events", len(dropped)),
		slog.Int("retained_events", len(retained)),
		slog.Int("events_after", getResp.Session.Events().Len()),
		slog.Int("summary_tokens", int(summaryTokens)),
	)

	return getResp.Session, nil
}

// buildCompressionPrompt renders the summariser prompt from the dropped events
// only. It returns ok=false when the dropped set carries no text and no tool
// call worth an LLM call.
func buildCompressionPrompt(dropped []*session.Event) (string, bool) {
	var conversation strings.Builder
	lines := 0
	elided := false
	for _, ev := range dropped {
		if ev == nil || elided {
			continue
		}
		role := conversationRole(ev)
		for _, part := range eventParts(ev) {
			if part == nil || part.Text == "" {
				continue
			}
			// Prior summaries are carried through the anchored EXISTING SUMMARY
			// block; repeating them as dialogue would double-count and invite
			// the model to treat them as turns.
			if strings.HasPrefix(strings.TrimSpace(part.Text), compressorSummaryTag) {
				continue
			}
			text := part.Text
			if r := []rune(text); len(r) > compressorConversationPartMaxChars {
				text = string(r[:compressorConversationPartMaxChars]) + "… [part truncated]"
			}
			fmt.Fprintf(&conversation, "[%s]: %s\n", role, text)
			lines++
			if conversation.Len() >= compressorConversationMaxBytes {
				conversation.WriteString("… [remaining dropped history elided to bound the summariser input; it remains in the run transcript]\n")
				elided = true
				break
			}
		}
	}

	artefacts := artefactIndex(dropped)
	if lines == 0 && len(artefacts) == 0 {
		return "", false
	}

	var sb strings.Builder
	sb.WriteString("You are a context summariser for a long-running agent session. The history below is about to be removed from the model's context window; only your summary of it will remain. The most recent turns are kept verbatim and are NOT part of this excerpt.\n\n")
	sb.WriteString("Produce a concise but complete summary covering:\n")
	sb.WriteString("- The user's overall goal or task\n")
	sb.WriteString("- Key decisions or conclusions reached\n")
	sb.WriteString("- Important facts, data, or artefacts produced — preserve object, document, run and branch ids exactly as written\n")
	sb.WriteString("- Any pending actions or unresolved questions\n\n")

	if prior := priorSummaries(dropped); prior != "" {
		sb.WriteString("EXISTING SUMMARY — extend it. Keep every fact and id it records; do not restate or contradict earlier decisions.\n")
		sb.WriteString("---\n")
		sb.WriteString(prior)
		sb.WriteString("\n---\n\n")
	}

	if len(artefacts) > 0 {
		sb.WriteString("TOOL ARTEFACT INDEX (calls in the removed history; ids are authoritative):\n")
		for _, line := range artefacts {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("CONVERSATION TO SUMMARISE:\n---\n")
	sb.WriteString(conversation.String())
	sb.WriteString("---\n\nSUMMARY:")
	return sb.String(), true
}

// priorSummaries collects any [CONTEXT SUMMARY] placeholders in the dropped
// set, oldest first, so the next summary extends them instead of re-deriving
// from a summary.
func priorSummaries(dropped []*session.Event) string {
	var parts []string
	for _, ev := range dropped {
		if ev == nil {
			continue
		}
		for _, part := range eventParts(ev) {
			if part == nil {
				continue
			}
			text := strings.TrimSpace(part.Text)
			if !strings.HasPrefix(text, compressorSummaryTag) {
				continue
			}
			if body := strings.TrimSpace(strings.TrimPrefix(text, compressorSummaryTag)); body != "" {
				parts = append(parts, body)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// artefactIndex renders one bounded line per dropped tool call/result. The
// arguments are compact JSON so ids and key parameters survive while the line
// stays short. When the dropped set holds more calls than the cap, the NEWEST
// ones are kept: they are the ones adjacent to the retained tail and therefore
// the ones a continuation is most likely to act on.
func artefactIndex(dropped []*session.Event) []string {
	out := make([]string, 0, compressorMaxArtefactLines)
	for _, ev := range dropped {
		if ev == nil {
			continue
		}
		for _, part := range eventParts(ev) {
			if part == nil {
				continue
			}
			var line string
			switch {
			case part.FunctionCall != nil:
				line = fmt.Sprintf("- call %s(%s)", part.FunctionCall.Name, compactJSON(part.FunctionCall.Args))
			case part.FunctionResponse != nil:
				line = fmt.Sprintf("- result %s(%s)", part.FunctionResponse.Name, compactJSON(part.FunctionResponse.Response))
			default:
				continue
			}
			line = truncateLine(line, compressorArtefactLineMaxChars)
			if len(out) == compressorMaxArtefactLines {
				copy(out, out[1:])
				out[compressorMaxArtefactLines-1] = line
				continue
			}
			out = append(out, line)
		}
	}
	return out
}

// compactJSON renders a value as one line of JSON. Encoding errors degrade to
// an empty string rather than dropping the call entirely.
func compactJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// truncateLine cuts s to at most maxChars runes, appending an ellipsis.
func truncateLine(s string, maxChars int) string {
	r := []rune(s)
	if len(r) <= maxChars {
		return s
	}
	return string(r[:maxChars]) + "…"
}

// conversationRole labels a line in the summariser transcript. The executor
// persists the ADK event author (a sanitised agent name), so the author is the
// most informative label; the content role is the fallback.
func conversationRole(ev *session.Event) string {
	if ev.Author != "" {
		return ev.Author
	}
	if ev.Content != nil && ev.Content.Role != "" {
		return string(ev.Content.Role)
	}
	return "unknown"
}

// partitionForCompaction splits one event snapshot into the events to drop and
// the tail to retain. numKeep is the number of trailing events the token trim
// wants to keep. numKeep <= 0 drops everything and retains nothing; numKeep >=
// len(events) drops nothing and retains everything (compressSession then treats
// the empty dropped set as "no change").
//
// Both halves come from the same snapshot on purpose: deriving the retained
// half from a second read could put a boundary event (same timestamp, no
// tiebreaker in the ORDER BY) in neither half or in both.
func partitionForCompaction(events session.Events, numKeep int) (dropped, retained []*session.Event) {
	if events == nil {
		return nil, nil
	}
	n := events.Len()
	if numKeep < 0 {
		numKeep = 0
	}
	if numKeep > n {
		numKeep = n
	}
	return eventSlice(events, 0, n-numKeep), eventSlice(events, n-numKeep, n)
}

// eventSlice copies events [from, to) into a slice. Callers use it to split the
// full history into the dropped and retained sets they pass to
// compressSession. Out-of-range bounds are clamped; an empty range is nil.
func eventSlice(events session.Events, from, to int) []*session.Event {
	if events == nil {
		return nil
	}
	if from < 0 {
		from = 0
	}
	if to > events.Len() {
		to = events.Len()
	}
	if from >= to {
		return nil
	}
	out := make([]*session.Event, 0, to-from)
	for i := from; i < to; i++ {
		out = append(out, events.At(i))
	}
	return out
}
