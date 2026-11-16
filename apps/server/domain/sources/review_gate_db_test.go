package sources

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

func TestReviewGateAllReviewersAndHeadBinding(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "review_gate")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "review-gate-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "review-gate-project")
	require.NoError(t, err)

	reviewerA, reviewerB := uuid.New(), uuid.New()
	required := `[{"agentId":"` + reviewerA.String() + `","name":"security"},{"agentId":"` + reviewerB.String() + `","name":"ui"}]`
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.review_gates
		(project_id, subject_type, subject_ref, head_sha, required_reviewers, status)
		VALUES (?, 'GitHubPullRequest', 'owner/repo#7', 'head-1', ?::jsonb, 'pending')`, projectID, required)
	require.NoError(t, err)

	svc := NewReviewGateService(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	gate, err := svc.RecordVerdict(ctx, projectID, "GitHubPullRequest", "owner/repo#7", "head-1", reviewerA.String(), "security", "MERGE", "ok")
	require.NoError(t, err)
	require.Equal(t, ReviewGatePending, gate.Status, "one approval is not enough")

	gate, err = svc.RecordVerdict(ctx, projectID, "GitHubPullRequest", "owner/repo#7", "head-1", reviewerB.String(), "ui", "MERGE", "ok")
	require.NoError(t, err)
	require.Equal(t, ReviewGateApproved, gate.Status)

	gate, err = svc.RecordVerdict(ctx, projectID, "GitHubPullRequest", "owner/repo#7", "head-1", reviewerB.String(), "ui", "CHANGES_REQUESTED", "fix it")
	require.NoError(t, err)
	require.Equal(t, ReviewGateChangesRequested, gate.Status, "a rejection outranks approvals")

	_, err = svc.RecordVerdict(ctx, projectID, "GitHubPullRequest", "owner/repo#7", "head-2", reviewerA.String(), "security", "MERGE", "ok")
	require.Error(t, err, "a verdict for another head must not apply")
}
