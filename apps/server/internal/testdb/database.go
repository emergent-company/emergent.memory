package testdb

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/migrations"
)

const (
	templateDBName = "go_test_template"

	// templateMetaTable records a fingerprint of the exact schema the template
	// was built from. It lives in the `public` schema so it is not truncated by
	// TruncateTables (which only touches kb/core) and does not shadow any
	// application table.
	templateMetaTable = "public.go_test_template_meta"
)

// templateLockKey is a stable Postgres advisory-lock key that serializes the
// go_test_template bootstrap across concurrent processes. It is a session-level
// lock, so it is released automatically if the holding process dies.
const templateLockKey int64 = 0x6d656d6f7279 // "memory"

// schemaFingerprint identifies the embedded migration set a template was built
// from. A template whose recorded fingerprint differs — a stale template left
// on a persistent Postgres by an older migration set, or a bootstrap that
// stopped before recording it — is treated as incomplete and rebuilt under the
// advisory lock, so throwaway databases can never be cloned from an out-of-date
// schema.
var schemaFingerprint = migrationsFingerprint()

// migrationsFingerprint hashes the embedded migration set (filename + content,
// sorted) so the template is rebuilt whenever any migration changes. This is
// the same guarantee the old schema.sql content hash provided, but keyed to the
// single source of truth (migrations) instead of a snapshot that could drift
// behind it (issue #820).
func migrationsFingerprint() string {
	h := sha256.New()
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("read embedded migrations: %v", err))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fmt.Fprintf(h, "%s\x00", e.Name())
		b, err := migrations.FS.ReadFile(e.Name())
		if err != nil {
			panic(fmt.Sprintf("read migration %q: %v", e.Name(), err))
		}
		h.Write(b)
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

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
	tx    bun.Tx
	hasTx bool
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

	// Load .env files if present (for local development), matching server startup.
	loadRepoEnvFiles()

	// Load base config from environment
	baseCfg, err := config.NewConfig(log)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Prefer an explicit test DSN over ambient POSTGRES_* so a test run cannot
	// accidentally target a shared / application database.
	if err := Apply(&baseCfg.Database); err != nil {
		return nil, err
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

// SetupTestDBOrFail is SetupTestDB with the repo's standard database-required
// behaviour: when REQUIRE_DB is set (CI's DB-backed job) a setup failure fails
// the test instead of skipping it, so database coverage cannot silently
// vanish. Locally (REQUIRE_DB unset) it skips exactly like the hand-rolled
// t.Skipf call sites it replaces.
//
// Prefer this over SetupTestDB in tests. See the package docs for the env vars.
func SetupTestDBOrFail(t testing.TB, ctx context.Context, suffix string) *TestDB {
	t.Helper()
	db, err := SetupTestDB(ctx, suffix)
	if err != nil {
		SkipOrFatal(t, UnavailableMsg, err)
	}
	return db
}

// loadRepoEnvFiles loads .env and .env.local from the nearest ancestor
// directory containing .env.local, matching server startup. This lets tests
// pick up local database credentials without re-exporting them.
func loadRepoEnvFiles() {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		envLocal := filepath.Join(dir, ".env.local")
		if _, statErr := os.Stat(envLocal); statErr == nil {
			_ = godotenv.Load(filepath.Join(dir, ".env"))
			_ = godotenv.Overload(envLocal)
			return
		}
	}
}

// ensureTemplateDB creates the template database with schema if it doesn't
// already exist complete. It is called once per test run via sync.Once, but its
// correctness does not depend on that: the whole check + create + apply-schema
// sequence runs under a Postgres session-level advisory lock so concurrent
// package binaries cannot race the CREATE DATABASE.
func ensureTemplateDB(ctx context.Context, baseCfg *config.Config, log *slog.Logger) error {
	adminCfg := *baseCfg
	adminCfg.Database.Database = "postgres"

	adminPool, err := createPool(ctx, &adminCfg)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer adminPool.Close()

	// Serialize the bootstrap across processes: acquire a session-level
	// advisory lock on one dedicated connection and hold it until the template
	// is verified complete or fully rebuilt.
	conn, err := adminPool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire template lock connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", templateLockKey); err != nil {
		return fmt.Errorf("acquire template advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", templateLockKey)
	}()

	complete, err := templateDBComplete(ctx, baseCfg)
	if err != nil {
		return err
	}
	if complete {
		log.Info("template database already exists", slog.String("name", templateDBName))
		return nil
	}

	// Missing or incomplete (e.g. a crashed bootstrap): drop any stale copy and
	// rebuild from scratch, all still under the advisory lock.
	var exists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", templateDBName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check template exists: %w", err)
	}
	if exists {
		if err := dropDatabaseConn(ctx, conn, templateDBName); err != nil {
			return fmt.Errorf("drop incomplete template: %w", err)
		}
	}

	log.Info("creating template database", slog.String("name", templateDBName))
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", templateDBName)); err != nil {
		return fmt.Errorf("create template db: %w", err)
	}

	// Connect to template database
	templateCfg := *baseCfg
	templateCfg.Database.Database = templateDBName
	templatePool, err := createPool(ctx, &templateCfg)
	if err != nil {
		_ = dropDatabaseConn(ctx, conn, templateDBName)
		return fmt.Errorf("connect to template db: %w", err)
	}
	defer templatePool.Close()

	// Apply the embedded migrations to head. The baseline migration (00001)
	// creates the kb/core schemas and the pgcrypto / uuid-ossp / vector
	// extensions, so the template is built from the exact same source
	// production migrates — it can no longer drift from a stale schema.sql
	// snapshot (issue #820).
	if err := applyMigrations(ctx, templatePool); err != nil {
		_ = dropDatabaseConn(ctx, conn, templateDBName)
		return fmt.Errorf("apply migrations to template: %w", err)
	}

	// Record the schema fingerprint last, so its presence proves the schema was
	// applied completely. templateDBComplete() compares it on reuse.
	if _, err := templatePool.Exec(ctx, fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (fingerprint text NOT NULL)", templateMetaTable),
	); err != nil {
		_ = dropDatabaseConn(ctx, conn, templateDBName)
		return fmt.Errorf("create template meta table: %w", err)
	}
	if _, err := templatePool.Exec(ctx, fmt.Sprintf(
		"INSERT INTO %s (fingerprint) VALUES ($1)", templateMetaTable),
		schemaFingerprint,
	); err != nil {
		_ = dropDatabaseConn(ctx, conn, templateDBName)
		return fmt.Errorf("record template schema fingerprint: %w", err)
	}

	log.Info("template database created with schema", slog.String("name", templateDBName))
	return nil
}

// templateDBComplete reports whether the template database already holds the
// schema built from the *current* migration set. It compares the fingerprint the
// template recorded at build time against schemaFingerprint; a template that is
// missing the fingerprint table (built by an older harness, or by a bootstrap
// that stopped before recording it) or records a different one is treated as
// incomplete and must be rebuilt.
func templateDBComplete(ctx context.Context, baseCfg *config.Config) (bool, error) {
	cfg := *baseCfg
	cfg.Database.Database = templateDBName
	pool, err := createPool(ctx, &cfg)
	if err != nil {
		// Cannot connect → template does not exist (or is not usable yet).
		return false, nil
	}
	defer pool.Close()

	var recorded string
	err = pool.QueryRow(ctx, fmt.Sprintf("SELECT fingerprint FROM %s LIMIT 1", templateMetaTable)).Scan(&recorded)
	if err != nil {
		// Missing/unreadable fingerprint table → stale or incomplete template.
		return false, nil
	}
	return recorded == schemaFingerprint, nil
}

// applyMigrations runs the embedded goose migrations to head against the given
// pool. It is used by both the template bootstrap and the TestMigrationsApplyToHead
// guard, so the two can never drift on how migrations are applied. Goose is
// used directly rather than via internal/migrate because internal/migrate's own
// tests import this package, which would form an import cycle.
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
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

// dropDatabaseConn drops a database using an already-acquired admin connection
// (used under the template advisory lock). dbName is a constant, not
// user-controlled, so the interpolation is safe.
func dropDatabaseConn(ctx context.Context, conn *pgxpool.Conn, dbName string) error {
	_, err := conn.Exec(ctx, fmt.Sprintf(`
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = '%s' AND pid <> pg_backend_pid()
	`, dbName))
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	return err
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
	defer func() { _, _ = db.NewRaw("SET session_replication_role = 'origin'").Exec(ctx) }()

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

	// Resolve the DSN exactly like SetupTestDB: load local dotenv files and let
	// TEST_DATABASE_URL override ambient POSTGRES_*. Otherwise a caller with a
	// throwaway TEST_DATABASE_URL but no matching POSTGRES_* would silently
	// target the shared localhost:5432 default here.
	loadRepoEnvFiles()
	baseCfg, err := config.NewConfig(log)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := Apply(&baseCfg.Database); err != nil {
		return err
	}
	dropTestDB(ctx, baseCfg, templateDBName)
	return nil
}
