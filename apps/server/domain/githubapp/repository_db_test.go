package githubapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func TestRepositoryDeliveryDedupeAndClaims(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testutil.SetupTestDBOrFail(t, ctx, "githubapp_repo")
	defer db.Close()
	repo := NewRepository(db.DB)

	orgID, projectID, agentID := uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "githubapp-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "githubapp-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`, agentID, "githubapp-review", projectID)
	require.NoError(t, err)

	app := &GitHubApp{
		OrgID:                  orgID,
		AppID:                  4884315,
		Slug:                   "memory-reviewer",
		PrivateKeyEncrypted:    "sealed-key",
		WebhookSecretEncrypted: "sealed-secret",
	}
	require.NoError(t, repo.CreateApp(ctx, app))

	inst := &Installation{
		AppPK:          app.ID,
		OrgID:          orgID,
		ProjectID:      projectID,
		InstallationID: 42,
		Repos:          []string{"owner/repo"},
		AgentID:        agentID,
		Queue:          "review",
		Enabled:        true,
	}
	require.NoError(t, repo.CreateInstallation(ctx, inst))

	found, err := repo.FindEnabledInstallationByInstallationID(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, inst.ID, found.ID)
	require.True(t, found.RepoAllowed("Owner/Repo"))
	require.False(t, found.RepoAllowed("owner/other"))

	// Delivery dedupe.
	require.True(t, mustInsertDelivery(t, repo, &Delivery{DeliveryID: "d1", InstallationID: 42, Repo: "owner/repo", PullRequest: 7, HeadSHA: "h1"}))
	require.False(t, mustInsertDelivery(t, repo, &Delivery{DeliveryID: "d1", InstallationID: 42, Repo: "owner/repo", PullRequest: 7, HeadSHA: "h1"}))

	// Claim reuse for the same head.
	claim1, superseded, err := repo.ClaimReview(ctx, 42, "owner/repo", 7, "h1", "h1")
	require.NoError(t, err)
	require.Empty(t, superseded)
	require.Equal(t, ClaimPending, claim1.State)

	runID := uuid.New()
	ok, err := repo.SetClaimRun(ctx, claim1.ID, runID, ClaimRunning)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.SetClaimRun(ctx, claim1.ID, uuid.New(), ClaimRunning)
	require.NoError(t, err)
	require.False(t, ok, "a claim that already has a run must not accept another")

	claim1b, superseded, err := repo.ClaimReview(ctx, 42, "owner/repo", 7, "h1", "h1")
	require.NoError(t, err)
	require.Empty(t, superseded)
	require.Equal(t, claim1.ID, claim1b.ID, "retry must re-use the claim")

	// An out-of-order delivery for an older head must not cancel the current one.
	_, _, err = repo.ClaimReview(ctx, 42, "owner/repo", 7, "h2", "h1")
	require.ErrorIs(t, err, ErrStaleHead)
	require.Equal(t, ClaimRunning, claimState(t, db.DB, claim1.ID))

	// A newer current head supersedes the old claim and reports its run.
	claim2, superseded, err := repo.ClaimReview(ctx, 42, "owner/repo", 7, "h2", "h2")
	require.NoError(t, err)
	require.NotEqual(t, claim1.ID, claim2.ID)
	require.Contains(t, superseded, runID)
	require.Equal(t, ClaimSuperseded, claimState(t, db.DB, claim1.ID))

	// A head that moves back (force-push/revert) reactivates the claim.
	claim1c, superseded, err := repo.ClaimReview(ctx, 42, "owner/repo", 7, "h1", "h1")
	require.NoError(t, err)
	require.Equal(t, claim1.ID, claim1c.ID)
	require.Equal(t, ClaimPending, claim1c.State)
	require.Nil(t, claim1c.RunID)
	require.Equal(t, ClaimSuperseded, claimState(t, db.DB, claim2.ID))

	// Terminal claims reject late run attachment/state changes.
	ok, err = repo.SetClaimRun(ctx, claim2.ID, uuid.New(), ClaimRunning)
	require.NoError(t, err)
	require.False(t, ok, "superseded claim must not accept a run")
	ok, err = repo.SetClaimState(ctx, claim2.ID, ClaimDone)
	require.NoError(t, err)
	require.False(t, ok, "superseded claim must not transition")
}

func claimState(t *testing.T, db *bun.DB, id uuid.UUID) string {
	t.Helper()
	var state string
	require.NoError(t, db.NewSelect().
		Table("core.github_pr_review_claims").
		Column("state").
		Where("id = ?", id).
		Scan(context.Background(), &state))
	return state
}

func mustInsertDelivery(t *testing.T, repo *Repository, d *Delivery) bool {
	t.Helper()
	inserted, err := repo.InsertDelivery(context.Background(), d)
	require.NoError(t, err)
	return inserted
}
