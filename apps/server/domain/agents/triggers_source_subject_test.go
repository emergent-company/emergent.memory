package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

type inlineBindingStore struct {
	fakeWorkObjectStore
	head *graph.WorkObjectHead
}

func (s *inlineBindingStore) GetHeadObject(context.Context, string, string) (*graph.WorkObjectHead, error) {
	return s.head, nil
}

func TestInlineWorkBindingUsesTrustedIssueSubject(t *testing.T) {
	ts := &TriggerService{workObjects: &inlineBindingStore{head: &graph.WorkObjectHead{
		CanonicalID: "obj-1",
		Type:        "GitHubIssue",
		Version:     3,
		Properties: map[string]any{
			"source_id": "src-1", "repo": "acme/repo", "issue_number": float64(9),
		},
	}}}

	subject, meta := ts.inlineWorkBinding(context.Background(), "proj-1", "obj-1")
	require.NotNil(t, subject)
	require.Equal(t, "src-1", subject.SourceID)
	require.Equal(t, "acme/repo", subject.Repo)
	require.Equal(t, "issue", subject.Kind)
	require.Equal(t, 9, subject.IssueNumber)
	require.True(t, subject.IssueValid())
	require.Equal(t, "acme/repo", meta["repo"])
	require.Equal(t, "src-1", meta["sourceId"])
	require.Equal(t, 9, meta["issueNumber"])
}

func TestInlineWorkBindingWithoutStoreReturnsNil(t *testing.T) {
	subject, meta := (&TriggerService{}).inlineWorkBinding(context.Background(), "proj-1", "obj-1")
	require.Nil(t, subject)
	require.Nil(t, meta)
}
