package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// fakeBudgetChecker is a controllable BudgetChecker for unit tests.
type fakeBudgetChecker struct {
	exceeded bool
	err      error
	calls    int
	projects []string
}

func (f *fakeBudgetChecker) CheckBudgetExceeded(_ context.Context, projectID string) (bool, error) {
	f.calls++
	f.projects = append(f.projects, projectID)
	return f.exceeded, f.err
}

func TestCheckProjectBudget_NoopWhenDisabledOrUnwired(t *testing.T) {
	ctx := context.Background()
	checker := &fakeBudgetChecker{exceeded: true}

	require.NoError(t, checkProjectBudget(ctx, nil, true, "p-1", testAgentLogger()))
	require.NoError(t, checkProjectBudget(ctx, checker, false, "p-1", testAgentLogger()))
	require.NoError(t, checkProjectBudget(ctx, checker, true, "", testAgentLogger()))
	require.Zero(t, checker.calls, "disabled/empty inputs must not query the budget")
}

func TestCheckProjectBudget_FailsOpenOnQueryError(t *testing.T) {
	checker := &fakeBudgetChecker{err: errors.New("usage db down")}
	require.NoError(t, checkProjectBudget(context.Background(), checker, true, "p-1", testAgentLogger()),
		"a broken budget query must never halt agents")
}

func TestCheckProjectBudget_BlocksWhenEitherWindowExhausted(t *testing.T) {
	checker := &fakeBudgetChecker{exceeded: true}
	err := checkProjectBudget(context.Background(), checker, true, "p-1", testAgentLogger())
	require.Error(t, err)
	require.True(t, isBudgetExceededError(err))
	var budgetErr *BudgetExceededError
	require.True(t, errors.As(err, &budgetErr))
	require.Equal(t, "p-1", budgetErr.ProjectID)
	require.Contains(t, err.Error(), "spending budget")
}

// TestExecuteWithRun_BudgetBlockedDoesNotMutateRun verifies the async entry
// point (worker pool / A2A / HTTP session trigger) consults the budget before
// touching the pre-created run. repo is nil on purpose: a budget block must
// return before any run-row write.
func TestExecuteWithRun_BudgetBlockedDoesNotMutateRun(t *testing.T) {
	run := &AgentRun{ID: "run-1", Status: RunStatusQueued}
	ae := &AgentExecutor{
		budgetChecker: &fakeBudgetChecker{exceeded: true},
		safeguards: config.AgentSafeguardsConfig{
			ExecutionEnabled:         true,
			BudgetEnforcementEnabled: true,
		},
		log: testAgentLogger(),
	}

	_, err := ae.ExecuteWithRun(context.Background(), run, ExecuteRequest{ProjectID: "p-1"})
	require.Error(t, err)
	require.True(t, isBudgetExceededError(err))
	require.Equal(t, AgentRunStatus(RunStatusQueued), run.Status, "a blocked run must not be mutated")
}

// TestResume_BudgetBlocked checks the approval/A2A/agentcompat/coordination
// resume entry point is gated before claiming the pause.
func TestResume_BudgetBlocked(t *testing.T) {
	prior := &AgentRun{ID: "run-paused", Status: RunStatusPaused}
	ae := &AgentExecutor{
		budgetChecker: &fakeBudgetChecker{exceeded: true},
		safeguards: config.AgentSafeguardsConfig{
			ExecutionEnabled:         true,
			BudgetEnforcementEnabled: true,
		},
		log: testAgentLogger(),
	}

	_, err := ae.Resume(context.Background(), prior, ExecuteRequest{ProjectID: "p-1"})
	require.Error(t, err)
	require.True(t, isBudgetExceededError(err))
}
