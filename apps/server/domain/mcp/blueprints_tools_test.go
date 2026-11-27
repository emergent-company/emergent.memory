package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBlueprintToolDefinitions_MergeSupport guards the advertised MCP surface:
// blueprint-apply accepts mergeTypes and blueprint-apply-preview exists with a
// read scope.
func TestBlueprintToolDefinitions_MergeSupport(t *testing.T) {
	var applyDef, previewDef *ToolDefinition
	defs := blueprintsToolDefinitions()
	for i := range defs {
		switch defs[i].Name {
		case "blueprint-apply":
			applyDef = &defs[i]
		case "blueprint-apply-preview":
			previewDef = &defs[i]
		}
	}

	require.NotNil(t, applyDef, "blueprint-apply definition missing")
	require.NotNil(t, previewDef, "blueprint-apply-preview definition missing")

	require.Contains(t, applyDef.InputSchema.Properties, "mergeTypes")
	assert.Equal(t, "boolean", applyDef.InputSchema.Properties["mergeTypes"].Type)
	assert.Equal(t, "schema:write", applyDef.RequiredScope)

	assert.Equal(t, "schema:read", previewDef.RequiredScope)
	require.Contains(t, previewDef.InputSchema.Properties, "id")
}

// TestCatalogInstallToolDefinition_ExtendSupport pins the argument the catalog
// install refuses to work without on a cross-pack collision: a type another pack
// provides is only merged when the caller names it in `extend`, so the tool must
// advertise the field or the MCP caller has no way to accept the merge.
func TestCatalogInstallToolDefinition_ExtendSupport(t *testing.T) {
	defs := blueprintsToolDefinitions()
	var installDef *ToolDefinition
	for i := range defs {
		if defs[i].Name == "blueprint-catalog-install" {
			installDef = &defs[i]
			break
		}
	}
	require.NotNil(t, installDef, "blueprint-catalog-install definition missing")

	extend, ok := installDef.InputSchema.Properties["extend"]
	require.True(t, ok, "blueprint-catalog-install must advertise extend")
	assert.Equal(t, "array", extend.Type)
	require.NotNil(t, extend.Items)
	assert.Equal(t, "string", extend.Items.Type)
	require.Contains(t, installDef.InputSchema.Properties, "replace")
	assert.Equal(t, "schema:write", installDef.RequiredScope)
}
