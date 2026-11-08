package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteListEntityTypes_InstalledPacksOnly pins issue #403: entity-type-list
// must (a) return the schema description, (b) exclude types whose owning pack is
// no longer installed, and (c) surface installed relationship types. Before the
// fix the tool read the stale kb.project_object_schema_registry without checking
// whether the pack was still installed (so uninstalled-pack types leaked with
// count 0), read the legacy description column instead of json_schema, and
// computed relationships from live graph_relationships instances instead of the
// installed relationship schema (so a project with installed relationship types
// but no instances reported an empty relationships list).
func TestExecuteListEntityTypes_InstalledPacksOnly(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	ctx := context.Background()

	installedPack := uuid.NewString()
	phantomPack := uuid.NewString()

	// Installed pack: defines Meeting (with a description) and relationship
	// "attends" (Person -> Meeting).
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_schemas (id, name, version, source, object_type_schemas, relationship_type_schemas)
		VALUES (?, 'meetings', '1.0.0', 'custom',
			'{"Meeting":{"description":"A meeting record","properties":{}}}'::jsonb,
			'[{"name":"attends","sourceType":"Person","targetType":"Meeting"}]'::jsonb)
	`, installedPack)
	require.NoError(t, err)

	// Phantom pack: defines AgendaItem, but is never assigned to the project.
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.graph_schemas (id, name, version, source, object_type_schemas, relationship_type_schemas)
		VALUES (?, 'old-meetings', '1.0.0', 'custom',
			'{"AgendaItem":{"description":"leftover agenda","properties":{}}}'::jsonb,
			'[]'::jsonb)
	`, phantomPack)
	require.NoError(t, err)

	// Assign the installed pack only.
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
		VALUES (?, ?, true, now())
	`, projectID, installedPack)
	require.NoError(t, err)

	// Registry rows: Meeting (installed) and AgendaItem (phantom — schema_id
	// points at a pack with no assignment). Descriptions live in json_schema;
	// the legacy description column is left NULL.
	insertRegistry := func(typeName, schemaID, desc string) {
		t.Helper()
		schema, mErr := json.Marshal(map[string]any{"description": desc, "properties": map[string]any{}})
		require.NoError(t, mErr)
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.project_object_schema_registry
				(project_id, type_name, source, schema_id, json_schema, ui_config, extraction_config, enabled)
			VALUES (?, ?, 'template', ?::uuid, ?::jsonb, '{}'::jsonb, '{}'::jsonb, true)
		`, projectID, typeName, schemaID, string(schema))
		require.NoError(t, err)
	}
	insertRegistry("Meeting", installedPack, "A meeting record")
	insertRegistry("AgendaItem", phantomPack, "leftover agenda")

	// One Meeting object so the installed type has a non-zero count.
	insertGraphObject(t, db, projectID, "Meeting", "meeting-1")

	svc := &Service{db: db}

	result := listEntityTypes(t, svc, projectID, map[string]any{"namespace": "all"})

	// (a) description is the schema description, not "No description".
	// (b) the phantom type from the unassigned pack is absent.
	var meeting *EntityType
	for i := range result.Types {
		assert.NotEqual(t, "AgendaItem", result.Types[i].Name, "phantom type from uninstalled pack must not be listed")
		if result.Types[i].Name == "Meeting" {
			meeting = &result.Types[i]
		}
	}
	require.NotNil(t, meeting, "installed type Meeting must be listed")
	assert.Equal(t, "A meeting record", meeting.Description)
	assert.Equal(t, 1, meeting.Count)

	// (c) installed relationship types appear even with zero instances, and
	// their declared endpoint object types are surfaced.
	with := listEntityTypes(t, svc, projectID, map[string]any{"include_relationships": true, "namespace": "all"})
	require.Len(t, with.Relationships, 1, "installed relationship type must appear")
	assert.Equal(t, "attends", with.Relationships[0].Type)
	assert.Equal(t, "Person", with.Relationships[0].FromType)
	assert.Equal(t, "Meeting", with.Relationships[0].ToType)
	assert.Equal(t, 0, with.Relationships[0].Count)
}

// TestExecuteListEntityTypes_MapShapeRelationshipTypes pins the map storage
// shape of graph_schemas.relationship_type_schemas — {"name": {"sourceTypes":
// [...], "targetTypes": [...]}} — used by older seeds/blueprints. The
// jsonb_object_keys branch must list the relationship type with its declared
// endpoint object types and a zero count.
func TestExecuteListEntityTypes_MapShapeRelationshipTypes(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	ctx := context.Background()

	packID := uuid.NewString()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_schemas (id, name, version, source, object_type_schemas, relationship_type_schemas)
		VALUES (?, 'meetings-map', '1.0.0', 'custom',
			'{}'::jsonb,
			'{"attends":{"sourceTypes":["Person"],"targetTypes":["Meeting"]}}'::jsonb)
	`, packID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
		VALUES (?, ?, true, now())
	`, projectID, packID)
	require.NoError(t, err)

	svc := &Service{db: db}
	with := listEntityTypes(t, svc, projectID, map[string]any{"include_relationships": true, "namespace": "all"})
	require.Len(t, with.Relationships, 1, "map-shape relationship type must appear")
	rel := with.Relationships[0]
	assert.Equal(t, "attends", rel.Type)
	assert.Equal(t, "Person", rel.FromType)
	assert.Equal(t, "Meeting", rel.ToType)
	assert.Equal(t, 0, rel.Count)
}

// TestExecuteListEntityTypes_DeclaredRelationshipScope pins the scope precision
// of the declared-relationship fallback: when the schema names both endpoint
// object types, the relationship is scope-checked exactly like the in-use path
// (the from/to object names must also be permitted on the object axis), so an
// out-of-scope endpoint hides it rather than leaking the type name.
func TestExecuteListEntityTypes_DeclaredRelationshipScope(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	ctx := context.Background()

	packID := uuid.NewString()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_schemas (id, name, version, source, object_type_schemas, relationship_type_schemas)
		VALUES (?, 'scoped', '1.0.0', 'custom', '{}'::jsonb,
			'[{"name":"attends","sourceType":"Person","targetType":"Meeting"}]'::jsonb)
	`, packID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
		VALUES (?, ?, true, now())
	`, projectID, packID)
	require.NoError(t, err)

	svc := &Service{db: db}

	// Object axis restricted to Person; relationship axis allows attends.
	// The declared "attends" (Person -> Meeting) has an out-of-scope target, so
	// it must be hidden, matching how the in-use path filters its rows.
	scoped := ContextWithTypeScope(ctx, NewTypeScope([]string{"Person"}, []string{"attends"}))
	res, err := svc.executeListEntityTypes(scoped, projectID, map[string]any{"include_relationships": true, "namespace": "all"})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var result EntityTypesResult
	require.NoError(t, json.Unmarshal([]byte(text), &result))
	assert.Empty(t, result.Relationships, "declared relationship with an out-of-scope endpoint must be hidden")
}
