package agents

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// These tests pin the #1137 contract: after a compaction the retained tail
// still carries the cumulative PromptTokenCount it recorded *before* the head
// was replaced by the summary. The resume path must not read those stale counts
// as the current prompt size, and it must never let the sliding-window trim cut
// the synthetic summary/ack pair off the head.

// trimTestEvents is a hand-built session.Events so the pump logic can be tested
// without a session service.
type trimTestEvents []*session.Event

func (e trimTestEvents) Len() int                { return len(e) }
func (e trimTestEvents) At(i int) *session.Event { return e[i] }
func (e trimTestEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, ev := range e {
			if !yield(ev) {
				return
			}
		}
	}
}

// trimTestEvent builds one conversation event, optionally carrying the
// cumulative prompt size the model reported for it.
func trimTestEvent(id string, ts time.Time, author, text string, promptTokens int32) *session.Event {
	ev := &session.Event{
		ID:        id,
		Timestamp: ts,
		Author:    author,
		LLMResponse: model.LLMResponse{
			Content: genai.NewContentFromText(text, genai.RoleUser),
		},
	}
	if promptTokens > 0 {
		ev.LLMResponse.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount: promptTokens,
		}
	}
	return ev
}

func trimTestEventText(ev *session.Event) string {
	if ev == nil || ev.Content == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range ev.Content.Parts {
		if part != nil {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

// stubModelLimits satisfies ModelLimitsLookup with a fixed context window.
type stubModelLimits struct{ input int }

func (s stubModelLimits) GetModelOutputLimit(context.Context, string) (int, error) { return 0, nil }
func (s stubModelLimits) GetModelInputLimit(context.Context, string) (int, error) {
	return s.input, nil
}

// newTrimTestExecutor builds an executor backed by the in-memory session
// service and the deterministic test LLM, so compaction runs without a provider
// and without a database.
func newTrimTestExecutor(t *testing.T, contextWindow int) *AgentExecutor {
	t.Helper()
	mf := adk.NewModelFactory(&config.LLMConfig{}, testAgentLogger(), nil, nil, nil)
	mf.WithTestLLMChecker(alwaysTestLLM{})
	mf.WithTestLLMResponse(adk.TestLLMResponse{Final: "summary of the earlier conversation"})
	return &AgentExecutor{
		modelFactory:   mf,
		sessionService: session.InMemoryService(),
		modelLimits:    stubModelLimits{input: contextWindow},
		log:            testAgentLogger(),
	}
}

// seedTrimSession creates the session and appends evs in order.
func seedTrimSession(t *testing.T, svc session.Service, sessionID string, evs []*session.Event) session.Session {
	t.Helper()
	ctx := context.Background()
	createResp, err := svc.Create(ctx, &session.CreateRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	})
	require.NoError(t, err)
	for _, ev := range evs {
		require.NoError(t, svc.AppendEvent(ctx, createResp.Session, ev))
	}
	getResp, err := svc.Get(ctx, &session.GetRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	})
	require.NoError(t, err)
	require.NotNil(t, getResp.Session)
	return getResp.Session
}

// reloadTrimSession re-fetches the session after the compressor replaced it.
func reloadTrimSession(t *testing.T, svc session.Service, sessionID string) session.Session {
	t.Helper()
	getResp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: sessionID,
	})
	require.NoError(t, err)
	require.NotNil(t, getResp.Session)
	return getResp.Session
}

// conversationBeforeCompaction builds the eight-event session every compaction
// test starts from. Prompt counts grow as the conversation does.
func conversationBeforeCompaction(base time.Time) []*session.Event {
	evs := make([]*session.Event, 0, 8)
	for i := range 8 {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		evs = append(evs, trimTestEvent(
			fmt.Sprintf("pre-%d", i),
			base.Add(time.Duration(i)*time.Minute),
			role,
			fmt.Sprintf("turn %d", i),
			int32(20_000+i*10_000),
		))
	}
	return evs
}

func TestPlanSessionTrimIgnoresStalePreCompactionUsage(t *testing.T) {
	compactedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	events := trimTestEvents{
		trimTestEvent("summary", compactedAt, "user", compressorSummaryTag+"\nprior context", 0),
		trimTestEvent("ack", compactedAt.Add(time.Millisecond), "model", compressorAckText, 0),
		// Retained tail: indices after the summary, timestamps before it, and
		// the cumulative counts they recorded before compaction.
		trimTestEvent("tail-0", compactedAt.Add(-3*time.Minute), "user", "tail zero", 90_000),
		trimTestEvent("tail-1", compactedAt.Add(-2*time.Minute), "model", "tail one", 95_000),
		trimTestEvent("tail-2", compactedAt.Add(-time.Minute), "user", "tail two", 98_000),
	}

	plan := planSessionTrim(events, 100_000)

	assert.Equal(t, 0, plan.SummaryIndex, "newest summary placeholder must be found")
	assert.Equal(t, int32(0), plan.PromptTokens,
		"pre-compaction prompt counts describe a prompt the summary replaced and must be ignored")
	assert.False(t, plan.overHighWatermark(),
		"a freshly compacted session must not re-enter the reduction path")

	// The trim scan must not stop on a stale count either.
	assert.Equal(t, defaultMaxSessionEvents, plan.numKeepToFit(events))
}

func TestPlanSessionTrimUsesPostCompactionUsage(t *testing.T) {
	compactedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	events := trimTestEvents{
		trimTestEvent("summary", compactedAt, "user", compressorSummaryTag+"\nprior context", 0),
		trimTestEvent("ack", compactedAt.Add(time.Millisecond), "model", compressorAckText, 0),
		// Stale pre-compaction tail.
		trimTestEvent("tail-0", compactedAt.Add(-time.Minute), "user", "tail zero", 98_000),
		// Events produced after the compaction carry trustworthy counts.
		trimTestEvent("fresh-0", compactedAt.Add(time.Minute), "user", "fresh zero", 60_000),
		trimTestEvent("fresh-1", compactedAt.Add(2*time.Minute), "model", "fresh one", 70_000),
		trimTestEvent("fresh-2", compactedAt.Add(3*time.Minute), "user", "fresh two", 85_000),
	}

	plan := planSessionTrim(events, 100_000)

	assert.Equal(t, 0, plan.SummaryIndex)
	assert.Equal(t, int32(85_000), plan.PromptTokens, "only post-compaction counts are trustworthy")
	assert.True(t, plan.overHighWatermark(), "85k of a 100k window is over the 80% watermark")
	// The newest count that fits 75k is fresh-1 at index 4, so keep the last two.
	assert.Equal(t, 2, plan.numKeepToFit(events))
}

func TestPlanSessionTrimWithoutSummaryKeepsExistingBehaviour(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	events := trimTestEvents{
		trimTestEvent("e0", base, "user", "turn 0", 20_000),
		trimTestEvent("e1", base.Add(time.Minute), "model", "turn 1", 40_000),
		trimTestEvent("e2", base.Add(2*time.Minute), "user", "turn 2", 60_000),
		trimTestEvent("e3", base.Add(3*time.Minute), "model", "turn 3", 70_000),
		trimTestEvent("e4", base.Add(4*time.Minute), "user", "turn 4", 80_000),
		trimTestEvent("e5", base.Add(5*time.Minute), "model", "turn 5", 95_000),
	}

	plan := planSessionTrim(events, 100_000)

	assert.Equal(t, -1, plan.SummaryIndex, "no compaction has happened")
	assert.Equal(t, int32(95_000), plan.PromptTokens)
	assert.True(t, plan.overHighWatermark())
	assert.Equal(t, 3, plan.numKeepToFit(events),
		"the 70k event is the newest that fits the 75k target, so keep the last three")
}

// TestResumeAfterCompactionKeepsSummary is the #1137 regression test. The first
// resume after a compaction must rebuild the session starting at the summary:
// the retained tail's pre-compaction counts must not drive numKeep into the
// synthetic pair.
func TestResumeAfterCompactionKeepsSummary(t *testing.T) {
	ctx := adk.WithProjectID(context.Background(), "project-1137")
	ae := newTrimTestExecutor(t, 100_000)
	const sessionID = "resume-after-compaction"

	base := time.Now().Add(-time.Hour)
	full := seedTrimSession(t, ae.sessionService, sessionID, conversationBeforeCompaction(base))

	dropped, retained := partitionForCompaction(full.Events(), 5)
	compressed, err := ae.compressSession(ctx, dropped, retained, sessionID, 100_000, "test-model", CompactionTriggerLoad)
	require.NoError(t, err)
	require.Less(t, compressed.Events().Len(), full.Events().Len(), "compaction must have replaced the head")
	require.Contains(t, trimTestEventText(compressed.Events().At(0)), compressorSummaryTag)

	got, err := ae.loadResumedSession(ctx, sessionID, "test-model")
	require.NoError(t, err)

	require.GreaterOrEqual(t, got.Events().Len(), 2)
	require.Contains(t, trimTestEventText(got.Events().At(0)), compressorSummaryTag,
		"resume must start at the compaction summary, not inside the retained tail")
	require.Equal(t, compressorAckText, trimTestEventText(got.Events().At(1)))
	require.Equal(t, compressed.Events().Len(), got.Events().Len(),
		"a freshly compacted session is under the watermark and must not be trimmed again")
	require.NotNil(t, got.Events().At(2).UsageMetadata,
		"the retained tail keeps its recorded metadata; the fix must not mutate event data")
}

// TestResumeAfterCompactionReSummarisesWhenGrowing covers the case where a
// compacted session has genuinely grown past the watermark again: the resume
// path must re-summarise the head (folding the old summary into the new one)
// instead of trimming the summary off.
func TestResumeAfterCompactionReSummarisesWhenGrowing(t *testing.T) {
	ctx := adk.WithProjectID(context.Background(), "project-1137")
	ae := newTrimTestExecutor(t, 100_000)
	const sessionID = "resume-grow-after-compaction"

	base := time.Now().Add(-time.Hour)
	full := seedTrimSession(t, ae.sessionService, sessionID, conversationBeforeCompaction(base))

	dropped, retained := partitionForCompaction(full.Events(), 5)
	compacted, err := ae.compressSession(ctx, dropped, retained, sessionID, 100_000, "test-model", CompactionTriggerLoad)
	require.NoError(t, err)
	require.Less(t, compacted.Events().Len(), full.Events().Len())

	// Simulate the session growing after the compaction: fresh events carry
	// trustworthy, near-limit cumulative counts.
	live := reloadTrimSession(t, ae.sessionService, sessionID)
	now := time.Now()
	require.NoError(t, ae.sessionService.AppendEvent(ctx, live, trimTestEvent(
		"fresh-0", now.Add(time.Minute), "user", "one more question", 90_000)))
	require.NoError(t, ae.sessionService.AppendEvent(ctx, live, trimTestEvent(
		"fresh-1", now.Add(2*time.Minute), "model", "one more answer", 95_000)))
	before := reloadTrimSession(t, ae.sessionService, sessionID).Events().Len()

	got, err := ae.loadResumedSession(ctx, sessionID, "test-model")
	require.NoError(t, err)

	require.Contains(t, trimTestEventText(got.Events().At(0)), compressorSummaryTag,
		"the rebuilt session must still start at a compaction summary")
	require.Equal(t, compressorAckText, trimTestEventText(got.Events().At(1)))
	require.Less(t, got.Events().Len(), before,
		"an over-watermark compacted session is re-summarised, not left to grow")
}

// TestResumeWithoutSummaryStillTrims guards the ordinary Pass 2a path: a
// session that has never been compacted is still windowed from the trailing
// events' token counts.
func TestResumeWithoutSummaryStillTrims(t *testing.T) {
	ctx := adk.WithProjectID(context.Background(), "project-1137")
	ae := newTrimTestExecutor(t, 100_000)
	const sessionID = "resume-no-summary"

	base := time.Now().Add(-time.Hour)
	seedTrimSession(t, ae.sessionService, sessionID, []*session.Event{
		trimTestEvent("e0", base, "user", "turn 0", 20_000),
		trimTestEvent("e1", base.Add(time.Minute), "model", "turn 1", 40_000),
		trimTestEvent("e2", base.Add(2*time.Minute), "user", "turn 2", 60_000),
		trimTestEvent("e3", base.Add(3*time.Minute), "model", "turn 3", 70_000),
		trimTestEvent("e4", base.Add(4*time.Minute), "user", "turn 4", 80_000),
		trimTestEvent("e5", base.Add(5*time.Minute), "model", "turn 5", 95_000),
	})

	got, err := ae.loadResumedSession(ctx, sessionID, "test-model")
	require.NoError(t, err)

	require.Equal(t, 3, got.Events().Len(), "the last three events fit the 75% target")
	require.Equal(t, "turn 3", trimTestEventText(got.Events().At(0)))
}
