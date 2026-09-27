package agents

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// capturingToolPool is a fake toolPool that captures the context it receives at
// the two executor tool-dispatch seams: StripOperatorTools (the run's dispatch
// context inside runPipeline) and CallTool (an actual tool dispatch, reached by
// the Resume confirm gate). It never touches a real MCP service or LLM.
type capturingToolPool struct {
	mu         sync.Mutex
	stripCtxs  []context.Context
	callCtxs   []context.Context
	resolveErr error
}

func (p *capturingToolPool) ResolveTools(string, *AgentDefinition, int, int) ([]tool.Tool, error) {
	return nil, p.resolveErr
}

func (p *capturingToolPool) StripOperatorTools(ctx context.Context, tools []tool.Tool) []tool.Tool {
	p.mu.Lock()
	p.stripCtxs = append(p.stripCtxs, ctx)
	p.mu.Unlock()
	return tools
}

func (p *capturingToolPool) ToolScopes() map[string]string { return nil }

func (p *capturingToolPool) CallTool(ctx context.Context, projectID, toolName string, args map[string]any) (map[string]any, error) {
	p.mu.Lock()
	p.callCtxs = append(p.callCtxs, ctx)
	p.mu.Unlock()
	return map[string]any{"ok": true}, nil
}

func (p *capturingToolPool) lastStripCtx() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.stripCtxs) == 0 {
		return nil
	}
	return p.stripCtxs[len(p.stripCtxs)-1]
}

func (p *capturingToolPool) lastCallCtx() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.callCtxs) == 0 {
		return nil
	}
	return p.callCtxs[len(p.callCtxs)-1]
}

// alwaysTestLLM reports every project as test-LLM, so the model factory returns
// the deterministic canned model (text-only, no provider, no network).
type alwaysTestLLM struct{}

func (alwaysTestLLM) IsTestLLM(context.Context, string) bool { return true }

// transportEnforcedExecutor returns an executor wired with a capturing tool
// pool, a canned-model factory, and the given ADK session service, so the
// pipeline can be driven to the tool-dispatch seams without a real LLM or MCP
// service.
func transportEnforcedExecutor(repo *Repository, tp toolPool, sess session.Service) *AgentExecutor {
	mf := adk.NewModelFactory(&config.LLMConfig{}, testAgentLogger(), nil, nil, nil)
	mf.WithTestLLMChecker(alwaysTestLLM{})
	return &AgentExecutor{
		repo:           repo,
		modelFactory:   mf,
		toolPool:       tp,
		sessionService: sess,
		safeguards:     config.AgentSafeguardsConfig{ExecutionEnabled: true},
		log:            testAgentLogger(),
	}
}

// TestRunPipelineStripsTransportEnforcedAtDispatch is the executor-level
// regression for the runPipeline call site (issue #1133): an HTTP-triggered run
// enters runPipeline with TransportEnforced set, and the tool pool's dispatch
// context (observed at StripOperatorTools, the first tool-pool seam the run's
// context flows into) must no longer carry it. Removing runDispatchContext from
// runPipeline makes this RED.
func TestRunPipelineStripsTransportEnforcedAtDispatch(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_transport_enforced_pipeline")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "pipeline-agent")

	repo := NewRepository(tdb.DB)
	tp := &capturingToolPool{}
	ae := transportEnforcedExecutor(repo, tp, session.InMemoryService())

	// Simulate an HTTP transport marking its dispatch, then crossing into the
	// run. The run is untrusted (external surface).
	dispatchCtx := mcp.ContextWithTransportEnforced(context.Background())
	res, err := ae.Execute(dispatchCtx, ExecuteRequest{
		Agent:           &Agent{ID: agentID, ProjectID: projectID, Name: "pipeline-agent"},
		ProjectID:       projectID,
		OrgID:           "",
		UserMessage:     "hello",
		TrustedInternal: false,
	})
	require.NoError(t, err, "pipeline run should complete")
	require.NotNil(t, res)

	stripCtx := tp.lastStripCtx()
	require.NotNil(t, stripCtx, "StripOperatorTools must have been reached (a tool-pool seam)")
	require.False(t, mcp.TransportEnforcedFromContext(stripCtx),
		"the transport-enforced marker must be stripped before the run's tool dispatch")
	require.False(t, mcp.TrustedInternalFromContext(stripCtx),
		"an untrusted run's dispatch context must be untrusted")
}

// TestResumeStripsTransportEnforcedAtDispatch is the executor-level regression
// for the Resume call site (issue #1133): the Resume confirm gate re-dispatches
// a pending tool via ToolPool.CallTool, and that dispatch context must not carry
// a transport-enforced marker inherited from the waking transport. Removing
// runDispatchContext from Resume makes this RED.
func TestResumeStripsTransportEnforcedAtDispatch(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_transport_enforced_resume")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "resume-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusPaused), false)

	// The resume confirm gate loads the ADK session keyed by the root run ID
	// (AppName "agents", UserID "system"); pre-create it so the gate reaches the
	// tool re-dispatch instead of bailing on a missing session.
	sess := session.InMemoryService()
	_, err := sess.Create(ctx, &session.CreateRequest{
		AppName:   "agents",
		UserID:    "system",
		SessionID: runID,
		State:     map[string]any{},
	})
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)
	tp := &capturingToolPool{}
	ae := transportEnforcedExecutor(repo, tp, sess)

	priorRun := &AgentRun{
		ID:              runID,
		AgentID:         agentID,
		Status:          RunStatusPaused,
		StepCount:       0,
		TrustedInternal: false,
		SuspendContext: SuspendSignal{
			Reason:                 SuspendReasonAwaitingToolConfirm,
			PendingToolCallID:      "fc-1",
			PendingToolName:        "agent-create",
			PendingToolConfirmArgs: map[string]any{"name": "x"},
		}.ToMap(),
	}

	// Simulate a transport waking the run with a transport-enforced marker.
	dispatchCtx := mcp.ContextWithTransportEnforced(context.Background())
	res, err := ae.Resume(dispatchCtx, priorRun, ExecuteRequest{
		Agent:           &Agent{ID: agentID, ProjectID: projectID, Name: "resume-agent"},
		ProjectID:       projectID,
		OrgID:           "",
		UserMessage:     "approve",
		TrustedInternal: false,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	callCtx := tp.lastCallCtx()
	require.NotNil(t, callCtx, "the resume confirm gate must have re-dispatched the tool")
	require.False(t, mcp.TransportEnforcedFromContext(callCtx),
		"the transport-enforced marker must be stripped before the confirm-gate tool dispatch")
	require.False(t, mcp.TrustedInternalFromContext(callCtx),
		"an untrusted resumed run's dispatch context must be untrusted")
}
