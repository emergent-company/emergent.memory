package blueprints

import (
	"testing"
	"testing/fstest"

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

// TestListBuiltinObjectTypesSkipsMalformedPack pins that one unloadable pack is
// skipped (with a log) instead of failing the whole taxonomy load: otherwise a
// single malformed pack would silently disable alignment for every other pack.
func TestListBuiltinObjectTypesSkipsMalformedPack(t *testing.T) {
	fsys := fstest.MapFS{
		"builtin_packs/good/project.yaml": &fstest.MapFile{Data: []byte("description: a good taxonomy pack\n")},
		"builtin_packs/good/schemas/good.yaml": &fstest.MapFile{Data: []byte(`
name: good
version: 1.0.0
taxonomy: true
objectTypeSchemas:
  - name: Widget
    description: a widget
`)},
		// Invalid YAML: must not abort the multi-pack load.
		"builtin_packs/bad/schemas/bad.yaml": &fstest.MapFile{Data: []byte("name: [unterminated\n")},
		// A valid non-taxonomy pack is never offered as an alignment target.
		"builtin_packs/feature/schemas/feature.yaml": &fstest.MapFile{Data: []byte(`
name: feature
version: 1.0.0
objectTypeSchemas:
  - name: Note
`)},
	}

	types, err := listBuiltinObjectTypes(fsys, "builtin_packs")
	require.NoError(t, err)
	require.Len(t, types, 1)
	require.Equal(t, "Widget", types[0].Name)
}
