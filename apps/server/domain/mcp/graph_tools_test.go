package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toolDefByName(t *testing.T, tools []ToolDefinition, name string) ToolDefinition {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in GetToolDefinitions()", name)
	return ToolDefinition{}
}

// =============================================================================
// min_score schema exposure
// =============================================================================

func TestGetToolDefinitions_MinScoreSchema(t *testing.T) {
	svc := &Service{}
	tools := svc.GetToolDefinitions()

	searchTools := []string{"search-hybrid", "search-semantic"}
	for _, name := range searchTools {
		t.Run(name, func(t *testing.T) {
			tool := toolDefByName(t, tools, name)
			prop, ok := tool.InputSchema.Properties["min_score"]
			require.True(t, ok, "tool %q must expose a min_score property", name)
			assert.Equal(t, "number", prop.Type)
			assert.NotEmpty(t, prop.Description)
		})
	}
}

// =============================================================================
// parseMinScoreArg
// =============================================================================

func TestParseMinScoreArg(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		want    *float32
		wantErr bool
	}{
		{
			name: "absent argument returns nil",
			args: map[string]any{"query": "foo"},
			want: nil,
		},
		{
			name: "in-range value parsed",
			args: map[string]any{"min_score": 0.4},
			want: float32Ptr(0.4),
		},
		{
			name:    "above range rejected",
			args:    map[string]any{"min_score": 1.5},
			wantErr: true,
		},
		{
			name:    "below range rejected",
			args:    map[string]any{"min_score": -0.1},
			wantErr: true,
		},
		{
			name: "zero allowed",
			args: map[string]any{"min_score": 0.0},
			want: float32Ptr(0),
		},
		{
			name: "one allowed",
			args: map[string]any{"min_score": 1.0},
			want: float32Ptr(1),
		},
		{
			name:    "present non-number string rejected",
			args:    map[string]any{"min_score": "0.4"},
			wantErr: true,
		},
		{
			name:    "present non-number bool rejected",
			args:    map[string]any{"min_score": true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMinScoreArg(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tt.want, *got)
		})
	}
}

// =============================================================================
// include_relationships plumbing
// =============================================================================

func TestToolDefinitions_IncludeRelationshipsSchema(t *testing.T) {
	svc := &Service{}
	tools := svc.GetToolDefinitions()

	t.Run("entity-type-list exposes include_relationships", func(t *testing.T) {
		tool := toolDefByName(t, tools, "entity-type-list")
		prop, ok := tool.InputSchema.Properties["include_relationships"]
		require.True(t, ok, "entity-type-list must expose include_relationships")
		assert.Equal(t, "boolean", prop.Type)
		assert.NotEmpty(t, prop.Description)
	})

	t.Run("search-hybrid does not expose include_relationships", func(t *testing.T) {
		tool := toolDefByName(t, tools, "search-hybrid")
		_, ok := tool.InputSchema.Properties["include_relationships"]
		assert.False(t, ok, "search-hybrid must not expose include_relationships (the relationship leg is always skipped for MCP)")
	})
}

func TestBuildHybridUnifiedRequest_IncludeRelationships(t *testing.T) {
	req := buildHybridUnifiedRequest(map[string]any{}, "q", 20, nil, nil, nil, "")
	require.NotNil(t, req.IncludeRelationships)
	assert.False(t, *req.IncludeRelationships, "MCP search-hybrid must always skip the relationship leg")
}
