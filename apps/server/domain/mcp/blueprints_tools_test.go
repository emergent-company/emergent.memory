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
