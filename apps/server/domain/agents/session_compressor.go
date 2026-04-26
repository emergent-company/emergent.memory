package agents

// session_compressor.go — LLM-based ADK session compression (Phase 2 of #209).
//
// When a resumed session's accumulated token usage approaches the model's context
// window, the 2-pass sliding-window trim in runPipeline (Phase 1) reduces the
// visible history by discarding the oldest events.  Phase 2 goes further: instead
// of silently dropping those events, it asks a fast LLM to summarize them into a
// compact placeholder that is prepended to the retained tail.
//
// Algorithm (adapted from Hermes Agent context_compressor.py):
//
//  1. Split events into HEAD (oldest ~50 %) and TAIL (newest ~50 %).
//  2. Serialize HEAD into plain text, stripping tool call / result noise.
//  3. Call the summarizer model (configurable; defaults to gemini-2.5-flash).
//  4. Delete the current session and create a fresh one with the same ID.
//  5. Append two synthetic events:
//       user  → "[CONTEXT SUMMARY]\n<summary>"
//       model → "Understood, I have reviewed the prior context and will continue."
//  6. Append each TAIL event verbatim.
//
// The result is a session whose token count is roughly:
//   summary_tokens + tail_tokens  ≪  original_tokens
//
// Anti-thrash guard: if the summary alone is > 30 % of the target budget the
// compressor skips and lets the plain sliding window handle it.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// compressorDefaultModel is the cheap/fast model used for summarisation.
	// Overridable via AgentDefinition.Compression.Model in the future.
	compressorDefaultModel = "gemini-2.5-flash"

	// compressThresholdRatio triggers compression when promptTokens/contextWindow
	// exceeds this ratio (only relevant when the sliding window is exhausted or
	// the context window is unknown).
	compressThresholdRatio = 0.80

	// compressHeadRatio controls what fraction of the total event list is
	// treated as "old context" to be summarized (oldest events).
	compressHeadRatio = 0.50

	// compressSummaryMaxRatio is the anti-thrash guard: if the generated
	// summary token count exceeds this fraction of the context window the
	// compressor bails out and returns the session unchanged.
	compressSummaryMaxRatio = 0.30

	// compressorSummaryTag is injected at the start of the summary placeholder.
	compressorSummaryTag = "[CONTEXT SUMMARY]"

	// compressorAckText is the synthetic model acknowledgement that follows
	// the summary placeholder.
	compressorAckText = "Understood, I have reviewed the prior context and will continue."
)

// compressSession summarises the oldest portion of sess and returns a new session
// with the same ID that contains a compact summary + the retained tail events.
//
// Parameters:
//   - sess            — the current (possibly oversized) session
//   - contextWindow   — max_input_tokens for the model (0 = unknown, skip guard)
//   - modelName       — name used to resolve the summariser (falls back to default)
//
// On any non-fatal error the original session is returned unchanged so the caller
// can proceed normally.
func (ae *AgentExecutor) compressSession(
	ctx context.Context,
	sess session.Session,
	sessionID string,
	contextWindow int,
	modelName string,
) (session.Session, error) {
	events := sess.Events()
	n := events.Len()
	if n < 4 {
		// Not enough events to bother compressing.
		return sess, nil
	}

	headCount := n / 2
	if headCount < 2 {
		headCount = 2
	}
	tailStart := headCount

	// --- Build summary prompt from HEAD events ---
	var sb strings.Builder
	sb.WriteString("You are a context summariser. The following is the beginning of a conversation between a user and an AI assistant. Produce a concise but complete summary covering:\n")
	sb.WriteString("- The user's overall goal or task\n")
	sb.WriteString("- Key decisions or conclusions reached\n")
	sb.WriteString("- Important facts, data, or artefacts produced\n")
	sb.WriteString("- Any pending actions or unresolved questions\n\n")
	sb.WriteString("CONVERSATION TO SUMMARISE:\n")
	sb.WriteString("---\n")

	for i := 0; i < headCount; i++ {
		ev := events.At(i)
		if ev.Content == nil {
			continue
		}
		role := ev.Author
		if role == "" {
			role = string(ev.Content.Role)
		}
		for _, part := range ev.Content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				sb.WriteString(fmt.Sprintf("[%s]: %s\n", role, part.Text))
			}
			// Skip function calls/results — they add noise to the summary.
		}
	}
	sb.WriteString("---\n\nSUMMARY:")

	promptText := sb.String()

	// --- Call summariser LLM ---
	summarizerModelName := compressorDefaultModel
	if modelName != "" {
		// Prefer the agent's own model for the summarizer so credentials
		// are always available; only override if a fast model is explicitly set.
		summarizerModelName = modelName
	}

	llm, err := ae.modelFactory.CreateModelWithName(ctx, summarizerModelName)
	if err != nil {
		ae.log.Warn("session compressor: failed to create summariser model, skipping compression",
			slog.String("model", summarizerModelName), logger.Error(err))
		return sess, nil
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
			return sess, nil
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

	if strings.TrimSpace(summaryText) == "" {
		ae.log.Warn("session compressor: empty summary returned, skipping compression",
			slog.String("session_id", sessionID))
		return sess, nil
	}

	// Anti-thrash guard: bail if summary is too large.
	if contextWindow > 0 && summaryTokens > 0 {
		maxSummaryTokens := int32(float64(contextWindow) * compressSummaryMaxRatio)
		if summaryTokens > maxSummaryTokens {
			ae.log.Warn("session compressor: summary too large, skipping compression",
				slog.String("session_id", sessionID),
				slog.Int("summary_tokens", int(summaryTokens)),
				slog.Int("max_summary_tokens", int(maxSummaryTokens)),
			)
			return sess, nil
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
		return sess, nil
	}

	createResp, err := ae.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	})
	if err != nil {
		ae.log.Error("session compressor: failed to create replacement session",
			slog.String("session_id", sessionID), logger.Error(err))
		return sess, fmt.Errorf("session compressor: create replacement session: %w", err)
	}
	newSess := createResp.Session

	// Append summary placeholder (user turn).
	summaryEvent := &session.Event{
		ID:        fmt.Sprintf("compress-%d-user", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Author:    "user",
		LLMResponse: model.LLMResponse{
			Content: genai.NewContentFromText(
				compressorSummaryTag+"\n"+strings.TrimSpace(summaryText),
				genai.RoleUser,
			),
		},
	}
	if err := ae.sessionService.AppendEvent(ctx, newSess, summaryEvent); err != nil {
		ae.log.Warn("session compressor: failed to append summary event",
			slog.String("session_id", sessionID), logger.Error(err))
		return sess, nil
	}

	// Append model acknowledgement.
	ackEvent := &session.Event{
		ID:        fmt.Sprintf("compress-%d-model", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Author:    "model",
		LLMResponse: model.LLMResponse{
			Content: genai.NewContentFromText(compressorAckText, genai.RoleModel),
		},
	}
	if err := ae.sessionService.AppendEvent(ctx, newSess, ackEvent); err != nil {
		ae.log.Warn("session compressor: failed to append ack event",
			slog.String("session_id", sessionID), logger.Error(err))
		return sess, nil
	}

	// Append TAIL events verbatim.
	for i := tailStart; i < n; i++ {
		ev := events.At(i)
		if err := ae.sessionService.AppendEvent(ctx, newSess, ev); err != nil {
			ae.log.Warn("session compressor: failed to append tail event",
				slog.String("session_id", sessionID),
				slog.Int("event_index", i),
				logger.Error(err))
			// Best-effort: continue appending remaining events.
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
		slog.String("model", summarizerModelName),
		slog.Int("events_before", n),
		slog.Int("events_after", getResp.Session.Events().Len()),
		slog.Int("head_summarized", headCount),
		slog.Int("tail_kept", n-tailStart),
		slog.Int("summary_tokens", int(summaryTokens)),
	)

	return getResp.Session, nil
}
