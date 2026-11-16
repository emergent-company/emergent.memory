package graph

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmbeddingTextUnchanged_TitleIsNameColumn pins the enqueue guard (#824):
// the embedding text is built from the `name` column, so a title-only write must
// not reuse the previous vector. Before the column was threaded through the
// hash, a rename produced identical text and the object kept the old embedding.
func TestEmbeddingTextUnchanged_TitleIsNameColumn(t *testing.T) {
	alice := "Alice"
	aliceB := "Alice B"
	key := "person/alice"

	base := &GraphObject{
		Type:       "Person",
		Name:       &alice,
		Key:        &key,
		Properties: map[string]any{"description": "Engineer"},
	}
	require.True(t, embeddingTextUnchanged(base, base), "identical state must hash equal")

	renamed := &GraphObject{
		Type:       "Person",
		Name:       &aliceB,
		Key:        &key,
		Properties: map[string]any{"description": "Engineer"},
	}
	require.False(t, embeddingTextUnchanged(base, renamed), "a name-only change must re-embed")

	unnamed := &GraphObject{
		Type:       "Person",
		Key:        &key,
		Properties: map[string]any{"description": "Engineer"},
	}
	require.False(t, embeddingTextUnchanged(base, unnamed), "losing the title must re-embed")

	reworded := &GraphObject{
		Type:       "Person",
		Name:       &alice,
		Key:        &key,
		Properties: map[string]any{"description": "Engineer II"},
	}
	require.False(t, embeddingTextUnchanged(base, reworded), "a prose change must still re-embed")

	// properties.name is prose now: writing it is a content change, but it is
	// not the title, so it must not stand in for the column.
	proseOnly := &GraphObject{
		Type:       "Person",
		Key:        &key,
		Properties: map[string]any{"description": "Engineer", "name": "Alice"},
	}
	require.False(t, embeddingTextUnchanged(unnamed, proseOnly),
		"a properties.name write is prose and must not be read as the title")
}
