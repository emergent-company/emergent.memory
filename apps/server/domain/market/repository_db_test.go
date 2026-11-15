package market

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

func marketTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "market")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

func TestRepositoryRecordInstallAndIdempotentUpsert(t *testing.T) {
	db := marketTestDB(t)
	ctx := context.Background()
	repo := NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID, blueprintID := seedMarketProject(t, db)

	install := &Install{
		ProjectID:    projectID,
		BlueprintID:  blueprintID,
		MarketIssuer: "https://market.example",
		ListingSlug:  "demo",
		Semver:       "1.0.0",
		Digest:       "sha256:abc",
		InstallID:    "inst-1",
		LicenseJTI:   "jti-1",
	}
	_ = orgID
	require.NoError(t, repo.Upsert(ctx, install))

	got, err := repo.Get(ctx, projectID, "https://market.example", "demo", "abc")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, blueprintID, got.BlueprintID)
	assert.Equal(t, InstallStateApplied, got.State)

	// Same (project, issuer, slug, digest) converges on the same row.
	install.InstallID = "inst-2"
	require.NoError(t, repo.Upsert(ctx, install))
	again, err := repo.Get(ctx, projectID, "https://market.example", "demo", "abc")
	require.NoError(t, err)
	assert.Equal(t, got.ID, again.ID)
	assert.Equal(t, "inst-2", again.InstallID)

	missing, err := repo.Get(ctx, projectID, "https://market.example", "other", "abc")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func seedMarketProject(t *testing.T, db bun.IDB) (orgID, projectID, blueprintID string) {
	t.Helper()
	ctx := context.Background()
	orgID = uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "market-test-org")
	require.NoError(t, err)
	projectID = uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "market-test-project")
	require.NoError(t, err)
	blueprintID = uuid.NewString()
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.blueprints (id, name, version, origin, manifest)
		VALUES (?, ?, '1.0.0', 'market', '{}'::jsonb)`, blueprintID, "market-bp-"+uuid.NewString()[:8])
	require.NoError(t, err)
	return orgID, projectID, blueprintID
}
