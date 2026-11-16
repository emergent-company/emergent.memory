package graph_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestPatchGraphObjectTitleWritesNameColumn pins the fix for the last
// properties-based title write: set_session_title's graph patch must land in the
// `name` column, because no read path consults properties.title any more (#824).
// properties is asserted untouched so the patch cannot silently reintroduce the
// second title source.
func TestPatchGraphObjectTitleWritesNameColumn(t *testing.T) {
	db := openAuditTestDB(t)
	repo := newAuditTestRepo(t, db)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, nil, nil, nil)

	ctx := context.Background()
	projectID := uuid.New()
	objectID := insertAuditTestObject(t, db, projectID, "Session", "active")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
	})

	const title = "Investigate the login redirect"
	require.NoError(t, svc.PatchGraphObjectTitle(ctx, projectID.String(), objectID.String(), title))

	var name *string
	var props []byte
	require.NoError(t, db.NewRaw(
		`SELECT name, properties::text FROM kb.graph_objects WHERE canonical_id = ?
		 ORDER BY version DESC LIMIT 1`, objectID,
	).Scan(ctx, &name, &props))
	require.NotNil(t, name, "the title must be written to the name column")
	require.Equal(t, title, *name)
	require.NotContains(t, string(props), `"title"`, "the patch must not write properties.title")
}

// TestPatchGraphObjectTitleIgnoresMissingObject keeps the documented best-effort
// contract: a session with no graph object yet is not an error.
func TestPatchGraphObjectTitleIgnoresMissingObject(t *testing.T) {
	db := openAuditTestDB(t)
	repo := newAuditTestRepo(t, db)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, nil, nil, nil)

	require.NoError(t, svc.PatchGraphObjectTitle(context.Background(), uuid.NewString(), uuid.NewString(), "no such object"))
}
