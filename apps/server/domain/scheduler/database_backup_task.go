package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	dbBackupBucket        = "database-backups"
	dbBackupRetentionDays = 10
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

	backupErr := t.runBackup(ctx, record)

	// 2. Update record with result
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

	if _, err := t.db.NewUpdate().Model(record).WherePK().Exec(ctx); err != nil {
		t.log.Error("failed to update backup record",
			slog.String("id", record.ID),
			slog.String("error", err.Error()),
		)
	}

	if backupErr != nil {
		return backupErr
	}

	// 3. Enforce retention
	if err := t.enforceRetention(ctx); err != nil {
		// Log but don't fail the task — backup succeeded
		t.log.Error("failed to enforce backup retention", slog.String("error", err.Error()))
	}

	return nil
}

// runBackup runs pg_dump and streams output to MinIO.
func (t *DatabaseBackupTask) runBackup(ctx context.Context, record *DatabaseBackup) error {
	dbCfg := t.cfg.Database
	key := fmt.Sprintf("database-backups/%s.dump", time.Now().UTC().Format("2006-01-02_15-04-05"))

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

	// Wait for pg_dump to finish, then close the write end of the pipe
	cmdErr := cmd.Wait()
	pw.Close()
	stderrWriter.Close()
	stderrBuf = <-stderrDone

	// Wait for upload to finish
	uploadErr := <-uploadErrCh
	pr.Close()

	if cmdErr != nil {
		stderr := string(stderrBuf)
		return fmt.Errorf("pg_dump failed: %w (stderr: %s)", cmdErr, stderr)
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
