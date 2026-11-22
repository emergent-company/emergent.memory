package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// insertObjectVersion writes one row of an object's version chain under a
// caller-chosen id. The unique upsert index (project, type, key) applies only to
// rows with supersedes_id IS NULL, so a version chain is built by pointing the
// older row at the newer one *before* the newer row is inserted.
func insertObjectVersion(t *testing.T, db bun.IDB, id, projectID, canonicalID, key string, version int, props map[string]any) {
	t.Helper()
	propsJSON, err := json.Marshal(props)
	require.NoError(t, err)

	_, err = db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, name, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, ?, 'Task', ?, 'Boiler service — confirm parts availability', 'ready', ?::jsonb, '{}'::text[], now(), now())
	`, id, projectID, canonicalID, version, key, string(propsJSON))
	require.NoError(t, err)
}

func setSupersededBy(t *testing.T, db bun.IDB, oldID, newID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"UPDATE kb.graph_objects SET supersedes_id = ? WHERE id = ?", newID, oldID)
	require.NoError(t, err)
}

// TestEntityQueryIDsAcceptsVersionID pins the by-ids contract: `ids[]` accepts
// either the canonical id or a version row id.
//
// The object page deep-links the version row it renders, while the by-ids path
// used to match `canonical_id` only, so every object-chat session started by
// claiming the supplied id did not resolve (the agent then spent a call
// recovering via entity-search). The REST object lookup has always matched both;
// this path now agrees with it.
func TestEntityQueryIDsAcceptsVersionID(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	canonicalID := uuid.NewString()
	v1 := uuid.NewString()
	v2 := uuid.NewString()
	props := map[string]any{"title": "Boiler service", "priority": "high"}

	insertObjectVersion(t, db, v1, projectID, canonicalID, "boiler-service-parts", 1, props)
	setSupersededBy(t, db, v1, v2)
	insertObjectVersion(t, db, v2, projectID, canonicalID, "boiler-service-parts", 2, props)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE canonical_id = ?", canonicalID)
	})

	svc := &Service{db: db, log: slog.Default()}

	t.Run("version id resolves", func(t *testing.T) {
		data := runEntityQuery(t, svc, projectID, map[string]any{"ids": []any{v2}})
		require.Len(t, data.Entities, 1)
		// The result keeps reporting the canonical id: callers key on it, and the
		// version id is only an input convenience.
		assert.Equal(t, canonicalID, data.Entities[0].ID)
		assert.Equal(t, 2, data.Entities[0].Version)
		assert.Empty(t, data.MissingIDs)
		assert.Empty(t, data.Warning)
	})

	t.Run("canonical id still resolves", func(t *testing.T) {
		data := runEntityQuery(t, svc, projectID, map[string]any{"ids": []any{canonicalID}})
		require.Len(t, data.Entities, 1)
		assert.Equal(t, 2, data.Entities[0].Version)
		assert.Empty(t, data.MissingIDs)
	})

	t.Run("superseded version row is not a result", func(t *testing.T) {
		data := runEntityQuery(t, svc, projectID, map[string]any{"ids": []any{v1}})
		assert.Empty(t, data.Entities)
		assert.Equal(t, []string{v1}, data.MissingIDs)
	})

	t.Run("unknown id is reported, not silently empty", func(t *testing.T) {
		unknown := uuid.NewString()
		data := runEntityQuery(t, svc, projectID, map[string]any{"ids": []any{canonicalID, unknown}})
		require.Len(t, data.Entities, 1)
		assert.Equal(t, []string{unknown}, data.MissingIDs)
		assert.Contains(t, data.Warning, "1 of 2 requested ids did not resolve")
	})

	t.Run("all unknown ids warn loudly", func(t *testing.T) {
		unknown := uuid.NewString()
		data := runEntityQuery(t, svc, projectID, map[string]any{"ids": []any{unknown}})
		assert.Empty(t, data.Entities)
		assert.Equal(t, []string{unknown}, data.MissingIDs)
		assert.Contains(t, data.Warning, "none of the 1 requested ids resolve")
	})
}
