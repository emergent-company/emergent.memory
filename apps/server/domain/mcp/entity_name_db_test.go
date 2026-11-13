package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The display name is a first-class column on kb.graph_objects (migration
// 00280), and it is what the app's object list and entity-query show. Before
// #718 the MCP write path dropped it: create could only set it sideways through
// properties.name (resolveObjectName backfills it on create), and an update had
// no route at all, so a renamed entity kept rendering its key. These tests pin
// both halves of the fix: an explicit spec name and an explicit update name.

// createNamedEntity creates one entity through the batch-create path (the only
// create path the tool exposes) and returns the canonical id the response
// reports, so a test can read back the head version by id rather than by key.
func createNamedEntity(t *testing.T, svc *Service, ctx context.Context, projectID string, spec map[string]any) string {
	t.Helper()
	res, err := svc.executeBatchCreateEntities(ctx, projectID, map[string]any{
		"entities": []any{spec},
	})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var top struct {
		OK   bool `json:"ok"`
		Data struct {
			Results []struct {
				OK     bool `json:"ok"`
				Error  string
				Entity struct {
					ID string `json:"id"`
				} `json:"entity"`
			} `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &top), "create response: %s", text)
	require.True(t, top.OK, "create must succeed: %s", text)
	require.Len(t, top.Data.Results, 1, "create response: %s", text)
	require.True(t, top.Data.Results[0].OK, "create must succeed: %s", text)
	id := top.Data.Results[0].Entity.ID
	require.NotEmpty(t, id, "create response must carry the entity id: %s", text)
	return id
}

// objectNameParts reads the stored name column and properties.name for a
// canonical id's head version.
func objectNameParts(t *testing.T, svc *Service, projectID, canonicalID string) (column, prop string) {
	t.Helper()
	var col *string
	var props []byte
	err := svc.db.NewRaw(`
		SELECT name, properties FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND supersedes_id IS NULL`,
		projectID, canonicalID).Scan(context.Background(), &col, &props)
	require.NoError(t, err, "object %q must exist", canonicalID)
	if col != nil {
		column = *col
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal(props, &m))
	prop, _ = m["name"].(string)
	return column, prop
}

// TestEntityCreateSetsDisplayName verifies that an entity spec's `name` lands on
// the name column — the path a caller uses when the display name is not the
// property (a variant keyed ui-variant-… titled "Button size").
func TestEntityCreateSetsDisplayName(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)

	id := createNamedEntity(t, svc, ctx, projectID, map[string]any{
		"type":       "UIVariant",
		"key":        "ui-variant-probe-buttonsize",
		"name":       "Button size",
		"properties": map[string]any{"name": "Button size", "go_type": "ButtonSize"},
	})

	column, prop := objectNameParts(t, svc, projectID, id)
	assert.Equal(t, "Button size", column, "spec name must be written to the name column")
	assert.Equal(t, "Button size", prop)
}

// TestEntityUpdateSetsDisplayNameFromName is the #718 regression: updating with
// `name` must move the name column, while properties.name keeps whatever the
// caller last set there. Passing only properties.name must NOT change the
// column — that asymmetry is why the field exists.
func TestEntityUpdateSetsDisplayNameFromName(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)

	const key = "ui-pattern-probe-rename"
	id := createNamedEntity(t, svc, ctx, projectID, map[string]any{
		"type":       "UIPattern",
		"key":        key,
		"properties": map[string]any{"name": "Old title"},
	})

	// A properties-only patch leaves the column alone.
	res, err := svc.executeUpdateEntity(ctx, projectID, map[string]any{
		"entity_id":  key,
		"properties": map[string]any{"name": "Property only"},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	column, prop := objectNameParts(t, svc, projectID, id)
	assert.Equal(t, "Old title", column, "properties.name must not move the name column")
	assert.Equal(t, "Property only", prop)

	// An explicit `name` moves it.
	res, err = svc.executeUpdateEntity(ctx, projectID, map[string]any{
		"entity_id": key,
		"name":      "New title",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	column, prop = objectNameParts(t, svc, projectID, id)
	assert.Equal(t, "New title", column, "update name must be written to the name column")
	assert.Equal(t, "Property only", prop, "update name must not rewrite properties.name")

	// The response reports the name the caller just set, not the stale property:
	// an agent must be able to verify a rename from the write's own result.
	var top struct {
		Data struct {
			Entity struct {
				Name string `json:"name"`
			} `json:"entity"`
		} `json:"data"`
	}
	text := toolResultText(t, res, nil)
	require.NoError(t, json.Unmarshal([]byte(text), &top), "update response: %s", text)
	assert.Equal(t, "New title", top.Data.Entity.Name,
		"the update response must report the name column, not properties.name: %s", text)
}

// TestEntityUpdateBatchSetsDisplayName verifies the batch form accepts a
// per-item name, so a lane renaming many objects is one call.
func TestEntityUpdateBatchSetsDisplayName(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)

	const key = "ui-rule-probe-batch"
	id := createNamedEntity(t, svc, ctx, projectID, map[string]any{
		"type":       "UIRule",
		"key":        key,
		"properties": map[string]any{"name": "Rule old"},
	})

	res, err := svc.executeUpdateEntity(ctx, projectID, map[string]any{
		"updates": []any{
			map[string]any{"entity_id": key, "name": "Rule new"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	column, _ := objectNameParts(t, svc, projectID, id)
	assert.Equal(t, "Rule new", column, "batch item name must be written to the name column")
}

// TestEntityUpdateNameSchema pins the advertised schema: an agent can only use
// the field if the tool declares it, in both forms.
func TestEntityUpdateNameSchema(t *testing.T) {
	svc := &Service{}
	tool := toolDefByName(t, svc, "entity-update")

	prop, ok := tool.InputSchema.Properties["name"]
	require.True(t, ok, "entity-update must expose a top-level name")
	assert.Equal(t, "string", prop.Type)
	assert.NotEmpty(t, prop.Description)

	updates, ok := tool.InputSchema.Properties["updates"]
	require.True(t, ok)
	require.NotNil(t, updates.Items, "updates must declare its item schema")
	itemName, ok := updates.Items.Properties["name"]
	require.True(t, ok, "a batch item must accept name")
	assert.Equal(t, "string", itemName.Type)

	create := toolDefByName(t, svc, "entity-create")
	entities, ok := create.InputSchema.Properties["entities"]
	require.True(t, ok)
	require.NotNil(t, entities.Items)
	assert.Contains(t, entities.Description, "name",
		"entity-create must tell the caller that a spec accepts a display name")
}
