package blueprints

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStringMapArgRefusesWhatRESTRefuses pins the MCP/REST parity of the
// modelOverrides argument (#18): REST binds map[string]string and answers 400,
// so the MCP handler must not quietly drop a non-string value (or a malformed
// JSON string) and preview with no overrides applied.
func TestStringMapArgRefusesWhatRESTRefuses(t *testing.T) {
	t.Run("nil and empty mean no overrides", func(t *testing.T) {
		for _, in := range []any{nil, "", "   "} {
			got, err := stringMapArg(in)
			require.NoError(t, err)
			require.Nil(t, got)
		}
	})

	t.Run("object of strings is accepted", func(t *testing.T) {
		got, err := stringMapArg(map[string]any{"embedding": "text-embedding-3-small"})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"embedding": "text-embedding-3-small"}, got)
	})

	t.Run("JSON-encoded object of strings is accepted", func(t *testing.T) {
		got, err := stringMapArg(`{"embedding":"text-embedding-3-small"}`)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"embedding": "text-embedding-3-small"}, got)
	})

	t.Run("non-string value is refused, naming the key", func(t *testing.T) {
		_, err := stringMapArg(map[string]any{"embedding": 1})
		require.Error(t, err)
		require.Contains(t, err.Error(), `"embedding"`)
	})

	t.Run("malformed JSON string is refused", func(t *testing.T) {
		_, err := stringMapArg(`{"embedding":`)
		require.Error(t, err)
	})

	t.Run("non-object shape is refused", func(t *testing.T) {
		_, err := stringMapArg([]any{"embedding"})
		require.Error(t, err)
	})
}
