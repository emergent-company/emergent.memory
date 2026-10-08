package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/journal"
	"github.com/emergent-company/emergent.memory/internal/config"
)

// scopedCtx builds a context carrying a TypeScope with the given allowlists.
// Pass nil for an unrestricted axis.
func scopedCtx(objectTypes, relationshipTypes []string) context.Context {
	return ContextWithTypeScope(context.Background(), NewTypeScope(objectTypes, relationshipTypes))
}

// insertRelationship inserts a HEAD graph relationship of the given type between
// srcID and dstID (both physical ids, which equal canonical_id for v1 HEAD rows).
func insertRelationship(t *testing.T, db bun.IDB, projectID, relType, srcID, dstID string) string {
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

// insertArchiveObject inserts a graph object with a non-empty migration_archive,
// so executeListMigrationArchives / executeGetMigrationArchive see it.
func insertArchiveObject(t *testing.T, db bun.IDB, projectID, typ, key string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at, migration_archive)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, ?, 'active', '{"name":"archived"}'::jsonb, '{}'::text[], now(), now(),
			'[{"from_version":"1","to_version":"2"}]'::jsonb)
	`, id, projectID, id, typ, key)
	require.NoError(t, err)
	return id
}

// insertJournalEntryFull inserts a kb.project_journal row with explicit event
// type, nullable entity_type/object_type, and arbitrary metadata JSON.
func insertJournalEntryFull(t *testing.T, db bun.IDB, projectID, eventType string, entityType, objectType *string, metadataJSON string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.project_journal
			(id, project_id, branch_id, event_type, entity_type, entity_id, object_type,
			 actor_type, actor_id, metadata, created_at)
		VALUES (?, ?, NULL, ?, ?, NULL, ?, 'agent', NULL, ?::jsonb, now())
	`, id, projectID, eventType, entityType, objectType, metadataJSON)
	require.NoError(t, err)
	return id
}

// runEntityQueryScoped executes executeQueryEntities with the given ctx and
// decodes the envelope data payload.
func runEntityQueryScoped(t *testing.T, svc *Service, ctx context.Context, projectID string, args map[string]any) (QueryEntitiesResult, error) {
	t.Helper()
	res, err := svc.executeQueryEntities(ctx, projectID, args)
	if err != nil {
		return QueryEntitiesResult{}, err
	}
	text := toolResultText(t, res, nil)
	var env struct {
		OK   bool                `json:"ok"`
		Data QueryEntitiesResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &env))
	require.True(t, env.OK, "entity-query envelope ok=false: %s", text)
	return env.Data, nil
}

// =============================================================================
// executeListEntityTypes
// =============================================================================

func TestTypeScope_ExecuteListEntityTypes(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	// Object type registry rows for both an allowed and an excluded type.
	insertScopedType(t, db, projectID, "Law", nil, nil)
	insertScopedType(t, db, projectID, "Person", nil, nil)

	law1 := insertGraphObject(t, db, projectID, "Law", "law-1")
	law2 := insertGraphObject(t, db, projectID, "Law", "law-2")
	person1 := insertGraphObject(t, db, projectID, "Person", "person-1")

	insertRelationship(t, db, projectID, "CITES", law1, law2)    // Law -> Law (allowed)
	insertRelationship(t, db, projectID, "KNOWS", law1, person1) // Law -> Person (to_type excluded)
	insertRelationship(t, db, projectID, "OWNS", person1, law1)  // Person -> Law (from_type excluded)

	ctx := scopedCtx([]string{"Law"}, nil)
	svc := &Service{db: db}

	res, err := svc.executeListEntityTypes(ctx, projectID, map[string]any{"include_relationships": true})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var result EntityTypesResult
	require.NoError(t, json.Unmarshal([]byte(text), &result))

	// Object types: excluded "Person" must be absent.
	require.Len(t, result.Types, 1)
	assert.Equal(t, "Law", result.Types[0].Name)

	// Relationship types: only CITES (from/to both Law) survives; KNOWS and OWNS
	// leak an excluded object type via from_type/to_type and must be dropped.
	require.Len(t, result.Relationships, 1)
	assert.Equal(t, "CITES", result.Relationships[0].Type)
	assert.Equal(t, "Law", result.Relationships[0].FromType)
	assert.Equal(t, "Law", result.Relationships[0].ToType)
}

// =============================================================================
// executeQueryEntities
// =============================================================================

func TestTypeScope_ExecuteQueryEntities(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	insertGraphObject(t, db, projectID, "Law", "law-1")
	insertGraphObject(t, db, projectID, "Person", "person-1")

	ctx := scopedCtx([]string{"Law"}, nil)
	svc := &Service{db: db}

	// Excluded type_name rejected.
	_, err := svc.executeQueryEntities(ctx, projectID, map[string]any{"type_name": "Person"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")

	// Allowed type_name accepted, returns only Law.
	out, err := runEntityQueryScoped(t, svc, ctx, projectID, map[string]any{"type_name": "Law"})
	require.NoError(t, err)
	require.Len(t, out.Entities, 1)
	assert.Equal(t, "Law", out.Entities[0].Type)

	// Empty type_name (query-all) is narrowed to allowed types only.
	out, err = runEntityQueryScoped(t, svc, ctx, projectID, map[string]any{})
	require.NoError(t, err)
	require.Len(t, out.Entities, 1)
	assert.Equal(t, "Law", out.Entities[0].Type)
}

// =============================================================================
// executeSearchEntities
// =============================================================================

func TestTypeScope_ExecuteSearchEntities(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	insertSearchEntity(t, db, projectID, "Law", "law/common", "", "Common Law", "", time.Now())
	insertSearchEntity(t, db, projectID, "Person", "person/common", "", "Common Person", "", time.Now())

	ctx := scopedCtx([]string{"Law"}, nil)
	svc := &Service{db: db}

	// Excluded type_name rejected.
	_, err := svc.executeSearchEntities(ctx, projectID, map[string]any{"query": "Common", "type_name": "Person"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")

	// Empty type_name (search-all) returns only allowed types.
	res, err := svc.executeSearchEntities(ctx, projectID, map[string]any{"query": "Common"})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)
	var out SearchEntitiesResult
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	require.Len(t, out.Entities, 1)
	assert.Equal(t, "Law", out.Entities[0].Type)
}

// =============================================================================
// executeTraverseGraph — relationship rejection (guard runs before graphService)
// =============================================================================

func TestTypeScope_ExecuteTraverseGraph_RejectsExcludedRelationshipTypes(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	startID := insertGraphObject(t, db, projectID, "Law", "law-a")

	ctx := scopedCtx([]string{"Law"}, []string{"CITES"})
	svc := &Service{db: db} // nil graphService: rejection happens before it is reached

	_, err := svc.executeTraverseGraph(ctx, projectID, map[string]any{
		"start_entity_id":    startID,
		"relationship_types": []any{"OWNS"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "none of the requested relationship types are allowed")
}

// =============================================================================
// filterTraverseResponseByScope — node/edge filtering (pure, no graphService)
// =============================================================================

func TestFilterTraverseResponseByScope(t *testing.T) {
	scope := NewTypeScope([]string{"Law"}, []string{"CITES"})

	res := &graph.TraverseGraphResponse{
		Nodes: []*graph.TraverseNode{
			{Type: "Law"},
			{Type: "Person"},
			{Type: "Law"},
		},
		Edges: []*graph.TraverseEdge{
			{Type: "CITES"},
			{Type: "OWNS"},
		},
	}

	out := filterTraverseResponseByScope(scope, res)
	require.Len(t, out.Nodes, 2)
	for _, n := range out.Nodes {
		assert.Equal(t, "Law", n.Type)
	}
	require.Len(t, out.Edges, 1)
	assert.Equal(t, "CITES", out.Edges[0].Type)
}

// =============================================================================
// executeGetSchema
// =============================================================================

// insertSchemaFixture seeds a graph schema + project schema + object/edge
// registry rows, returning the schema id.
func insertSchemaFixture(t *testing.T, db bun.IDB, projectID string) string {
	t.Helper()
	ctx := context.Background()
	schemaID := uuid.NewString()

	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_schemas (id, name, version, project_id, object_type_schemas, source)
		VALUES (?, 'test-schema', '1.0.0', ?, '{}'::jsonb, 'custom')
	`, schemaID, projectID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
		VALUES (?, ?, true, now())
	`, projectID, schemaID)
	require.NoError(t, err)

	for _, typ := range []string{"Law", "Person"} {
		_, err = db.ExecContext(ctx, `
			INSERT INTO kb.project_object_schema_registry
				(project_id, type_name, source, schema_id, json_schema, enabled)
			VALUES (?, ?, 'template', ?, '{}'::jsonb, true)
		`, projectID, typ, schemaID)
		require.NoError(t, err)
	}
	for _, typ := range []string{"CITES", "KNOWS"} {
		_, err = db.ExecContext(ctx, `
			INSERT INTO kb.project_edge_schema_registry
				(project_id, schema_id, type_name, json_schema)
			VALUES (?, ?, ?, '{}'::jsonb)
		`, projectID, schemaID, typ)
		require.NoError(t, err)
	}
	return schemaID
}

func TestTypeScope_ExecuteGetSchema(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	schemaID := insertSchemaFixture(t, db, projectID)

	ctx := scopedCtx([]string{"Law"}, []string{"CITES"})
	svc := &Service{db: db}

	res, err := svc.executeGetSchema(ctx, projectID, map[string]any{"schema_id": schemaID})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var out struct {
		ObjectTypes []struct {
			TypeName string `json:"type_name"`
		} `json:"object_types"`
		RelationshipTypes []struct {
			TypeName string `json:"type_name"`
		} `json:"relationship_types"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &out))

	require.Len(t, out.ObjectTypes, 1)
	assert.Equal(t, "Law", out.ObjectTypes[0].TypeName)

	require.Len(t, out.RelationshipTypes, 1)
	assert.Equal(t, "CITES", out.RelationshipTypes[0].TypeName)
}

// =============================================================================
// executeListMigrationArchives / executeGetMigrationArchive
// =============================================================================

func TestTypeScope_ExecuteListMigrationArchives(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	insertArchiveObject(t, db, projectID, "Law", "law-archived")
	insertArchiveObject(t, db, projectID, "Person", "person-archived")

	ctx := scopedCtx([]string{"Law"}, nil)
	svc := &Service{db: db}

	res, err := svc.executeListMigrationArchives(ctx, projectID, map[string]any{"limit": 20})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	assert.Contains(t, text, "Law")
	assert.NotContains(t, text, "Person")
}

func TestTypeScope_ExecuteGetMigrationArchive(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	personID := insertArchiveObject(t, db, projectID, "Person", "person-archived")

	ctx := scopedCtx([]string{"Law"}, nil)
	svc := &Service{db: db}

	res, err := svc.executeGetMigrationArchive(ctx, projectID, map[string]any{"object_id": personID})
	require.Error(t, err)
	assert.NotNil(t, res)
	assert.Contains(t, toolResultText(t, res, nil), "outside this agent's permitted object types")
}

// =============================================================================
// executeJournalList
// =============================================================================

func TestTypeScope_ExecuteJournalList(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	entityObject := journal.EntityObject
	entityRelationship := journal.EntityRelationship
	objLaw := "Law"
	objPerson := "Person"
	relCITES := "CITES"
	relKNOWS := "KNOWS"

	// Allowed/excluded object rows (object_type set).
	insertJournalEntryFull(t, db, projectID, "updated", &entityObject, &objLaw, "{}")
	insertJournalEntryFull(t, db, projectID, "updated", &entityObject, &objPerson, "{}")
	// Allowed/excluded relationship rows (object_type set).
	insertJournalEntryFull(t, db, projectID, "updated", &entityRelationship, &relCITES, "{}")
	insertJournalEntryFull(t, db, projectID, "updated", &entityRelationship, &relKNOWS, "{}")
	// related-shaped row: relationship type only in metadata, object_type NULL.
	insertJournalEntryFull(t, db, projectID, "related", &entityRelationship, nil,
		`{"src_key":"a1","rel_type":"KNOWS","dst_key":"b1"}`)
	// merge-shaped row: nil entity_type, object types only in metadata.
	insertJournalEntryFull(t, db, projectID, "merge", nil, nil,
		`{"left_for_review_objects":[{"type":"Person","name":"p"},{"type":"Law"}],"left_for_review_count":2}`)
	// bulk-batch row: graph_object with NULL object_type, types in filter metadata.
	insertJournalEntryFull(t, db, projectID, "batch", &entityObject, nil,
		`{"action":"update_status","filter":{"types":["Person"]}}`)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &Service{
		db:         db,
		journalSvc: journal.NewService(journal.NewRepository(db, nil), &config.Config{}, log),
	}

	ctx := scopedCtx([]string{"Law"}, []string{"CITES"})

	res, err := svc.executeJournalList(ctx, projectID, map[string]any{})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	// No excluded object/relationship type name may survive in any field
	// (top-level columns or nested metadata).
	assert.NotContains(t, text, "Person")
	assert.NotContains(t, text, "KNOWS")
	// Allowed names must survive.
	assert.Contains(t, text, "Law")
	assert.Contains(t, text, "CITES")
}

// TestFilterJournalEntriesByScope_MetadataScrub is a pure (no-DB) test that pins
// the read-side metadata scrub: a nil-object_type relationship row resolves its
// type from metadata.rel_type, and a merge row's left_for_review_objects drops
// excluded object types wholesale.
func TestFilterJournalEntriesByScope_MetadataScrub(t *testing.T) {
	scope := NewTypeScope([]string{"Law"}, []string{"CITES"})
	entityRel := journal.EntityRelationship
	cites := "CITES"
	knows := "KNOWS"

	entries := []*journal.JournalEntry{
		// related-shaped: relationship type only in metadata, object_type nil.
		{
			EntityType: &entityRel,
			Metadata:   map[string]any{"src_key": "a1", "rel_type": "KNOWS", "dst_key": "b1"},
		},
		// merge-shaped: nil entity_type, object types only in metadata.
		{
			Metadata: map[string]any{
				"left_for_review_count": 2,
				"left_for_review_objects": []any{
					map[string]any{"type": "Person", "name": "p"},
					map[string]any{"type": "Law"},
				},
			},
		},
		// allowed relationship (kept).
		{EntityType: &entityRel, ObjectType: &cites},
		// excluded relationship (dropped).
		{EntityType: &entityRel, ObjectType: &knows},
	}

	out := filterJournalEntriesByScope(scope, entries)
	require.Len(t, out, 2, "related (rel_type=KNOWS) and excluded rel must drop; merge + allowed rel kept")

	var merge *journal.JournalEntry
	for _, e := range out {
		if e.EntityType == nil {
			merge = e
		}
	}
	require.NotNil(t, merge, "merge entry must survive")
	sl, ok := merge.Metadata["left_for_review_objects"].([]any)
	require.True(t, ok)
	require.Len(t, sl, 1, "excluded Person item must be dropped wholesale")
	assert.Equal(t, "Law", sl[0].(map[string]any)["type"])
}

// =============================================================================
// fuzzy key suggestions
// =============================================================================

func TestTypeScope_FuzzySuggestionsNoExcludedType(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	// Same first token so both keys are fuzzy candidates for "shared-missing";
	// without the scope filter the Person sample would surface in suggestions.
	insertGraphObject(t, db, projectID, "Law", "shared-law")
	insertGraphObject(t, db, projectID, "Person", "shared-person")

	ctx := scopedCtx([]string{"Law"}, nil)
	svc := &Service{db: db}

	pid := uuid.MustParse(projectID)
	suggestions := svc.suggestEntityKeysByFuzzy(ctx, pid, "shared-missing", 10)
	require.NotEmpty(t, suggestions)
	for _, s := range suggestions {
		assert.NotContains(t, s, "Person", "suggestion must not leak excluded type")
	}
	assert.Contains(t, strings.Join(suggestions, " "), "shared-law")
}
