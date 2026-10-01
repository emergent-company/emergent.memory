package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

// fakeTypeConfigStore returns a fixed per-type config from GetObjectTypeWorkConfig.
type fakeTypeConfigStore struct {
	fakeWorkObjectStore
	cfg *agents.ObjectTypeWorkConfig
}

func (f *fakeTypeConfigStore) GetObjectTypeWorkConfig(ctx context.Context, projectID, typeName string) (*agents.ObjectTypeWorkConfig, error) {
	return f.cfg, nil
}

func newPolicyPool(cfg *agents.ObjectTypeWorkConfig) *WorkerPool {
	pool := NewWorkerPool(nil, nil, testAgentLogger(), 0, time.Second)
	pool.SetWorkObjectStore(&fakeTypeConfigStore{cfg: cfg})
	return pool
}

func TestResolveWorkPolicy_TypeOverridesAgent(t *testing.T) {
	pool := newPolicyPool(&agents.ObjectTypeWorkConfig{
		FailureLimit: 7,
		RetryPolicy:  &agents.RetryPolicy{MaxAttempts: 5},
	})
	def := &AgentDefinition{WorkConfig: AgentWorkConfig{
		FailureLimit: 3,
		RetryPolicy:  AgentRetryPolicy{MaxAttempts: 2},
	}}

	pol := pool.resolveWorkPolicy(context.Background(), "p1", "BoardTask", def)
	require.Equal(t, 7, pol.failureLimit)
	require.Equal(t, 5, pol.maxAttempts)
}

func TestResolveWorkPolicy_AgentValueIsDefault(t *testing.T) {
	pool := newPolicyPool(nil)
	def := &AgentDefinition{WorkConfig: AgentWorkConfig{
		FailureLimit: 4,
		RetryPolicy:  AgentRetryPolicy{MaxAttempts: 3},
	}}

	pol := pool.resolveWorkPolicy(context.Background(), "p1", "BoardTask", def)
	require.Equal(t, 4, pol.failureLimit)
	require.Equal(t, 3, pol.maxAttempts)
}

func TestResolveWorkPolicy_BuiltInDefaults(t *testing.T) {
	pool := newPolicyPool(nil)

	pol := pool.resolveWorkPolicy(context.Background(), "p1", "BoardTask", nil)
	require.Equal(t, defaultWorkFailureLimit, pol.failureLimit)
	require.Equal(t, 1, pol.maxAttempts)
}
