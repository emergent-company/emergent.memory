package testutil

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent/emergent-core/internal/config"
)

//go:embed schema.sql
var schemaSQL string

const templateDBName = "go_test_template"

var (
	templateOnce sync.Once
	templateErr  error
)

// TestDB holds test database resources
type TestDB struct {
	Config  *config.Config
	Pool    *pgxpool.Pool
	DB      *bun.DB
	Name    string
	cleanup func()

	// Transaction support for per-test isolation
	tx     bun.Tx
	hasTx  bool
}

// Close releases test database resources
func (t *TestDB) Close() {
	if t.cleanup != nil {
		t.cleanup()
	}
}

// GetDB returns the current database connection.
// If a transaction is active, returns the transaction; otherwise returns the base DB.
func (t *TestDB) GetDB() bun.IDB {
	if t.hasTx {
		return t.tx
	}
	return t.DB
}

// BeginTestTx starts a new transaction for test isolation.
// All database operations should use GetDB() which will return this transaction.
func (t *TestDB) BeginTestTx(ctx context.Context) error {
	if t.hasTx {
		return fmt.Errorf("transaction already started")
	}
	tx, err := t.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	t.tx = tx
	t.hasTx = true
	return nil
}

// RollbackTestTx rolls back the current transaction, discarding all changes.
// This provides fast test cleanup without TRUNCATE.
func (t *TestDB) RollbackTestTx() error {
	if !t.hasTx {
		return nil // No transaction to rollback
	}
	err := t.tx.Rollback()
	t.hasTx = false
	return err
}

// HasTx returns true if a transaction is currently active.
func (t *TestDB) HasTx() bool {
	return t.hasTx
}

// SetupTestDB creates an isolated test database for Go e2e tests.
// It uses a template database pattern for maximum speed:
//   - First call: Creates template DB with schema (~1s)
//   - Subsequent calls: CREATE DATABASE ... TEMPLATE (~50ms)
//
// Requirements:
//   - PostgreSQL must be running
//   - The base database (from POSTGRES_DB) must exist
//
// The test database is automatically dropped when Close() is called.
func SetupTestDB(ctx context.Context, suffix string) (*TestDB, error) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Load base config from environment
	baseCfg, err := config.NewConfig(log)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Ensure template database exists (only done once per test run)
	templateOnce.Do(func() {
		templateErr = ensureTemplateDB(ctx, baseCfg, log)
	})
	if templateErr != nil {
		return nil, fmt.Errorf("ensure template db: %w", templateErr)
	}

	// Create unique database name with go_test prefix
	testDBName := fmt.Sprintf("go_test_%s_%d", suffix, time.Now().UnixNano())

	// Connect to postgres database to create test database from template
	adminCfg := *baseCfg
	adminCfg.Database.Database = "postgres"

	adminPool, err := createPool(ctx, &adminCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	// Create test database from template (very fast - just copies file pointers)
	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", testDBName, templateDBName))
	if err != nil {
		adminPool.Close()
		return nil, fmt.Errorf("create test db from template: %w", err)
	}
	adminPool.Close()

	log.Info("created test database from template", slog.String("name", testDBName))

	// Update config to use test database
	testCfg := *baseCfg
	testCfg.Database.Database = testDBName

	// Connect to test database
	testPool, err := createPool(ctx, &testCfg)
	if err != nil {
		dropTestDB(ctx, baseCfg, testDBName)
		return nil, fmt.Errorf("connect to test db: %w", err)
	}

	// Create Bun DB
	sqldb := stdlib.OpenDBFromPool(testPool)
	bunDB := bun.NewDB(sqldb, pgdialect.New())

	// Cleanup function
	cleanup := func() {
		bunDB.Close()
		testPool.Close()
		dropTestDB(context.Background(), baseCfg, testDBName)
		log.Info("dropped test database", slog.String("name", testDBName))
	}

	return &TestDB{
		Config:  &testCfg,
		Pool:    testPool,
		DB:      bunDB,
		Name:    testDBName,
		cleanup: cleanup,
	}, nil
}

// ensureTemplateDB creates the template database with schema if it doesn't exist.
// This is called once per test run via sync.Once.
func ensureTemplateDB(ctx context.Context, baseCfg *config.Config, log *slog.Logger) error {
	adminCfg := *baseCfg
	adminCfg.Database.Database = "postgres"

	adminPool, err := createPool(ctx, &adminCfg)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer adminPool.Close()

	// Check if template already exists
	var exists bool
	err = adminPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", templateDBName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check template exists: %w", err)
	}

	if exists {
		log.Info("template database already exists", slog.String("name", templateDBName))
		return nil
	}

	log.Info("creating template database", slog.String("name", templateDBName))

	// Create template database
	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", templateDBName))
	if err != nil {
		return fmt.Errorf("create template db: %w", err)
	}

	// Connect to template database
	templateCfg := *baseCfg
	templateCfg.Database.Database = templateDBName
	templatePool, err := createPool(ctx, &templateCfg)
	if err != nil {
		dropTestDB(ctx, baseCfg, templateDBName)
		return fmt.Errorf("connect to template db: %w", err)
	}
	defer templatePool.Close()

	// Create required extensions
	extensions := []string{"pgcrypto", `"uuid-ossp"`, "vector"}
	for _, ext := range extensions {
		_, err = templatePool.Exec(ctx, fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", ext))
		if err != nil {
			dropTestDB(ctx, baseCfg, templateDBName)
			return fmt.Errorf("create extension %s: %w", ext, err)
		}
	}

	// Apply schema
	_, err = templatePool.Exec(ctx, schemaSQL)
	if err != nil {
		dropTestDB(ctx, baseCfg, templateDBName)
		return fmt.Errorf("apply schema: %w", err)
	}

	log.Info("template database created with schema", slog.String("name", templateDBName))
	return nil
}

// createPool creates a pgx connection pool
func createPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.Database.DSN())
	if err != nil {
		return nil, err
	}
	poolConfig.MaxConns = 5
	return pgxpool.NewWithConfig(ctx, poolConfig)
}

// dropTestDB drops a test database
func dropTestDB(ctx context.Context, baseCfg *config.Config, dbName string) {
	// Connect to postgres database (not app database) to drop
	adminCfg := *baseCfg
	adminCfg.Database.Database = "postgres"

	pool, err := createPool(ctx, &adminCfg)
	if err != nil {
		return
	}
	defer pool.Close()

	// Terminate all connections to the test database
	_, _ = pool.Exec(ctx, fmt.Sprintf(`
		SELECT pg_terminate_backend(pid) 
		FROM pg_stat_activity 
		WHERE datname = '%s' AND pid <> pg_backend_pid()
	`, dbName))

	// Drop the database
	_, _ = pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
}

// TruncateTables truncates all tables in the test database.
// Use this between tests to reset state without recreating the database.
// Note: When using transaction rollback pattern, this is typically not needed.
func TruncateTables(ctx context.Context, db bun.IDB) error {
	// Get all tables from kb and core schemas using raw SQL
	type tableInfo struct {
		Schema string `bun:"schemaname"`
		Table  string `bun:"tablename"`
	}
	var tables []tableInfo

	err := db.NewRaw(`
		SELECT schemaname, tablename 
		FROM pg_tables 
		WHERE schemaname IN ('kb', 'core') 
		AND tablename != 'migrations'
	`).Scan(ctx, &tables)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}

	if len(tables) == 0 {
		return nil
	}

	// Build single TRUNCATE statement for all tables (much faster than individual truncates)
	var tableNames []string
	for _, t := range tables {
		tableNames = append(tableNames, fmt.Sprintf("%s.%s", t.Schema, t.Table))
	}

	// Disable triggers and truncate all tables in one statement
	_, _ = db.NewRaw("SET session_replication_role = 'replica'").Exec(ctx)
	defer db.NewRaw("SET session_replication_role = 'origin'").Exec(ctx)

	// Single TRUNCATE for all tables is much faster than 60 individual truncates
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", strings.Join(tableNames, ", "))
	_, err = db.NewRaw(truncateSQL).Exec(ctx)
	if err != nil {
		return fmt.Errorf("truncate tables: %w", err)
	}

	return nil
}

// DropTemplateDB drops the template database. Call this at the end of a test run
// if you want to force schema refresh on next run.
func DropTemplateDB(ctx context.Context) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	baseCfg, err := config.NewConfig(log)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	dropTestDB(ctx, baseCfg, templateDBName)
	return nil
}
