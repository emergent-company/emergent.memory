package scheduler

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestDatabaseBackupTaskSkipsOverlappingRun verifies the skip-if-running guard:
// while a run is in flight, a second Run returns immediately (nil) without
// touching its dependencies and without clearing the in-flight flag.
func TestDatabaseBackupTaskSkipsOverlappingRun(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	task := &DatabaseBackupTask{log: log}

	// Simulate an in-flight run. db/storage are intentionally nil: if the guard
	// fails to short-circuit, Run will nil-deref and the test fails loudly.
	if !task.running.CompareAndSwap(false, true) {
		t.Fatal("failed to mark task as running")
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("overlapping Run returned error, want nil: %v", err)
	}
	if !task.running.Load() {
		t.Fatal("overlapping Run cleared the running flag; in-flight run would no longer be guarded")
	}
}

// TestDatabaseBackupTaskRun_PersistsFailedOnTimeout verifies the terminal record
// write survives the backup deadline: when the dump is canceled, the row must
// end as "failed" (not stuck "running").
func TestDatabaseBackupTaskRun_PersistsFailedOnTimeout(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database-backed test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "backup_timeout")
	defer tdb.Close()

	task := &DatabaseBackupTask{
		db:  tdb.DB,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Simulate a hung backup: block until the backup deadline fires, then return
	// the cancellation error (mirrors pg_dump being killed on timeout).
	entered := make(chan struct{})
	task.backupFn = func(ctx context.Context, record *DatabaseBackup) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}

	runDone := make(chan error, 1)
	go func() { runDone <- task.Run(runCtx) }()

	<-entered
	cancel() // fire the "timeout"

	if err := <-runDone; err == nil {
		t.Fatal("Run returned nil, want cancellation error")
	}

	var rec DatabaseBackup
	if err := tdb.DB.NewSelect().Model(&rec).Order("created_at DESC").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("query backup record: %v", err)
	}
	if rec.Status != "failed" {
		t.Fatalf("backup record status = %q, want failed", rec.Status)
	}
	if rec.Error == nil || *rec.Error == "" {
		t.Fatal("backup record error should be set on timeout")
	}
}

// TestDatabaseBackupTaskAcquireLock_RefusesCrossProcessOverlap verifies the
// DB-level guard refuses a concurrent holder across two independent task
// instances (each with its own zero-value in-process flag, which alone would
// not stop the second run).
func TestDatabaseBackupTaskAcquireLock_RefusesCrossProcessOverlap(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database-backed test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "backup_lock")
	defer tdb.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	first := &DatabaseBackupTask{db: tdb.DB, log: log}
	second := &DatabaseBackupTask{db: tdb.DB, log: log}

	release, err := first.acquireBackupLock(ctx)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if release == nil {
		t.Fatal("first acquire returned nil release, want success")
	}
	defer release()

	secondRelease, err := second.acquireBackupLock(ctx)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if secondRelease != nil {
		secondRelease()
		t.Fatal("second acquire succeeded; DB-level guard did not refuse cross-process overlap")
	}
}

func TestPgMajorFromVersionNum(t *testing.T) {
	tests := []struct {
		name string
		num  int
		want int
	}{
		{name: "pg17.11", num: 170011, want: 17},
		{name: "pg16.5", num: 160005, want: 16},
		{name: "pg15", num: 150000, want: 15},
		{name: "zero", num: 0, want: 0},
		{name: "future pg18", num: 180000, want: 18},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pgMajorFromVersionNum(tt.num); got != tt.want {
				t.Errorf("pgMajorFromVersionNum(%d) = %d, want %d", tt.num, got, tt.want)
			}
		})
	}
}

func TestPgDumpMajorFromVersionOutput(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    int
		wantErr bool
	}{
		{
			name: "debian pgdg 17",
			out:  "pg_dump (PostgreSQL) 17.11 (Debian 17.11-3.pgdg12+1)",
			want: 17,
		},
		{
			name: "plain 16",
			out:  "pg_dump (PostgreSQL) 16.5",
			want: 16,
		},
		{
			name:    "missing marker",
			out:     "some unrelated output",
			wantErr: true,
		},
		{
			name:    "empty output",
			out:     "",
			wantErr: true,
		},
		{
			name:    "no number after marker",
			out:     "pg_dump (PostgreSQL) ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pgDumpMajorFromVersionOutput(tt.out)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("pgDumpMajorFromVersionOutput(%q) = %d, want error", tt.out, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("pgDumpMajorFromVersionOutput(%q) unexpected error: %v", tt.out, err)
			}
			if got != tt.want {
				t.Errorf("pgDumpMajorFromVersionOutput(%q) = %d, want %d", tt.out, got, tt.want)
			}
		})
	}
}

func TestCheckPgDumpCompatible(t *testing.T) {
	tests := []struct {
		name        string
		dumpMajor   int
		serverMajor int
		wantErr     bool
	}{
		{name: "equal majors", dumpMajor: 17, serverMajor: 17, wantErr: false},
		{name: "newer client", dumpMajor: 18, serverMajor: 17, wantErr: false},
		{name: "older client", dumpMajor: 16, serverMajor: 17, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPgDumpCompatible(tt.dumpMajor, tt.serverMajor)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("checkPgDumpCompatible(%d, %d) = nil, want error", tt.dumpMajor, tt.serverMajor)
				}
				if !strings.Contains(err.Error(), "PG_CLIENT_MAJOR") {
					t.Errorf("error %q should name PG_CLIENT_MAJOR", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("checkPgDumpCompatible(%d, %d) unexpected error: %v", tt.dumpMajor, tt.serverMajor, err)
			}
		})
	}
}
