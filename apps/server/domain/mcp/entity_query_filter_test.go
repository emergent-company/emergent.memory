package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// insertQueryEntity inserts a HEAD LegalParagraph graph object with the given
// key and properties so the entity-query filter/scoping paths can be exercised
// against a real properties JSONB column (and the GIN index from migration
// 00195). Returns the object id.
func insertQueryEntity(t *testing.T, db bun.IDB, projectID, key string, props map[string]any) string {
	t.Helper()
	id := uuid.NewString()
	propsJSON, err := json.Marshal(props)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, 'LegalParagraph', ?, 'active', ?::jsonb, '{}'::text[], now(), now())
	`, id, projectID, id, key, string(propsJSON))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE id = ?", id)
	})
	return id
}

// runEntityQuery executes executeQueryEntities and decodes the envelope's
// `data` payload into a QueryEntitiesResult.
func runEntityQuery(t *testing.T, svc *Service, projectID string, args map[string]any) QueryEntitiesResult {
	t.Helper()
	res, err := svc.executeQueryEntities(context.Background(), projectID, args)
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var env struct {
		OK   bool                `json:"ok"`
		Data QueryEntitiesResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &env))
	require.True(t, env.OK, "entity-query envelope ok=false: %s", text)
	return env.Data
}

// TestExecuteQueryEntities_PropertyFilterScope pins the two halves of the
// #1148 wrong-scope defect:
//
//   - a bare `chapter_id` filter is NOT unique to one document: it matches every
//     law/forskrift that has that chapter (the reported wrong-scope leak); and
//   - key_prefix scopes such a filter to the intended identity (the fix).
//
// The key_prefix sub-assertions are fail-first: before the fix the parameter
// was ignored and both laws were returned.
func TestExecuteQueryEntities_PropertyFilterScope(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	const (
		lawA    = "lov/1997-06-13-44" // aksjeloven
		lawB    = "lov/2018-06-22-83"
		chapter = "kapittel-2-kapittel-1"
	)
	// Two paragraphs in each law share the same chapter_id; law A also has one
	// paragraph in a different chapter.
	insertQueryEntity(t, db, projectID, lawA+"#kapittel-2-kapittel-1-paragraf-1",
		map[string]any{"chapter_id": chapter, "law_ref_id": lawA, "content": "a1"})
	insertQueryEntity(t, db, projectID, lawA+"#kapittel-2-kapittel-1-paragraf-2",
		map[string]any{"chapter_id": chapter, "law_ref_id": lawA, "content": "a2"})
	insertQueryEntity(t, db, projectID, lawB+"#kapittel-2-kapittel-1-paragraf-1",
		map[string]any{"chapter_id": chapter, "law_ref_id": lawB, "content": "b1"})
	insertQueryEntity(t, db, projectID, lawB+"#kapittel-2-kapittel-1-paragraf-2",
		map[string]any{"chapter_id": chapter, "law_ref_id": lawB, "content": "b2"})
	insertQueryEntity(t, db, projectID, lawA+"#kapittel-1-kapittel-1-paragraf-1",
		map[string]any{"chapter_id": "kapittel-1-kapittel-1", "law_ref_id": lawA, "content": "a3"})

	t.Run("chapter_id alone leaks across laws", func(t *testing.T) {
		out := runEntityQuery(t, svc, projectID, map[string]any{
			"type_name": "LegalParagraph",
			"filters":   map[string]any{"chapter_id": chapter},
		})
		require.NotNil(t, out.Pagination)
		assert.Equal(t, 4, out.Pagination.Total, "chapter_id is not unique to one law")
	})

	t.Run("key_prefix scopes to one law", func(t *testing.T) {
		out := runEntityQuery(t, svc, projectID, map[string]any{
			"type_name":  "LegalParagraph",
			"filters":    map[string]any{"chapter_id": chapter},
			"key_prefix": lawA + "#",
		})
		require.NotNil(t, out.Pagination)
		require.Equal(t, 2, out.Pagination.Total, "only law A's two paragraphs must match")
		require.Len(t, out.Entities, 2)
		for _, e := range out.Entities {
			assert.Contains(t, e.Key, lawA+"#", "key_prefix must scope results to law A")
		}
	})

	t.Run("invalid filter keys are dropped, not injected", func(t *testing.T) {
		out := runEntityQuery(t, svc, projectID, map[string]any{
			"type_name": "LegalParagraph",
			"filters":   map[string]any{"chapter_id' OR '1'='1": chapter},
		})
		require.NotNil(t, out.Pagination)
		assert.Equal(t, 5, out.Pagination.Total, "invalid key must be ignored; no injection")
	})
}

// TestExecuteQueryEntities_FullStrategyLimitBound verifies the field_strategy
// "full" payload bound (#1148): a large full-text limit is capped and the cap
// is surfaced to the caller.
func TestExecuteQueryEntities_FullStrategyLimitBound(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db, entityQueryFullMaxLimit: 3}

	for i := 0; i < 10; i++ {
		insertQueryEntity(t, db, projectID, fmt.Sprintf("lov/scope#p-%d", i),
			map[string]any{"chapter_id": "kapittel-2-kapittel-1", "content": "text"})
	}

	out := runEntityQuery(t, svc, projectID, map[string]any{
		"type_name":      "LegalParagraph",
		"field_strategy": "full",
		"limit":          float64(10),
	})
	require.NotNil(t, out.Pagination)
	assert.Equal(t, 3, out.Pagination.Limit, "limit must be capped under field_strategy=full")
	require.Len(t, out.Entities, 3)
	assert.Contains(t, out.Warning, "capped to 3")
}

// TestExecuteQueryEntities_Timeout verifies the hard per-call deadline (#1148):
// a query that cannot finish within the configured timeout returns an explicit
// timeout error rather than stalling the agent turn.
func TestExecuteQueryEntities_Timeout(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db, entityQueryTimeout: time.Nanosecond}
	insertQueryEntity(t, db, projectID, "lov/scope#p-1",
		map[string]any{"chapter_id": "kapittel-2-kapittel-1"})

	_, err := svc.executeQueryEntities(context.Background(), projectID, map[string]any{
		"type_name": "LegalParagraph",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out after")
}

// TestExecuteQueryEntities_KeyPrefixWithIDsRejected pins the fail-closed
// handling of `ids` + `key_prefix` (#1148 follow-up): an explicit id list
// already identifies entities exactly, so a prefix scope is meaningless and must
// be rejected rather than silently ignored.
func TestExecuteQueryEntities_KeyPrefixWithIDsRejected(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}
	id := insertQueryEntity(t, db, projectID, "lov/scope#p-1",
		map[string]any{"chapter_id": "kapittel-2-kapittel-1"})

	_, err := svc.executeQueryEntities(context.Background(), projectID, map[string]any{
		"type_name":  "LegalParagraph",
		"ids":        []any{id},
		"key_prefix": "lov/scope#",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_prefix cannot be combined with ids")
}

// TestExecuteQueryEntities_TimeoutCoversIDsPath pins that the per-call deadline
// is created at the tool entry so it also covers the ids[] fast-path, which the
// original fix left on the caller's (undeadlined) context.
func TestExecuteQueryEntities_TimeoutCoversIDsPath(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db, entityQueryTimeout: time.Nanosecond}
	id := insertQueryEntity(t, db, projectID, "lov/scope#p-1",
		map[string]any{"chapter_id": "kapittel-2-kapittel-1"})

	_, err := svc.executeQueryEntities(context.Background(), projectID, map[string]any{
		"type_name": "LegalParagraph",
		"ids":       []any{id},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out after")
}
