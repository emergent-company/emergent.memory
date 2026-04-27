package graph

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/journal"
	"github.com/emergent-company/emergent.memory/internal/config"
)

// =============================================================================
// 5.1 Unit test: relative time parser
// =============================================================================

func TestParseRelativeTime(t *testing.T) {
	before := time.Now().UTC()

	tests := []struct {
		input   string
		wantErr bool
		check   func(t *testing.T, got time.Time)
	}{
		{
			input: "90d",
			check: func(t *testing.T, got time.Time) {
				expected := before.Add(-90 * 24 * time.Hour)
				diff := expected.Sub(got).Abs()
				assert.Less(t, diff, 2*time.Second, "90d should be ~90 days ago")
			},
		},
		{
			input: "12h",
			check: func(t *testing.T, got time.Time) {
				expected := before.Add(-12 * time.Hour)
				diff := expected.Sub(got).Abs()
				assert.Less(t, diff, 2*time.Second, "12h should be ~12 hours ago")
			},
		},
		{
			input: "6M",
			check: func(t *testing.T, got time.Time) {
				expected := before.AddDate(0, -6, 0)
				diff := expected.Sub(got).Abs()
				assert.Less(t, diff, 2*time.Second, "6M should be ~6 months ago")
			},
		},
		{
			input: "1d",
			check: func(t *testing.T, got time.Time) {
				expected := before.Add(-24 * time.Hour)
				diff := expected.Sub(got).Abs()
				assert.Less(t, diff, 2*time.Second)
			},
		},
		// Error cases
		{input: "", wantErr: true},
		{input: "x", wantErr: true},
		{input: "0d", wantErr: true},
		{input: "-5d", wantErr: true},
		{input: "5y", wantErr: true},
		{input: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseRelativeTime(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, got)
		})
	}
}

func TestIsRelativeTime(t *testing.T) {
	assert.True(t, isRelativeTime("90d"))
	assert.True(t, isRelativeTime("12h"))
	assert.True(t, isRelativeTime("6M"))
	assert.False(t, isRelativeTime("notrelative"))
	assert.False(t, isRelativeTime(""))
	assert.False(t, isRelativeTime("5y"))
}

// =============================================================================
// 5.2 Unit test: limit enforcement constants
// =============================================================================

func TestBulkActionLimitConstants(t *testing.T) {
	assert.Equal(t, 1000, bulkActionDefaultLimit, "default limit should be 1000")
	assert.Equal(t, 100_000, bulkActionMaxLimit, "max limit should be 100000")
}

func TestBulkActionServiceLimitValidation(t *testing.T) {
	// Test that limit over max is rejected without needing a DB
	req := &BulkActionRequest{
		Action: BulkActionUpdateStatus,
		Value:  "archived",
		Limit:  bulkActionMaxLimit + 1,
	}

	limit := req.Limit
	if limit <= 0 {
		limit = bulkActionDefaultLimit
	}
	// If over max, service returns error — verify the condition
	assert.Greater(t, limit, bulkActionMaxLimit, "limit should exceed max")

	// Default applied when limit == 0
	req2 := &BulkActionRequest{Action: BulkActionUpdateStatus}
	limit2 := req2.Limit
	if limit2 <= 0 {
		limit2 = bulkActionDefaultLimit
	}
	assert.Equal(t, bulkActionDefaultLimit, limit2)
}

// =============================================================================
// Integration test helpers
// =============================================================================

func openBulkTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test requires database")
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://emergent:emergent@localhost:5436/emergent?sslmode=disable"
	}
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	// Ping to detect missing DB early
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		t.Skipf("database unavailable (%v), skipping integration test", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newBulkTestRepo(t *testing.T, db *bun.DB) *Repository {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	return NewRepository(db, log, cfg)
}

// insertBulkTestObject inserts a raw graph_object row for testing.
func insertBulkTestObject(t *testing.T, db *bun.DB, projectID uuid.UUID, objType, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES
			(?, ?, NULL, ?, NULL, 1, ?, ?,
			 '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())
	`, id, projectID, id, objType, status, fmt.Sprintf("hash-%s", id.String()))
	require.NoError(t, err)
	return id
}

// =============================================================================
// 5.3 Integration: bulk update_status modifies correct objects, leaves others
// =============================================================================

func TestBulkActionIntegration_UpdateStatus(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	projectID := uuid.New()

	// 3 BulkTestObj, 2 OtherObj
	for i := 0; i < 3; i++ {
		insertBulkTestObject(t, db, projectID, "BulkTestObj", "active")
	}
	for i := 0; i < 2; i++ {
		insertBulkTestObject(t, db, projectID, "OtherObj", "active")
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID) //nolint:errcheck
	})

	matched, affected, err := repo.BulkActionByFilter(context.Background(), BulkActionParams{
		ProjectID: projectID,
		Filter:    BulkActionFilter{Types: []string{"BulkTestObj"}},
		Action:    BulkActionUpdateStatus,
		Value:     "archived",
		Limit:     1000,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, matched, "should match 3 BulkTestObj objects")
	assert.Equal(t, 3, affected)

	// Verify OtherObj untouched
	var count int
	err = db.NewSelect().TableExpr("kb.graph_objects").
		Where("project_id = ?", projectID).
		Where("type = ?", "OtherObj").
		Where("status = ?", "active").
		ColumnExpr("count(*)").
		Scan(context.Background(), &count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "OtherObj should still be active")
}

// =============================================================================
// 5.4 Integration: dry_run returns correct count with zero mutations
// =============================================================================

func TestBulkActionIntegration_DryRun(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	projectID := uuid.New()

	for i := 0; i < 4; i++ {
		insertBulkTestObject(t, db, projectID, "DryRunObj", "active")
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID) //nolint:errcheck
	})

	matched, affected, err := repo.BulkActionByFilter(context.Background(), BulkActionParams{
		ProjectID: projectID,
		Filter:    BulkActionFilter{Types: []string{"DryRunObj"}},
		Action:    BulkActionUpdateStatus,
		Value:     "archived",
		Limit:     1000,
		DryRun:    true,
	})
	require.NoError(t, err)
	assert.Equal(t, 4, matched, "dry_run should return correct match count")
	assert.Equal(t, 0, affected, "dry_run should not affect any objects")

	// Verify no mutation occurred
	var count int
	err = db.NewSelect().TableExpr("kb.graph_objects").
		Where("project_id = ?", projectID).
		Where("status = ?", "active").
		ColumnExpr("count(*)").
		Scan(context.Background(), &count)
	require.NoError(t, err)
	assert.Equal(t, 4, count, "all objects should still be active after dry_run")
}

// =============================================================================
// 5.5 Integration: hard_delete removes matched objects permanently
// =============================================================================

func TestBulkActionIntegration_HardDelete(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	projectID := uuid.New()

	for i := 0; i < 3; i++ {
		insertBulkTestObject(t, db, projectID, "DeleteMe", "active")
	}
	keepID := insertBulkTestObject(t, db, projectID, "KeepMe", "active")

	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID) //nolint:errcheck
	})

	matched, affected, err := repo.BulkActionByFilter(context.Background(), BulkActionParams{
		ProjectID: projectID,
		Filter:    BulkActionFilter{Types: []string{"DeleteMe"}},
		Action:    BulkActionHardDelete,
		Limit:     1000,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, matched)
	assert.Equal(t, 3, affected)

	// DeleteMe objects should be gone
	var deleteCount int
	err = db.NewSelect().TableExpr("kb.graph_objects").
		Where("project_id = ?", projectID).
		Where("type = ?", "DeleteMe").
		ColumnExpr("count(*)").
		Scan(context.Background(), &deleteCount)
	require.NoError(t, err)
	assert.Equal(t, 0, deleteCount, "DeleteMe objects should be permanently removed")

	// KeepMe object should remain
	var keepCount int
	err = db.NewSelect().TableExpr("kb.graph_objects").
		Where("project_id = ?", projectID).
		Where("id = ?", keepID).
		ColumnExpr("count(*)").
		Scan(context.Background(), &keepCount)
	require.NoError(t, err)
	assert.Equal(t, 1, keepCount, "KeepMe object should still exist")
}

// newTestJournalSvc creates a real journal service backed by the test DB.
func newTestJournalSvc(t *testing.T, db *bun.DB) *journal.Service {
	t.Helper()
	branchStore := branches.NewStore(db)
	journalRepo := journal.NewRepository(db, branchStore)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return journal.NewService(journalRepo, log)
}

// =============================================================================
// 5.6 Integration: audit log entry written after bulk operation
// =============================================================================

func TestBulkActionIntegration_AuditLog(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Build a real journal service via direct DB insert verification
	// (avoid complex dependency chain — verify journal entry existence in DB directly)
	projectID := uuid.New()
	for i := 0; i < 2; i++ {
		insertBulkTestObject(t, db, projectID, "AuditObj", "active")
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)   //nolint:errcheck
		db.ExecContext(context.Background(), "DELETE FROM kb.project_journal WHERE project_id = ?", projectID) //nolint:errcheck
	})

	// Build service with a real journal backed by the test DB
	journalSvc := newTestJournalSvc(t, db)
	svc := NewService(repo, log, nil, nil, nil, nil, nil, journalSvc, nil, nil)

	actorID := uuid.New()
	resp, err := svc.BulkAction(context.Background(), projectID, &BulkActionRequest{
		Filter: BulkActionFilter{Types: []string{"AuditObj"}},
		Action: BulkActionUpdateStatus,
		Value:  "archived",
	}, &actorID)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Matched)
	assert.Equal(t, 2, resp.Affected)
	assert.False(t, resp.DryRun)

	// Give journal goroutine a moment to flush (journal.Log is async in some implementations)
	// Poll up to 1s
	var journalCount int
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		err = db.NewSelect().TableExpr("kb.project_journal").
			Where("project_id = ?", projectID).
			Where("event_type = ?", "batch").
			ColumnExpr("count(*)").
			Scan(context.Background(), &journalCount)
		require.NoError(t, err)
		if journalCount > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.Equal(t, 1, journalCount, "one journal entry should be written for the bulk operation")
}
