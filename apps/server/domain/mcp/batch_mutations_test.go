package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ─────────────────────────────────────────────────────────────────────────────
// Batch mutations (#408): entity-update/entity-delete/relationship-delete take
// an array, and entity-bulk-update applies a filter-based action.
// ─────────────────────────────────────────────────────────────────────────────

// batchMutationService wires a real graph.Service behind an mcp.Service, the
// same way production does (see newEntityCreateDedupService).
func batchMutationService(t *testing.T, db bun.IDB) *Service {
	t.Helper()
	return newEntityCreateDedupService(t, db)
}

// seedBatchObject inserts a HEAD graph object on the main branch (branch_id
// NULL) and returns its canonical id.
func seedBatchObject(t *testing.T, db bun.IDB, projectID, typ, key, propsJSON string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, ?, 'active', ?::jsonb, '{}'::text[], now(), now())
	`, id, projectID, id, typ, key, propsJSON)
	require.NoError(t, err)
	return id
}

// seedBatchObjectInNamespace inserts a HEAD object carrying a namespace.
func seedBatchObjectInNamespace(t *testing.T, db bun.IDB, projectID, typ, key, namespace, propsJSON string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, namespace, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, ?, 'active', ?::jsonb, '{}'::text[], ?, now(), now())
	`, id, projectID, id, typ, key, propsJSON, namespace)
	require.NoError(t, err)
	return id
}

// seedBatchRelationship inserts a HEAD relationship on the main branch.
func seedBatchRelationship(t *testing.T, db bun.IDB, projectID, relType, srcID, dstID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_relationships
			(id, project_id, canonical_id, supersedes_id, version, type, src_id, dst_id,
			 properties, branch_id, deleted_at, created_at)
		VALUES (?, ?, ?, NULL, 1, ?, ?, ?, '{}'::jsonb, NULL, NULL, now())
	`, id, projectID, id, relType, srcID, dstID)
	require.NoError(t, err)
	return id
}

// cleanupBatchProjects removes the projects/orgs a test seeded.
func cleanupBatchProjects(t *testing.T, db bun.IDB, orgID, projectID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.orgs WHERE id = ?", orgID)
	})
}

type batchEnvelope struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Data  struct {
		Results []struct {
			Index    int            `json:"index"`
			EntityID string         `json:"entity_id"`
			RelID    string         `json:"relationship_id"`
			OK       bool           `json:"ok"`
			Error    string         `json:"error"`
			Entity   map[string]any `json:"entity"`
		} `json:"results"`
		Entity  map[string]any `json:"entity"`
		Matched int            `json:"matched"`
	} `json:"data"`
	Meta struct {
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
		Total     int `json:"total"`
	} `json:"meta"`
}

func decodeBatchEnvelope(t *testing.T, res *ToolResult, err error) batchEnvelope {
	t.Helper()
	text := toolResultText(t, res, err)
	var env batchEnvelope
	require.NoError(t, json.Unmarshal([]byte(text), &env), "result must be the uniform envelope: %s", text)
	return env
}

// decodeBatchCall runs one tool call and decodes its envelope.
func decodeBatchCall(t *testing.T, call func() (*ToolResult, error)) batchEnvelope {
	t.Helper()
	res, err := call()
	return decodeBatchEnvelope(t, res, err)
}

// ── catalog / bounds (no database) ───────────────────────────────────────────

// TestBatchMutationDescriptionsMentionBatchForms pins requirement 4: every
// touched tool advertises its batch form and the item cap.
func TestBatchMutationDescriptionsMentionBatchForms(t *testing.T) {
	svc := &Service{}

	cases := []struct {
		tool     string
		contains []string
	}{
		{"entity-update", []string{"updates", "up to 100", "per-item results", "retry exactly the failed indexes"}},
		{"entity-delete", []string{"entity_ids", "up to 100", "per-item results", "retry exactly the failed indexes"}},
		{"relationship-delete", []string{"relationship_ids", "up to 100", "per-item results", "retry exactly the failed indexes"}},
	}
	for _, tc := range cases {
		def := toolDefByName(t, svc, tc.tool)
		for _, want := range tc.contains {
			assert.Contains(t, def.Description, want, "%s description must mention %q", tc.tool, want)
		}
	}

	bulk := toolDefByName(t, svc, "entity-bulk-update")
	assert.Equal(t, "graph:write", bulk.RequiredScope, "entity-bulk-update must require the graph write scope")
	for _, want := range []string{"soft_delete", "hard_delete", "dry_run", "matched", "affected", "default"} {
		assert.Contains(t, strings.ToLower(bulk.Description), want, "entity-bulk-update description must mention %q", want)
	}
	filterProp, ok := bulk.InputSchema.Properties["filter"]
	require.True(t, ok, "entity-bulk-update must advertise a filter")
	assert.NotEmpty(t, filterProp.Description, "the filter parameter needs a description")
	actionProp := bulk.InputSchema.Properties["action"]
	assert.Contains(t, actionProp.Enum, "hard_delete", "hard_delete must be a named enum value, never implicit")
	require.Equal(t, []string{"filter"}, bulk.InputSchema.Required, "only filter is mandatory; action defaults to soft_delete")
}

// TestBatchMutationOverLimitRejected pins the 100-item cap and the actionable
// error: a 101-item batch is refused before any row is touched.
func TestBatchMutationOverLimitRejected(t *testing.T) {
	svc := &Service{}
	projectID := uuid.NewString()
	ctx := context.Background()

	manyUpdates := make([]any, batchMutationMaxItems+1)
	manyIDs := make([]any, batchMutationMaxItems+1)
	for i := range manyUpdates {
		manyUpdates[i] = map[string]any{"entity_id": uuid.NewString(), "properties": map[string]any{"n": i}}
		manyIDs[i] = uuid.NewString()
	}

	cases := []struct {
		name string
		run  func() (*ToolResult, error)
	}{
		{"entity-update", func() (*ToolResult, error) {
			return svc.executeUpdateEntity(ctx, projectID, map[string]any{"updates": manyUpdates})
		}},
		{"entity-delete", func() (*ToolResult, error) {
			return svc.executeDeleteEntity(ctx, projectID, map[string]any{"entity_ids": manyIDs})
		}},
		{"relationship-delete", func() (*ToolResult, error) {
			return svc.executeDeleteRelationship(ctx, projectID, map[string]any{"relationship_ids": manyIDs})
		}},
	}
	for _, tc := range cases {
		_, err := tc.run()
		require.Error(t, err, "%s must reject an over-limit batch", tc.name)
		assert.Contains(t, err.Error(), "maximum 100", "%s over-limit error must name the limit: %v", tc.name, err)
		assert.Contains(t, err.Error(), "split", "%s over-limit error must say how to proceed: %v", tc.name, err)
	}
}

// TestBulkUpdateEntitiesRejectsEmptyFilter pins the safety guard: an
// unconstrained filter would match the whole project, so it is refused.
func TestBulkUpdateEntitiesRejectsEmptyFilter(t *testing.T) {
	svc := &Service{}
	_, err := svc.executeBulkUpdateEntities(context.Background(), uuid.NewString(), map[string]any{
		"filter": map[string]any{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of")
}

// TestBulkUpdateEntitiesNarrowsToTypeScope pins that a type-scoped agent cannot
// use the filter tool to reach a type outside its allowlist: an all-types
// request is narrowed to the allowlist, and a request naming only denied types
// matches nothing instead of falling back to "all types".
func TestBulkUpdateEntitiesNarrowsToTypeScope(t *testing.T) {
	svc := &Service{}
	ctx := ContextWithTypeScope(context.Background(), NewTypeScope([]string{"Allowed"}, nil))
	projectID := uuid.NewString()

	env := decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeBulkUpdateEntities(ctx, projectID, map[string]any{
			"filter": map[string]any{"types": []any{"Forbidden"}},
		})
	})
	assert.True(t, env.OK)
	assert.Equal(t, 0, env.Data.Matched, "a denied type must match nothing, not every type")
	assert.Empty(t, env.Data.Results)
}

// TestBulkUpdateEntitiesNarrowsToContextNamespace pins namespace scoping: a
// namespaced caller's filter is narrowed to its namespace, so it cannot match a
// sibling namespace in the same project.
func TestBulkUpdateEntitiesNarrowsToContextNamespace(t *testing.T) {
	db := connectTestDB(t)
	orgID, projectID := seedProject(t, db)
	cleanupBatchProjects(t, db, orgID, projectID)
	svc := batchMutationService(t, db)

	seedBatchObjectInNamespace(t, db, projectID, "Doc", "ns-a-doc", "team-a", `{}`)
	seedBatchObjectInNamespace(t, db, projectID, "Doc", "ns-b-doc", "team-b", `{}`)

	namespacedCtx := auth.ContextWithNamespace(context.Background(), "team-a")
	env := decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeBulkUpdateEntities(namespacedCtx, projectID, map[string]any{
			"filter":  map[string]any{"types": []any{"Doc"}},
			"dry_run": true,
		})
	})
	assert.True(t, env.OK)
	assert.Equal(t, 1, env.Data.Matched, "a namespaced caller must only match its own namespace")
}

// ── DB-backed behaviour ──────────────────────────────────────────────────────

// TestBatchUpdateEntitiesPartialFailure pins requirement 1 + partial-failure:
// two good updates and one bad id yield 2 ok / 1 error with correct counts, and
// the good rows are really updated.
func TestBatchUpdateEntitiesPartialFailure(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedProject(t, db)
	cleanupBatchProjects(t, db, orgID, projectID)
	svc := batchMutationService(t, db)

	id1 := seedBatchObject(t, db, projectID, "Note", "batch-note-1", `{"state":"old"}`)
	id2 := seedBatchObject(t, db, projectID, "Note", "batch-note-2", `{"state":"old"}`)
	missing := uuid.NewString()

	env := decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeUpdateEntity(ctx, projectID, map[string]any{
			"updates": []any{
				map[string]any{"entity_id": id1, "properties": map[string]any{"state": "new"}},
				map[string]any{"entity_id": missing, "properties": map[string]any{"state": "new"}},
				map[string]any{"entity_id": id2, "properties": map[string]any{"state": "new"}},
			},
		})
	})

	assert.False(t, env.OK, "a batch with a failed item must report ok=false")
	assert.Contains(t, env.Error, "1 of 3", "the envelope error must say how many failed")
	assert.Len(t, env.Data.Results, 3)
	assert.True(t, env.Data.Results[0].OK, "item 0 (good id) must succeed")
	assert.False(t, env.Data.Results[1].OK, "item 1 (missing id) must fail")
	assert.Equal(t, missing, env.Data.Results[1].EntityID, "the failed item must echo its id so the caller can retry it")
	assert.NotEmpty(t, env.Data.Results[1].Error)
	assert.True(t, env.Data.Results[2].OK, "item 2 (good id) must succeed")
	assert.Equal(t, 2, env.Meta.Succeeded)
	assert.Equal(t, 1, env.Meta.Failed)
	assert.Equal(t, 3, env.Meta.Total)

	for _, id := range []string{id1, id2} {
		var state string
		require.NoError(t, db.NewSelect().TableExpr("kb.graph_objects").
			ColumnExpr("properties->>'state'").
			Where("project_id = ? AND canonical_id = ? AND supersedes_id IS NULL", projectID, id).
			Scan(ctx, &state))
		assert.Equal(t, "new", state, "a successful item must actually be applied")
	}
}

// TestBatchUpdateEntitiesForeignProjectRejectedPerItem pins tenant scoping: an
// id from another project fails that item with the same not-found shape as a
// missing id, and the foreign row is untouched.
func TestBatchUpdateEntitiesForeignProjectRejectedPerItem(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgA, projectA := seedProject(t, db)
	cleanupBatchProjects(t, db, orgA, projectA)
	orgB, projectB := seedProject(t, db)
	cleanupBatchProjects(t, db, orgB, projectB)
	svc := batchMutationService(t, db)

	ownID := seedBatchObject(t, db, projectA, "Note", "own-note", `{"owner":"a"}`)
	foreignID := seedBatchObject(t, db, projectB, "Note", "foreign-note", `{"owner":"b"}`)
	missing := uuid.NewString()

	env := decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeUpdateEntity(ctx, projectA, map[string]any{
			"updates": []any{
				map[string]any{"entity_id": foreignID, "properties": map[string]any{"owner": "hijacked"}},
				map[string]any{"entity_id": missing, "properties": map[string]any{"owner": "hijacked"}},
				map[string]any{"entity_id": ownID, "properties": map[string]any{"owner": "a2"}},
			},
		})
	})

	require.Len(t, env.Data.Results, 3)
	assert.False(t, env.Data.Results[0].OK, "a foreign-project id must fail its own item")
	assert.False(t, env.Data.Results[1].OK, "a missing id must fail")
	assert.True(t, env.Data.Results[2].OK, "the in-project id must still succeed")
	assert.Equal(t, 1, env.Meta.Succeeded)
	assert.Equal(t, 2, env.Meta.Failed)

	// No existence oracle: a foreign id and a missing id produce the same error.
	assert.Equal(t, env.Data.Results[1].Error, env.Data.Results[0].Error,
		"a foreign-project id must not be distinguishable from a missing id")

	var owner string
	require.NoError(t, db.NewSelect().TableExpr("kb.graph_objects").
		ColumnExpr("properties->>'owner'").
		Where("project_id = ? AND canonical_id = ?", projectB, foreignID).
		Scan(ctx, &owner))
	assert.Equal(t, "b", owner, "the foreign project's row must never be touched")
}

// TestBatchDeleteEntitiesPartialFailureAndForeignProject pins requirement 2 for
// entities: ids or keys in one call, one shared reason, per-item results, and
// cross-project ids rejected per item without touching the foreign row.
func TestBatchDeleteEntitiesPartialFailureAndForeignProject(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgA, projectA := seedProject(t, db)
	cleanupBatchProjects(t, db, orgA, projectA)
	orgB, projectB := seedProject(t, db)
	cleanupBatchProjects(t, db, orgB, projectB)
	svc := batchMutationService(t, db)

	keyID := seedBatchObject(t, db, projectA, "Note", "delete-me", `{}`)
	idOnly := seedBatchObject(t, db, projectA, "Note", "delete-by-id", `{}`)
	foreignID := seedBatchObject(t, db, projectB, "Note", "not-yours", `{}`)

	env := decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeDeleteEntity(ctx, projectA, map[string]any{
			"entity_ids": []any{"delete-me", idOnly, foreignID, uuid.NewString()},
			"reason":     "batch cleanup",
		})
	})

	require.Len(t, env.Data.Results, 4)
	assert.True(t, env.Data.Results[0].OK, "an in-project key must delete")
	assert.True(t, env.Data.Results[1].OK, "an in-project id must delete")
	assert.False(t, env.Data.Results[2].OK, "a foreign-project id must fail")
	assert.False(t, env.Data.Results[3].OK, "a missing id must fail")
	assert.Equal(t, env.Data.Results[3].Error, env.Data.Results[2].Error,
		"foreign and missing ids must be indistinguishable")
	assert.Equal(t, 2, env.Meta.Succeeded)
	assert.Equal(t, 2, env.Meta.Failed)

	var deleted int
	for _, canonical := range []string{keyID, idOnly} {
		require.NoError(t, db.NewSelect().TableExpr("kb.graph_objects").
			ColumnExpr("count(*)").
			Where("project_id = ? AND canonical_id = ? AND supersedes_id IS NULL AND deleted_at IS NOT NULL", projectA, canonical).
			Scan(ctx, &deleted))
		assert.Equal(t, 1, deleted, "the in-project entity must be soft-deleted (head tombstone)")
	}

	require.NoError(t, db.NewSelect().TableExpr("kb.graph_objects").
		ColumnExpr("count(*)").
		Where("project_id = ? AND canonical_id = ? AND deleted_at IS NULL", projectB, foreignID).
		Scan(ctx, &deleted))
	assert.Equal(t, 1, deleted, "the foreign project's entity must remain live")
}

// TestBatchDeleteRelationshipsPartialFailure pins requirement 2 for
// relationships: one batch, one reason, per-item results, foreign ids refused.
func TestBatchDeleteRelationshipsPartialFailure(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgA, projectA := seedProject(t, db)
	cleanupBatchProjects(t, db, orgA, projectA)
	orgB, projectB := seedProject(t, db)
	cleanupBatchProjects(t, db, orgB, projectB)
	svc := batchMutationService(t, db)

	src := seedBatchObject(t, db, projectA, "Note", "rel-src", `{}`)
	dst := seedBatchObject(t, db, projectA, "Note", "rel-dst", `{}`)
	ownRel := seedBatchRelationship(t, db, projectA, "links", src, dst)
	foreignRel := seedBatchRelationship(t, db, projectB, "links", uuid.NewString(), uuid.NewString())

	env := decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeDeleteRelationship(ctx, projectA, map[string]any{
			"relationship_ids": []any{ownRel, foreignRel, "not-a-uuid"},
			"reason":           "batch cleanup",
		})
	})

	require.Len(t, env.Data.Results, 3)
	assert.True(t, env.Data.Results[0].OK, "the in-project relationship must delete")
	assert.False(t, env.Data.Results[1].OK, "a foreign-project relationship must fail")
	assert.False(t, env.Data.Results[2].OK, "a malformed id must fail its own item")
	assert.Equal(t, ownRel, env.Data.Results[0].RelID)
	assert.Equal(t, 1, env.Meta.Succeeded)
	assert.Equal(t, 2, env.Meta.Failed)

	var deleted int
	require.NoError(t, db.NewSelect().TableExpr("kb.graph_relationships").
		ColumnExpr("count(*)").
		Where("project_id = ? AND canonical_id = ? AND deleted_at IS NOT NULL", projectA, ownRel).
		Scan(ctx, &deleted))
	assert.Equal(t, 1, deleted, "the in-project relationship must be soft-deleted")

	require.NoError(t, db.NewSelect().TableExpr("kb.graph_relationships").
		ColumnExpr("count(*)").
		Where("project_id = ? AND canonical_id = ? AND deleted_at IS NULL", projectB, foreignRel).
		Scan(ctx, &deleted))
	assert.Equal(t, 1, deleted, "the foreign project's relationship must remain live")
}

// TestBulkUpdateEntitiesDryRunAndDefaultSoftDelete pins requirement 3: dry_run
// changes nothing but counts, and an omitted action defaults to soft_delete
// (never hard_delete).
func TestBulkUpdateEntitiesDryRunAndDefaultSoftDelete(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgA, projectA := seedProject(t, db)
	cleanupBatchProjects(t, db, orgA, projectA)
	orgB, projectB := seedProject(t, db)
	cleanupBatchProjects(t, db, orgB, projectB)
	svc := batchMutationService(t, db)

	seedBatchObject(t, db, projectA, "Doc", "doc-1", `{}`)
	seedBatchObject(t, db, projectA, "Doc", "doc-2", `{}`)
	seedBatchObject(t, db, projectB, "Doc", "doc-foreign", `{}`)

	liveCount := func(projectID string) int {
		t.Helper()
		var n int
		require.NoError(t, db.NewSelect().TableExpr("kb.graph_objects").
			ColumnExpr("count(*)").
			Where("project_id = ? AND type = 'Doc' AND deleted_at IS NULL", projectID).
			Scan(ctx, &n))
		return n
	}

	// dry_run: counts only, no change.
	env := decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeBulkUpdateEntities(ctx, projectA, map[string]any{
			"filter":  map[string]any{"types": []any{"Doc"}},
			"dry_run": true,
		})
	})
	assert.True(t, env.OK)
	assert.Equal(t, 2, env.Data.Matched)
	assert.Empty(t, env.Data.Results, "dry_run must affect no rows")
	assert.Equal(t, 2, liveCount(projectA), "dry_run must not delete anything")
	assert.Equal(t, 1, liveCount(projectB), "dry_run must not touch another project")

	// Omitted action defaults to soft_delete, not hard_delete.
	env = decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeBulkUpdateEntities(ctx, projectA, map[string]any{
			"filter": map[string]any{"types": []any{"Doc"}},
		})
	})
	assert.True(t, env.OK)
	assert.Equal(t, 2, env.Data.Matched)
	assert.Equal(t, 0, liveCount(projectA), "the default action must soft-delete the matched rows")
	assert.Equal(t, 1, liveCount(projectB), "another project's rows must never be matched")

	var rows int
	require.NoError(t, db.NewSelect().TableExpr("kb.graph_objects").
		ColumnExpr("count(*)").
		Where("project_id = ? AND type = 'Doc'", projectA).
		Scan(ctx, &rows))
	assert.Equal(t, 2, rows, "soft delete must keep the rows (recoverable), unlike hard_delete")
}

// TestEntityDeleteSingleFormUnchanged pins back-compat: the single-item envelope
// is still {ok, data:{entity_id}} with no meta block.
func TestEntityDeleteSingleFormUnchanged(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedProject(t, db)
	cleanupBatchProjects(t, db, orgID, projectID)
	svc := batchMutationService(t, db)

	id := seedBatchObject(t, db, projectID, "Note", "single-delete", `{}`)
	res, err := svc.executeDeleteEntity(ctx, projectID, map[string]any{"entity_id": id})
	text := toolResultText(t, res, err)

	var top map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &top))
	assert.Equal(t, true, top["ok"])
	data, ok := top["data"].(map[string]any)
	require.True(t, ok, "single-item data must be an object")
	assert.Equal(t, id, data["entity_id"])
	_, hasMeta := top["meta"]
	assert.False(t, hasMeta, "the single-item result must not grow a batch meta block")
}

// TestBatchUpdateEntityItemBranchID pins that the per-item branch_id from the
// issue's shape is accepted (and validated) without breaking the item.
func TestBatchUpdateEntityItemBranchID(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedProject(t, db)
	cleanupBatchProjects(t, db, orgID, projectID)
	svc := batchMutationService(t, db)

	id := seedBatchObject(t, db, projectID, "Note", "branch-item", `{"state":"old"}`)
	env := decodeBatchCall(t, func() (*ToolResult, error) {
		return svc.executeUpdateEntity(ctx, projectID, map[string]any{
			"updates": []any{
				map[string]any{"entity_id": id, "branch_id": "not-a-uuid", "properties": map[string]any{"state": "new"}},
			},
		})
	})
	require.Len(t, env.Data.Results, 1)
	assert.False(t, env.Data.Results[0].OK)
	assert.Contains(t, env.Data.Results[0].Error, "branch_id", "an invalid branch_id must fail only that item, naming the field")
	assert.Contains(t, env.Error, "retry the failed indexes", "the envelope error must name the retry surface")
}
