package agents

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/a2ui"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// These tests drive the real pipeline (Execute → runPipeline → the ADK runner)
// with the deterministic canned model, so they exercise the three executor call
// sites that own surface identity — the progressive stream namespace, the
// authoritative final frame and the persisted row — instead of re-deriving the
// ids in the test. Reverting any of those call sites makes them RED.

const (
	a2uiWiringCreate = `{"createSurface":{"surfaceId":"s1","catalogId":"memory-basic"}}`
	a2uiWiringUpdate = `{"updateComponents":{"surfaceId":"s1","components":[{"id":"r1","component":"result","rows":[{"label":"status","value":"OK"}]}]}}`
)

// a2uiWiringBlock is one answer's card, named "s1" exactly as the prompt
// teaches.
const a2uiWiringBlock = "```a2ui\n" + a2uiWiringCreate + "\n" + a2uiWiringUpdate + "\n```\n"

// a2uiWiringPartials splits the answer and its block into the chunks a
// streaming model emits, one complete line at a time, so the progressive parser
// sees the surface mid-stream rather than only in the finished response.
func a2uiWiringPartials(answer string) []string {
	return []string{
		answer + "```a2ui\n",
		a2uiWiringCreate + "\n",
		a2uiWiringUpdate + "\n",
		"```\n",
	}
}

// a2uiWiringExecutor returns an executor whose model factory serves the given
// scripted deterministic answer (no provider, no network) over the real repo.
func a2uiWiringExecutor(repo *Repository, resp adk.TestLLMResponse) *AgentExecutor {
	mf := adk.NewModelFactory(&config.LLMConfig{}, testAgentLogger(), nil, nil, nil)
	mf.WithTestLLMChecker(alwaysTestLLM{})
	mf.WithTestLLMResponse(resp)
	return &AgentExecutor{
		repo:           repo,
		modelFactory:   mf,
		toolPool:       &capturingToolPool{},
		sessionService: session.InMemoryService(),
		safeguards:     config.AgentSafeguardsConfig{ExecutionEnabled: true},
		log:            testAgentLogger(),
	}
}

// runA2UIWiring runs one canned answer through Execute and returns the run id,
// the A2UI stream events it produced, and the repo the run was persisted into.
func runA2UIWiring(t *testing.T, resp adk.TestLLMResponse) (runID string, events []StreamEvent, repo *Repository) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_a2ui_wiring")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "a2ui-wiring-agent")

	repo = NewRepository(tdb.DB)
	ae := a2uiWiringExecutor(repo, resp)
	res, err := ae.Execute(ctx, ExecuteRequest{
		Agent:       &Agent{ID: agentID, ProjectID: projectID, Name: "a2ui-wiring-agent"},
		ProjectID:   projectID,
		UserMessage: "show me a card",
		StreamCallback: func(ev StreamEvent) {
			if ev.Type == StreamEventA2UI {
				events = append(events, ev)
			}
		},
	})
	require.NoError(t, err, "pipeline run should complete")
	require.NotNil(t, res)
	require.NotEmpty(t, res.RunID)
	return res.RunID, events, repo
}

// splitOneScope asserts id is exactly `<run>.<step>:<bare>` and returns the two
// halves. More than one ':' means the id nested under a previous scope.
func splitOneScope(t *testing.T, id, runID, bare string) (scope string) {
	t.Helper()
	require.Equal(t, 1, strings.Count(id, ":"),
		"surface id %q carries more than one scope layer", id)
	scope, modelID, ok := strings.Cut(id, ":")
	require.True(t, ok, "surface id %q is not `<run>.<step>:<model id>`", id)
	require.Equal(t, bare, modelID, "the model's own id must be preserved inside the scope")
	require.True(t, strings.HasPrefix(scope, runID+"."),
		"surface id %q is not scoped to run %s", id, runID)
	step := strings.TrimPrefix(scope, runID+".")
	_, err := strconv.Atoi(step)
	require.NoError(t, err, "scope step %q is not a decimal step", step)
	return scope
}

// TestA2UISurfaceIDsAreScopedThroughThePipeline is the wiring guard for #592:
// one answer both streams a surface and finishes with it. The progressive frame,
// the authoritative final frame and the persisted row that history replay reads
// must all carry the same run+step-scoped id, and that id must differ from the
// model's bare "s1". Reverting the progressive-stream namespace, the
// final-frame namespace or the persisted-surfaces rewrite makes this RED.
func TestA2UISurfaceIDsAreScopedThroughThePipeline(t *testing.T) {
	const answer = "Here is your card.\n"
	runID, events, repo := runA2UIWiring(t, adk.TestLLMResponse{
		Partials: a2uiWiringPartials(answer),
		Final:    answer + a2uiWiringBlock,
	})

	var progressive, final []StreamEvent
	for _, ev := range events {
		if ev.Final {
			final = append(final, ev)
		} else {
			progressive = append(progressive, ev)
		}
	}
	require.NotEmpty(t, progressive, "the streamed answer must emit a progressive surface frame")
	require.NotEmpty(t, final, "the finished answer must emit the authoritative surface frame")

	progressiveID := progressive[0].SurfaceID
	finalID := final[0].SurfaceID
	require.Equal(t, progressiveID, finalID,
		"the final frame must address the surface the progressive frame created")
	scope := splitOneScope(t, progressiveID, runID, "s1")

	// The frame and the message payload inside it must agree, or the renderer
	// registers the card under one id and updates it under another.
	require.NotEmpty(t, final[0].A2UI)
	require.NotNil(t, final[0].A2UI[0].CreateSurface)
	require.Equal(t, finalID, final[0].A2UI[0].CreateSurface.SurfaceID,
		"the frame id and the createSurface payload id must agree")
	require.NotNil(t, final[0].A2UI[1].UpdateComponents)
	require.Equal(t, finalID, final[0].A2UI[1].UpdateComponents.SurfaceID)

	// The persisted row is what a reload replays: same id, same messages.
	msgs, err := repo.FindMessagesByRunID(context.Background(), runID)
	require.NoError(t, err)
	var persisted []a2ui.Message
	for _, msg := range msgs {
		if surfaces := storedSurfaces(msg.Content["a2ui"]); len(surfaces) > 0 {
			persisted = surfaces
		}
	}
	require.NotEmpty(t, persisted, "the assistant turn must persist its surface for reload")
	require.NotNil(t, persisted[0].CreateSurface)
	require.Equal(t, progressiveID, persisted[0].CreateSurface.SurfaceID,
		"the persisted surface id must match the streamed frame")
	require.Equal(t, scope, strings.SplitN(progressiveID, ":", 2)[0])
	require.NotNil(t, persisted[1].UpdateComponents)
	require.Equal(t, progressiveID, persisted[1].UpdateComponents.SurfaceID,
		"the persisted payload must use one id throughout")
	require.NotContains(t, persisted[0].CreateSurface.SurfaceID, "s1:s1",
		"the persisted id must not nest the model id under itself")
}

// TestA2UITeardownUsesTheScopedProgressiveID covers the third call site from the
// other direction: a surface streamed progressively but absent from the finished
// response must be torn down under the exact id the renderer knows. A teardown
// that dropped the namespace would delete nothing (or worse, a bare "s1" that
// some other run rendered).
func TestA2UITeardownUsesTheScopedProgressiveID(t *testing.T) {
	const answer = "Here is your card.\n"
	runID, events, _ := runA2UIWiring(t, adk.TestLLMResponse{
		Partials: a2uiWiringPartials(answer),
		// The finished answer drops the card: only the progressive surface ever
		// existed, so the run must delete it.
		Final: answer,
	})

	var teardown *StreamEvent
	for i := range events {
		if ev := events[i]; ev.Final || len(ev.A2UI) != 1 || ev.A2UI[0].DeleteSurface == nil {
			continue
		}
		teardown = &events[i]
	}
	require.NotNil(t, teardown, "a progressive surface missing from the final answer must be torn down")

	progressiveID := teardown.SurfaceID
	splitOneScope(t, progressiveID, runID, "s1")
	require.Equal(t, progressiveID, teardown.A2UI[0].DeleteSurface.SurfaceID,
		"the deleteSurface payload must use the id the frame names")
}
