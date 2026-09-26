package graph_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestUpdateAccessTimestampsDebounces is the fail-first for the primary-path
// debounce (issue #1070): graph.Service.UpdateAccessTimestamps — the method the
// unified search path (search.Service.executeGraphSearch, reached by
// search-hybrid / search-semantic / chat) calls directly — must coalesce writes
// so a single object is written at most once per window, not once per result set.
func TestUpdateAccessTimestampsDebounces(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, nil, nil, nil, nil, nil)

	id := insertKeyedObject(t, ctx, db, projectID, "Law", "lov/0001", `{"title":"statute"}`)

	// First write lands.
	require.NoError(t, svc.UpdateAccessTimestamps(ctx, []uuid.UUID{id}))

	// Pin last_accessed_at to a sentinel so a second write would be observable
	// (it would overwrite the sentinel with time.Now()).
	sentinel := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := db.NewRaw(`UPDATE kb.graph_objects SET last_accessed_at = ? WHERE id = ?`, sentinel, id).Exec(ctx)
	require.NoError(t, err)

	// A second write within the window must be a no-op (debounced).
	require.NoError(t, svc.UpdateAccessTimestamps(ctx, []uuid.UUID{id}))

	var got time.Time
	require.NoError(t, db.NewRaw(`SELECT last_accessed_at FROM kb.graph_objects WHERE id = ?`, id).Scan(ctx, &got))
	require.True(t, got.Equal(sentinel),
		"second write must be debounced; last_accessed_at = %v, want sentinel %v", got, sentinel)
}
