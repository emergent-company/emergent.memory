package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	dbBackupBucket        = "database-backups"
	dbBackupRetentionDays = 10
	// dbBackupTimeout is a self-contained bound on the preflight + pg_dump +
	// upload work so Run does not rely on its caller to supply a deadline.
	// The scheduler already wraps every task in its own 30m timeout, but Run is
	// also reachable outside that path, so it bounds itself defensively.
	dbBackupTimeout = 30 * time.Minute
	// dbBackupResultTimeout bounds the terminal failed/completed record write,
	// which runs on a context detached from the backup deadline so a timed-out
	// run is still persisted.
	dbBackupResultTimeout = 30 * time.Second
	// dbBackupUnlockTimeout bounds the advisory-lock release on the cleanup path.
	dbBackupUnlockTimeout = 5 * time.Second

	// dbBackupLockKey is a stable, arbitrary Postgres session-level advisory-lock
	// key that serializes database_backup runs across processes. The in-process
	// `running` flag cannot observe other server replicas, so this key is what
	// actually prevents overlapping pg_dump processes exhausting max_connections
	// when the API runs with replicas > 1. Chosen to not collide with
	// testdb.templateLockKey (0x6d656d6f7279).
	dbBackupLockKey int64 = 0x6d656d64626275 // "mem_dbbu"
)

// DatabaseBackup represents a database backup record in kb.database_backups
type DatabaseBackup struct {
	bun.BaseModel `bun:"table:kb.database_backups,alias:db"`

	ID          string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	Status      string     `bun:"status,notnull,default:'pending'"`
	StorageKey  *string    `bun:"storage_key"`
	SizeBytes   *int64     `bun:"size_bytes"`
	StartedAt   *time.Time `bun:"started_at"`
	CompletedAt *time.Time `bun:"completed_at"`
	Error       *string    `bun:"error"`
	CreatedAt   time.Time  `bun:"created_at,notnull,default:current_timestamp"`
}

// DatabaseBackupTask runs pg_dump and uploads the result to MinIO
type DatabaseBackupTask struct {
	db      *bun.DB
	storage *storage.Service
	cfg     *config.Config
	log     *slog.Logger

	// running guards against overlapping runs within this process. Without it a
	// hung run can stack additional pg_dump processes within a single replica.
	// Cross-process overlap is prevented by a Postgres advisory lock (see Run).
	running atomic.Bool

	// backupFn, when non-nil, replaces the preflight + dump + upload work in Run.
	// Tests use it to simulate a hung/blocking backup; production leaves it nil.
	backupFn func(ctx context.Context, record *DatabaseBackup) error
}

// NewDatabaseBackupTask creates a new DatabaseBackupTask and ensures the backup bucket exists.
func NewDatabaseBackupTask(db *bun.DB, storageSvc *storage.Service, cfg *config.Config, log *slog.Logger) *DatabaseBackupTask {
	t := &DatabaseBackupTask{
		db:      db,
		storage: storageSvc,
		cfg:     cfg,
		log:     log.With(logger.Scope("scheduler.database_backup")),
	}

	// Ensure bucket exists at startup (best-effort)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := storageSvc.EnsureBucket(ctx, dbBackupBucket); err != nil {
		log.Warn("failed to ensure database-backups bucket (will retry on first run)",
			slog.String("error", err.Error()))
	}

	return t
}

// Run executes the database backup task.
func (t *DatabaseBackupTask) Run(ctx context.Context) error {
	// Skip-if-running guard must come first, before inserting the running
	// record, so an overlapping run in this process does not stack a second
	// pg_dump.
	if !t.running.CompareAndSwap(false, true) {
		t.log.Warn("database backup already running; skipping overlapping run")
		return nil
	}
	defer t.running.Store(false)

	// Cross-process guard: a session-level Postgres advisory lock on a dedicated
	// connection refuses a concurrent run from another replica. The in-process
	// flag above cannot observe other processes, and a per-process flag alone
	// cannot prevent max_connections exhaustion under replicas > 1.
	unlock, err := t.acquireBackupLock(ctx)
	if err != nil {
		return err
	}
	if unlock == nil {
		t.log.Warn("database backup already running in another process; skipping overlapping run")
		return nil
	}
	defer unlock()

	t.log.Info("starting database backup")
	start := time.Now()

	// 1. Insert a running record
	now := time.Now()
	record := &DatabaseBackup{
		Status:    "running",
		StartedAt: &now,
	}
	if _, err := t.db.NewInsert().Model(record).Returning("id").Exec(ctx); err != nil {
		return fmt.Errorf("insert backup record: %w", err)
	}

	// 2. Preflight the pg_dump ↔ server version pairing before dumping, so a
	// mismatch is persisted on the record with an actionable message. Only run
	// the dump itself once the preflight passes. Bound preflight + dump + upload
	// with backupCtx so a hung pg_dump/upload cannot run forever; the terminal
	// write below uses a detached context so a timed-out run is still recorded.
	backupCtx, cancel := context.WithTimeout(ctx, dbBackupTimeout)
	defer cancel()
	backupErr := t.runBackupWithPreflight(backupCtx, record)

	// 3. Update record with result. The terminal write must survive the backup
	// deadline: ctx is canceled exactly when the dump times out, so writing the
	// result on ctx would leave the row stuck in `running`. Detach from ctx and
	// apply a short independent bound instead.
	completedAt := time.Now()
	record.CompletedAt = &completedAt

	if backupErr != nil {
		record.Status = "failed"
		errMsg := backupErr.Error()
		record.Error = &errMsg
		t.log.Error("database backup failed",
			slog.String("id", record.ID),
			slog.String("error", backupErr.Error()),
			slog.Duration("duration", time.Since(start)),
		)
	} else {
		record.Status = "completed"
		t.log.Info("database backup completed",
			slog.String("id", record.ID),
			slog.Duration("duration", time.Since(start)),
		)
	}

	resultCtx, resultCancel := context.WithTimeout(context.WithoutCancel(ctx), dbBackupResultTimeout)
	defer resultCancel()
	if _, err := t.db.NewUpdate().Model(record).WherePK().Exec(resultCtx); err != nil {
		t.log.Error("failed to update backup record",
			slog.String("id", record.ID),
			slog.String("error", err.Error()),
		)
	}

	// 4. Enforce retention after every attempt — including failures — so failed
	// rows age out instead of accumulating forever.
	if err := t.enforceRetention(ctx); err != nil {
		// Log but don't fail the task — retention failure must not mask the
		// backup outcome.
		t.log.Error("failed to enforce backup retention", slog.String("error", err.Error()))
	}

	return backupErr
}

// acquireBackupLock takes the cross-process advisory lock on a dedicated
// connection. It returns a release func on success. When another process
// already holds the lock it returns (nil, nil) so the caller skips the run. A
// non-nil error means the lock state could not be determined.
func (t *DatabaseBackupTask) acquireBackupLock(ctx context.Context) (func(), error) {
	conn, err := t.db.DB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire backup lock connection: %w", err)
	}

	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", dbBackupLockKey).Scan(&locked); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire backup advisory lock: %w", err)
	}
	if !locked {
		_ = conn.Close()
		return nil, nil
	}

	// Release the session-level lock on this same connection before returning it
	// to the pool. Use a detached, bounded context so a canceled run context
	// cannot skip the release.
	return func() {
		defer conn.Close()
		releaseCtx, cancel := context.WithTimeout(context.Background(), dbBackupUnlockTimeout)
		defer cancel()
		if _, err := conn.ExecContext(releaseCtx, "SELECT pg_advisory_unlock($1)", dbBackupLockKey); err != nil {
			t.log.Warn("failed to release backup advisory lock", slog.String("error", err.Error()))
		}
	}, nil
}

// runBackupWithPreflight runs the preflight check and then the dump/upload,
// unless a test seam (backupFn) overrides the whole operation.
func (t *DatabaseBackupTask) runBackupWithPreflight(ctx context.Context, record *DatabaseBackup) error {
	if t.backupFn != nil {
		return t.backupFn(ctx, record)
	}
	if err := t.preflightPgDump(ctx); err != nil {
		return err
	}
	return t.runBackup(ctx, record)
}

// pgMajorFromVersionNum extracts the major version from a PostgreSQL
// server_version_num integer (e.g. 170011 → 17).
func pgMajorFromVersionNum(num int) int {
	return num / 10000
}

// pgDumpMajorFromVersionOutput parses `pg_dump --version` output such as
// `pg_dump (PostgreSQL) 17.11 (Debian 17.11-3.pgdg12+1)` and returns the first
// integer appearing after the `PostgreSQL)` marker (the client major version).
func pgDumpMajorFromVersionOutput(out string) (int, error) {
	const marker = "PostgreSQL)"
	idx := strings.Index(out, marker)
	if idx < 0 {
		return 0, fmt.Errorf("pg_dump --version output %q does not contain the %q version marker", out, marker)
	}
	rest := out[idx+len(marker):]

	i := 0
	for i < len(rest) && (rest[i] < '0' || rest[i] > '9') {
		i++
	}
	if i >= len(rest) {
		return 0, fmt.Errorf("pg_dump --version output %q has no version number after the %q marker", out, marker)
	}
	start := i
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}

	major, err := strconv.Atoi(rest[start:i])
	if err != nil {
		return 0, fmt.Errorf("parse pg_dump --version output %q: %w", out, err)
	}
	return major, nil
}

// checkPgDumpCompatible returns an error when the pg_dump client major is
// older than the database server major, naming both majors and the fix.
func checkPgDumpCompatible(dumpMajor, serverMajor int) error {
	if dumpMajor >= serverMajor {
		return nil
	}
	return fmt.Errorf("pg_dump client major %d is older than database server major %d: cannot dump; align PG_CLIENT_MAJOR in deploy/self-hosted/Dockerfile.server with the database image major", dumpMajor, serverMajor)
}

// preflightPgDump verifies that pg_dump exists and that its major version is
// at least the database server's major version, so a client/server mismatch
// fails loudly (and is recorded) instead of surfacing as an opaque exec error.
func (t *DatabaseBackupTask) preflightPgDump(ctx context.Context) error {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return fmt.Errorf("pg_dump not found on PATH: %w", err)
	}

	versionOut, err := exec.CommandContext(ctx, "pg_dump", "--version").Output()
	if err != nil {
		return fmt.Errorf("pg_dump --version: %w", err)
	}
	dumpMajor, err := pgDumpMajorFromVersionOutput(string(versionOut))
	if err != nil {
		return err
	}

	var serverVersionNum int
	if err := t.db.NewRaw("SELECT current_setting('server_version_num')::int").Scan(ctx, &serverVersionNum); err != nil {
		return fmt.Errorf("read database server version: %w", err)
	}
	serverMajor := pgMajorFromVersionNum(serverVersionNum)

	if err := checkPgDumpCompatible(dumpMajor, serverMajor); err != nil {
		return err
	}

	t.log.Info("pg_dump preflight passed",
		slog.String("pg_dump_version", strings.TrimSpace(string(versionOut))),
		slog.Int("server_version", serverVersionNum),
	)
	return nil
}

// runBackup runs pg_dump and streams output to MinIO.
func (t *DatabaseBackupTask) runBackup(ctx context.Context, record *DatabaseBackup) error {
	dbCfg := t.cfg.Database
	// Per-run unique key: the timestamp alone is only second-level precision and
	// the scheduler has no skip-if-running guard, so overlapping runs could share
	// a key and a failed run's cleanup could delete another run's object.
	// record.ID is populated by the Returning("id") insert in Run.
	key := fmt.Sprintf("database-backups/%s-%s.dump", time.Now().UTC().Format("2006-01-02_15-04-05"), record.ID)

	// Build pg_dump command
	cmd := exec.CommandContext(ctx,
		"pg_dump",
		"-Fc",
		"-h", dbCfg.Host,
		"-p", fmt.Sprintf("%d", dbCfg.Port),
		"-U", dbCfg.User,
		dbCfg.Database,
	)
	// Pass password via environment (never logged)
	cmd.Env = append(cmd.Environ(), fmt.Sprintf("PGPASSWORD=%s", dbCfg.Password))

	// Pipe stdout to MinIO
	pr, pw := io.Pipe()
	cmd.Stdout = pw

	// Capture stderr for error reporting
	var stderrBuf []byte
	stderrReader, stderrWriter := io.Pipe()
	cmd.Stderr = stderrWriter

	// Read stderr in background
	stderrDone := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(stderrReader)
		stderrDone <- b
	}()

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		stderrWriter.Close()
		return fmt.Errorf("start pg_dump: %w", err)
	}

	// Upload in goroutine while pg_dump writes
	uploadErrCh := make(chan error, 1)
	var uploadedSize int64
	go func() {
		result, err := t.storage.UploadToBucket(ctx, dbBackupBucket, key, pr, -1, storage.UploadOptions{
			ContentType: "application/octet-stream",
		})
		if err != nil {
			uploadErrCh <- err
			return
		}
		uploadedSize = result.Size
		uploadErrCh <- nil
	}()

	// Wait for pg_dump to finish, then close the write end of the pipe.
	// On failure, close with the error so the uploader aborts mid-stream instead
	// of finalizing a truncated or empty object.
	cmdErr := cmd.Wait()
	if cmdErr != nil {
		pw.CloseWithError(cmdErr)
	} else {
		pw.Close()
	}
	stderrWriter.Close()
	stderrBuf = <-stderrDone

	// Wait for upload to finish
	uploadErr := <-uploadErrCh
	pr.Close()

	if cmdErr != nil {
		stderr := string(stderrBuf)
		pgDumpErr := fmt.Errorf("pg_dump failed: %w (stderr: %s)", cmdErr, stderr)

		// Defensive cleanup: an S3 PutObject can commit before returning an
		// error, so a failed pg_dump may still leave an object under this run's
		// key. Delete unconditionally — deleting a missing key is a no-op — so
		// a failed dump never leaves a stray object. The pg_dump error is always
		// the one returned; cleanup noise never replaces the real diagnostic.
		if delErr := t.storage.DeleteFromBucket(ctx, dbBackupBucket, key); delErr != nil {
			t.log.Warn("failed to delete backup object after pg_dump failure",
				slog.String("key", key),
				slog.String("error", delErr.Error()),
			)
		} else {
			t.log.Warn("deleted backup object left by failed pg_dump",
				slog.String("key", key),
			)
		}
		return pgDumpErr
	}
	if uploadErr != nil {
		return fmt.Errorf("upload to MinIO: %w", uploadErr)
	}

	// Update record with storage key and size
	record.StorageKey = &key
	record.SizeBytes = &uploadedSize

	return nil
}

// enforceRetention deletes backups older than dbBackupRetentionDays days.
func (t *DatabaseBackupTask) enforceRetention(ctx context.Context) error {
	cutoff := time.Now().AddDate(0, 0, -dbBackupRetentionDays)

	// Find old records
	var old []DatabaseBackup
	if err := t.db.NewSelect().
		Model(&old).
		Where("created_at < ?", cutoff).
		Scan(ctx); err != nil {
		return fmt.Errorf("query old backups: %w", err)
	}

	if len(old) == 0 {
		return nil
	}

	t.log.Info("enforcing backup retention",
		slog.Int("count", len(old)),
		slog.Time("cutoff", cutoff),
	)

	for _, b := range old {
		// Delete from MinIO
		if b.StorageKey != nil {
			if err := t.storage.DeleteFromBucket(ctx, dbBackupBucket, *b.StorageKey); err != nil {
				t.log.Warn("failed to delete backup object from storage",
					slog.String("id", b.ID),
					slog.String("key", *b.StorageKey),
					slog.String("error", err.Error()),
				)
				// Continue — still delete DB record
			}
		}

		// Delete DB record
		if _, err := t.db.NewDelete().Model(&b).WherePK().Exec(ctx); err != nil {
			t.log.Warn("failed to delete backup record",
				slog.String("id", b.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	t.log.Info("retention enforcement complete", slog.Int("deleted", len(old)))
	return nil
}
