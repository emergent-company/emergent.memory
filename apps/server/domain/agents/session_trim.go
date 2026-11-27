package agents

// session_trim.go — token-aware windowing for resumed ADK sessions (Pass 2 of #209).
//
// On resume the executor loads the full persisted session and, when the
// cumulative prompt size approaches the model's context window, reduces what
// the model sees. Two mechanisms live behind this file:
//
//	Pass 2a  sliding-window trim: keep only the trailing events whose prompt
//	         size fits within 75 % of the window (planSessionTrim +
//	         numKeepToFit).
//	Pass 2b  LLM compression: summarise the head of the kept events into a
//	         [CONTEXT SUMMARY] placeholder (compressSession, in
//	         session_compressor.go).
//
// Compaction interacts with the trim in a non-obvious way (#1137). The
// compressor re-appends the retained tail verbatim, so those events keep the
// cumulative UsageMetadata.PromptTokenCount they recorded *before* the head was
// replaced by the summary. Reading those counts as the current prompt size
// makes the next resume trim into the summary placeholder — and the placeholder
// carries no usage metadata of its own, so the scan then lands on an event
// before it and the summary is invisible on every run after the run that
// produced it.
//
// planSessionTrim therefore ignores usage metadata from events that predate the
// newest summary placeholder (their timestamps are older than the compaction),
// and the resume path re-summarises a compacted session instead of trimming its
// summary off the head.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/adk/v2/session"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// defaultMaxSessionEvents is the fallback event window when the current
	// prompt size cannot be derived from event metadata.
	defaultMaxSessionEvents = 50

	// sessionTrimHighWatermarkRatio is the fraction of the context window at
	// which the resume path starts reducing the visible history.
	sessionTrimHighWatermarkRatio = 0.80

	// sessionTrimTargetRatio is the fraction of the context window the kept
	// history should fit within.
	sessionTrimTargetRatio = 0.75

	// largeSessionThreshold is the event-count fallback used when the model's
	// context window is unknown: sessions longer than this are compressed even
	// without token data so they cannot grow unbounded.
	largeSessionThreshold = 200
)

// summaryPlaceholderIndex returns the index of the newest synthetic compaction
// summary in events, or -1 when the session has never been compacted.
func summaryPlaceholderIndex(events session.Events) int {
	for i := events.Len() - 1; i >= 0; i-- {
		ev := events.At(i)
		if ev == nil || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part != nil && strings.Contains(part.Text, compressorSummaryTag) {
				return i
			}
		}
	}
	return -1
}

// predatesSummary reports whether events[i] was recorded before the compaction
// summarised by the placeholder at summaryIdx.
//
// The check is timestamp-based on purpose: compressSession deletes the old
// session and re-appends the retained tail, so a retained event sits *after*
// the placeholder in the event order while having been produced before it. Its
// UsageMetadata.PromptTokenCount therefore describes the pre-compaction prompt
// and must not be read as the current prompt size.
func predatesSummary(events session.Events, i, summaryIdx int) bool {
	if summaryIdx < 0 || i == summaryIdx {
		return false
	}
	summary := events.At(summaryIdx)
	ev := events.At(i)
	if summary == nil || ev == nil {
		return false
	}
	return ev.Timestamp.Before(summary.Timestamp)
}

// sessionTrimPlan is the Pass 2a decision for a resumed session.
type sessionTrimPlan struct {
	// PromptTokens is the most recent cumulative prompt size that can be
	// trusted, or 0 when no trustworthy count is available.
	PromptTokens int32

	// SummaryIndex is the index of the newest compaction summary placeholder,
	// or -1 when the session has never been compacted.
	SummaryIndex int

	// contextWindow is the model's max input tokens.
	contextWindow int
}

// planSessionTrim inspects events and derives the prompt size and compaction
// state used to decide whether (and how) to window the resumed session.
func planSessionTrim(events session.Events, contextWindow int) sessionTrimPlan {
	plan := sessionTrimPlan{
		SummaryIndex:  summaryPlaceholderIndex(events),
		contextWindow: contextWindow,
	}
	for i := events.Len() - 1; i >= 0; i-- {
		ev := events.At(i)
		if ev == nil || ev.UsageMetadata == nil || ev.UsageMetadata.PromptTokenCount <= 0 {
			continue
		}
		if predatesSummary(events, i, plan.SummaryIndex) {
			// Stale pre-compaction count: the prompt no longer contains the
			// head this count was taken with.
			continue
		}
		plan.PromptTokens = ev.UsageMetadata.PromptTokenCount
		break
	}
	return plan
}

// highWatermark is the prompt size at which the resume path reduces history.
func (p sessionTrimPlan) highWatermark() int32 {
	if p.contextWindow <= 0 {
		return 0
	}
	return int32(float64(p.contextWindow) * sessionTrimHighWatermarkRatio)
}

// targetTokens is the prompt size the kept history should fit within.
func (p sessionTrimPlan) targetTokens() int32 {
	if p.contextWindow <= 0 {
		return 0
	}
	return int32(float64(p.contextWindow) * sessionTrimTargetRatio)
}

// overHighWatermark reports whether the session's visible history is large
// enough to need reduction.
func (p sessionTrimPlan) overHighWatermark() bool {
	return p.PromptTokens > 0 && p.PromptTokens >= p.highWatermark()
}

// numKeepToFit returns how many trailing events to keep so that the earliest
// kept event's cumulative prompt size still fits within targetTokens.
//
// Counts that predate the newest compaction summary are skipped: they describe
// a prompt that no longer exists, and following them would trim past the
// summary. When no usable count fits, the safe default window is returned.
func (p sessionTrimPlan) numKeepToFit(events session.Events) int {
	n := events.Len()
	target := p.targetTokens()
	for i := n - 1; i >= 0; i-- {
		ev := events.At(i)
		if ev == nil || ev.UsageMetadata == nil || ev.UsageMetadata.PromptTokenCount <= 0 {
			continue
		}
		if predatesSummary(events, i, p.SummaryIndex) {
			continue
		}
		if ev.UsageMetadata.PromptTokenCount <= target {
			return n - i
		}
	}
	return defaultMaxSessionEvents
}

// loadResumedSession fetches the persisted ADK session for sessionID and applies
// the token-aware windowing described at the top of this file.
//
// A non-nil error means the session could not be loaded at all; the caller
// deletes the stale row and starts a fresh session. Windowing failures are
// non-fatal: the session is returned as loaded and the failure is logged.
func (ae *AgentExecutor) loadResumedSession(ctx context.Context, sessionID, modelName string) (session.Session, error) {
	getResp, err := ae.sessionService.Get(ctx, &session.GetRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}
	if getResp == nil || getResp.Session == nil {
		return nil, fmt.Errorf("session %s loaded without a session payload", sessionID)
	}

	var contextWindow int
	if ae.modelLimits != nil && modelName != "" {
		var limErr error
		contextWindow, limErr = ae.modelLimits.GetModelInputLimit(ctx, modelName)
		if limErr != nil {
			ae.log.Warn("failed to look up model context window, skipping token trim",
				slog.String("model", modelName), logger.Error(limErr))
		}
	}

	sess, trimmed := ae.windowResumedSession(ctx, getResp.Session, sessionID, modelName, contextWindow)
	if !trimmed {
		ae.log.Info("resumed ADK session from database",
			slog.String("session_id", sessionID),
			slog.Int("history_events", sess.Events().Len()),
		)
	}
	return sess, nil
}

// windowResumedSession applies Pass 2a/2b to a freshly loaded session and
// reports whether it changed anything.
func (ae *AgentExecutor) windowResumedSession(
	ctx context.Context,
	sess session.Session,
	sessionID string,
	modelName string,
	contextWindow int,
) (session.Session, bool) {
	if contextWindow <= 0 {
		// Context window unknown — fall back to the event-count guard so a
		// long session cannot grow unbounded without token data.
		if sess.Events().Len() <= largeSessionThreshold {
			return sess, false
		}
		compressed, compErr := ae.compressSession(ctx, sess, sessionID, 0, modelName)
		if compErr != nil {
			ae.log.Warn("session compression (no context window) failed, proceeding",
				slog.String("session_id", sessionID), logger.Error(compErr))
			return sess, false
		}
		return compressed, true
	}

	plan := planSessionTrim(sess.Events(), contextWindow)
	if !plan.overHighWatermark() {
		return sess, false
	}

	if plan.SummaryIndex >= 0 {
		// The head is already a compaction summary. Pass 2a drops the oldest
		// events, which would take the summary (and every fact it stands for)
		// with it, so re-summarise the head instead: compressSession folds the
		// existing summary into the new one and keeps the tail.
		before := sess.Events().Len()
		compressed, compErr := ae.compressSession(ctx, sess, sessionID, contextWindow, modelName)
		if compErr != nil {
			ae.log.Warn("session re-compression at high watermark failed, keeping session as loaded",
				slog.String("session_id", sessionID), logger.Error(compErr))
			return sess, false
		}
		if compressed.Events().Len() >= before {
			ae.log.Warn("session re-compression at high watermark made no progress, keeping session as loaded",
				slog.String("session_id", sessionID),
				slog.Int("events", before),
			)
			return sess, false
		}
		ae.log.Info("compacted session re-summarised at high watermark",
			slog.String("session_id", sessionID),
			slog.String("model", modelName),
			slog.Int("context_window", contextWindow),
			slog.Int("prompt_tokens_before", int(plan.PromptTokens)),
			slog.Int("events_before", before),
			slog.Int("events_after", compressed.Events().Len()),
		)
		return compressed, true
	}

	// Pass 2a: sliding-window trim for a session that has never been compacted.
	events := sess.Events()
	n := events.Len()
	numKeep := plan.numKeepToFit(events)

	trimResp, trimErr := ae.sessionService.Get(ctx, &session.GetRequest{
		AppName:         "agents",
		UserID:          "system",
		SessionID:       sessionID,
		NumRecentEvents: numKeep,
	})
	if trimErr != nil || trimResp == nil || trimResp.Session == nil {
		ae.log.Warn("token-aware session re-fetch failed, using full history",
			slog.String("session_id", sessionID),
			logger.Error(trimErr),
		)
		return sess, false
	}
	sess = trimResp.Session
	ae.log.Info("token-aware session trim applied",
		slog.String("session_id", sessionID),
		slog.String("model", modelName),
		slog.Int("context_window", contextWindow),
		slog.Int("prompt_tokens_before", int(plan.PromptTokens)),
		slog.Int("events_kept", numKeep),
		slog.Int("events_total", n),
	)

	// Pass 2b: LLM compression — summarise the dropped head. Only runs when a
	// significant portion of the history was dropped (i.e. we are genuinely
	// near the limit, not just a precautionary trim).
	if numKeep < n/2 {
		compressed, compErr := ae.compressSession(ctx, sess, sessionID, contextWindow, modelName)
		if compErr != nil {
			ae.log.Warn("session compression failed, proceeding with trimmed session",
				slog.String("session_id", sessionID), logger.Error(compErr))
		} else {
			sess = compressed
		}
	}

	return sess, true
}
