package blueprints

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyUpgradeRefreshesRegistry is the blueprint-path half of the #294
// regression: applying a new version of a blueprint whose pack already owns a
// type must carry the pack's current definitions into the type registry.
func TestApplyUpgradeRefreshesRegistry(t *testing.T) {
	db := connectTestDB(t)
	svc, _, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	manifest := func(version string, props map[string]any) BlueprintManifest {
		return BlueprintManifest{Packs: []PackManifest{{
			Name: "ot-core", Version: version,
			ObjectTypes: []ObjectTypeDef{{Name: "Person", Properties: props}},
		}}}
	}

	v1 := manifest("0.1.0", map[string]any{
		"name":   map[string]any{"type": "string"},
		"legacy": map[string]any{"type": "string"},
	})
	bp1 := createPublishedMergeBlueprint(t, ctx, svc, projectID, uniqueName("registry-upgrade"), v1)
	_, err := svc.Apply(ctx, bp1.ID, projectID, "", ApplyOptions{})
	require.NoError(t, err)

	// Version bump: a new pack row with the same name still owns the type, so
	// the upgrade replaces the registered definition without a merge gate.
	v2 := manifest("0.2.0", map[string]any{
		"name":         map[string]any{"type": "string"},
		"email":        map[string]any{"type": "string"},
		"relationship": map[string]any{"type": "string"},
	})
	bp2 := createPublishedMergeBlueprint(t, ctx, svc, projectID, uniqueName("registry-upgrade"), v2)
	preview, err := svc.PreviewApply(ctx, bp2.ID, projectID)
	require.NoError(t, err)
	assert.False(t, preview.RequiresMerge, "an upgrade of the owning pack is not a collision")

	_, err = svc.Apply(ctx, bp2.ID, projectID, "", ApplyOptions{})
	require.NoError(t, err)

	got := registrySchemaJSON(t, ctx, db, projectID, "Person")
	props, _ := got["properties"].(map[string]any)
	require.NotNil(t, props)
	assert.Contains(t, props, "email", "version bump must refresh the registry")
	assert.Contains(t, props, "relationship")
	assert.NotContains(t, props, "legacy", "a removed property must be removed on upgrade")
}

// TestApplySameRunTypeCollisionRequiresMerge is the #293 regression at the
// blueprint boundary: one blueprint whose two packs declare the same type is a
// collision, and applying it without merge confirmation must be a no-op.
func TestApplySameRunTypeCollisionRequiresMerge(t *testing.T) {
	db := connectTestDB(t)
	svc, _, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	manifest := BlueprintManifest{Packs: []PackManifest{
		{Name: "aa-core", Version: "0.1.0", ObjectTypes: []ObjectTypeDef{{
			Name: "Person", Properties: map[string]any{"name": map[string]any{"type": "string"}},
		}}},
		{Name: "bb-companies", Version: "0.1.0", ObjectTypes: []ObjectTypeDef{{
			Name: "Person", Properties: map[string]any{"legal_name": map[string]any{"type": "string"}},
		}}},
	}}
	bp := createPublishedMergeBlueprint(t, ctx, svc, projectID, uniqueName("same-run"), manifest)

	preview, err := svc.PreviewApply(ctx, bp.ID, projectID)
	require.NoError(t, err)
	assert.True(t, preview.RequiresMerge)
	require.Len(t, preview.Conflicts, 1)

	before := countRows(t, ctx, db, "kb.project_object_schema_registry")
	_, err = svc.Apply(ctx, bp.ID, projectID, "", ApplyOptions{})
	require.Error(t, err)
	assert.Equal(t, before, countRows(t, ctx, db, "kb.project_object_schema_registry"),
		"a blocked apply must not materialize a type")

	_, err = svc.Apply(ctx, bp.ID, projectID, "", ApplyOptions{MergeTypeConflicts: true})
	require.NoError(t, err)

	got := registrySchemaJSON(t, ctx, db, projectID, "Person")
	props, _ := got["properties"].(map[string]any)
	require.NotNil(t, props)
	assert.Contains(t, props, "name")
	assert.Contains(t, props, "legal_name")
}

// TestApplyCarriesPackUIBlock is the #291 regression: a pack's inline `ui:`
// block must reach the pack's top-level ui_configs, which is where the schema
// registry reads appearance from.
func TestApplyCarriesPackUIBlock(t *testing.T) {
	db := connectTestDB(t)
	svc, _, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	manifest := BlueprintManifest{Packs: []PackManifest{{
		Name: "ui-pack", Version: "0.1.0",
		ObjectTypes: []ObjectTypeDef{{
			Name:       "UIPattern",
			Label:      "UI Pattern",
			UI:         map[string]any{"icon": "layers", "color": "#8B5CF6"},
			Properties: map[string]any{"name": map[string]any{"type": "string"}},
		}},
	}}}
	bp := createPublishedMergeBlueprint(t, ctx, svc, projectID, uniqueName("ui-pack"), manifest)
	_, err := svc.Apply(ctx, bp.ID, projectID, "", ApplyOptions{})
	require.NoError(t, err)

	var raw []byte
	require.NoError(t, db.NewRaw(`
		SELECT ui_configs FROM kb.graph_schemas WHERE project_id = ? AND name = 'ui-pack'
	`, projectID).Scan(ctx, &raw))
	var ui map[string]map[string]any
	require.NoError(t, json.Unmarshal(raw, &ui))
	require.Contains(t, ui, "UIPattern")
	assert.Equal(t, "layers", ui["UIPattern"]["icon"])
	assert.Equal(t, "#8B5CF6", ui["UIPattern"]["color"])
}
