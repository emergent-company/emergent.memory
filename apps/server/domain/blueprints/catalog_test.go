package blueprints

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/schemas"
)

func hasPair(rels []RelationshipTypeDef, name, source, target string) bool {
	for _, r := range rels {
		if r.Name == name && r.SourceType == source && r.TargetType == target {
			return true
		}
	}
	return false
}

func hasType(types []ObjectTypeDef, name string) bool {
	for _, t := range types {
		if t.Name == name {
			return true
		}
	}
	return false
}

// TestListBuiltinTaxonomyIncludesRelationships pins that the embedded catalog
// exposes the schema.org candidate relationships alongside its types.
func TestListBuiltinTaxonomyIncludesRelationships(t *testing.T) {
	cat, err := ListBuiltinTaxonomy()
	require.NoError(t, err)
	require.NotEmpty(t, cat.Types)
	require.NotEmpty(t, cat.Relationships)

	require.True(t, hasType(cat.Types, "Movie"))
	require.True(t, hasPair(cat.Relationships, "directed_by", "Movie", "Person"))
	require.True(t, hasPair(cat.Relationships, "works_for", "Person", "Organization"))

	// No duplicate (name, source, target) pairs survive the loader.
	seen := map[string]bool{}
	for _, r := range cat.Relationships {
		key := relationshipKey(r)
		require.False(t, seen[key], "duplicate relationship pair %q", key)
		seen[key] = true
	}
}

// TestProjectCatalogGatesByEndpoints pins endpoint gating: a relationship is
// active only when BOTH of its endpoint types are requested; otherwise it is
// deferred, and unrelated types are not materialized.
func TestProjectCatalogGatesByEndpoints(t *testing.T) {
	cat, err := ListBuiltinTaxonomy()
	require.NoError(t, err)

	onlyMovie := ProjectCatalog(cat, []string{"Movie"})
	require.True(t, hasType(onlyMovie.Types, "Movie"))
	require.False(t, hasType(onlyMovie.Types, "Person"), "Person must not be installed")
	require.False(t, hasType(onlyMovie.Types, "CreativeWork"), "parent type must not be auto-installed")
	require.Empty(t, onlyMovie.Relationships, "no relationship has both endpoints installed")
	require.True(t, hasPair(onlyMovie.Deferred, "directed_by", "Movie", "Person"))

	withPerson := ProjectCatalog(cat, []string{"Movie", "Person"})
	require.True(t, hasPair(withPerson.Relationships, "directed_by", "Movie", "Person"))
	require.True(t, hasPair(withPerson.Relationships, "featured_actor", "Movie", "Person"))
	require.False(t, hasPair(withPerson.Deferred, "directed_by", "Movie", "Person"),
		"an activated edge must leave the deferred set")
	require.True(t, hasPair(withPerson.Deferred, "produced_by", "Movie", "Organization"),
		"an edge to an uninstalled type stays deferred")
}

// TestProjectCatalogReconcileActivatesDeferred pins the reconcile path: adding
// the missing endpoint to the request activates the previously deferred edge.
func TestProjectCatalogReconcileActivatesDeferred(t *testing.T) {
	cat, err := ListBuiltinTaxonomy()
	require.NoError(t, err)

	first := ProjectCatalog(cat, []string{"Movie"})
	require.True(t, hasPair(first.Deferred, "directed_by", "Movie", "Person"))
	require.False(t, hasPair(first.Relationships, "directed_by", "Movie", "Person"))

	// Reconcile = re-project with the union of installed + newly wanted types.
	reconciled := ProjectCatalog(cat, []string{"Movie", "Person"})
	require.True(t, hasPair(reconciled.Relationships, "directed_by", "Movie", "Person"))
	require.False(t, hasPair(reconciled.Deferred, "directed_by", "Movie", "Person"))
}

// TestProjectCatalogReportsUnknownAndProblems pins the diagnostics: unknown
// requested names and declared edges with a missing endpoint are surfaced, not
// silently dropped.
func TestProjectCatalogReportsUnknownAndProblems(t *testing.T) {
	cat := &TaxonomyCatalog{
		Types: []ObjectTypeDef{{Name: "A"}, {Name: "B"}},
		Relationships: []RelationshipTypeDef{
			{Name: "ok", SourceType: "A", TargetType: "B"},
			{Name: "dangling", SourceType: "A", TargetType: "Ghost"},
		},
	}
	proj := ProjectCatalog(cat, []string{"A", "Nope"})
	require.Equal(t, []string{"Nope"}, proj.Unknown)
	require.Len(t, proj.Problems, 1)
	require.Contains(t, proj.Problems[0], "Ghost")
	require.False(t, hasPair(proj.Relationships, "dangling", "A", "Ghost"))
}

// TestProjectionPackExcludesDeferred pins that the materialized pack carries
// only active relationships; deferred edges await a later reconcile.
func TestProjectionPackExcludesDeferred(t *testing.T) {
	cat, err := ListBuiltinTaxonomy()
	require.NoError(t, err)

	proj := ProjectCatalog(cat, []string{"Movie", "Person"})
	pack := proj.Pack(CatalogPackOptions{Name: "schemaorg-catalog", Version: "1.0.0"})
	require.Equal(t, "schemaorg-catalog", pack.Name)
	require.True(t, hasPair(pack.RelationshipTypes, "directed_by", "Movie", "Person"))
	require.False(t, hasPair(pack.RelationshipTypes, "produced_by", "Movie", "Organization"))
	require.True(t, hasType(pack.ObjectTypes, "Movie"))
	require.False(t, hasType(pack.ObjectTypes, "Organization"))
}

// TestListBuiltinTaxonomyExpandsPluralRelationships pins that a plural
// declaration in a taxonomy pack expands into singular, deduplicated pairs.
func TestListBuiltinTaxonomyExpandsPluralRelationships(t *testing.T) {
	fsys := fstest.MapFS{
		"builtin_packs/tax/schemas/tax.yaml": &fstest.MapFile{Data: []byte(`
name: tax
version: 1.0.0
taxonomy: true
objectTypeSchemas:
  - name: A
  - name: B
relationshipTypeSchemas:
  - name: links
    sourceTypes: [A, B]
    targetTypes: [B]
`)},
	}
	cat, err := listBuiltinTaxonomy(fsys, "builtin_packs")
	require.NoError(t, err)
	require.Len(t, cat.Relationships, 2)
	require.True(t, hasPair(cat.Relationships, "links", "A", "B"))
	require.True(t, hasPair(cat.Relationships, "links", "B", "B"))
}

// TestCatalogInstalledTypes pins the reconcile input: only object types
// contributed by the named catalog pack, deduplicated.
func TestCatalogInstalledTypes(t *testing.T) {
	compiled := &schemas.CompiledTypesResponse{
		ObjectTypes: []schemas.ObjectTypeSchema{
			{Name: "Movie", SchemaName: "schemaorg-catalog"},
			{Name: "Person", SchemaName: "schemaorg-catalog"},
			{Name: "Task", SchemaName: "task-board"},
			{Name: "Movie", SchemaName: "other-pack"},
		},
	}
	require.Equal(t, []string{"Movie", "Person"}, catalogInstalledTypes(compiled, "schemaorg-catalog"))
	require.Nil(t, catalogInstalledTypes(nil, "schemaorg-catalog"))
	require.Nil(t, catalogInstalledTypes(compiled, ""))
}

// TestDescribeCatalogInstall pins the human-readable install report.
func TestDescribeCatalogInstall(t *testing.T) {
	proj := CatalogProjection{
		Types:         []ObjectTypeDef{{Name: "Movie"}},
		Relationships: []RelationshipTypeDef{{Name: "directed_by", SourceType: "Movie", TargetType: "Person"}},
		Deferred:      []RelationshipTypeDef{{Name: "produced_by", SourceType: "Movie", TargetType: "Organization"}},
		Unknown:       []string{"Nope"},
		Problems:      []string{"bad edge"},
	}
	res := describeCatalogInstall("pack-1", true, proj, []string{"OldType"})
	require.Equal(t, "pack-1", res.PackID)
	require.True(t, res.Created)
	require.Equal(t, []string{"Movie"}, res.Types)
	require.Equal(t, []string{"directed_by(Movie->Person)"}, res.Relationships)
	require.Equal(t, []string{"produced_by(Movie->Organization)"}, res.Deferred)
	require.Equal(t, []string{"Nope"}, res.Unknown)
	require.Equal(t, []string{"bad edge"}, res.Problems)
	require.Equal(t, []string{"OldType"}, res.Removed)
}

// TestPlanCatalogUnion pins additive vs replace reconcile semantics.
func TestPlanCatalogUnion(t *testing.T) {
	cases := []struct {
		name      string
		installed []string
		requested []string
		replace   bool
		wantUnion []string
		wantDrop  []string
	}{
		{"additive adds", []string{"Movie"}, []string{"Person"}, false, []string{"Movie", "Person"}, nil},
		{"additive dedupes", []string{"Movie"}, []string{"Movie", "Person"}, false, []string{"Movie", "Person"}, nil},
		{"replace drops omitted", []string{"Movie", "Person"}, []string{"Movie"}, true, []string{"Movie"}, []string{"Person"}},
		{"replace dedupes request", []string{"Movie", "Person"}, []string{"Movie", "Movie"}, true, []string{"Movie"}, []string{"Person"}},
		{"replace empty request drops all", []string{"Movie", "Person"}, nil, true, []string{}, []string{"Movie", "Person"}},
		{"fresh additive", nil, []string{"Movie"}, false, []string{"Movie"}, nil},
	}
	for _, c := range cases {
		union, removed := planCatalogUnion(c.installed, c.requested, c.replace)
		require.Equal(t, c.wantUnion, union, "%s: union", c.name)
		require.Equal(t, c.wantDrop, removed, "%s: removed", c.name)
	}
}

// TestCatalogPackConflict pins the guard that prevents catalog install from
// rewriting an unrelated pack whose name collides with the catalog pack name.
func TestCatalogPackConflict(t *testing.T) {
	catalogTypes := map[string]bool{"Movie": true, "Person": true}
	_, ok := catalogPackConflict(catalogTypes, []string{"Movie", "Person"})
	require.False(t, ok)

	conflict, ok := catalogPackConflict(catalogTypes, []string{"Movie", "Task"})
	require.True(t, ok)
	require.Equal(t, "Task", conflict)
}
