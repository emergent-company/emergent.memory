package agents

// session_overflow.go — reactive compaction after a mid-run context overflow
// (#1138).
//
// The load path compacts a resumed session before the run starts. A single long
// run can still overflow the model's context window mid-run: the provider
// rejects the next model call, the step fails, and without recovery the run ends
// or wedges. This is the reactive path: on a recognised context-window rejection
// the executor partitions the current session with the same helper the load path
// uses, summarises the dropped set with compressSession, and retries the step
// exactly once.
//
// The retry passes no new user content: the failed turn's user message is
// already persisted in the session (the runner appends it before the model
// call), so re-sending it would duplicate the turn. The rebuilt session keeps
// the same id, so the runner's next Run picks it up.
//
// Anti-loop guard: the caller allows at most one reactive compaction per run.
// If the retried call overflows again, the run fails with the original error.

import (
	"context"
	"log/slog"

	"google.golang.org/adk/v2/session"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// reactiveCompactionKeepRatio is the fraction of the model's context window
	// the retained tail should fit within. It matches the load-path trim's
	// target so both paths leave comparable headroom.
	reactiveCompactionKeepRatio = 0.75

	// reactiveCompactionMinRetain is the smallest non-empty tail to keep. The
	// split always drops at least one event.
	reactiveCompactionMinRetain = 1
)

// reactiveCompactionKeep chooses how many trailing events to retain when
// compacting after a context overflow.
//
// When the model's context window is known, it mirrors the load path: walk back
// to the newest event whose cumulative prompt-token count fits within 75 % of
// the window and keep from there. Without usage metadata (or without a known
// window) it keeps the newer half, which at least halves the prompt. The result
// is always in [1, n-1] so the split both drops and retains something.
func reactiveCompactionKeep(events session.Events, contextWindow int) int {
	if events == nil {
		return 0
	}
	n := events.Len()
	if n < 2 {
		return 0
	}

	keep := n / 2
	if contextWindow > 0 {
		target := int32(float64(contextWindow) * reactiveCompactionKeepRatio)
		for i := n - 1; i >= 0; i-- {
			ev := events.At(i)
			if ev == nil || ev.UsageMetadata == nil {
				continue
			}
			if tokens := ev.UsageMetadata.PromptTokenCount; tokens > 0 && tokens <= target {
				keep = n - i
				break
			}
		}
	}

	if keep < reactiveCompactionMinRetain {
		keep = reactiveCompactionMinRetain
	}
	if keep >= n {
		keep = n - 1
	}
	return keep
}

// reactiveCompactSession rebuilds the session after a mid-run context-window
// rejection. It returns true when the session was rebuilt and the failed step
// should be retried; false means the caller should fail with the original error
// (nothing useful to drop, the summariser declined, or the rebuild failed).
//
// Writing the ancestry row is compressSession's job; this function only chooses
// the split and classifies the outcome for the retry decision.
func (ae *AgentExecutor) reactiveCompactSession(ctx context.Context, sessionID, modelName string) bool {
	if ae == nil || ae.sessionService == nil || sessionID == "" {
		return false
	}

	getResp, err := ae.sessionService.Get(ctx, &session.GetRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	})
	if err != nil || getResp == nil || getResp.Session == nil {
		ae.log.Warn("reactive compaction: failed to load session",
			slog.String("session_id", sessionID), logger.Error(err))
		return false
	}

	events := getResp.Session.Events()
	contextWindow := ae.modelInputLimit(ctx, modelName)
	keep := reactiveCompactionKeep(events, contextWindow)
	if keep <= 0 {
		ae.log.Warn("reactive compaction: session too short to compact",
			slog.String("session_id", sessionID),
			slog.Int("events", events.Len()))
		return false
	}

	dropped, retained := partitionForCompaction(events, keep)
	if len(dropped) == 0 {
		ae.log.Warn("reactive compaction: nothing to drop",
			slog.String("session_id", sessionID),
			slog.Int("events", events.Len()))
		return false
	}

	compressed, compErr := ae.compressSession(
		ctx, dropped, retained, sessionID, contextWindow, modelName, CompactionTriggerOverflow,
	)
	if compErr != nil {
		ae.log.Warn("reactive compaction: rebuild failed",
			slog.String("session_id", sessionID), logger.Error(compErr))
		return false
	}
	if compressed == nil {
		ae.log.Warn("reactive compaction: compressor declined, failing run",
			slog.String("session_id", sessionID),
			slog.Int("dropped_events", len(dropped)),
			slog.Int("retained_events", len(retained)))
		return false
	}

	ae.log.Info("reactive session compaction applied after context overflow",
		slog.String("session_id", sessionID),
		slog.String("model", modelName),
		slog.Int("dropped_events", len(dropped)),
		slog.Int("retained_events", len(retained)),
		slog.Int("events_after", compressed.Events().Len()),
	)
	return true
}

// modelInputLimit resolves the model's context window, returning 0 when it is
// unknown (the caller then falls back to halving the session).
func (ae *AgentExecutor) modelInputLimit(ctx context.Context, modelName string) int {
	if ae == nil || ae.modelLimits == nil || modelName == "" {
		return 0
	}
	limit, err := ae.modelLimits.GetModelInputLimit(ctx, modelName)
	if err != nil {
		ae.log.Warn("reactive compaction: failed to look up model context window",
			slog.String("model", modelName), logger.Error(err))
		return 0
	}
	return limit
}
