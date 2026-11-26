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

// TestReviewGateSingleReviewerApprovesWhenQuorumCapped is the end-to-end guard
// for the #1005 deadlock: a source-authored subject keeps the two-reviewer
// policy, but with one configured reviewer the effective quorum is capped at one,
// so that reviewer's MERGE approves instead of leaving the required `reviewed`
// check pending forever (emergent.memory#1030).
func TestReviewGateSingleReviewerApprovesWhenQuorumCapped(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "review_gate_quorum")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	orgID, projectID := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "review-quorum-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "review-quorum-project")
	require.NoError(t, err)

	reviewer := uuid.New()
	required := `[{"agentId":"` + reviewer.String() + `","name":"review"}]`
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.review_gates
		(project_id, subject_type, subject_ref, head_sha, required_reviewers, status)
		VALUES (?, 'GitHubPullRequest', 'owner/repo#1002', 'head-1', ?::jsonb, 'pending')`, projectID, required)
	require.NoError(t, err)

	svc := NewReviewGateService(repo, nil, log)

	// A replay that reveals a bot author raises the stored policy quorum to two.
	gate, err := svc.Ensure(ctx, projectID, "GitHubPullRequest", "owner/repo#1002", "head-1", map[string]any{
		"repo": "owner/repo", "author": "memory-coding-memory-coding[bot]", "author_type": "Bot",
	})
	require.NoError(t, err)
	require.Equal(t, ReviewGateMinReviewersSourceAuthored, gate.MinReviewers)
	require.Equal(t, ReviewGatePending, gate.Status, "no verdict yet keeps the gate pending")

	// With one configured reviewer the effective quorum is capped at one, so the
	// sole reviewer's MERGE approves instead of deadlocking.
	require.Equal(t, 1, gate.minimumReviewers())
	gate, err = svc.RecordVerdict(ctx, projectID, "GitHubPullRequest", "owner/repo#1002", "head-1",
		reviewer.String(), "review", "MERGE", "looks good")
	require.NoError(t, err)
	require.Equal(t, ReviewGateApproved, gate.Status)
}
