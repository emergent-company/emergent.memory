package agents

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// recordingBudgetParkListener captures the budget-park events the enqueue path
// publishes. The regression it guards is #1032: the refusal used to be a lone
// WARN, so nothing that owned the dependent required check could observe it.
type recordingBudgetParkListener struct {
	mu     sync.Mutex
	events []WorkBudgetParkedEvent
}

func (r *recordingBudgetParkListener) handle(_ context.Context, ev WorkBudgetParkedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recordingBudgetParkListener) snapshot() []WorkBudgetParkedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]WorkBudgetParkedEvent(nil), r.events...)
}

// TestTriggerBudgetRefusalParksAndNotifies is the #1032 regression for the
// trigger dispatch path: an EnqueueWorkRunDeduped budget refusal must be
// observable to the caller (the check owner) and must park the item durably so
// the reconciler stops re-attempting it every minute.
func TestTriggerBudgetRefusalParksAndNotifies(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "reviewer",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "reviewer", defID)
	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "budget-park-1", "ready")

	agent, err := repo.FindByID(ctx, agentID, &projectID)
	require.NoError(t, err)
	def, err := repo.ResolveDefinitionForAgent(ctx, agent)
	require.NoError(t, err)

	listener := &recordingBudgetParkListener{}
	repo.AddWorkBudgetParkListener(listener.handle)

	gate := &fakeBudgetChecker{exceeded: true}
	repo.SetBudgetGate(gate, true)

	ts := newWorkTriggerService(repo)
	ts.SetWorkObjectStore(&fakeListStore{heads: []*graph.WorkObjectHead{{
		ProjectID: projectID, CanonicalID: canonicalID, Status: "ready",
		Type: "ResearchRequest", Assignee: "reviewer",
	}}})

	input := map[string]any{
		"id":   canonicalID,
		"data": map[string]any{"status": "ready", "assignee": "reviewer"},
	}
	ts.enqueueWorkRun(ctx, agent, def, "ResearchRequest", EventTypeStatusChanged, projectID, input)

	require.Zero(t, countQueuedRuns(t, db, ctx, agentID, canonicalID), "a capped project must not enqueue a run")

	block, err := repo.GetWorkBlock(ctx, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, block, "the refusal must be a durable parked state, not only a log line")
	require.NotNil(t, block.FailureClass)
	require.Equal(t, string(FailureClassBudget), *block.FailureClass)
	require.Contains(t, block.Reason, "budget_usd", "the parked reason must name the escape hatch")
	require.Contains(t, block.Reason, "reset the budget window")

	events := listener.snapshot()
	require.Len(t, events, 1, "the caller that owns the dependent check must observe the refusal")
	require.Equal(t, canonicalID, events[0].CanonicalID)
	require.Equal(t, projectID, events[0].ProjectID)
	require.Equal(t, "ResearchRequest", events[0].ObjectType)
	require.Equal(t, agentID, events[0].AgentID)
	require.NotNil(t, events[0].Head)
	require.Contains(t, events[0].Reason, "budget_usd")

	var taskCount int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.tasks WHERE source_id = ? AND type = 'work-escalation'`,
		canonicalID,
	).Scan(ctx, &taskCount))
	require.Equal(t, 1, taskCount, "parking must create one human-visible escalation task")

	// A duplicate delivery is suppressed by the durable block: no second park,
	// no second listener notification, no duplicate escalation.
	ts.enqueueWorkRun(ctx, agent, def, "ResearchRequest", EventTypeStatusChanged, projectID, input)
	require.Len(t, listener.snapshot(), 1)
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.tasks WHERE source_id = ? AND type = 'work-escalation'`,
		canonicalID,
	).Scan(ctx, &taskCount))
	require.Equal(t, 1, taskCount)

	// Refreshing the park (another delivery racing the first) updates the
	// reason but must not create a second escalation task.
	require.NoError(t, repo.ParkWorkItemForBudget(ctx, projectID, canonicalID, "dispatch refused: still over cap"))
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.tasks WHERE source_id = ? AND type = 'work-escalation'`,
		canonicalID,
	).Scan(ctx, &taskCount))
	require.Equal(t, 1, taskCount)
}

// TestParkWorkItemForBudgetConcurrentDeliveriesEscalateOnce exercises the race
// the sequential test above cannot: several deliveries for one canonical id
// parked at the same instant. The upsert reports insertion via
// RETURNING (xmax = 0), and only that one caller may mint the escalation task.
// A rewrite of the upsert (or a return to select-then-insert) would produce
// duplicate escalations here even though the sequential path stays green.
func TestParkWorkItemForBudgetConcurrentDeliveriesEscalateOnce(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	canonicalID := uuid.NewString()

	const deliveries = 8
	reasons := make([]string, deliveries)
	for i := range reasons {
		reasons[i] = fmt.Sprintf("dispatch %d refused: still over cap", i)
	}
	errs := make([]error, deliveries)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < deliveries; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = repo.ParkWorkItemForBudget(ctx, projectID, canonicalID, reasons[i])
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "concurrent delivery %d", i)
	}

	var taskCount int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.tasks WHERE source_id = ? AND type = 'work-escalation'`,
		canonicalID,
	).Scan(ctx, &taskCount))
	require.Equal(t, 1, taskCount, "exactly one concurrent delivery may mint the escalation task")

	block, err := repo.GetWorkBlock(ctx, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, block, "every delivery must leave the durable budget park in place")
	require.NotNil(t, block.FailureClass)
	require.Equal(t, string(FailureClassBudget), *block.FailureClass)
}

// TestWorkReconciler_BudgetRefusalParksAndResumes pins both halves of the
// reconciler contract: a budget refusal parks the item and notifies listeners
// once (stop condition, no per-minute WARN), and raising the cap clears the
// budget park and lets the same item dispatch without a human unblock.
func TestWorkReconciler_BudgetRefusalParksAndResumes(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "reviewer",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "reviewer", defID)
	_, err := db.ExecContext(ctx,
		`UPDATE kb.agents SET trigger_type = 'reaction', reaction_config = '{"objectTypes":["ResearchRequest"],"events":["created","status_changed"]}'::jsonb WHERE id = ?`,
		agentID)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "reconcile-budget-1", "ready")

	store := &fakeListStore{heads: []*graph.WorkObjectHead{{
		ProjectID: projectID, CanonicalID: canonicalID, Status: "ready",
		Type: "ResearchRequest", Assignee: "reviewer",
	}}}
	listener := &recordingBudgetParkListener{}
	repo.AddWorkBudgetParkListener(listener.handle)
	gate := &fakeBudgetChecker{exceeded: true}
	repo.SetBudgetGate(gate, true)

	rc := NewWorkReconciler(repo, testAgentLogger(), time.Minute)
	rc.SetWorkObjectStore(store)

	rc.reconcile(ctx)

	require.Zero(t, countQueuedRuns(t, db, ctx, agentID, canonicalID))
	block, err := repo.GetWorkBlock(ctx, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, block)
	require.NotNil(t, block.FailureClass)
	require.Equal(t, string(FailureClassBudget), *block.FailureClass)
	require.Len(t, listener.snapshot(), 1, "the refusal must reach a listener instead of only a WARN")

	// The budget verdict is cached per project per pass, and the parked item
	// itself is skipped on subsequent passes: the reconciler does not spin.
	gate.calls = 0
	rc.reconcile(ctx)
	require.Zero(t, countQueuedRuns(t, db, ctx, agentID, canonicalID))
	require.Equal(t, 1, gate.calls, "one gate read per project per reconcile pass")
	require.Len(t, listener.snapshot(), 1, "a parked item must not re-notify every tick")

	// Raising the cap clears the auto-recoverable budget park and dispatches
	// the same item again.
	gate.exceeded = false
	rc.reconcile(ctx)
	require.Equal(t, 1, countQueuedRuns(t, db, ctx, agentID, canonicalID))
	block, err = repo.GetWorkBlock(ctx, canonicalID)
	require.NoError(t, err)
	require.Nil(t, block, "a relaxed cap must clear the budget park")
	require.Len(t, listener.snapshot(), 1, "a successful dispatch must not notify a budget park")
}

// TestIsBudgetBlock distinguishes a spending-gate park from any other durable
// block; only the former is auto-cleared when the cap relaxes.
func TestIsBudgetBlock(t *testing.T) {
	class := string(FailureClassBudget)
	other := string(FailureClassDeterministic)
	require.False(t, isBudgetBlock(nil))
	require.False(t, isBudgetBlock(&WorkBlock{}))
	require.False(t, isBudgetBlock(&WorkBlock{FailureClass: &other}))
	require.True(t, isBudgetBlock(&WorkBlock{FailureClass: &class}))
}
