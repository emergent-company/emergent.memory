package testdb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// TestMigrationsApplyToHead is a regression guard for the embedded migration
// set that the test fixture is now built from.
//
// Every *_db_test.go in the repo treats any SetupTestDB error as "database
// unavailable" and calls t.Skipf, so a broken migration produced a silently
// PASSing suite (see #601: schema.sql referenced core.api_tokens(id) before
// its primary key was declared). This test connects to Postgres directly,
// applies the embedded migrations to a throwaway database, and fails loudly
// when they are broken. It skips only when Postgres itself is unreachable.
func TestMigrationsApplyToHead(t *testing.T) {
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

	dbName := fmt.Sprintf("go_test_migrate_check_%d", time.Now().UnixNano())
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
}
