package graph

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchGraphObjectsResponse_TotalPresence pins the wire semantics of the
// include_total opt-out (#733): a nil Total omits the field entirely, while an
// explicit zero is still reported as "total": 0.
func TestSearchGraphObjectsResponse_TotalPresence(t *testing.T) {
	zero := 0

	tests := []struct {
		name        string
		total       *int
		wantPresent bool
	}{
		{name: "explicit zero total is present", total: &zero, wantPresent: true},
		{name: "skipped total is omitted", total: nil, wantPresent: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(SearchGraphObjectsResponse{
				Items: []*GraphObjectResponse{},
				Total: tt.total,
			})
			require.NoError(t, err)

			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &raw))

			if tt.wantPresent {
				require.Contains(t, raw, "total", "total must be present when the count was computed")
				assert.JSONEq(t, "0", string(raw["total"]))
			} else {
				assert.NotContains(t, raw, "total", "total must be omitted when the count was skipped")
			}
		})
	}
}

// TestListSkipTotalIntegration verifies the service-level opt-out against a
// real database: the default returns an exact total for the HEAD/main/not-
// deleted predicates, SkipTotal returns no total at all, and the two agree on
// the items (#733). Non-HEAD, branch-scoped and deleted rows must never be
// counted.
func TestListSkipTotalIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires database")
	}
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc := NewService(repo, log, nil, nil, nil, nil, nil, nil, nil, nil)

	ctx := context.Background()
	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID) //nolint:errcheck
	})

	const headCount = 3
	for i := 0; i < headCount; i++ {
		insertBulkTestObject(t, db, projectID, "Entity", "active")
	}

	// Rows excluded by the HEAD predicate: a superseded version, a branch row,
	// and a soft-deleted row.
	canonicalID := uuid.New()

	supersededID := uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.graph_objects
		(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
		 properties, labels, content_hash, created_at, updated_at)
		VALUES (?, ?, NULL, ?, ?, 2, 'Entity', 'active', '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())`,
		supersededID, projectID, canonicalID, uuid.New(), "hash-"+supersededID.String())
	require.NoError(t, err, "seeding superseded row")

	branchID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.graph_objects
		(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
		 properties, labels, content_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULL, 1, 'Entity', 'active', '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())`,
		branchID, projectID, uuid.New(), canonicalID, "hash-"+branchID.String())
	require.NoError(t, err, "seeding branch row")

	deletedID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.graph_objects
		(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
		 properties, labels, content_hash, deleted_at, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, 'Entity', 'active', '{}'::jsonb, '{}'::text[], ?, NOW(), NOW(), NOW())`,
		deletedID, projectID, canonicalID, "hash-"+deletedID.String())
	require.NoError(t, err, "seeding deleted row")

	// Default: exact total, pointer populated even though it is non-zero here.
	exact, err := svc.List(ctx, ListParams{ProjectID: projectID, Limit: 10})
	require.NoError(t, err)
	require.NotNil(t, exact.Total, "default request must return an exact total")
	assert.Equal(t, headCount, *exact.Total)
	assert.Len(t, exact.Items, headCount)

	// Opt-out: no total at all, same items.
	skipped, err := svc.List(ctx, ListParams{ProjectID: projectID, Limit: 10, SkipTotal: true})
	require.NoError(t, err)
	assert.Nil(t, skipped.Total, "SkipTotal must omit the total")
	assert.Len(t, skipped.Items, headCount)

	// Empty project: the default reports an explicit zero; the opt-out still
	// omits the field.
	emptyProjectID := uuid.New()
	seedProject(t, db, emptyProjectID)

	empty, err := svc.List(ctx, ListParams{ProjectID: emptyProjectID})
	require.NoError(t, err)
	require.NotNil(t, empty.Total)
	assert.Equal(t, 0, *empty.Total)

	emptySkipped, err := svc.List(ctx, ListParams{ProjectID: emptyProjectID, SkipTotal: true})
	require.NoError(t, err)
	assert.Nil(t, emptySkipped.Total)
}
