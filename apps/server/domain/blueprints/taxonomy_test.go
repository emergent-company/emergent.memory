package blueprints

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListBuiltinObjectTypesTvMovies pins that the tv-movies taxonomy is bundled
// and carries the well-described, tagged types auto-discovery aligns against.
func TestListBuiltinObjectTypesTvMovies(t *testing.T) {
	types, err := ListBuiltinObjectTypes()
	require.NoError(t, err)
	require.NotEmpty(t, types)

	byName := map[string]ObjectTypeDef{}
	for _, d := range types {
		byName[d.Name] = d
	}
	for _, want := range []string{
		"Show", "Season", "Episode", "Character", "Actor",
		"Person", "Location", "Organization", "Occupation",
	} {
		require.Contains(t, byName, want, "builtin taxonomy must include %s", want)
	}

	ch := byName["Character"]
	require.NotEmpty(t, ch.Description, "Character must be well-described")
	require.Contains(t, ch.Labels, "fictional_character", "Character must carry the disambiguation tag")

	person := byName["Person"]
	require.Contains(t, person.Labels, "person")

	// Platform-internal packs define object types too, but they are not a
	// common-entity taxonomy and must never be offered as alignment targets.
	for _, notWant := range []string{
		"Note", "NoteCluster", "Task", "Session", "Message", "Dream",
	} {
		require.NotContains(t, byName, notWant, "builtin taxonomy must NOT include %s", notWant)
	}
}
