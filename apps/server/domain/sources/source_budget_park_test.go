package sources

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestReviewLaneBudgetTargetFromHead pins the extraction the budget-park
// listener needs: only a fully-provisioned GitHubReviewLane has a repo, head
// SHA and source to publish a head-bound check for (#1032).
func TestReviewLaneBudgetTargetFromHead(t *testing.T) {
	head := &graph.WorkObjectHead{
		Type: "GitHubReviewLane",
		Properties: map[string]any{
			"repo":      "org/repo",
			"head_sha":  "deadbeef",
			"source_id": "11111111-1111-1111-1111-111111111111",
		},
	}
	target, ok := reviewLaneBudgetTargetFromHead(head)
	require.True(t, ok)
	require.Equal(t, "org/repo", target.Repo)
	require.Equal(t, "deadbeef", target.HeadSHA)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", target.SourceID)

	for name, tt := range map[string]*graph.WorkObjectHead{
		"nil head":       nil,
		"other type":     {Type: "GitHubPullRequest", Properties: head.Properties},
		"missing repo":   {Type: "GitHubReviewLane", Properties: map[string]any{"head_sha": "x", "source_id": "s"}},
		"missing sha":    {Type: "GitHubReviewLane", Properties: map[string]any{"repo": "o/r", "source_id": "s"}},
		"missing source": {Type: "GitHubReviewLane", Properties: map[string]any{"repo": "o/r", "head_sha": "x"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := reviewLaneBudgetTargetFromHead(tt)
			require.False(t, ok)
		})
	}
}

// TestOnWorkBudgetParkedIgnoresUnrelatedWork keeps the listener inert for work
// that has no GitHub check to publish, and nil-safe when the dispatcher is
// only partially wired.
func TestOnWorkBudgetParkedIgnoresUnrelatedWork(t *testing.T) {
	d := &SourceEventDispatcher{}
	ctx := context.Background()
	d.OnWorkBudgetParked(ctx, agents.WorkBudgetParkedEvent{Head: nil})
	d.OnWorkBudgetParked(ctx, agents.WorkBudgetParkedEvent{
		Head: &graph.WorkObjectHead{Type: "GitHubIssue", Properties: map[string]any{"repo": "o/r"}},
	})
}
