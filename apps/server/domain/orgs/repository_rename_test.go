package orgs

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// connectOrgsTestDB opens a throwaway test database owned by this test, so each
// test runs against a uniquely-named database it drops on cleanup. Skips when
// unavailable or in short mode; fails when REQUIRE_DB is set.
func connectOrgsTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "orgs_rename")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// TestRepository_UpdateName exercises the rename against a real Postgres when
// one is reachable: success persists name + updated_at, deleted_at IS NULL is
// respected, and an unknown id reports not found.
func TestRepository_UpdateName(t *testing.T) {
	db := connectOrgsTestDB(t)
	repo := NewRepository(db, slog.Default())
	ctx := context.Background()

	org := &Org{Name: "org-rename-src-" + uuid.NewString()}
	_, err := db.NewInsert().Model(org).Returning("*").Exec(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.NewDelete().Model((*Org)(nil)).Where("id = ?", org.ID).Exec(ctx)
	})

	before := org.UpdatedAt

	updated, err := repo.UpdateName(ctx, org.ID, "org-rename-dst-"+uuid.NewString())
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, org.ID, updated.ID)
	assert.NotEqual(t, org.Name, updated.Name)
	assert.False(t, updated.UpdatedAt.Before(before))

	persisted, err := repo.GetByID(ctx, org.ID)
	require.NoError(t, err)
	assert.Equal(t, updated.Name, persisted.Name)

	// Unknown id → not found.
	_, err = repo.UpdateName(ctx, uuid.NewString(), "nope")
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.HTTPStatus)

	// Soft-deleted row → not found, name unchanged.
	_, err = db.NewUpdate().
		Model((*Org)(nil)).
		Set("deleted_at = NOW()").
		Where("id = ?", org.ID).
		Exec(ctx)
	require.NoError(t, err)

	_, err = repo.UpdateName(ctx, org.ID, "after-delete-"+uuid.NewString())
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.HTTPStatus)

	var name string
	require.NoError(t, db.NewSelect().Model((*Org)(nil)).Column("name").Where("id = ?", org.ID).Scan(ctx, &name))
	assert.Equal(t, updated.Name, name)
}
