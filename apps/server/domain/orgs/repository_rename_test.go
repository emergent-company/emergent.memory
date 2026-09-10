package orgs

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// loadOrgsEnvFiles loads .env / .env.local walking up from CWD, so the orgs
// integration tests reach the same local Postgres as the server (mirrors
// internal/testutil).
func loadOrgsEnvFiles() {
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
			envLocal := filepath.Join(dir, ".env.local")
			if _, statErr := os.Stat(envLocal); statErr == nil {
				_ = godotenv.Load(filepath.Join(dir, ".env"))
				_ = godotenv.Overload(envLocal)
				break
			}
		}
	}
}

// connectOrgsTestDB connects to Postgres for integration tests, skipping when
// unavailable or in short mode.
func connectOrgsTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	loadOrgsEnvFiles()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		host := cmp.Or(os.Getenv("POSTGRES_HOST"), "localhost")
		port := cmp.Or(os.Getenv("POSTGRES_PORT"), "5432")
		user := cmp.Or(os.Getenv("POSTGRES_USER"), "emergent")
		pass := cmp.Or(os.Getenv("POSTGRES_PASSWORD"), "emergent")
		name := cmp.Or(os.Getenv("POSTGRES_DB"), "emergent")
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			url.QueryEscape(user), url.QueryEscape(pass), host, port, name)
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		t.Skipf("database unavailable (%v), skipping integration test", err)
	}
	db := bun.NewDB(sqldb, pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	return db
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
