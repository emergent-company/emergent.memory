package sources_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sources"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestNamedOAuthApps_MultiplePerVendorOnRealSchema is the regression guard for
// migration 00283. Before that migration qualified the legacy index drops, the
// unique indexes on (scope, provider) survived it and a second named app for the
// same vendor in the same scope failed with 23505 — which defeats the whole
// point of named, vendor-level apps. The test runs against the real, fully
// migrated schema: it asserts the legacy indexes are gone, then inserts two
// Google apps in one org and two in one project.
func TestNamedOAuthApps_MultiplePerVendorOnRealSchema(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "oauth_apps_multi_per_vendor")
	t.Cleanup(tdb.Close)

	orgID, projectID := uuid.New(), uuid.New()
	require.NoError(t, testutil.CreateTestProject(ctx, tdb.DB, testutil.TestProject{
		ID:    projectID.String(),
		Name:  "Named OAuth Apps",
		OrgID: orgID.String(),
	}, ""))

	// The legacy unique indexes must not exist after 00283. If they do, the
	// inserts below fail, so this is the direct assertion of the migration fix.
	var legacyIndexes int
	require.NoError(t, tdb.DB.NewRaw(`
		SELECT count(*)
		FROM pg_indexes
		WHERE schemaname = 'kb'
		  AND indexname IN (
		      'idx_oauth_client_configs_org_provider',
		      'idx_oauth_client_configs_project_provider'
		  )
	`).Scan(ctx, &legacyIndexes))
	require.Zero(t, legacyIndexes,
		"legacy unique (scope, provider) index still exists; a second app per vendor would be rejected")

	repo := sources.NewRepository(tdb.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	blob := []byte("encrypted-client-blob")

	createOrg := func(name string) error {
		_, err := repo.CreateOAuthClientConfig(ctx, &sources.OAuthClientConfig{
			OrganizationID:  orgID,
			Vendor:          "google",
			Name:            name,
			Provider:        "google",
			EncryptedClient: blob,
		})
		return err
	}
	require.NoError(t, createOrg("Drive Production"))
	require.NoError(t, createOrg("Drive Sandbox"),
		"a second named app for the same vendor in one org scope must insert")

	createProject := func(name string) error {
		_, err := repo.CreateProjectOAuthClientConfig(ctx, &sources.OAuthClientConfig{
			OrganizationID:  orgID,
			ProjectID:       &projectID,
			Vendor:          "google",
			Name:            name,
			Provider:        "google",
			EncryptedClient: blob,
		})
		return err
	}
	require.NoError(t, createProject("Project Production"))
	require.NoError(t, createProject("Project Sandbox"),
		"a second named app for the same vendor in one project scope must insert")

	// The replacement index must still reject a duplicate name (case-insensitive)
	// in the same scope and vendor, or the feature would lose its uniqueness.
	err := createOrg("drive production")
	require.Error(t, err, "the same name in one scope and vendor must still conflict")
	require.Contains(t, err.Error(), "idx_oauth_client_configs_org_vendor_name",
		"the conflict must come from the new (scope, vendor, name) index")
}
