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

// TestSchemaSQLApplies is a regression guard for the embedded test fixture
// (schema.sql).
//
// Every *_db_test.go in the repo treats any SetupTestDB error as "database
// unavailable" and calls t.Skipf, so a fixture that fails to apply produced a
// silently PASSing suite (see #601: schema.sql referenced core.api_tokens(id)
// before its primary key was declared). This test connects to Postgres
// directly, applies the embedded fixture to a throwaway database, and fails
// loudly when the fixture is broken. It skips only when Postgres itself is
// unreachable.
func TestSchemaSQLApplies(t *testing.T) {
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

	dbName := fmt.Sprintf("go_test_schema_check_%d", time.Now().UnixNano())
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

	for _, ext := range []string{"pgcrypto", `"uuid-ossp"`, "vector"} {
		if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", ext)); err != nil {
			t.Fatalf("create extension %s: %v", ext, err)
		}
	}

	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		t.Fatalf("apply embedded schema.sql: %v", err)
	}
}
