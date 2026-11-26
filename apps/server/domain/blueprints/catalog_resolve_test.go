package blueprints

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func loadCatalog(t *testing.T) *TaxonomyCatalog {
	t.Helper()
	cat, err := ListBuiltinTaxonomy()
	require.NoError(t, err)
	require.NotEmpty(t, cat.Types)
	return cat
}

func TestResolveCatalogTypesExactName(t *testing.T) {
	cat := loadCatalog(t)
	got := ResolveCatalogTypes(cat, "Movie", 5)
	require.NotEmpty(t, got)
	require.Equal(t, "Movie", got[0].Name)
	require.Equal(t, 1.0, got[0].Score)
	require.Contains(t, got[0].Matched, "name")
}

func TestResolveCatalogTypesAlias(t *testing.T) {
	cat := loadCatalog(t)
	got := ResolveCatalogTypes(cat, "film", 5)
	require.NotEmpty(t, got)
	require.Equal(t, "Movie", got[0].Name, "the film alias must resolve to Movie")
	require.Contains(t, got[0].Matched, "alias")
}

func TestResolveCatalogTypesMultiWord(t *testing.T) {
	cat := loadCatalog(t)
	got := ResolveCatalogTypes(cat, "tv series", 5)
	require.NotEmpty(t, got)
	require.Equal(t, "TVSeries", got[0].Name)
}

func TestResolveCatalogTypesParentAndCategory(t *testing.T) {
	cat := loadCatalog(t)

	creative := ResolveCatalogTypes(cat, "creative work", 10)
	require.NotEmpty(t, creative)
	require.Equal(t, "CreativeWork", creative[0].Name)

	commerce := ResolveCatalogTypes(cat, "commerce", 50)
	require.NotEmpty(t, commerce)
	found := false
	for _, m := range commerce {
		if m.Name == "Product" {
			found = true
			require.Equal(t, "commerce", m.Category)
			break
		}
	}
	require.True(t, found, "commerce query must surface Product")
}

func TestResolveCatalogTypesUnknownAndEmpty(t *testing.T) {
	cat := loadCatalog(t)
	require.Empty(t, ResolveCatalogTypes(cat, "zzzznotatype", 5))
	require.Empty(t, ResolveCatalogTypes(cat, "   ", 5))
	require.Empty(t, ResolveCatalogTypes(nil, "Movie", 5))
}

func TestResolveCatalogTypesLimitAndOrder(t *testing.T) {
	cat := loadCatalog(t)
	got := ResolveCatalogTypes(cat, "commerce", 3)
	require.Len(t, got, 3)
	for i := 1; i < len(got); i++ {
		if got[i-1].Score == got[i].Score {
			require.LessOrEqual(t, got[i-1].Name, got[i].Name, "ties must order by name")
		}
	}
	require.NotEmpty(t, ResolveCatalogTypes(cat, "commerce", 0))
	require.LessOrEqual(t, len(ResolveCatalogTypes(cat, "commerce", 0)), defaultResolveLimit)
}
