// Package migrate provides database migration functionality using Goose.
package migrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/uptrace/bun"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/emergent-company/emergent/migrations"
)

// Module provides migration dependencies.
var Module = fx.Options(
	fx.Provide(NewMigrator),
)

// Migrator handles database migrations.
type Migrator struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewMigrator creates a new Migrator instance.
func NewMigrator(db *bun.DB, logger *zap.Logger) *Migrator {
	return &Migrator{
		db:     db,
		logger: logger.Named("migrator"),
	}
}

// Up runs all pending migrations.
func (m *Migrator) Up(ctx context.Context) error {
	m.logger.Info("running database migrations")

	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	sqlDB := m.db.DB

	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	m.logger.Info("migrations completed successfully")
	return nil
}

// UpTo runs migrations up to a specific version.
func (m *Migrator) UpTo(ctx context.Context, version int64) error {
	m.logger.Info("running database migrations up to version", zap.Int64("version", version))

	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	sqlDB := m.db.DB

	if err := goose.UpToContext(ctx, sqlDB, ".", version); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	m.logger.Info("migrations completed successfully", zap.Int64("version", version))
	return nil
}

// Down rolls back the last migration.
func (m *Migrator) Down(ctx context.Context) error {
	m.logger.Info("rolling back last migration")

	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	sqlDB := m.db.DB

	if err := goose.DownContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	m.logger.Info("rollback completed successfully")
	return nil
}

// Status returns the current migration status.
func (m *Migrator) Status(ctx context.Context) error {
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	sqlDB := m.db.DB

	if err := goose.StatusContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("failed to get migration status: %w", err)
	}

	return nil
}

// Version returns the current database version.
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return 0, fmt.Errorf("failed to set dialect: %w", err)
	}

	sqlDB := m.db.DB

	version, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return 0, fmt.Errorf("failed to get version: %w", err)
	}

	return version, nil
}

// CreateMigration creates a new migration file.
func (m *Migrator) CreateMigration(name string, migrationType string) error {
	m.logger.Info("creating new migration", zap.String("name", name), zap.String("type", migrationType))

	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	if err := goose.Create(nil, "migrations", name, migrationType); err != nil {
		return fmt.Errorf("failed to create migration: %w", err)
	}

	return nil
}

// MarkApplied marks a migration as applied without running it.
// This is useful for existing databases that already have the schema.
func (m *Migrator) MarkApplied(ctx context.Context, version int64) error {
	m.logger.Info("marking migration as applied", zap.Int64("version", version))

	sqlDB := m.db.DB

	// Insert into goose_db_version table
	_, err := sqlDB.ExecContext(ctx, `
		INSERT INTO goose_db_version (version_id, is_applied)
		VALUES ($1, true)
		ON CONFLICT (version_id) DO UPDATE SET is_applied = true
	`, version)
	if err != nil {
		return fmt.Errorf("failed to mark migration as applied: %w", err)
	}

	return nil
}

// EnsureVersionTable creates the goose_db_version table if it doesn't exist.
func (m *Migrator) EnsureVersionTable(ctx context.Context) error {
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	sqlDB := m.db.DB

	if _, err := goose.EnsureDBVersionContext(ctx, sqlDB); err != nil {
		return fmt.Errorf("failed to ensure version table: %w", err)
	}

	return nil
}

// RunWithDB runs migrations using a raw *sql.DB connection.
func RunWithDB(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
