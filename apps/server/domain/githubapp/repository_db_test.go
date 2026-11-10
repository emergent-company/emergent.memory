package githubapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

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

	app := &GitHubApp{
		OrgID:                  uuid.New(),
		AppID:                  4884315,
		Slug:                   "memory-reviewer",
		PrivateKeyEncrypted:    "sealed-key",
		WebhookSecretEncrypted: "sealed-secret",
	}
	require.NoError(t, repo.CreateApp(ctx, app))

	inst := &Installation{
		AppPK:          app.ID,
		OrgID:          app.OrgID,
		ProjectID:      uuid.New(),
		InstallationID: 42,
		Repos:          []string{"owner/repo"},
		AgentID:        uuid.New(),
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
	claim1, superseded, err := repo.ClaimReview(ctx, 42, "owner/repo", 7, "h1")
	require.NoError(t, err)
	require.Empty(t, superseded)
	require.Equal(t, ClaimPending, claim1.State)

	runID := uuid.New()
	require.NoError(t, repo.SetClaimRun(ctx, claim1.ID, runID, ClaimRunning))

	claim1b, superseded, err := repo.ClaimReview(ctx, 42, "owner/repo", 7, "h1")
	require.NoError(t, err)
	require.Empty(t, superseded)
	require.Equal(t, claim1.ID, claim1b.ID, "retry must re-use the claim")

	// A newer head supersedes the old claim and reports its run for cancellation.
	claim2, superseded, err := repo.ClaimReview(ctx, 42, "owner/repo", 7, "h2")
	require.NoError(t, err)
	require.NotEqual(t, claim1.ID, claim2.ID)
	require.Contains(t, superseded, runID)

	var state string
	require.NoError(t, db.DB.NewSelect().
		Table("core.github_pr_review_claims").
		Column("state").
		Where("id = ?", claim1.ID).
		Scan(ctx, &state))
	require.Equal(t, ClaimSuperseded, state)
}

func mustInsertDelivery(t *testing.T, repo *Repository, d *Delivery) bool {
	t.Helper()
	inserted, err := repo.InsertDelivery(context.Background(), d)
	require.NoError(t, err)
	return inserted
}
