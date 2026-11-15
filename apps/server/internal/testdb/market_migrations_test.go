package testdb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/migrations"
)

// TestMarketInstallsMigrationUpDown proves migration 00301 adds
// kb.market_installs and the 'market' blueprint origin, and that rolling it back
// restores the narrower origin CHECK and drops the table. It skips only when
// Postgres itself is unreachable.
func TestMarketInstallsMigrationUpDown(t *testing.T) {
	if testing.Short() {
		SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
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

	dbName := fmt.Sprintf("go_test_migrate_check_market_%d", time.Now().UnixNano())
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

	if !tableExists(ctx, pool, "kb", "market_installs") {
		t.Fatalf("expected kb.market_installs after migrations to head")
	}

	var orgID, projectID, blueprintID string
	if err := pool.QueryRow(ctx, "INSERT INTO kb.orgs (name) VALUES ('market-migration-test') RETURNING id::text").Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO kb.projects (organization_id, name) VALUES ($1, 'market-project') RETURNING id::text", orgID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO kb.blueprints (name, version, origin, manifest)
		VALUES ('market-bp', '1.0.0', 'market', '{}'::jsonb)
		RETURNING id::text`).Scan(&blueprintID); err != nil {
		t.Fatalf("insert market blueprint (origin CHECK should allow market): %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO kb.market_installs (project_id, blueprint_id, market_issuer, listing_slug, semver, digest)
		VALUES ($1, $2, 'https://market.example', 'demo', '1.0.0', 'abc123')`, projectID, blueprintID); err != nil {
		t.Fatalf("insert market install: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO kb.market_installs (project_id, blueprint_id, market_issuer, listing_slug, semver, digest)
		VALUES ($1, $2, 'https://market.example', 'demo', '1.0.0', 'abc123')`, projectID, blueprintID); err == nil {
		t.Fatalf("expected duplicate (project, issuer, slug, digest) to be refused")
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
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
	dropped := false
	for range migrationCount {
		if err := goose.DownContext(ctx, sqlDB, "."); err != nil {
			t.Fatalf("roll back one migration: %v", err)
		}
		if !tableExists(ctx, pool, "kb", "market_installs") {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Fatalf("expected kb.market_installs to be dropped by the down migration")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO kb.blueprints (name, version, origin, manifest)
		VALUES ('market-bp-down', '1.0.0', 'market', '{}'::jsonb)`); err == nil {
		t.Fatalf("expected the restored origin CHECK to refuse 'market' after rollback")
	}

	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("reapply migrations: %v", err)
	}
	if !tableExists(ctx, pool, "kb", "market_installs") {
		t.Fatalf("expected kb.market_installs after reapplying the migration")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO kb.blueprints (name, version, origin, manifest)
		VALUES ('market-bp-again', '1.0.0', 'market', '{}'::jsonb)`); err != nil {
		t.Fatalf("expected 'market' origin to be accepted after reapply: %v", err)
	}
}
