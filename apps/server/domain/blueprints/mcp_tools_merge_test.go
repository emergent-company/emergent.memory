package blueprints

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPBlueprintApplyPreviewAndMerge confirms blueprint-apply-preview exposes
// type collisions and blueprint-apply's mergeTypes flag proceeds through them.
func TestMCPBlueprintApplyPreviewAndMerge(t *testing.T) {
	db := connectTestDB(t)
	svc, schemasSvc, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	seedRegisteredPack(t, ctx, schemasSvc, projectID, "Existing Pack", widgetTypes(map[string]any{
		"name": map[string]any{"type": "string"},
	}))
	manifest := BlueprintManifest{Packs: mergedPackManifest("Incoming Pack", "1.0.0").Packs}
	bp := createPublishedMergeBlueprint(t, ctx, svc, projectID, uniqueName("mcp-merge"), manifest)

	h := NewMCPBlueprintToolHandler(svc)

	previewRes, err := h.ExecuteBlueprintApplyPreview(ctx, projectID, map[string]any{"id": bp.ID})
	require.NoError(t, err)
	var preview ApplyPreview
	require.NoError(t, json.Unmarshal([]byte(mcpResultText(t, previewRes, nil)), &preview))
	assert.True(t, preview.RequiresMerge)
	require.Len(t, preview.Conflicts, 1)
	assert.Equal(t, "Widget", preview.Conflicts[0].TypeName)

	blockedRes, err := h.ExecuteBlueprintApply(ctx, projectID, map[string]any{"id": bp.ID})
	require.NoError(t, err)
	require.Contains(t, mcpResultText(t, blockedRes, nil), "object type name conflicts require merge confirmation")

	mergedRes, err := h.ExecuteBlueprintApply(ctx, projectID, map[string]any{
		"id":         bp.ID,
		"mergeTypes": true,
	})
	require.NoError(t, err)
	mergedText := mcpResultText(t, mergedRes, nil)
	require.Contains(t, mergedText, `"typeConflicts"`)
	require.Contains(t, mergedText, `"Widget"`)

	// Registry reflects the merge: existing definition wins, new property added.
	schema := registrySchemaJSON(t, ctx, db, projectID, "Widget")
	props := schema["properties"].(map[string]any)
	assert.True(t, props["email"] != nil)
	assert.Equal(t, "string", props["name"].(map[string]any)["type"])
}
