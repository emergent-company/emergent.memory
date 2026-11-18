package sources

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestTriggersServePullRequests pins the signal a manual review run uses to
// tell a review App from a coding App sharing the same repo (#1029). Unlike
// triggersOwnReviewGate, no triggers means "does not serve pull requests":
// an unconfigured source must not compete for a review it knows nothing about.
func TestTriggersServePullRequests(t *testing.T) {
	tests := []struct {
		name     string
		triggers []*SourceTrigger
		want     bool
	}{
		{"no triggers", nil, false},
		{"empty", []*SourceTrigger{}, false},
		{"issues only", []*SourceTrigger{{EventType: "issues"}}, false},
		{"pull request", []*SourceTrigger{{EventType: "issues"}, {EventType: "pull_request"}}, true},
		{"case insensitive", []*SourceTrigger{{EventType: "Pull_Request"}}, true},
		{"disabled pull request still names the review App", []*SourceTrigger{{EventType: "pull_request", Enabled: false}}, true},
		{"nil entries ignored", []*SourceTrigger{nil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, triggersServePullRequests(tt.triggers))
		})
	}
}

// TestResolvedReviewSourceAttribution pins the source attribution a manual run
// gets back: the operator must be able to see which App served the diff, and
// the structured fields carry the same fact for machine callers (#1029).
func TestResolvedReviewSourceAttribution(t *testing.T) {
	srcID := uuid.MustParse("357590e8-ed26-4189-961e-7c62a2bfb7de")
	resolved := &resolvedReviewSource{
		source:    &Source{ID: srcID, DisplayName: "GitHub Review"},
		selection: sourceSelectionReviewTrigger,
	}
	require.Equal(t, "GitHub Review ("+srcID.String()+"), selected by review-trigger", resolved.attribution())

	fields := resolved.fields(map[string]any{"sha": "head"})
	require.Equal(t, "head", fields["sha"])
	require.Equal(t, srcID.String(), fields["sourceId"])
	require.Equal(t, "GitHub Review", fields["sourceName"])
	require.Equal(t, "review-trigger", fields["sourceSelection"])

	// An unnamed source still attributes by id, and a nil resolution is inert so
	// a caller can merge fields unconditionally.
	unnamed := &resolvedReviewSource{source: &Source{ID: srcID}, selection: sourceSelectionAllowlist}
	require.Equal(t, srcID.String()+", selected by allowlist", unnamed.attribution())
	require.NotContains(t, unnamed.fields(nil), "sourceName")

	var missing *resolvedReviewSource
	require.Empty(t, missing.attribution())
	require.Equal(t, map[string]any{"files": 1}, missing.fields(map[string]any{"files": 1}))
}
