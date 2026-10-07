package blueprints

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestService_ImportFromGitHub_AdoptsExisting verifies that a repeated import of
// the same (name, version) in the same scope adopts the existing row instead of
// returning the permanent 409.
func TestService_ImportFromGitHub_AdoptsExisting(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	name := uniqueName("adopt")
	version := "1.0.0"
	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: "name: " + name + "\nversion: " + version + "\ndescription: Imported\nauthor: importer\nobjectTypes:\n  - name: Thing\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	// First import creates + publishes.
	bp1, err := svc.ImportFromGitHub(ctx, "", &ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)
	assert.Equal(t, name, bp1.Name)
	assert.Equal(t, StatusPublished, bp1.Status)

	// Second import of the same repo adopts the existing published row (no 409).
	bp2, err := svc.ImportFromGitHub(ctx, "", &ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)
	assert.Equal(t, bp1.ID, bp2.ID, "second import must adopt the existing published row")
	assert.Equal(t, StatusPublished, bp2.Status)
}

// TestService_ImportFromGitHub_AdoptsDraft verifies that importing over a
// pre-existing DRAFT (same name, version, scope) refreshes the draft in place
// and publishes it, rather than failing or duplicating.
func TestService_ImportFromGitHub_AdoptsDraft(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	name := uniqueName("adopt-draft")
	version := "1.0.0"

	// Pre-create a draft in the same (global) scope with stale content.
	stale, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: name, Version: version, Manifest: []byte(`{"stale":true}`), Origin: OriginImport,
	})
	require.NoError(t, err)
	require.Equal(t, StatusDraft, stale.Status)

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: "name: " + name + "\nversion: " + version + "\ndescription: Refreshed\nauthor: importer\nobjectTypes:\n  - name: Thing\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	bp, err := svc.ImportFromGitHub(ctx, "", &ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)
	assert.Equal(t, stale.ID, bp.ID, "import must adopt the existing draft row")
	assert.Equal(t, StatusPublished, bp.Status, "adopted draft must be published")
	assert.Equal(t, "Refreshed", bp.Description, "adopted draft must be refreshed with the imported description")

	stored, err := svc.repo.GetByID(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.Contains(t, string(stored.Manifest), "Thing", "adopted draft must carry the imported manifest")
	assert.NotEmpty(t, stored.Checksum)
}

// TestService_ImportFromGitHub_FailedApplyRevertsAdoptedDraft verifies the
// atomic-import guarantee for the adoption path: when an import adopts (and
// refreshes) a pre-existing DRAFT and then the apply fails, the row must not be
// left PUBLISHED. Pre-#1634 the colliding path returned 409 without mutating;
// the idempotent path must likewise not leak a published artifact on failure.
func TestService_ImportFromGitHub_FailedApplyRevertsAdoptedDraft(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	projectID := uuid.NewString()
	name := uniqueName("adopt-fail")
	version := "1.0.0"

	// Pre-create a draft in the importing project's scope with stale content.
	stale, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: name, Version: version, Manifest: []byte(`{"stale":true}`), ProjectID: &projectID, Origin: OriginImport,
	})
	require.NoError(t, err)
	require.Equal(t, StatusDraft, stale.Status)

	// The archive loads cleanly (loader only validates agent name) but the
	// manifest fails apply-time validation (invalid visibility), so Apply
	// returns before any materialization.
	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/project.yaml", body: "name: " + name + "\nversion: " + version + "\n"},
		{name: "repo-HEAD/agents/bad.yaml", body: "name: BadAgent\nvisibility: bogus\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	_, err = svc.ImportFromGitHub(ctx, projectID, &ImportGitHubRequest{URL: "https://github.com/org/repo", Apply: true})
	require.Error(t, err, "import apply must fail on the invalid agent manifest")

	stored, lerr := svc.repo.GetByID(ctx, projectID, stale.ID)
	require.NoError(t, lerr)
	assert.Equal(t, StatusDraft, stored.Status, "failed apply must revert the adopted draft, not leave it published")
}
