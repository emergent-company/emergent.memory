package testdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// truncateHolderTable is a table the baseline migration always creates (00001).
// TRUNCATE takes ACCESS EXCLUSIVE on every kb/core table, so an ACCESS SHARE
// lock held on any one of them is enough to block the whole reset.
const truncateHolderTable = "kb.projects"

// withShortLockTimeout shortens the production lock_timeout for the duration of
// one test, so a deliberately blocked reset fails in milliseconds instead of
// waiting out the 5 s default. Tests in this package run sequentially and none
// call t.Parallel, so the package-level variable is not shared concurrently.
func withShortLockTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := truncateLockTimeout
	truncateLockTimeout = d
	t.Cleanup(func() { truncateLockTimeout = prev })
}

// relationLockWaitCount counts backends waiting for a lock on table (schema
// qualified). It is the observable side effect of the truncate block.
func relationLockWaitCount(ctx context.Context, conn *pgxpool.Conn, table string) (int, error) {
	var n int
	err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_locks l
		JOIN pg_class c ON c.oid = l.relation
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname || '.' || c.relname = $1
		  AND NOT l.granted
	`, table).Scan(&n)
	return n, err
}

// waitForRelationLockWait blocks until a backend is (want=true) or is no longer
// (want=false) waiting for a lock on table. Polling pg_locks keeps the test
// event-driven instead of relying on fixed sleeps.
func waitForRelationLockWait(ctx context.Context, conn *pgxpool.Conn, table string, want bool) error {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		n, err := relationLockWaitCount(ctx, conn, table)
		if err != nil {
			return err
		}
		if (n > 0) == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for a lock wait on %s (want=%t): %w", table, want, ctx.Err())
		case <-ticker.C:
		}
	}
}

// advisoryLockCount reports how many backends in the current database hold the
// TruncateTables advisory lock (pg_advisory_lock(bigint) is stored as the two
// 32-bit halves classid/objid).
func advisoryLockCount(ctx context.Context, conn *pgxpool.Conn) (int, error) {
	var n int
	err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_locks
		WHERE locktype = 'advisory'
		  AND granted
		  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
		  AND ((classid::bigint << 32) | objid::bigint) = $1
	`, truncateLockKey).Scan(&n)
	return n, err
}

// TestTruncateTablesConcurrent is a regression guard for issue #2325: two or
// more resets of the same database used to race on TRUNCATE's per-table
// ACCESS EXCLUSIVE locks and deadlock (SQLSTATE 40P01). TruncateTables now
// serializes resets with a session-level advisory lock, so concurrent resets
// must all succeed.
func TestTruncateTablesConcurrent(t *testing.T) {
	if testing.Short() {
		SkipOrFatal(t, "skipping database integration test in short mode")
	}

	ctx := context.Background()
	db, err := SetupTestDB(ctx, "truncate_concurrent")
	if err != nil {
		SkipOrFatal(t, UnavailableMsg, err)
	}
	defer db.Close()

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- TruncateTables(ctx, db.DB)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent truncate failed: %v", err)
		}
	}
}

// TestTruncateTablesRetriesBlockingLockHolder is the wiring guard for #2449 and
// #2450: a read transaction holding ACCESS SHARE on a kb table makes TRUNCATE
// wait rather than deadlock. With lock_timeout set on the pinned connection the
// first attempt must abort with 55P03 and the existing retry must run, succeeding
// once the holder releases. The advisory lock must stay held across the retry
// (so the reset is still serialized) and be released exactly once at the end.
func TestTruncateTablesRetriesBlockingLockHolder(t *testing.T) {
	if testing.Short() {
		SkipOrFatal(t, "skipping database integration test in short mode")
	}

	// Long enough for the watcher's 2 ms polls to observe both waits, short
	// enough to keep the test fast.
	withShortLockTimeout(t, 250*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := SetupTestDBOrFail(t, ctx, "truncate_blocking_retry")
	defer db.Close()

	// Holder: an open read transaction keeping ACCESS SHARE on the table.
	holdConn, err := db.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire holder connection: %v", err)
	}
	defer holdConn.Release()
	holdTx, err := holdConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder transaction: %v", err)
	}
	// Unconditional: rolling back an already-closed transaction is a no-op
	// error, and this only runs after the watcher goroutine has finished.
	defer func() { _ = holdTx.Rollback(context.Background()) }()
	if _, err := holdTx.Exec(ctx, "SELECT count(*) FROM "+truncateHolderTable); err != nil {
		t.Fatalf("take ACCESS SHARE on %s: %v", truncateHolderTable, err)
	}

	// Watch from a second connection: attempt 1 waits, aborts on lock_timeout,
	// attempt 2 waits again. Only release the holder while attempt 2 is waiting,
	// so a pass proves the retry (not a first attempt that merely waited longer).
	watchConn, err := db.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire watcher connection: %v", err)
	}
	defer watchConn.Release()

	watcher := make(chan error, 1)
	go func() {
		if err := waitForRelationLockWait(ctx, watchConn, truncateHolderTable, true); err != nil {
			watcher <- fmt.Errorf("attempt 1 never blocked on the lock holder: %w", err)
			return
		}
		if err := waitForRelationLockWait(ctx, watchConn, truncateHolderTable, false); err != nil {
			watcher <- fmt.Errorf("attempt 1 never aborted with lock_timeout: %w", err)
			return
		}
		if err := waitForRelationLockWait(ctx, watchConn, truncateHolderTable, true); err != nil {
			watcher <- fmt.Errorf("attempt 2 was not retried while the holder was still live: %w", err)
			return
		}
		held, err := advisoryLockCount(ctx, watchConn)
		if err != nil {
			watcher <- fmt.Errorf("read advisory locks during retry: %w", err)
			return
		}
		if held != 1 {
			watcher <- fmt.Errorf("advisory lock held %d times across the retry, want exactly 1", held)
			return
		}
		// Release the holder from inside attempt 2's wait so it can succeed.
		if err := holdTx.Rollback(context.Background()); err != nil {
			watcher <- fmt.Errorf("release holder transaction: %w", err)
			return
		}
		watcher <- nil
	}()

	truncErr := TruncateTables(ctx, db.DB)
	watchErr := <-watcher
	if watchErr != nil {
		t.Fatalf("lock-holder watcher: %v (TruncateTables error: %v)", watchErr, truncErr)
	}
	if truncErr != nil {
		t.Fatalf("TruncateTables() = %v, want success after the holder released", truncErr)
	}

	// Unlock exactly once: the advisory lock is gone once TruncateTables returns.
	held, err := advisoryLockCount(ctx, watchConn)
	if err != nil {
		t.Fatalf("read advisory locks after reset: %v", err)
	}
	if held != 0 {
		t.Fatalf("advisory lock held %d times after TruncateTables returned, want 0", held)
	}
}

// TestTruncateTablesFailsLoudlyWhenLockHolderNeverReleases is the other half of
// #2449: a holder that never commits must not hang the suite. The bounded retry
// must surface the 55P03 with its attempt count, release the advisory lock, and
// return well inside the context deadline.
func TestTruncateTablesFailsLoudlyWhenLockHolderNeverReleases(t *testing.T) {
	if testing.Short() {
		SkipOrFatal(t, "skipping database integration test in short mode")
	}

	withShortLockTimeout(t, 100*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db := SetupTestDBOrFail(t, ctx, "truncate_blocking_terminal")
	defer db.Close()

	holdConn, err := db.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire holder connection: %v", err)
	}
	defer holdConn.Release()
	holdTx, err := holdConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder transaction: %v", err)
	}
	defer func() { _ = holdTx.Rollback(context.Background()) }()
	if _, err := holdTx.Exec(ctx, "SELECT count(*) FROM "+truncateHolderTable); err != nil {
		t.Fatalf("take ACCESS SHARE on %s: %v", truncateHolderTable, err)
	}

	start := time.Now()
	truncErr := TruncateTables(ctx, db.DB)
	elapsed := time.Since(start)

	if truncErr == nil {
		t.Fatal("TruncateTables() = nil, want a loud failure while the lock holder never releases")
	}
	var pgErrOut *pgconn.PgError
	if !errors.As(truncErr, &pgErrOut) || pgErrOut.Code != "55P03" {
		t.Fatalf("TruncateTables() = %v, want SQLSTATE 55P03 from lock_timeout", truncErr)
	}
	if !strings.Contains(truncErr.Error(), "(after 5 attempts)") {
		t.Fatalf("error %q does not record the bounded attempt count", truncErr)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("bounded retries took %v, want well under the 15 s context deadline", elapsed)
	}

	// The advisory lock must be released exactly once on the failure path too.
	watchConn, err := db.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire watcher connection: %v", err)
	}
	defer watchConn.Release()
	held, err := advisoryLockCount(ctx, watchConn)
	if err != nil {
		t.Fatalf("read advisory locks after failure: %v", err)
	}
	if held != 0 {
		t.Fatalf("advisory lock held %d times after TruncateTables failed, want 0", held)
	}
}

// TestTruncateTablesRestoresSessionSettings proves the pinned connection leaves
// no session-scoped state behind: lock_timeout must be back to its previous
// value and session_replication_role to 'origin', so a pooled connection cannot
// leak the reset's 5 s timeout into later queries.
func TestTruncateTablesRestoresSessionSettings(t *testing.T) {
	if testing.Short() {
		SkipOrFatal(t, "skipping database integration test in short mode")
	}

	ctx := context.Background()
	db := SetupTestDBOrFail(t, ctx, "truncate_session_reset")
	defer db.Close()

	// A caller-supplied *bun.Conn binds one session, exactly like the pinned
	// connection TruncateTables acquires for a *bun.DB, so the settings can be
	// read back on the same backend.
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire session: %v", err)
	}
	defer conn.Close()

	var beforeTimeout string
	if err := conn.NewRaw("SELECT current_setting('lock_timeout')").Scan(ctx, &beforeTimeout); err != nil {
		t.Fatalf("read lock_timeout before reset: %v", err)
	}
	if err := TruncateTables(ctx, &conn); err != nil {
		t.Fatalf("TruncateTables() = %v", err)
	}

	var afterTimeout, afterRole string
	if err := conn.NewRaw("SELECT current_setting('lock_timeout')").Scan(ctx, &afterTimeout); err != nil {
		t.Fatalf("read lock_timeout after reset: %v", err)
	}
	if err := conn.NewRaw("SELECT current_setting('session_replication_role')").Scan(ctx, &afterRole); err != nil {
		t.Fatalf("read session_replication_role after reset: %v", err)
	}
	if afterTimeout != beforeTimeout {
		t.Fatalf("lock_timeout after reset = %q, want restored %q", afterTimeout, beforeTimeout)
	}
	if afterRole != "origin" {
		t.Fatalf("session_replication_role after reset = %q, want \"origin\"", afterRole)
	}
}
