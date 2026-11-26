package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/internal/config"
)

type fakeBudgetChecker struct {
	exceeded bool
	err      error
	calls    int
}

func (f *fakeBudgetChecker) CheckBudgetExceeded(context.Context, string) (bool, error) {
	f.calls++
	return f.exceeded, f.err
}

func newBudgetTestExecutor(checker budgetChecker, enforce bool) *AgentExecutor {
	return &AgentExecutor{
		usageService: checker,
		safeguards:   config.AgentSafeguardsConfig{BudgetEnforcementEnabled: enforce},
		log:          testAgentLogger(),
	}
}

func TestEnsureBudgetAvailable(t *testing.T) {
	ctx := context.Background()

	t.Run("no checker is a no-op", func(t *testing.T) {
		ae := newBudgetTestExecutor(nil, true)
		require.NoError(t, ae.ensureBudgetAvailable(ctx, "p1", "a1", "execute"))
	})

	t.Run("empty project is a no-op", func(t *testing.T) {
		fake := &fakeBudgetChecker{exceeded: true}
		ae := newBudgetTestExecutor(fake, true)
		require.NoError(t, ae.ensureBudgetAvailable(ctx, "", "a1", "execute"))
		require.Zero(t, fake.calls)
	})

	t.Run("exceeded and enforced blocks", func(t *testing.T) {
		ae := newBudgetTestExecutor(&fakeBudgetChecker{exceeded: true}, true)
		err := ae.ensureBudgetAvailable(ctx, "p1", "a1", "resume")
		budgetErr, ok := errors.AsType[*BudgetExceededError](err)
		require.True(t, ok)
		require.Equal(t, "p1", budgetErr.ProjectID)
	})

	t.Run("exceeded but enforcement disabled proceeds", func(t *testing.T) {
		ae := newBudgetTestExecutor(&fakeBudgetChecker{exceeded: true}, false)
		require.NoError(t, ae.ensureBudgetAvailable(ctx, "p1", "a1", "execute"))
	})

	t.Run("gate error fails open", func(t *testing.T) {
		ae := newBudgetTestExecutor(&fakeBudgetChecker{err: errors.New("db down")}, true)
		require.NoError(t, ae.ensureBudgetAvailable(ctx, "p1", "a1", "execute_with_run"))
	})
}

func TestRepositoryCheckBudgetGate(t *testing.T) {
	ctx := context.Background()

	t.Run("nil gate and disabled enforcement pass", func(t *testing.T) {
		r := NewRepository(nil)
		require.NoError(t, r.checkBudgetGate(ctx, "p1"))
		r.SetBudgetGate(&fakeBudgetChecker{exceeded: true}, false)
		require.NoError(t, r.checkBudgetGate(ctx, "p1"))
	})

	t.Run("enforced gate blocks", func(t *testing.T) {
		r := NewRepository(nil)
		r.SetBudgetGate(&fakeBudgetChecker{exceeded: true}, true)
		_, ok := errors.AsType[*BudgetExceededError](r.checkBudgetGate(ctx, "p1"))
		require.True(t, ok)
	})

	t.Run("gate error fails open", func(t *testing.T) {
		r := NewRepository(nil)
		r.SetBudgetGate(&fakeBudgetChecker{err: errors.New("db down")}, true)
		require.NoError(t, r.checkBudgetGate(ctx, "p1"))
	})
}

func TestClassifyRunFailureBudget(t *testing.T) {
	cls := classifyRunFailure(&BudgetExceededError{ProjectID: "p1"}, nil)
	require.Equal(t, FailureClassBudget, cls)
}

// TestIsBudgetFailure guards the predicate the budget tests assert with: it
// must recognise a wrapped BudgetExceededError and reject anything else, or a
// test asserting through it would pass on the wrong error.
func TestIsBudgetFailure(t *testing.T) {
	require.False(t, isBudgetFailure(nil))
	require.False(t, isBudgetFailure(errors.New("db down")))
	require.True(t, isBudgetFailure(&BudgetExceededError{ProjectID: "p1"}))
	require.True(t, isBudgetFailure(fmt.Errorf("enqueue: %w", &BudgetExceededError{ProjectID: "p1"})))
}

func TestBudgetCheckerOrNil(t *testing.T) {
	var nilSvc *provider.UsageService
	require.Nil(t, budgetCheckerOrNil(nilSvc))
	require.NotNil(t, budgetCheckerOrNil(&provider.UsageService{}))
}

// TestBudgetGateWiredAtEntryPoints is the regression guard for the wiring: the
// gate must be called from each entry point, not merely exist.
func TestBudgetGateWiredAtEntryPoints(t *testing.T) {
	ctx := context.Background()
	ae := newBudgetTestExecutor(&fakeBudgetChecker{exceeded: true}, true)
	ae.safeguards.ExecutionEnabled = true

	_, err := ae.Execute(ctx, ExecuteRequest{ProjectID: "p1"})
	require.True(t, isBudgetFailure(err), "Execute")
	_, err = ae.ExecuteWithRun(ctx, nil, ExecuteRequest{ProjectID: "p1"})
	require.True(t, isBudgetFailure(err), "ExecuteWithRun")
	_, err = ae.Resume(ctx, nil, ExecuteRequest{ProjectID: "p1"})
	require.True(t, isBudgetFailure(err), "Resume")
}

// TestBudgetExceededMessageNamesEscapeHatch pins the #1032 UX requirement: the
// MCP trigger/queue refusal must name what to change (budget_usd, the budget
// settings / window reset), not only the project UUID.
func TestBudgetExceededMessageNamesEscapeHatch(t *testing.T) {
	msg := budgetExceededMessage("p-1")
	require.Contains(t, msg, "p-1")
	require.Contains(t, msg, "exceeded its spending budget")
	require.Contains(t, msg, "budget_usd")
	require.Contains(t, msg, "reset the budget window")

	err := (&BudgetExceededError{ProjectID: "p-1", Message: msg}).Error()
	require.Equal(t, msg, err)
}
