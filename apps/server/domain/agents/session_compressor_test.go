package agents

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// compressorTextEvent builds a conversational event for the summariser tests.
func compressorTextEvent(id, author string, role genai.Role, text string) *session.Event {
	return &session.Event{
		ID:        id,
		Timestamp: time.Now(),
		Author:    author,
		LLMResponse: model.LLMResponse{
			Content: genai.NewContentFromText(text, role),
		},
	}
}

// compressorToolEvent builds an event carrying a single function call, the
// artefact shape the summary must preserve.
func compressorToolEvent(id, author, tool string, args map[string]any) *session.Event {
	return &session.Event{
		ID:        id,
		Timestamp: time.Now(),
		Author:    author,
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Role:  string(genai.RoleModel),
				Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: tool, Args: args}}},
			},
		},
	}
}

// compressorEventText concatenates the text parts of an event.
func compressorEventText(ev *session.Event) string {
	if ev == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range eventParts(ev) {
		if part != nil && part.Text != "" {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

// compressorTestExecutor returns an executor backed by the in-memory ADK
// session service and the deterministic test model, so a compression runs the
// real rebuild path with no provider and no database.
func compressorTestExecutor(summary string) (*AgentExecutor, session.Service) {
	mf := adk.NewModelFactory(nil, testAgentLogger(), nil, nil, nil)
	mf.WithTestLLMChecker(alwaysTestLLM{})
	mf.WithTestLLMResponse(adk.TestLLMResponse{Final: summary})
	svc := session.InMemoryService()
	return &AgentExecutor{
		modelFactory:   mf,
		sessionService: svc,
		log:            testAgentLogger(),
	}, svc
}

// TestBuildCompressionPromptSummarisesOnlyDroppedEvents is the core regression:
// the prompt is built from the events being removed, never from the retained
// tail. The old implementation split the post-trim session in half and
// summarised survivors, so the oldest history (goals, instructions) was
// discarded unsummarised.
func TestBuildCompressionPromptSummarisesOnlyDroppedEvents(t *testing.T) {
	dropped := []*session.Event{
		compressorTextEvent("e1", "user", genai.RoleUser, "GOAL: reconcile the Q3 ledger"),
		compressorTextEvent("e2", "model", genai.RoleModel, "I will list the accounts first."),
	}
	retained := []*session.Event{
		compressorTextEvent("e3", "user", genai.RoleUser, "RETAINED TAIL TEXT"),
	}

	prompt, ok := buildCompressionPrompt(dropped)
	require.True(t, ok)
	require.Contains(t, prompt, "GOAL: reconcile the Q3 ledger")
	require.Contains(t, prompt, "I will list the accounts first.")
	require.NotContains(t, prompt, compressorEventText(retained[0]),
		"the retained tail must never be part of the summariser input")
}

// TestBuildCompressionPromptExtendsPriorSummary asserts an earlier
// [CONTEXT SUMMARY] in the dropped set is fed back as an anchor rather than
// re-summarised from scratch.
func TestBuildCompressionPromptExtendsPriorSummary(t *testing.T) {
	dropped := []*session.Event{
		compressorTextEvent("e1", "user", genai.RoleUser,
			compressorSummaryTag+"\nDecided to use branch main; created object obj-42."),
		compressorTextEvent("e2", "user", genai.RoleUser, "Next: verify obj-42."),
	}

	prompt, ok := buildCompressionPrompt(dropped)
	require.True(t, ok)
	require.Contains(t, prompt, "EXISTING SUMMARY")
	require.Contains(t, prompt, "Decided to use branch main; created object obj-42.")
	require.Contains(t, prompt, "extend it")

	// The prior summary body is also present once as a prior summary; the raw
	// tagged placeholder text is not what the summariser is told to summarise.
	require.Equal(t, 1, strings.Count(prompt, "Decided to use branch main"),
		"the anchored summary must not be duplicated into the conversation section")
}

// TestBuildCompressionPromptKeepsToolArtefacts asserts the tool record of the
// dropped set survives as an id-carrying index. A graph agent's artefacts ARE
// the work.
func TestBuildCompressionPromptKeepsToolArtefacts(t *testing.T) {
	dropped := []*session.Event{
		compressorToolEvent("e1", "coder", "entity-create",
			map[string]any{"type": "Task", "id": "obj-7f3a", "branch": "b-main"}),
	}

	prompt, ok := buildCompressionPrompt(dropped)
	require.True(t, ok)
	require.Contains(t, prompt, "TOOL ARTEFACT INDEX")
	require.Contains(t, prompt, "entity-create")
	require.Contains(t, prompt, "obj-7f3a")
	require.Contains(t, prompt, "b-main")
}

// TestArtefactIndexKeepsNewestWithinBound guards the bound that keeps the
// summariser input from growing with the number of dropped calls, and the
// retention order: when the cap bites, the newest calls — the ones adjacent to
// the retained tail — are the ones kept.
func TestArtefactIndexKeepsNewestWithinBound(t *testing.T) {
	var dropped []*session.Event
	for i := 0; i < compressorMaxArtefactLines*3; i++ {
		dropped = append(dropped, compressorToolEvent("", "coder", "entity-query",
			map[string]any{"id": fmt.Sprintf("obj-%03d", i), "payload": strings.Repeat("x", 400)}))
	}

	lines := artefactIndex(dropped)
	require.Len(t, lines, compressorMaxArtefactLines)
	for _, line := range lines {
		require.LessOrEqual(t, len([]rune(line)), compressorArtefactLineMaxChars+1,
			"one artefact line must stay within the char cap (plus the ellipsis)")
	}

	joined := strings.Join(lines, "\n")
	require.Contains(t, joined, fmt.Sprintf("obj-%03d", compressorMaxArtefactLines*3-1),
		"the newest dropped call must be indexed")
	require.NotContains(t, joined, "obj-000",
		"the oldest dropped calls are the ones to elide once the cap bites")
}

// TestPartitionForCompaction covers the split the executor uses, including the
// degenerate numKeep values: the two halves must always be a partition of the
// same snapshot, with no event in both and none in neither.
func TestPartitionForCompaction(t *testing.T) {
	events := compressorFakeEvents{
		compressorTextEvent("e1", "user", genai.RoleUser, "a"),
		compressorTextEvent("e2", "model", genai.RoleModel, "b"),
		compressorTextEvent("e3", "user", genai.RoleUser, "c"),
		compressorTextEvent("e4", "model", genai.RoleModel, "d"),
		compressorTextEvent("e5", "user", genai.RoleUser, "e"),
	}

	dropped, retained := partitionForCompaction(events, 2)
	require.Equal(t, []string{"e1", "e2", "e3"}, compressorIDs(dropped))
	require.Equal(t, []string{"e4", "e5"}, compressorIDs(retained))

	// A numKeep at or above the length drops nothing; the caller then sees a
	// nil dropped set and compressSession reports "no change".
	dropped, retained = partitionForCompaction(events, 5)
	require.Empty(t, dropped)
	require.Equal(t, []string{"e1", "e2", "e3", "e4", "e5"}, compressorIDs(retained))
	dropped, retained = partitionForCompaction(events, 99)
	require.Empty(t, dropped)
	require.Len(t, retained, 5)

	// Zero or negative keeps nothing.
	dropped, retained = partitionForCompaction(events, 0)
	require.Len(t, dropped, 5)
	require.Empty(t, retained)
	dropped, retained = partitionForCompaction(events, -3)
	require.Len(t, dropped, 5)
	require.Empty(t, retained)

	dropped, retained = partitionForCompaction(nil, 2)
	require.Nil(t, dropped)
	require.Nil(t, retained)
}

// TestBuildCompressionPromptBoundsHugePart asserts one oversized text part can
// not push the summariser request past its own window.
func TestBuildCompressionPromptBoundsHugePart(t *testing.T) {
	dropped := []*session.Event{
		compressorTextEvent("e1", "user", genai.RoleUser, strings.Repeat("A", compressorConversationPartMaxChars*4)),
	}

	prompt, ok := buildCompressionPrompt(dropped)
	require.True(t, ok)
	require.Contains(t, prompt, "[part truncated]")
	require.Less(t, len(prompt), compressorConversationPartMaxChars*2)
}

// compressorIDs returns the event ids of a slice, in order.
func compressorIDs(events []*session.Event) []string {
	ids := make([]string, 0, len(events))
	for _, ev := range events {
		ids = append(ids, ev.ID)
	}
	return ids
}

// TestBuildCompressionPromptEmptyDropped asserts an all-noise dropped set
// (no text, no tool calls) never costs an LLM call.
func TestBuildCompressionPromptEmptyDropped(t *testing.T) {
	prompt, ok := buildCompressionPrompt([]*session.Event{{ID: "e1"}, nil})
	require.False(t, ok)
	require.Empty(t, prompt)

	prompt, ok = buildCompressionPrompt(nil)
	require.False(t, ok)
	require.Empty(t, prompt)
}

// TestCompressSessionNoDropped returns "no change" without touching the model
// factory (which is nil here — a panic would fail the test).
func TestCompressSessionNoDropped(t *testing.T) {
	ae := &AgentExecutor{}
	got, err := ae.compressSession(context.Background(), nil, nil, "sess", 0, "", CompactionTriggerLoad)
	require.NoError(t, err)
	require.Nil(t, got)
}

// TestCompressSessionRebuildsRetainedTailVerbatim runs the real rebuild against
// the in-memory session service: summary + acknowledgement + the retained
// events, byte-identical and in order, with the dropped history gone.
func TestCompressSessionRebuildsRetainedTailVerbatim(t *testing.T) {
	ctx := adk.WithProjectID(context.Background(), "proj-compressor")
	ae, svc := compressorTestExecutor("SUMMARY-BODY: reconciled the ledger for acct-1.")

	created, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-compress",
	})
	require.NoError(t, err)
	sess := created.Session

	events := []*session.Event{
		compressorTextEvent("e1", "user", genai.RoleUser, "GOAL: reconcile the Q3 ledger"),
		compressorToolEvent("e2", "model", "entity-query",
			map[string]any{"type": "Account", "ids": []string{"acct-1"}}),
		compressorTextEvent("e3", "model", genai.RoleModel, "Found acct-1."),
		compressorTextEvent("e4", "user", genai.RoleUser, "RETAINED: reconcile acct-1"),
		compressorTextEvent("e5", "model", genai.RoleModel, "RETAINED: done"),
		compressorTextEvent("e6", "user", genai.RoleUser, "RETAINED: thanks"),
	}
	for _, ev := range events {
		require.NoError(t, svc.AppendEvent(ctx, sess, ev))
	}

	dropped := events[:3]
	retained := events[3:]
	got, err := ae.compressSession(ctx, dropped, retained, "sess-compress", 0, "", CompactionTriggerLoad)
	require.NoError(t, err)
	require.NotNil(t, got)

	gotEvents := got.Events()
	require.Equal(t, 2+len(retained), gotEvents.Len())

	summaryEvent := gotEvents.At(0)
	require.Contains(t, compressorEventText(summaryEvent), compressorSummaryTag)
	require.Contains(t, compressorEventText(summaryEvent), "SUMMARY-BODY")
	require.Equal(t, compressorAckText, compressorEventText(gotEvents.At(1)))

	// bunsession reads events ORDER BY timestamp ASC, so the synthetic events
	// must sort before the retained tail on any service, not only the in-memory
	// one. Without this the summary would land after the conversation.
	require.True(t, summaryEvent.Timestamp.Before(retained[0].Timestamp),
		"the summary must sort before the retained tail")
	require.True(t, gotEvents.At(1).Timestamp.After(summaryEvent.Timestamp),
		"the acknowledgement must sort after the summary")

	for i, want := range retained {
		gotEv := gotEvents.At(2 + i)
		require.Equal(t, want.ID, gotEv.ID, "retained event id must survive the rebuild")
		require.Equal(t, compressorEventText(want), compressorEventText(gotEv))
	}

	// The dropped history is absent from the rebuilt session, and the durable
	// session really is the rebuilt one (not just the returned object).
	persisted, err := svc.Get(ctx, &session.GetRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-compress",
	})
	require.NoError(t, err)
	require.NotNil(t, persisted)
	for i := 0; i < persisted.Session.Events().Len(); i++ {
		text := compressorEventText(persisted.Session.Events().At(i))
		require.NotContains(t, text, "GOAL: reconcile the Q3 ledger")
		require.NotContains(t, text, "Found acct-1.")
	}
}

// TestCompressSessionEmptySummaryLeavesSessionIntact asserts the earlier
// "no usable summary" path never deletes the session.
func TestCompressSessionEmptySummaryLeavesSessionIntact(t *testing.T) {
	ctx := adk.WithProjectID(context.Background(), "proj-compressor")
	ae, svc := compressorTestExecutor("   ")

	created, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-empty",
	})
	require.NoError(t, err)
	sess := created.Session

	events := []*session.Event{
		compressorTextEvent("e1", "user", genai.RoleUser, "first"),
		compressorTextEvent("e2", "model", genai.RoleModel, "second"),
		compressorTextEvent("e3", "user", genai.RoleUser, "third"),
	}
	for _, ev := range events {
		require.NoError(t, svc.AppendEvent(ctx, sess, ev))
	}

	got, err := ae.compressSession(ctx, events[:1], events[1:], "sess-empty", 0, "", CompactionTriggerLoad)
	require.NoError(t, err)
	require.Nil(t, got, "an unusable summary must report no change")

	persisted, err := svc.Get(ctx, &session.GetRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-empty",
	})
	require.NoError(t, err)
	require.Equal(t, 3, persisted.Session.Events().Len(), "the session must be untouched")
}

// TestEventSlice covers the split the executor uses to build the dropped and
// retained sets.
func TestEventSlice(t *testing.T) {
	events := compressorFakeEvents{
		compressorTextEvent("e1", "user", genai.RoleUser, "a"),
		compressorTextEvent("e2", "model", genai.RoleModel, "b"),
		compressorTextEvent("e3", "user", genai.RoleUser, "c"),
	}

	require.Len(t, eventSlice(events, 0, 2), 2)
	require.Equal(t, "e1", eventSlice(events, 0, 2)[0].ID)
	require.Equal(t, "e3", eventSlice(events, 2, 3)[0].ID)

	// Clamping and empty ranges.
	require.Len(t, eventSlice(events, -5, 100), 3)
	require.Nil(t, eventSlice(events, 2, 2))
	require.Nil(t, eventSlice(events, 3, 1))
	require.Nil(t, eventSlice(nil, 0, 1))
}

// compressorFakeEvents is a minimal session.Events for the split test.
type compressorFakeEvents []*session.Event

func (f compressorFakeEvents) Len() int                { return len(f) }
func (f compressorFakeEvents) At(i int) *session.Event { return f[i] }
func (f compressorFakeEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, ev := range f {
			if !yield(ev) {
				return
			}
		}
	}
}

// TestRecordSessionCompaction_AssignsSeqPerSession asserts the ancestry row's
// ordering contract: seq starts at 1 per session, increments, and is independent
// across sessions. It also covers the nil-receiver/nil-row early returns that
// keep a bookkeeping failure from failing a run.
func TestRecordSessionCompaction_AssignsSeqPerSession(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "session_compaction")
	t.Cleanup(tdb.Close)
	repo := NewRepository(tdb.DB)

	first := &SessionCompaction{
		SessionID: "sess-ancestry-a", DroppedEvents: 10, RetainedEvents: 4,
		FirstDroppedID: "e1", LastDroppedID: "e10",
		Summary: "first summary", SummaryTokens: 42, Model: "test/model",
	}
	require.NoError(t, repo.RecordSessionCompaction(ctx, first))
	require.Equal(t, 1, first.Seq)
	require.NotEmpty(t, first.ID)
	require.False(t, first.CreatedAt.IsZero())
	require.Equal(t, CompactionTriggerLoad, first.Trigger,
		"an unset trigger defaults to the load path")

	second := &SessionCompaction{SessionID: "sess-ancestry-a", DroppedEvents: 5, RetainedEvents: 8, Summary: "second"}
	require.NoError(t, repo.RecordSessionCompaction(ctx, second))
	require.Equal(t, 2, second.Seq)

	other := &SessionCompaction{SessionID: "sess-ancestry-b", DroppedEvents: 1, RetainedEvents: 1, Summary: "other"}
	require.NoError(t, repo.RecordSessionCompaction(ctx, other))
	require.Equal(t, 1, other.Seq, "seq is per session, not global")

	// The unique index backs the seq contract.
	var stored SessionCompaction
	require.NoError(t, tdb.DB.NewSelect().
		Model(&stored).
		Where("id = ?", first.ID).
		Scan(ctx))
	require.Equal(t, "sess-ancestry-a", stored.SessionID)
	require.Equal(t, 1, stored.Seq)
	require.Equal(t, "first summary", stored.Summary)
	require.Equal(t, 42, stored.SummaryTokens)
	require.Equal(t, "test/model", stored.Model)
	require.Equal(t, CompactionTriggerLoad, stored.Trigger)

	// Nil-safe early returns.
	require.NoError(t, repo.RecordSessionCompaction(ctx, nil))
	var nilRepo *Repository
	require.NoError(t, nilRepo.RecordSessionCompaction(ctx, other))
}
