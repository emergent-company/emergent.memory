package blueprints

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogDetailMovie(t *testing.T) {
	cat := loadCatalog(t)
	d, ok := CatalogDetail(cat, "Movie")
	require.True(t, ok)
	require.Equal(t, "Movie", d.Type.Name)
	require.Equal(t, []string{"CreativeWork", "Thing"}, d.Ancestors)
	require.Empty(t, d.Children, "Movie should have no subtypes")
	require.True(t, hasPair(d.RelatedRelationships, "directed_by", "Movie", "Person"))
	require.True(t, hasPair(d.RelatedRelationships, "featured_actor", "Movie", "Person"))
}

func TestCatalogDetailCreativeWorkChildren(t *testing.T) {
	cat := loadCatalog(t)
	d, ok := CatalogDetail(cat, "CreativeWork")
	require.True(t, ok)
	require.Equal(t, []string{"Thing"}, d.Ancestors)
	for _, want := range []string{"Movie", "Article", "Book", "TVSeries", "VideoObject", "SoftwareApplication"} {
		require.Contains(t, d.Children, want, "CreativeWork must list subtype %s", want)
	}
	require.True(t, hasPair(d.RelatedRelationships, "authored_by", "CreativeWork", "Person"))
}

func TestCatalogDetailRoot(t *testing.T) {
	cat := loadCatalog(t)
	d, ok := CatalogDetail(cat, "Thing")
	require.True(t, ok)
	require.Empty(t, d.Ancestors)
	for _, want := range []string{"Person", "Organization", "Event", "CreativeWork", "Product"} {
		require.Contains(t, d.Children, want)
	}
}

func TestCatalogDetailUnknown(t *testing.T) {
	cat := loadCatalog(t)
	_, ok := CatalogDetail(cat, "NotAType")
	require.False(t, ok)
	_, ok = CatalogDetail(nil, "Movie")
	require.False(t, ok)
}

func TestCatalogDetailCycleGuard(t *testing.T) {
	cat := &TaxonomyCatalog{Types: []ObjectTypeDef{
		{Name: "A", Parent: "B"},
		{Name: "B", Parent: "A"},
	}}
	d, ok := CatalogDetail(cat, "A")
	require.True(t, ok)
	require.Equal(t, []string{"B"}, d.Ancestors, "cycle must terminate after B")
}
