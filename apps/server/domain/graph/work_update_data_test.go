package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkUpdateData_PathsShapes verifies the `updated` event payload accepts
// both the in-process []string shape and the JSONB round-trip []any shape for
// ChangeSummary["paths"], so a reloaded object does not silently drop the
// property filter.
func TestWorkUpdateData_PathsShapes(t *testing.T) {
	t.Run("[]string paths", func(t *testing.T) {
		obj := &GraphObjectResponse{
			Status:        strPtr("todo"),
			Assignee:      strPtr("researcher"),
			ChangeSummary: map[string]any{"paths": []string{"/title", "/body"}},
		}
		got := workUpdateData(obj)
		require.Equal(t, []string{"/title", "/body"}, got["properties"])
		require.Equal(t, "todo", got["status"])
		require.Equal(t, "researcher", got["assignee"])
	})

	t.Run("[]any paths from JSONB round-trip", func(t *testing.T) {
		obj := &GraphObjectResponse{
			ChangeSummary: map[string]any{"paths": []any{"/title", "/body"}},
		}
		got := workUpdateData(obj)
		require.Equal(t, []string{"/title", "/body"}, got["properties"])
	})

	t.Run("empty paths omit properties", func(t *testing.T) {
		obj := &GraphObjectResponse{ChangeSummary: map[string]any{"paths": []string{}}}
		got := workUpdateData(obj)
		_, ok := got["properties"]
		assert.False(t, ok)
	})

	t.Run("unrecognised shape omits properties", func(t *testing.T) {
		obj := &GraphObjectResponse{ChangeSummary: map[string]any{"paths": 42}}
		got := workUpdateData(obj)
		_, ok := got["properties"]
		assert.False(t, ok)
	})
}
