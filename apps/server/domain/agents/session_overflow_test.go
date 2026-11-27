package agents

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// TestReactiveCompactionKeep covers the split choice made when a mid-run model
// call overflows: with token usage it mirrors the load path's 75 % target;
// without usage (or without a known window) it halves the history. It always
// drops and retains at least one event.
func TestReactiveCompactionKeep(t *testing.T) {
	events := compressorFakeEvents{
		compressorTextEvent("e1", "user", genai.RoleUser, "a"),
		compressorTextEvent("e2", "model", genai.RoleModel, "b"),
		compressorTextEvent("e3", "user", genai.RoleUser, "c"),
		compressorTextEvent("e4", "model", genai.RoleModel, "d"),
		compressorTextEvent("e5", "user", genai.RoleUser, "e"),
		compressorTextEvent("e6", "model", genai.RoleModel, "f"),
	}

	require.Equal(t, 0, reactiveCompactionKeep(nil, 0))
	require.Equal(t, 0, reactiveCompactionKeep(compressorFakeEvents{compressorTextEvent("e1", "user", genai.RoleUser, "a")}, 0))

	// No usage metadata: keep the newer half.
	require.Equal(t, 3, reactiveCompactionKeep(events, 0))
	require.Equal(t, 3, reactiveCompactionKeep(events, 100_000))

	// With usage metadata, keep from the newest event that fits 75 % of the
	// window. e4 is the newest event at/below 75 of 100 tokens.
	withUsage := compressorFakeEvents{
		usageEvent("e1", 95),
		usageEvent("e2", 90),
		usageEvent("e3", 70),
		usageEvent("e4", 75),
		usageEvent("e5", 110),
		usageEvent("e6", 120),
	}
	require.Equal(t, 3, reactiveCompactionKeep(withUsage, 100),
		"keep e4..e6: e4 is the newest event whose cumulative prompt fits the target")

	// The returned value is always a valid split: at least one retained and at
	// least one dropped.
	for n := 2; n <= 6; n++ {
		keep := reactiveCompactionKeep(compressorFakeEvents(events[:n]), 0)
		require.GreaterOrEqual(t, keep, 1)
		require.LessOrEqual(t, keep, n-1)
	}
}

// usageEvent builds an event carrying cumulative prompt-token usage.
func usageEvent(id string, promptTokens int32) *session.Event {
	ev := compressorTextEvent(id, "model", genai.RoleModel, id)
	ev.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: promptTokens}
	return ev
}

// scriptedResponse is one model call's outcome in the scripted overflow model.
type scriptedResponse struct {
	err  error
	text string
}

// scriptedModelWrapper is both an adk.ModelWrapper and a model.LLM. Every model
// the factory creates is this same object, so a single call script spans the
// main model and the summariser in deterministic order. That lets the test pin
// "provider error → summary call → retried step".
type scriptedModelWrapper struct {
	mu        sync.Mutex
	responses []scriptedResponse
	calls     int
}

func (w *scriptedModelWrapper) WrapModel(model.LLM, string, string) model.LLM { return w }

func (w *scriptedModelWrapper) Name() string { return "openai/gpt-test" }

func (w *scriptedModelWrapper) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		w.mu.Lock()
		call := w.calls
		w.calls++
		w.mu.Unlock()

		if call >= len(w.responses) {
			yield(nil, fmt.Errorf("scripted model: unexpected call %d", call))
			return
		}
		resp := w.responses[call]
		if resp.err != nil {
			yield(nil, resp.err)
			return
		}
		yield(&model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: resp.text}}},
		}, nil)
	}
}

func (w *scriptedModelWrapper) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

// overflowPipelineExecutor builds an executor whose model factory produces the
// scripted model and whose session service is the in-memory one (seeded by the
// caller). The model ref is explicit so the factory's env path has a model name
// to resolve; the wrapper replaces the resulting provider adapter.
func overflowPipelineExecutor(repo *Repository, svc session.Service, script *scriptedModelWrapper) *AgentExecutor {
	log := testAgentLogger()
	mf := adk.NewModelFactory(
		&config.LLMConfig{OpenAIAPIKey: "test-key", OpenAIModel: "gpt-test"},
		log, nil, script, nil,
	)
	return &AgentExecutor{
		repo:           repo,
		modelFactory:   mf,
		toolPool:       &capturingToolPool{},
		sessionService: svc,
		safeguards:     config.AgentSafeguardsConfig{ExecutionEnabled: true},
		log:            log,
	}
}

// seededHistoryEvents returns n conversational events with explicit ids and
// strictly increasing timestamps so event order is not left to the clock.
func seededHistoryEvents(n int) []*session.Event {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]*session.Event, 0, n)
	for i := 0; i < n; i++ {
		var role genai.Role = genai.RoleUser
		author := "user"
		if i%2 == 1 {
			role = genai.RoleModel
			author = "model"
		}
		out = append(out, &session.Event{
			ID:        fmt.Sprintf("hist-%d", i+1),
			Timestamp: base.Add(time.Duration(i) * time.Millisecond),
			Author:    author,
			LLMResponse: model.LLMResponse{
				Content: genai.NewContentFromText(fmt.Sprintf("history turn %d", i+1), role),
			},
		})
	}
	return out
}

// TestRunPipelineReactiveCompactionOnContextOverflow is the end-to-end
// regression for #1138. A long run's model call is rejected once for exceeding
// the context window; the executor must compact with the summariser, retry the
// step, and finish successfully. The rebuilt session must read
// summary → ack → retained, and exactly one ancestry row must record the
// overflow trigger.
func TestRunPipelineReactiveCompactionOnContextOverflow(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_reactive_overflow")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "overflow-agent")

	const conversationKey = "overflow-conversation"
	sessionID := agentADKSessionKey(projectID, conversationKey)

	svc := session.InMemoryService()
	created, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "agents", UserID: "system", SessionID: sessionID,
	})
	require.NoError(t, err)
	history := seededHistoryEvents(6)
	for _, ev := range history {
		require.NoError(t, svc.AppendEvent(ctx, created.Session, ev))
	}

	script := &scriptedModelWrapper{responses: []scriptedResponse{
		{err: fmt.Errorf(`openai-compatible: endpoint returned 400: {"error":{"message":"This model's maximum context length is 8192 tokens. However, you requested 9000 tokens."}}`)},
		{text: "SUMMARY-BODY: the user is reconciling the Q3 ledger for acct-1."},
		{text: "Done: reconciled the Q3 ledger."},
	}}

	repo := NewRepository(tdb.DB)
	ae := overflowPipelineExecutor(repo, svc, script)

	res, err := ae.Execute(ctx, ExecuteRequest{
		Agent:           &Agent{ID: agentID, ProjectID: projectID, Name: "overflow-agent"},
		ProjectID:       projectID,
		AgentDefinition: nil,
		UserMessage:     "continue the reconciliation",
		ConversationKey: conversationKey,
		Model:           "openai/gpt-test",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, RunStatusSuccess, res.Status, "the retried step must complete the run")
	require.Equal(t, 3, script.callCount(),
		"expected exactly: overflow, summary call, retried step")

	// One ancestry row, tagged with the reactive trigger.
	var rows []SessionCompaction
	require.NoError(t, tdb.DB.NewSelect().
		Model(&rows).
		Where("session_id = ?", sessionID).
		Scan(ctx))
	require.Len(t, rows, 1, "one overflow must record exactly one compaction")
	require.Equal(t, CompactionTriggerOverflow, rows[0].Trigger)
	require.Equal(t, 1, rows[0].Seq)
	require.Equal(t, 4, rows[0].DroppedEvents, "no token data: the older half (4 of 7 events) is dropped")
	require.Equal(t, 3, rows[0].RetainedEvents)
	require.Contains(t, rows[0].Summary, "SUMMARY-BODY")

	// The rebuilt session must read summary → ack → retained. The runner
	// appends the retried turn's final response after the retained tail.
	persisted, err := svc.Get(ctx, &session.GetRequest{
		AppName: "agents", UserID: "system", SessionID: sessionID,
	})
	require.NoError(t, err)
	require.NotNil(t, persisted)
	events := persisted.Session.Events()
	require.GreaterOrEqual(t, events.Len(), 5)

	require.Contains(t, compressorEventText(events.At(0)), compressorSummaryTag)
	require.Contains(t, compressorEventText(events.At(0)), "SUMMARY-BODY")
	require.Equal(t, compressorAckText, compressorEventText(events.At(1)))
	require.True(t, events.At(0).Timestamp.Before(events.At(2).Timestamp),
		"the summary must sort before the retained tail")

	require.Equal(t, "hist-5", events.At(2).ID)
	require.Equal(t, "hist-6", events.At(3).ID)
}

// overflowUnitExecutor returns an executor backed by the in-memory session
// service and a scripted summariser model (no database). It exercises the real
// reactive compaction + rebuild path.
func overflowUnitExecutor(script *scriptedModelWrapper) (*AgentExecutor, session.Service) {
	log := testAgentLogger()
	mf := adk.NewModelFactory(
		&config.LLMConfig{OpenAIAPIKey: "test-key", OpenAIModel: "gpt-test"},
		log, nil, script, nil,
	)
	svc := session.InMemoryService()
	return &AgentExecutor{modelFactory: mf, sessionService: svc, log: log}, svc
}

// TestReactiveCompactSessionRebuildsWithSummaryOrder is the DB-less half of the
// overflow regression: the compaction helper partitions the live session,
// summarises the dropped tail, and the rebuilt session reads
// summary → ack → retained. It also pins the trigger passed to the ancestry row
// by checking that a row is attempted only for the overflow path (repo is nil
// here, so the row itself is covered by the DB integration test).
func TestReactiveCompactSessionRebuildsWithSummaryOrder(t *testing.T) {
	ctx := adk.WithProjectID(context.Background(), "proj-overflow")
	script := &scriptedModelWrapper{responses: []scriptedResponse{
		{text: "SUMMARY-BODY: reconciled acct-1."},
	}}
	ae, svc := overflowUnitExecutor(script)

	created, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-overflow-unit",
	})
	require.NoError(t, err)
	history := seededHistoryEvents(6)
	for _, ev := range history {
		require.NoError(t, svc.AppendEvent(ctx, created.Session, ev))
	}

	require.True(t, ae.reactiveCompactSession(ctx, "sess-overflow-unit", "openai/gpt-test"))
	require.Equal(t, 1, script.callCount(), "exactly one summariser call")

	persisted, err := svc.Get(ctx, &session.GetRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-overflow-unit",
	})
	require.NoError(t, err)
	events := persisted.Session.Events()
	require.Equal(t, 5, events.Len(), "summary + ack + the retained newer half")

	require.Contains(t, compressorEventText(events.At(0)), compressorSummaryTag)
	require.Contains(t, compressorEventText(events.At(0)), "SUMMARY-BODY")
	require.Equal(t, compressorAckText, compressorEventText(events.At(1)))
	require.Equal(t, "hist-4", events.At(2).ID)
	require.Equal(t, "hist-5", events.At(3).ID)
	require.Equal(t, "hist-6", events.At(4).ID)
	require.True(t, events.At(0).Timestamp.Before(events.At(2).Timestamp))
}

// TestReactiveCompactSessionDeclinesWhenNothingToCompact covers the guard: a
// one-event session cannot be split, so no summariser call is made and the
// caller must fail with the original overflow.
func TestReactiveCompactSessionDeclinesWhenNothingToCompact(t *testing.T) {
	ctx := adk.WithProjectID(context.Background(), "proj-overflow")
	script := &scriptedModelWrapper{responses: []scriptedResponse{{text: "should not be called"}}}
	ae, svc := overflowUnitExecutor(script)

	created, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-overflow-short",
	})
	require.NoError(t, err)
	require.NoError(t, svc.AppendEvent(ctx, created.Session, seededHistoryEvents(1)[0]))

	require.False(t, ae.reactiveCompactSession(ctx, "sess-overflow-short", "openai/gpt-test"))
	require.Equal(t, 0, script.callCount())
}

// TestReactiveCompactSessionDeclinesOnEmptySummary covers the compressor's
// "no usable summary" contract: the helper reports failure and the session is
// left untouched.
func TestReactiveCompactSessionDeclinesOnEmptySummary(t *testing.T) {
	ctx := adk.WithProjectID(context.Background(), "proj-overflow")
	script := &scriptedModelWrapper{responses: []scriptedResponse{{text: "   "}}}
	ae, svc := overflowUnitExecutor(script)

	created, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-overflow-empty",
	})
	require.NoError(t, err)
	history := seededHistoryEvents(4)
	for _, ev := range history {
		require.NoError(t, svc.AppendEvent(ctx, created.Session, ev))
	}

	require.False(t, ae.reactiveCompactSession(ctx, "sess-overflow-empty", "openai/gpt-test"))
	require.Equal(t, 1, script.callCount())

	persisted, err := svc.Get(ctx, &session.GetRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-overflow-empty",
	})
	require.NoError(t, err)
	require.Equal(t, 4, persisted.Session.Events().Len(), "the session must be untouched")
}

// TestRunnerRetryWithNilContentAfterModelError proves the retry mechanism the
// reactive compaction relies on: after a model call fails, calling the runner
// again with nil content re-runs the step from the persisted history instead of
// appending a duplicate user turn. This is the assumption that lets the
// executor retry without a second user message (#1138).
func TestRunnerRetryWithNilContentAfterModelError(t *testing.T) {
	ctx := context.Background()
	svc := session.InMemoryService()
	script := &scriptedModelWrapper{responses: []scriptedResponse{
		{err: errors.New("openai-compatible: This model's maximum context length is 8192 tokens")},
		{text: "recovered answer"},
	}}

	ag, err := llmagent.New(llmagent.Config{
		Name:        "retry-agent",
		Model:       script,
		Instruction: "be brief",
	})
	require.NoError(t, err)
	r, err := runner.New(runner.Config{Agent: ag, SessionService: svc, AppName: "agents"})
	require.NoError(t, err)

	created, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-nil-retry",
	})
	require.NoError(t, err)
	for _, ev := range seededHistoryEvents(2) {
		require.NoError(t, svc.AppendEvent(ctx, created.Session, ev))
	}

	var firstErr error
	for _, runErr := range r.Run(ctx, "system", "sess-nil-retry", genai.NewContentFromText("do the task", genai.RoleUser), agent.RunConfig{}) {
		if runErr != nil {
			firstErr = runErr
		}
	}
	require.Error(t, firstErr)
	require.True(t, adk.IsContextOverflow(firstErr))

	var finalText string
	for ev, runErr := range r.Run(ctx, "system", "sess-nil-retry", nil, agent.RunConfig{}) {
		require.NoError(t, runErr)
		if ev == nil || !ev.IsFinalResponse() || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part != nil {
				finalText += part.Text
			}
		}
	}
	require.Contains(t, finalText, "recovered answer",
		"the retried step must run from history with nil content")
	require.Equal(t, 2, script.callCount(), "the nil-content retry must make exactly one more model call")

	// The failed turn's user message must not have been appended twice.
	persisted, err := svc.Get(ctx, &session.GetRequest{
		AppName: "agents", UserID: "system", SessionID: "sess-nil-retry",
	})
	require.NoError(t, err)
	var userTurns int
	for i := 0; i < persisted.Session.Events().Len(); i++ {
		ev := persisted.Session.Events().At(i)
		if ev != nil && ev.Author == "user" {
			for _, part := range eventParts(ev) {
				if part != nil && part.Text == "do the task" {
					userTurns++
				}
			}
		}
	}
	require.Equal(t, 1, userTurns, "the retry must not duplicate the user turn")
}
