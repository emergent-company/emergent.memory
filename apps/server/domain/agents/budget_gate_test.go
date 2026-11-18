package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

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
		var budgetErr *BudgetExceededError
		require.True(t, errors.As(err, &budgetErr))
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
		var budgetErr *BudgetExceededError
		require.True(t, errors.As(r.checkBudgetGate(ctx, "p1"), &budgetErr))
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
