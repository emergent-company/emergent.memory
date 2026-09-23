package blueprints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ---------------------------------------------------------------------------
// Shared test helpers (same pattern as domain/skills/store_test.go)
// ---------------------------------------------------------------------------

// connectTestDB opens a throwaway test database owned by this test, so each
// test runs against a uniquely-named database it drops on cleanup. Skips when
// unavailable or in short mode; fails when REQUIRE_DB is set.
func connectTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "blueprints")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// testLogger returns a discard logger for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// uniqueName returns a unique name for a test row (avoids (name, version)
// uniqueness collisions within a test's database).
func uniqueName(prefix string) string {
	return prefix + "-" + uuid.NewString()[:8]
}

// testBlueprint builds a blueprint with defaults suitable for repository tests.
func testBlueprint(name, version string) *Blueprint {
	return &Blueprint{
		Name:        name,
		Version:     version,
		Description: "description for " + name,
		Author:      "test-author",
		Status:      StatusDraft,
		Manifest:    json.RawMessage(fmt.Sprintf(`{"kind":"test","version":%q}`, version)),
		Checksum:    "",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// seedProject creates a real org + project row (kb.agent_definitions and other
// tables reference kb.projects) and returns their IDs.
func seedProject(t *testing.T, db bun.IDB) (orgID, projectID string) {
	t.Helper()
	ctx := context.Background()
	orgID = uuid.NewString()
	projectID = uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "test-org-"+orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "test-project-"+projectID)
	require.NoError(t, err)
	return orgID, projectID
}

// assertAppErrorCode asserts err is an *apperror.Error with the given code.
func assertAppErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr), "expected *apperror.Error, got %T: %v", err, err)
	assert.Equal(t, code, appErr.Code)
}

// ---------------------------------------------------------------------------
// Repository tests (data layer against real Postgres)
// ---------------------------------------------------------------------------

// TestRepository_CreateGetByID_RoundTrip verifies Create populates the ID and
// GetByID returns the full row (manifest survives the jsonb round-trip).
func TestRepository_CreateGetByID_RoundTrip(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	name := uniqueName("roundtrip")
	bp := testBlueprint(name, "1.0.0")
	require.NoError(t, repo.Create(ctx, bp))
	assert.NotEmpty(t, bp.ID, "Create must populate the DB-generated ID")

	got, err := repo.GetByID(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)
	assert.Equal(t, name, got.Name)
	assert.Equal(t, "1.0.0", got.Version)
	assert.Equal(t, StatusDraft, got.Status)
	assert.Equal(t, "test-author", got.Author)
	assert.JSONEq(t, string(bp.Manifest), string(got.Manifest), "manifest must survive the jsonb round-trip")
	assert.False(t, got.CreatedAt.IsZero())
	assert.Equal(t, "global", got.Scope(), "test blueprints have no project_id, so scope is global")
}

// TestRepository_Create_DuplicateNameVersion_Conflict proves the ON CONFLICT
// (name, version) enforcement in Create — the database, not just the service
// pre-check, rejects duplicates.
func TestRepository_Create_DuplicateNameVersion_Conflict(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	name := uniqueName("dup")
	bp1 := testBlueprint(name, "1.0.0")
	require.NoError(t, repo.Create(ctx, bp1))

	bp2 := testBlueprint(name, "1.0.0")
	err := repo.Create(ctx, bp2)
	assertAppErrorCode(t, err, apperror.ErrConflict.Code)
}

// TestRepository_List_FilterAndOrder verifies List with and without a name
// filter, ordered by name then version.
func TestRepository_List_FilterAndOrder(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	name := uniqueName("list")
	for _, v := range []string{"1.0.0", "2.0.0"} {
		require.NoError(t, repo.Create(ctx, testBlueprint(name, v)))
	}
	require.NoError(t, repo.Create(ctx, testBlueprint(uniqueName("other"), "1.0.0")))

	t.Run("filtered", func(t *testing.T) {
		got, err := repo.List(ctx, "", name)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, name, got[0].Name)
		assert.Equal(t, "1.0.0", got[0].Version)
		assert.Equal(t, "2.0.0", got[1].Version, "versions must be ordered ascending")
	})

	t.Run("unfiltered includes all with relative order", func(t *testing.T) {
		got, err := repo.List(ctx, "", "")
		require.NoError(t, err)
		require.NotEmpty(t, got)

		var idx1, idx2 = -1, -1
		for i, b := range got {
			switch {
			case b.Name == name && b.Version == "1.0.0":
				idx1 = i
			case b.Name == name && b.Version == "2.0.0":
				idx2 = i
			}
		}
		assert.GreaterOrEqual(t, idx1, 0, "v1.0.0 row must be present")
		assert.GreaterOrEqual(t, idx2, 0, "v2.0.0 row must be present")
		assert.Less(t, idx1, idx2, "same name must be ordered by version ascending")
	})
}

// TestRepository_ListVersionsByName verifies all versions of a name come back
// ordered by version.
func TestRepository_ListVersionsByName(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	name := uniqueName("versions")
	require.NoError(t, repo.Create(ctx, testBlueprint(name, "2.0.0")))
	require.NoError(t, repo.Create(ctx, testBlueprint(name, "1.0.0")))
	require.NoError(t, repo.Create(ctx, testBlueprint(uniqueName("vother"), "1.0.0")))

	got, err := repo.ListVersionsByName(ctx, "", name)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "1.0.0", got[0].Version)
	assert.Equal(t, "2.0.0", got[1].Version)
}

// TestRepository_Update_InPlace verifies Update persists description, author,
// and manifest changes and bumps updated_at.
func TestRepository_Update_InPlace(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	bp := testBlueprint(uniqueName("update"), "1.0.0")
	require.NoError(t, repo.Create(ctx, bp))

	before, err := repo.GetByID(ctx, "", bp.ID)
	require.NoError(t, err)

	time.Sleep(2 * time.Millisecond)
	bp.Description = "updated description"
	bp.Author = "updated-author"
	bp.Manifest = json.RawMessage(`{"kind":"updated"}`)
	require.NoError(t, repo.Update(ctx, bp))

	got, err := repo.GetByID(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated description", got.Description)
	assert.Equal(t, "updated-author", got.Author)
	assert.JSONEq(t, `{"kind":"updated"}`, string(got.Manifest))
	assert.True(t, got.UpdatedAt.After(before.UpdatedAt), "updated_at must be bumped")
}

// TestRepository_UpdateStatus verifies UpdateStatus transitions status and
// sets the checksum.
func TestRepository_UpdateStatus(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	bp := testBlueprint(uniqueName("status"), "1.0.0")
	require.NoError(t, repo.Create(ctx, bp))

	require.NoError(t, repo.UpdateStatus(ctx, bp.ID, StatusPublished, "abc123"))
	got, err := repo.GetByID(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusPublished, got.Status)
	assert.Equal(t, "abc123", got.Checksum)
}

// TestRepository_ExistsByNameVersion verifies the fast-path existence check.
func TestRepository_ExistsByNameVersion(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	name := uniqueName("exists")
	exists, err := repo.ExistsByNameVersion(ctx, nil, name, "1.0.0")
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, repo.Create(ctx, testBlueprint(name, "1.0.0")))

	exists, err = repo.ExistsByNameVersion(ctx, nil, name, "1.0.0")
	require.NoError(t, err)
	assert.True(t, exists)

	// Different version of the same name must not match.
	exists, err = repo.ExistsByNameVersion(ctx, nil, name, "9.9.9")
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestRepository_Delete_ThenGetByID_NotFound verifies Delete removes the row
// and subsequent lookups (and double deletes) return ErrNotFound.
func TestRepository_Delete_ThenGetByID_NotFound(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	bp := testBlueprint(uniqueName("delete"), "1.0.0")
	require.NoError(t, repo.Create(ctx, bp))

	require.NoError(t, repo.Delete(ctx, bp.ID))

	_, err := repo.GetByID(ctx, "", bp.ID)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)

	err = repo.Delete(ctx, bp.ID)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)
}

// TestRepository_RecordAndListApplied verifies application provenance: an
// apply record joins blueprint metadata, and a re-apply upserts (no duplicate
// row) with the updated checksum.
func TestRepository_RecordAndListApplied(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	_, projectID := seedProject(t, db)

	bp := testBlueprint(uniqueName("applied"), "1.0.0")
	require.NoError(t, repo.Create(ctx, bp))

	userID := uuid.NewString()
	app := &BlueprintApplication{
		BlueprintID: bp.ID,
		ProjectID:   projectID,
		Version:     bp.Version,
		Checksum:    "abc123",
		AppliedBy:   &userID,
		AppliedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, repo.RecordApplication(ctx, app))

	got, err := repo.ListApplied(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, bp.ID, got[0].BlueprintID)
	assert.Equal(t, bp.Name, got[0].Name)
	assert.Equal(t, bp.Version, got[0].Version)
	assert.Equal(t, "abc123", got[0].Checksum)

	// Re-apply with a new checksum must upsert, not insert a duplicate.
	app.Checksum = "def456"
	app.AppliedAt = time.Now()
	app.UpdatedAt = time.Now()
	require.NoError(t, repo.RecordApplication(ctx, app))

	got, err = repo.ListApplied(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, got, 1, "re-apply must upsert, not insert a duplicate row")
	assert.Equal(t, "def456", got[0].Checksum)
}

// TestRepository_SupersedeOnUpgrade verifies that applying a newer version of
// the same blueprint name supersedes the previous application, so ListApplied
// returns only the latest version.
func TestRepository_SupersedeOnUpgrade(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	_, projectID := seedProject(t, db)

	name := uniqueName("upgrade")
	bp1 := testBlueprint(name, "1.0.0")
	require.NoError(t, repo.Create(ctx, bp1))
	bp2 := testBlueprint(name, "2.0.0")
	require.NoError(t, repo.Create(ctx, bp2))

	// Apply v1, then apply v2 (superseding v1).
	require.NoError(t, repo.RecordApplication(ctx, &BlueprintApplication{
		BlueprintID: bp1.ID, ProjectID: projectID, Version: "1.0.0", Status: "applied",
		AppliedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	require.NoError(t, repo.RecordApplication(ctx, &BlueprintApplication{
		BlueprintID: bp2.ID, ProjectID: projectID, Version: "2.0.0", Status: "applied",
		AppliedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	require.NoError(t, repo.SupersedeApplications(ctx, projectID, bp2.ID, name))

	got, err := repo.ListApplied(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, got, 1, "upgrade must supersede the previous version")
	assert.Equal(t, "2.0.0", got[0].Version)
}

// TestRepository_Scope_GlobalVisibleEverywhere verifies a global blueprint is
// visible to every caller: an empty projectID (global-only scope) and any
// projectID (global + own).
func TestRepository_Scope_GlobalVisibleEverywhere(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	bp := testBlueprint(uniqueName("global-vis"), "1.0.0")
	require.NoError(t, repo.Create(ctx, bp))

	// Empty caller (no project context): sees global only.
	got, err := repo.GetByID(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)

	// Any project: sees global too.
	other := uuid.NewString()
	got, err = repo.GetByID(ctx, other, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)

	list, err := repo.List(ctx, other, "")
	require.NoError(t, err)
	found := false
	for _, b := range list {
		if b.ID == bp.ID {
			found = true
		}
	}
	assert.True(t, found, "global blueprint must appear in another project's list")
}

// TestRepository_Scope_PrivateHiddenFromOtherProjects verifies a private
// blueprint is visible only to its owning project — not to other projects and
// not to a caller without a project context.
func TestRepository_Scope_PrivateHiddenFromOtherProjects(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	ctx := context.Background()

	owner := uuid.NewString()
	other := uuid.NewString()
	bp := testBlueprint(uniqueName("private-vis"), "1.0.0")
	bp.ProjectID = &owner
	require.NoError(t, repo.Create(ctx, bp))

	// Owner sees it.
	got, err := repo.GetByID(ctx, owner, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)

	// Other project: not visible (not found).
	_, err = repo.GetByID(ctx, other, bp.ID)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)

	// Empty caller: not visible (global-only scope).
	_, err = repo.GetByID(ctx, "", bp.ID)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)

	// List from another project excludes it.
	list, err := repo.List(ctx, other, "")
	require.NoError(t, err)
	for _, b := range list {
		assert.NotEqual(t, bp.ID, b.ID, "private blueprint must not leak to another project")
	}

	// Scoped existence check: true only for the owning project's scope.
	exists, err := repo.ExistsByNameVersion(ctx, &owner, bp.Name, bp.Version)
	require.NoError(t, err)
	assert.True(t, exists)
	exists, err = repo.ExistsByNameVersion(ctx, &other, bp.Name, bp.Version)
	require.NoError(t, err)
	assert.False(t, exists)
	exists, err = repo.ExistsByNameVersion(ctx, nil, bp.Name, bp.Version)
	require.NoError(t, err)
	assert.False(t, exists)
}
