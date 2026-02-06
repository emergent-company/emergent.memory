package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/logger"
)

var Module = fx.Module("database",
	fx.Provide(
		NewPgxPool,
		NewBunDB,
		// Provide bun.IDB interface binding for modules that use the interface
		fx.Annotate(
			func(db *bun.DB) bun.IDB { return db },
			fx.As(new(bun.IDB)),
		),
	),
)

// NewPgxPool creates a new pgx connection pool
func NewPgxPool(lc fx.Lifecycle, cfg *config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	log = log.With(logger.Scope("database"))

	poolConfig, err := pgxpool.ParseConfig(cfg.Database.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse pgx config: %w", err)
	}

	// Configure pool settings
	poolConfig.MaxConns = int32(cfg.Database.MaxOpenConns)
	poolConfig.MinConns = int32(cfg.Database.MaxIdleConns)
	poolConfig.MaxConnIdleTime = cfg.Database.MaxIdleTime

	// Create pool
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	log.Info("database pool created",
		slog.String("host", cfg.Database.Host),
		slog.Int("port", cfg.Database.Port),
		slog.String("database", cfg.Database.Database),
		slog.Int("max_conns", cfg.Database.MaxOpenConns),
	)

	// Register lifecycle hooks
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info("closing database pool")
			pool.Close()
			return nil
		},
	})

	return pool, nil
}

// NewBunDB creates a Bun ORM instance wrapping the pgx pool
func NewBunDB(lc fx.Lifecycle, pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger) (*bun.DB, error) {
	log = log.With(logger.Scope("bun"))

	// Convert pgx pool to database/sql compatible connection
	sqldb := stdlib.OpenDBFromPool(pool)

	// Create Bun DB with PostgreSQL dialect
	db := bun.NewDB(sqldb, pgdialect.New())

	// Add query logging hook if debug enabled
	if cfg.Database.QueryDebug {
		db.AddQueryHook(&queryLoggingHook{log: log})
	}

	log.Info("bun database initialized")

	// Register lifecycle hooks
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info("closing bun database")
			return db.Close()
		},
	})

	return db, nil
}

// queryLoggingHook implements bun.QueryHook for query logging
type queryLoggingHook struct {
	log *slog.Logger
}

func (h *queryLoggingHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	return ctx
}

func (h *queryLoggingHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	duration := time.Since(event.StartTime)

	if event.Err != nil && event.Err != sql.ErrNoRows {
		h.log.Error("query error",
			slog.String("query", event.Query),
			slog.Duration("duration", duration),
			logger.Error(event.Err),
		)
		return
	}

	// Log slow queries as warnings
	if duration > 3*time.Second {
		h.log.Warn("slow query",
			slog.String("query", event.Query),
			slog.Duration("duration", duration),
		)
		return
	}

	// Debug log all queries
	h.log.Debug("query",
		slog.String("query", event.Query),
		slog.Duration("duration", duration),
	)
}

// SetRLSContext sets the RLS context variables for the current connection
// This must be called within a transaction to be effective
func SetRLSContext(ctx context.Context, db bun.IDB, projectID string) error {
	if projectID == "" {
		return nil
	}

	// Set the project ID for RLS policies
	_, err := db.ExecContext(ctx, "SET app.current_project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("set RLS context: %w", err)
	}

	return nil
}

// SafeTx wraps a bun.Tx to make Rollback safe to call after Commit.
//
// In PostgreSQL with savepoints (nested transactions), calling ROLLBACK TO SAVEPOINT
// after RELEASE SAVEPOINT causes an error that aborts the outer transaction.
// This wrapper tracks whether Commit was called and makes Rollback a no-op afterwards.
//
// Usage:
//
//	tx, err := BeginSafeTx(ctx, db)
//	if err != nil {
//	    return err
//	}
//	defer tx.Rollback() // Safe to call even after Commit
//
//	// ... do work ...
//
//	return tx.Commit()
type SafeTx struct {
	bun.Tx
	committed bool
}

// BeginSafeTx starts a new transaction and returns a SafeTx wrapper.
func BeginSafeTx(ctx context.Context, db bun.IDB) (*SafeTx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &SafeTx{Tx: tx}, nil
}

// Commit commits the transaction and marks it as committed.
func (tx *SafeTx) Commit() error {
	if tx.committed {
		return nil
	}
	err := tx.Tx.Commit()
	if err == nil {
		tx.committed = true
	}
	return err
}

// Rollback rolls back the transaction only if it hasn't been committed.
// This is safe to call in a defer statement even after Commit.
func (tx *SafeTx) Rollback() error {
	if tx.committed {
		return nil
	}
	return tx.Tx.Rollback()
}
