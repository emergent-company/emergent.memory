package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/extra/bunotel"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/logger"
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

	// Create pool. NewWithConfig does not retain this context; it only bounds
	// the construction call itself.
	poolCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(poolCtx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	// Verify connection with a bounded retry/backoff. A single Ping used to
	// hard-fail startup, so a transient DB blip during a restart-flap would
	// crash the process and amplify the flap (issue #1481). Retry the same pool
	// (never recreate it) and only give up once the attempt budget is spent.
	if err := pingWithRetry(context.Background(), pool.Ping,
		cfg.Database.PingMaxAttempts,
		cfg.Database.PingInitialBackoff,
		cfg.Database.PingMaxBackoff,
		log,
	); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	log.Info("database pool created",
		slog.String("host", cfg.Database.Host),
		slog.Int("port", cfg.Database.Port),
		slog.String("database", cfg.Database.Database),
		slog.Int("max_conns", cfg.Database.MaxOpenConns),
	)

	// Register lifecycle hooks.
	//
	// Shutdown ordering (issue #1481): fx runs OnStop in reverse registration
	// order. The HTTP server (server.StartServer) depends on *bun.DB, so it is
	// constructed after this hook is registered and therefore stops BEFORE it.
	// The server drains in-flight requests first; only then does bun close,
	// followed by the pgx pool here. Never give the server an earlier
	// registration slot without re-checking this contract.
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info("closing database pool")
			pool.Close()
			return nil
		},
	})

	return pool, nil
}

// pingAttemptTimeout bounds a single startup ping so a hung TCP connect cannot
// consume the whole retry budget.
const pingAttemptTimeout = 10 * time.Second

// pingWithRetry calls ping up to attempts times, sleeping with exponential
// backoff (initial, doubling, capped at maxBackoff) between failures, and
// returns nil on the first success. It returns the last ping error once the
// attempts are exhausted, or the context error if ctx is cancelled while
// waiting. attempts < 1 is treated as 1; a non-positive initial backoff or
// maxBackoff means no delay between attempts.
//
// Extracted from NewPgxPool so the retry policy is unit-testable with an
// injected ping (issue #1481).
func pingWithRetry(ctx context.Context, ping func(context.Context) error, attempts int, initial, maxBackoff time.Duration, log *slog.Logger) error {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	backoff := initial
	for attempt := 1; attempt <= attempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, pingAttemptTimeout)
		lastErr = ping(pingCtx)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == attempts {
			break
		}

		log.Warn("database ping failed, retrying",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", attempts),
			slog.Duration("backoff", backoff),
			logger.Error(lastErr))

		if backoff > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("ping retry aborted: %w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(backoff):
			}
			next := backoff * 2
			if maxBackoff > 0 && next > maxBackoff {
				next = maxBackoff
			}
			backoff = next
		}
	}

	return fmt.Errorf("ping failed after %d attempt(s): %w", attempts, lastErr)
}

// NewBunDB creates a Bun ORM instance wrapping the pgx pool
func NewBunDB(lc fx.Lifecycle, pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger) (*bun.DB, error) {
	log = log.With(logger.Scope("bun"))

	// Convert pgx pool to database/sql compatible connection
	sqldb := stdlib.OpenDBFromPool(pool)

	// Create Bun DB with PostgreSQL dialect
	db := bun.NewDB(sqldb, pgdialect.New())

	// Emit an OTel span per query when tracing is enabled (see addTracingHook).
	addTracingHook(db, cfg)

	// Add query logging hook if debug enabled
	if cfg.Database.QueryDebug {
		db.AddQueryHook(&queryLoggingHook{log: log})
	}

	log.Info("bun database initialized")

	// Register lifecycle hooks.
	//
	// Stops before the pgx pool (this hook is registered first, so it runs
	// last) and after the HTTP server (which depends on *bun.DB and therefore
	// registered later, so it runs first). See NewPgxPool for the full
	// server→bun→pool drain ordering contract (issue #1481).
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info("closing bun database")
			return db.Close()
		},
	})

	return db, nil
}

// addTracingHook registers the bunotel query hook when both the tracing feature
// (FEATURE_TRACING) and OTel export (OTEL_EXPORTER_OTLP_ENDPOINT) are enabled.
//
// It requires Features.Tracing because the tracing module — which installs the
// OTLP TracerProvider — is only loaded when that flag is set (see cmd/server
// main). Without the provider, spans would be created against the no-op global
// provider: useless work on every query. Inert (hook not registered) otherwise,
// so there is zero per-query overhead when tracing is disabled.
func addTracingHook(db *bun.DB, cfg *config.Config) {
	if cfg.Features.Tracing && cfg.Otel.Enabled() {
		db.AddQueryHook(bunotel.NewQueryHook(bunotel.WithDBName(cfg.Database.Database)))
	}
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

// statementTimeoutKey carries a per-request statement_timeout through context.
type statementTimeoutKey struct{}

// WithStatementTimeout returns a context that asks BeginSafeTx to bound every
// statement in transactions started from it with PostgreSQL's
// statement_timeout. A non-positive duration leaves ctx unchanged.
//
// The bound is applied inside each transaction via
// set_config('statement_timeout', …, true), so it is released on
// commit/rollback and never leaks to other requests or pooled connections. Bulk
// endpoints use it so one runaway statement fails cleanly instead of holding a
// pool connection (issue #1481).
func WithStatementTimeout(ctx context.Context, d time.Duration) context.Context {
	if d <= 0 {
		return ctx
	}
	return context.WithValue(ctx, statementTimeoutKey{}, d)
}

// statementTimeoutFromContext reports the per-request statement_timeout, if one
// was attached and is positive.
func statementTimeoutFromContext(ctx context.Context) (time.Duration, bool) {
	d, ok := ctx.Value(statementTimeoutKey{}).(time.Duration)
	return d, ok && d > 0
}

// StatementTimeoutFromContext reports the per-request statement_timeout, if one
// is attached and positive. It is the exported form of
// statementTimeoutFromContext, for callers outside this package that need to
// decide whether to run a statement inside a transaction so BeginSafeTx can
// apply the bound (e.g. a single-statement insert reached from a bulk endpoint).
func StatementTimeoutFromContext(ctx context.Context) (time.Duration, bool) {
	return statementTimeoutFromContext(ctx)
}

// BeginSafeTx starts a new transaction and returns a SafeTx wrapper.
//
// If ctx carries a statement timeout (see WithStatementTimeout), the
// transaction sets PostgreSQL's statement_timeout for the duration of the
// transaction before returning.
func BeginSafeTx(ctx context.Context, db bun.IDB) (*SafeTx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if d, ok := statementTimeoutFromContext(ctx); ok {
		// PostgreSQL's SET does not accept bind parameters, so use set_config
		// with the millisecond count as text. is_local=true scopes the setting
		// to this transaction only.
		if _, err := tx.ExecContext(ctx, "SELECT set_config('statement_timeout', ?, true)", strconv.FormatInt(d.Milliseconds(), 10)); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("set statement_timeout: %w", err)
		}
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
