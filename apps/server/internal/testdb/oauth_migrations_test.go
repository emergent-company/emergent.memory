package testdb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/migrations"
)

// TestOAuthClientConfigMigrationUpDown proves that migration 00232 (the
// kb.oauth_client_configs table) goes up and down cleanly. It applies the
// embedded migrations to head against a throwaway database, asserts both OAuth
// tables exist, rolls back exactly one migration, asserts the client-configs
// table is gone while the pending-states table survives, then reapplies and
// asserts the client-configs table is back. It skips only when Postgres itself
// is unreachable.
func TestOAuthClientConfigMigrationUpDown(t *testing.T) {
	if testing.Short() {
		SkipOrFatal(t, "skipping database integration test in short mode")
	}

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Match SetupTestDB: pick up .env / .env.local so local credentials are
	// honored, otherwise this guard would skip on the default (wrong) DSN.
	loadRepoEnvFiles()

	baseCfg, err := config.NewConfig(log)
	if err != nil {
		SkipOrFatal(t, "load config: %v", err)
	}
	if err := Apply(&baseCfg.Database); err != nil {
		t.Fatalf("apply %s: %v", URLEnv, err)
	}

	adminCfg := *baseCfg
	adminCfg.Database.Database = "postgres"
	adminPool, err := createPool(ctx, &adminCfg)
	if err != nil {
		SkipOrFatal(t, "postgres unavailable: %v", err)
	}
	defer adminPool.Close()
	if err := adminPool.Ping(ctx); err != nil {
		SkipOrFatal(t, "postgres unavailable: %v", err)
	}

	dbName := fmt.Sprintf("go_test_migrate_check_oauth_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	defer func() {
		_, _ = adminPool.Exec(ctx, fmt.Sprintf(`
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = '%s' AND pid <> pg_backend_pid()
		`, dbName))
		_, _ = adminPool.Exec(ctx, "DROP DATABASE IF EXISTS "+dbName)
	}()

	scratchCfg := *baseCfg
	scratchCfg.Database.Database = dbName
	pool, err := createPool(ctx, &scratchCfg)
	if err != nil {
		t.Fatalf("connect to scratch database: %v", err)
	}
	defer pool.Close()

	if err := applyMigrations(ctx, pool); err != nil {
		t.Fatalf("apply embedded migrations to head: %v", err)
	}

	// Both OAuth tables must exist after applying to head.
	if !tableExists(ctx, pool, "kb", "oauth_pending_states") {
		t.Fatalf("expected kb.oauth_pending_states to exist after applying migrations to head")
	}
	if !tableExists(ctx, pool, "kb", "oauth_client_configs") {
		t.Fatalf("expected kb.oauth_client_configs to exist after applying migrations to head")
	}

	// Open a database/sql handle for goose, which tracks its version in the
	// database itself, so a fresh handle resumes from the same applied state.
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}

	// Roll back migrations newest-first until the client-configs table is
	// dropped. Later migrations may sit between head and 00232 (e.g. 00233 adds
	// a column to it, 00238 adds blueprint model overrides, and 00239 adds a
	// project_id column), so step back one at a time rather than assuming a
	// fixed rollback depth.
	//
	// The bound is derived from the embedded migration set, not hard-coded: it
	// is the number of migrations above 00232 (the calls needed to reach
	// version 232) plus the one call that runs 00232's own Down. A fixed cap
	// was one short as soon as the head→00232 distance reached it — 00272
	// landed and made the old cap of 40 exactly the distance, so the drop was
	// never reached and this test failed on every branch.
	maxRollbacks := migrationsAbove(t, 232) + 1
	dropped := false
	for range maxRollbacks {
		if err := goose.DownContext(ctx, sqlDB, "."); err != nil {
			t.Fatalf("roll back one migration: %v", err)
		}
		if !tableExists(ctx, pool, "kb", "oauth_client_configs") {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Fatalf("expected kb.oauth_client_configs to be dropped after rolling back migrations (gave up after %d rollbacks)", maxRollbacks)
	}
	if !tableExists(ctx, pool, "kb", "oauth_pending_states") {
		t.Fatalf("expected kb.oauth_pending_states to survive rolling back the client-config migration")
	}

	// Reapply the rolled-back migrations and assert the client-configs table is
	// back.
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("reapply migration: %v", err)
	}
	if !tableExists(ctx, pool, "kb", "oauth_client_configs") {
		t.Fatalf("expected kb.oauth_client_configs to exist after reapplying the migration")
	}
}

// tableExists reports whether a table exists in the given schema.
func tableExists(ctx context.Context, pool *pgxpool.Pool, schema, name string) bool {
	var exists bool
	err := pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)",
		schema, name,
	).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

// migrationsAbove counts the embedded migrations numbered above version. Goose
// rolls back exactly one applied migration per call, so this is the number of
// calls needed to reach version's own Down; the numeric distance overestimates
// it when the list has gaps.
func migrationsAbove(t *testing.T, version int) int {
	t.Helper()
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(entry.Name(), "%d_", &n); err == nil && n > version {
			count++
		}
	}
	return count
}
