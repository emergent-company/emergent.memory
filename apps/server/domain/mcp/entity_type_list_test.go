package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// insertGraphObject inserts a HEAD graph object of the given type into the
// project (main branch, not deleted) and returns its physical id. Physical id
// equals canonical_id for a v1 HEAD row, so it can be used as a relationship
// src_id/dst_id directly.
func insertGraphObject(t *testing.T, db bun.IDB, projectID, typ, key string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, ?, 'active', '{}'::jsonb, '{}'::text[], now(), now())
	`, id, projectID, id, typ, key)
	require.NoError(t, err)
	return id
}

// listEntityTypes executes executeListEntityTypes and decodes the wrapped
// EntityTypesResult payload.
func listEntityTypes(t *testing.T, svc *Service, projectID string, args map[string]any) EntityTypesResult {
	t.Helper()
	res, err := svc.executeListEntityTypes(context.Background(), projectID, args)
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var result EntityTypesResult
	require.NoError(t, json.Unmarshal([]byte(text), &result))
	return result
}

// TestExecuteListEntityTypes_RelationshipTypesOptIn pins the latency fix: the
// relationship-type aggregation must NOT run (and the result must carry an empty
// relationships list) unless include_relationships=true, and when enabled it must
// return the correct (type, from_type, to_type, count) rows with a project-scoped
// query that can use idx_graph_relationships_head_main.
func TestExecuteListEntityTypes_RelationshipTypesOptIn(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	srcID := insertGraphObject(t, db, projectID, "Person", "alice")
	dstID := insertGraphObject(t, db, projectID, "Person", "bob")

	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_relationships
			(id, project_id, canonical_id, supersedes_id, version, type, src_id, dst_id,
			 properties, branch_id, deleted_at, created_at)
		VALUES (gen_random_uuid(), ?, gen_random_uuid(), NULL, 1, 'KNOWS', ?, ?,
			'{}'::jsonb, NULL, NULL, now())
	`, projectID, srcID, dstID)
	require.NoError(t, err)

	svc := &Service{db: db}

	// Default call must not compute relationship types.
	def := listEntityTypes(t, svc, projectID, map[string]any{})
	assert.Empty(t, def.Relationships, "default call must not return relationship types")

	// Opt-in call computes relationship types with the correct from/to types.
	with := listEntityTypes(t, svc, projectID, map[string]any{"include_relationships": true})
	require.Len(t, with.Relationships, 1)
	rel := with.Relationships[0]
	assert.Equal(t, "KNOWS", rel.Type)
	assert.Equal(t, "Person", rel.FromType)
	assert.Equal(t, "Person", rel.ToType)
	assert.Equal(t, 1, rel.Count)
}
