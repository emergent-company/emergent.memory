package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

type fakeWorkInvocationResolver struct {
	inv *AgentInvocation
	err error
}

func (f fakeWorkInvocationResolver) ResolveSourceTriggerInvocation(context.Context, string, string, string) (*AgentInvocation, error) {
	return f.inv, f.err
}

func TestWorkRunInvocationPrefersSourceTrigger(t *testing.T) {
	head := &graph.WorkObjectHead{Type: "GitHubIssue", Properties: map[string]any{
		"source_trigger_id": "trigger-1",
		"source_id":         "source-1",
	}}
	resolver := fakeWorkInvocationResolver{inv: &AgentInvocation{Model: "trigger/model"}}
	trigger := &AgentWorkTrigger{Invocation: &AgentInvocation{Model: "work-rule/model"}}
	inv := workRunInvocation(context.Background(), resolver, "project-1", head, trigger)
	require.NotNil(t, inv)
	require.Equal(t, "trigger/model", inv.Model)
}

func TestWorkRunInvocationIgnoresWritableSnapshot(t *testing.T) {
	head := &graph.WorkObjectHead{Type: "GitHubIssue", Properties: map[string]any{
		"run_invocation": map[string]any{"model": "attacker/model"},
	}}
	inv := workRunInvocation(context.Background(), nil, "project-1", head, nil)
	require.Nil(t, inv, "invocation must not be read from writable graph properties")
}

func TestWorkRunInvocationFallsBackToWorkTrigger(t *testing.T) {
	inv := workRunInvocation(context.Background(), nil, "project-1", &graph.WorkObjectHead{}, &AgentWorkTrigger{
		Invocation: &AgentInvocation{Model: "work-rule/model"},
	})
	require.NotNil(t, inv)
	require.Equal(t, "work-rule/model", inv.Model)
}

func TestWorkRunTrustedExternalIssue(t *testing.T) {
	require.False(t, workRunTrusted(&graph.WorkObjectHead{Type: "GitHubIssue"}))
	require.True(t, workRunTrusted(&graph.WorkObjectHead{Type: "GitHubReviewLane"}))
}
