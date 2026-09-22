package graph_test

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
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/journal"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// This test lives in the external `graph_test` package so it can import both
// `graph` and `journal`. Placing it in package `graph` would create a test-only
// import cycle (graph → journal → graph via journal/graph_sink.go).

// openAuditTestDB opens the test database, skipping when unavailable.
func openAuditTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://emergent:emergent@localhost:5436/emergent?sslmode=disable"
	}
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		testdb.SkipOrFatal(t, "database unavailable (%v), skipping integration test", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newAuditTestRepo builds a graph Repository backed by the test DB.
func newAuditTestRepo(t *testing.T, db *bun.DB) *graph.Repository {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	return graph.NewRepository(db, log, cfg)
}

// seedAuditProject inserts an org + project row so graph_object inserts with a
// random project_id satisfy the kb.graph_objects.project_id → kb.projects FK.
func seedAuditProject(t *testing.T, db *bun.DB, projectID uuid.UUID) {
	t.Helper()
	orgID := uuid.New()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'test-org', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO kb.projects (id, organization_id, name, created_at, updated_at)
		VALUES (?, ?, 'test-project', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, projectID, orgID)
	require.NoError(t, err)
}

// insertAuditTestObject inserts a raw graph_object row for testing.
func insertAuditTestObject(t *testing.T, db *bun.DB, projectID uuid.UUID, objType, status string) uuid.UUID {
	t.Helper()
	seedAuditProject(t, db, projectID)
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

// newTestJournalSvc creates a real journal service backed by the test DB.
func newTestJournalSvc(t *testing.T, db *bun.DB) *journal.Service {
	t.Helper()
	branchStore := branches.NewStore(db)
	journalRepo := journal.NewRepository(db, branchStore)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return journal.NewService(journalRepo, log)
}

// TestBulkActionIntegration_AuditLog verifies a bulk action writes a journal
// "batch" event. Uses a real journal service wrapped by its EventSink adapter.
func TestBulkActionIntegration_AuditLog(t *testing.T) {
	db := openAuditTestDB(t)
	repo := newAuditTestRepo(t, db)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	projectID := uuid.New()
	for i := 0; i < 2; i++ {
		insertAuditTestObject(t, db, projectID, "AuditObj", "active")
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)   //nolint:errcheck
		db.ExecContext(context.Background(), "DELETE FROM kb.project_journal WHERE project_id = ?", projectID) //nolint:errcheck
	})

	// Wire a real journal as the graph service's event sink via the adapter.
	journalSvc := newTestJournalSvc(t, db)
	sink := journal.NewGraphEventSinkAdapter(journalSvc)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, sink, nil, nil)

	actorID := uuid.New()
	resp, err := svc.BulkAction(context.Background(), projectID, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{Types: []string{"AuditObj"}},
		Action: graph.BulkActionUpdateStatus,
		Value:  "archived",
	}, &actorID)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Matched)
	assert.Equal(t, 2, resp.Affected)
	assert.False(t, resp.DryRun)

	// journal.Log is async — poll up to 1s for the batch event.
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
