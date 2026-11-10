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

	// The vendor model must allow two named apps for the same vendor in one org.
	// The legacy unique (organization_id, provider) indexes have to be gone, or
	// the second insert fails. This is a DB-backed guard for 00284: a fake store
	// cannot see the index.
	if indexExists(ctx, pool, "kb", "idx_oauth_client_configs_org_provider") {
		t.Fatalf("legacy idx_oauth_client_configs_org_provider still exists after 00284")
	}
	if !indexExists(ctx, pool, "kb", "idx_oauth_client_configs_org_vendor_name") {
		t.Fatalf("idx_oauth_client_configs_org_vendor_name is missing after 00284")
	}
	var orgID string
	if err := pool.QueryRow(ctx, "INSERT INTO kb.orgs (name) VALUES ('oauth-app-index-test') RETURNING id::text").Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	for _, name := range []string{"One", "Two"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO kb.oauth_client_configs (organization_id, vendor, name, provider, encrypted_client, provider_configs)
			VALUES ($1, 'google', $2, 'google', '\x00'::bytea, '{}'::jsonb)`, orgID, name); err != nil {
			t.Fatalf("insert app %s: %v", name, err)
		}
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
	// The bound is the size of the embedded migration set, not a hand-maintained
	// constant: the distance from head to 00232 grows with every new migration,
	// and a fixed cap silently falls one short (it did at 41 migrations, which
	// failed the DB-backed suite for every PR until the cap was raised).
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("list embedded migrations: %v", err)
	}
	migrationCount := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrationCount++
		}
	}
	if migrationCount == 0 {
		t.Fatalf("no embedded migrations found")
	}

	dropped := false
	for range migrationCount {
		if err := goose.DownContext(ctx, sqlDB, "."); err != nil {
			t.Fatalf("roll back one migration: %v", err)
		}
		if !tableExists(ctx, pool, "kb", "oauth_client_configs") {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Fatalf("expected kb.oauth_client_configs to be dropped after rolling back migrations")
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

func indexExists(ctx context.Context, pool *pgxpool.Pool, schema, name string) bool {
	var dummy int
	err := pool.QueryRow(ctx,
		"SELECT 1 FROM pg_indexes WHERE schemaname = $1 AND indexname = $2", schema, name).Scan(&dummy)
	return err == nil
}
