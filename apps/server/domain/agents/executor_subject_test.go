package agents

import (
	"context"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/stretchr/testify/require"
)

func TestRunDispatchContextDropsInheritedSourceSubject(t *testing.T) {
	parent := mcp.ContextWithRunSubject(context.Background(), mcp.RunSubject{
		SourceID: "src-1", Repo: "acme/repo", PRNumber: 7, HeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	_, ok := mcp.RunSubjectFromContext(runDispatchContext(parent, false))
	require.False(t, ok, "a nested run must not inherit the parent's review subject")

	own := mcp.ContextWithRunSubject(runDispatchContext(parent, false), mcp.RunSubject{
		SourceID: "src-2", Repo: "acme/repo", PRNumber: 8, HeadSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	subj, ok := mcp.RunSubjectFromContext(own)
	require.True(t, ok)
	require.Equal(t, 8, subj.PRNumber)
}
