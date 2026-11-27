package blueprints

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// TestListBuiltinObjectTypesCatalogOnly pins that discovery's taxonomy is the
// built-in catalog only: the tv-movies blueprint is retired, so pack-specific
// types are gone and the catalog owns every common entity (Place, Person,
// Organization, TVSeries, ...). Platform-internal packs never leak in.
func TestListBuiltinObjectTypesCatalogOnly(t *testing.T) {
	types, err := ListBuiltinObjectTypes()
	require.NoError(t, err)
	require.NotEmpty(t, types)

	byName := map[string]ObjectTypeDef{}
	for _, d := range types {
		byName[d.Name] = d
	}
	// Canonical catalog concepts are present.
	for _, want := range []string{
		"Thing", "Person", "Organization", "Place", "Event", "CreativeWork", "TVSeries", "Movie", "Episode",
	} {
		require.Contains(t, byName, want, "catalog taxonomy must include %s", want)
	}
	// The retired tv-movies pack contributed these; they must no longer appear.
	for _, gone := range []string{"Show", "Season", "Character", "Actor", "Occupation", "Location"} {
		require.NotContains(t, byName, gone, "retired tv-movies type %s must not be in the taxonomy", gone)
	}
	// Platform-internal packs define object types too, but they are not a
	// common-entity taxonomy and must never be offered as alignment targets.
	for _, notWant := range []string{
		"Note", "NoteCluster", "Task", "Session", "Message", "Dream",
	} {
		require.NotContains(t, byName, notWant, "builtin taxonomy must NOT include %s", notWant)
	}

	person := byName["Person"]
	require.Contains(t, person.Labels, "person")
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

// TestListBuiltinObjectTypesSchemaOrg pins the schema.org base taxonomy: it is
// bundled, carries the browse category and Tabler icons the UI renders, and its
// shared types win over a domain pack's same-named type (dedupe, first pack in
// lexical order wins).
func TestListBuiltinObjectTypesSchemaOrg(t *testing.T) {
	types, err := ListBuiltinObjectTypes()
	require.NoError(t, err)

	byName := map[string]ObjectTypeDef{}
	seen := map[string]bool{}
	for _, d := range types {
		require.False(t, seen[d.Name], "duplicate taxonomy type %q leaked through dedupe", d.Name)
		seen[d.Name] = true
		byName[d.Name] = d
	}

	for _, want := range []string{
		"Thing", "Person", "Organization", "Place", "Event", "CreativeWork",
		"Article", "Book", "Movie", "TVSeries", "VideoObject", "ImageObject",
		"WebPage", "WebSite",
		"Product", "Offer", "Brand", "Review", "AggregateRating", "Recipe",
		"LocalBusiness", "SoftwareApplication", "SoftwareSourceCode",
		"JobPosting", "Course", "HowTo", "MedicalEntity", "Drug",
	} {
		require.Contains(t, byName, want, "schemaorg taxonomy must include %s", want)
	}

	movie := byName["Movie"]
	require.Equal(t, "media", movie.Category, "Movie must carry its browse category")
	require.Equal(t, "CreativeWork", movie.Parent, "Movie must carry its schema.org supertype")
	require.Equal(t, "tabler--movie", movie.UI["icon"], "Movie must use the Tabler icon family")
	require.Contains(t, movie.Labels, "film", "Movie must carry resolution aliases")
	require.NotEmpty(t, movie.Description, "Movie must be well-described for alignment")

	product := byName["Product"]
	require.Equal(t, "commerce", product.Category)
	require.Equal(t, "tabler--package", product.UI["icon"])

	// Person carries the catalog's identity category.
	require.Equal(t, "identity", byName["Person"].Category)
}

// TestListBuiltinObjectTypesDedupesByName pins that two taxonomy packs defining
// the same type keep the lexically first pack's definition.
func TestListBuiltinObjectTypesDedupesByName(t *testing.T) {
	fsys := fstest.MapFS{
		"builtin_packs/aaa/schemas/aaa.yaml": &fstest.MapFile{Data: []byte(`
name: aaa
version: 1.0.0
taxonomy: true
objectTypeSchemas:
  - name: Person
    description: canonical person
    category: identity
`)},
		"builtin_packs/zzz/schemas/zzz.yaml": &fstest.MapFile{Data: []byte(`
name: zzz
version: 1.0.0
taxonomy: true
objectTypeSchemas:
  - name: Person
    description: domain person
    category: domain
`)},
	}

	types, err := listBuiltinObjectTypes(fsys, "builtin_packs")
	require.NoError(t, err)
	require.Len(t, types, 1)
	require.Equal(t, "canonical person", types[0].Description)
	require.Equal(t, "identity", types[0].Category)
}

// TestSchemaOrgIsNotABuiltinBlueprintPack pins the catalog separation: the
// schema.org vocabulary is shipped as reference data under domain/catalog, not
// as a builtin_packs blueprint. A directory reappearing here would be seeded
// into the blueprint gallery by BuiltinSeeder.
func TestSchemaOrgIsNotABuiltinBlueprintPack(t *testing.T) {
	entries, err := fs.ReadDir(builtinPacksFS, "builtin_packs")
	require.NoError(t, err)
	for _, e := range entries {
		require.NotEqual(t, "schemaorg", e.Name(),
			"schema.org must live in domain/catalog data, not builtin_packs")
	}
}
