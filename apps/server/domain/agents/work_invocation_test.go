package agents

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

func TestWorkRunInvocationPrefersItemSnapshot(t *testing.T) {
	head := &graph.WorkObjectHead{Properties: map[string]any{
		"run_invocation": map[string]any{"model": "item/model"},
	}}
	trigger := &AgentWorkTrigger{Invocation: &AgentInvocation{Model: "trigger/model"}}
	inv := workRunInvocation(head, trigger)
	require.NotNil(t, inv)
	require.Equal(t, "item/model", inv.Model)
}

func TestWorkRunTrustedExternalIssue(t *testing.T) {
	require.False(t, workRunTrusted(&graph.WorkObjectHead{Type: "GitHubIssue"}))
	require.True(t, workRunTrusted(&graph.WorkObjectHead{Type: "GitHubReviewLane"}))
}

func TestWorkRunInvocationFallsBackToTrigger(t *testing.T) {
	inv := workRunInvocation(&graph.WorkObjectHead{}, &AgentWorkTrigger{
		Invocation: &AgentInvocation{Model: "trigger/model"},
	})
	require.NotNil(t, inv)
	require.Equal(t, "trigger/model", inv.Model)
}
