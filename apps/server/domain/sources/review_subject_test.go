package sources

import (
	"context"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/stretchr/testify/require"
)

func TestValidateReviewSubjectBindsToolArgsToRunSubject(t *testing.T) {
	ctx := mcp.ContextWithRunSubject(context.Background(), mcp.RunSubject{
		SourceID: "src-1", Repo: "acme/repo", PRNumber: 7, HeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	require.NoError(t, validateReviewSubject(ctx, toolPRDiff, "acme/repo", map[string]any{
		"head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}))
	require.ErrorContains(t, validateReviewSubject(ctx, toolPRDiff, "acme/other", map[string]any{
		"head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}), "does not match")
	require.ErrorContains(t, validateReviewSubject(ctx, toolPRDiff, "acme/repo", map[string]any{
		"head_sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}), "does not match")
	require.ErrorContains(t, validateReviewSubject(ctx, toolReviewSubmit, "acme/repo", map[string]any{
		"number": 8, "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}), "does not match")
	require.NoError(t, validateReviewSubject(ctx, toolReviewSubmit, "acme/repo", map[string]any{
		"number": 7, "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}))
	// No marker = legacy trusted/internal path, no subject enforcement here.
	require.NoError(t, validateReviewSubject(context.Background(), toolPRDiff, "any/repo", nil))
}
