package blueprints

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/catalog"
	"github.com/emergent-company/emergent.memory/domain/schemas"
)

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

func TestDescribeCatalogInstall(t *testing.T) {
	proj := catalog.Projection{
		Types:         []catalog.Type{{Name: "Movie"}},
		Relationships: []catalog.Relationship{{Name: "directed_by", SourceType: "Movie", TargetType: "Person"}},
		Deferred:      []catalog.Relationship{{Name: "produced_by", SourceType: "Movie", TargetType: "Organization"}},
		Unknown:       []string{"Nope"},
		Problems:      []string{"bad edge"},
	}
	res := describeCatalogInstall("pack-1", true, proj, []string{"OldType"})
	require.Equal(t, "pack-1", res.PackID)
	require.True(t, res.Created)
	require.Equal(t, []string{"Movie"}, res.Types)
	require.Equal(t, []string{"directed_by(Movie->Person)"}, res.Relationships)
	require.Equal(t, []string{"produced_by(Movie->Organization)"}, res.Deferred)
	require.Equal(t, []string{"OldType"}, res.Removed)
	require.Equal(t, []string{"Nope"}, res.Unknown)
	require.Equal(t, []string{"bad edge"}, res.Problems)
}

func TestCatalogPackConflict(t *testing.T) {
	catalogTypes := map[string]bool{"Movie": true, "Person": true}
	_, ok := catalogPackConflict(catalogTypes, []string{"Movie", "Person"})
	require.False(t, ok)

	conflict, ok := catalogPackConflict(catalogTypes, []string{"Movie", "Task"})
	require.True(t, ok)
	require.Equal(t, "Task", conflict)
}

func TestCatalogPackExcludesDeferred(t *testing.T) {
	cat, err := catalog.Load()
	require.NoError(t, err)
	proj := catalog.Project(cat, []string{"Movie", "Person"})
	pack := catalogPack(proj, CatalogPackOptions{Name: "schemaorg-catalog", Version: "1.0.0"})
	require.Equal(t, "schemaorg-catalog", pack.Name)
	require.True(t, hasRelDef(pack.RelationshipTypes, "directed_by", "Movie", "Person"))
	require.False(t, hasRelDef(pack.RelationshipTypes, "produced_by", "Movie", "Organization"))
	require.True(t, hasObjDef(pack.ObjectTypes, "Movie"))
	require.False(t, hasObjDef(pack.ObjectTypes, "Organization"))
}

func hasObjDef(defs []ObjectTypeDef, name string) bool {
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}

func hasRelDef(defs []RelationshipTypeDef, name, source, target string) bool {
	for _, d := range defs {
		if d.Name == name && d.SourceType == source && d.TargetType == target {
			return true
		}
	}
	return false
}
