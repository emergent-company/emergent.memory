package main

import (
	"strings"
	"testing"
)

func TestRenderSchemaCatalogPage(t *testing.T) {
	list := &CatalogList{
		Version: "1.0.0",
		Total:   2,
		Categories: []CatalogCategoryCount{
			{Key: "media", Count: 1},
			{Key: "identity", Count: 1},
		},
		Types: []CatalogSummary{
			{Name: "Movie", Label: "Movie", Description: "A film.", Category: "media", Parent: "CreativeWork", Labels: []string{"film"}, UI: &CatalogUI{Icon: "tabler--movie", Color: "#DB2777"}},
			{Name: "Person", Label: "Person", Category: "identity", UI: &CatalogUI{Icon: "tabler--user"}},
		},
	}
	view := &SchemaCatalogView{
		Catalog:   list,
		Installed: map[string]bool{"Person": true},
		Groups:    groupCatalogTypes(list),
	}
	html := renderHTML(t, SchemaCatalogPage(view, nil))
	for _, want := range []string{
		`data-testid="catalog-search-form"`,
		`data-testid="catalog-category-filter"`,
		`category=media`,
		`data-catalog-type="Movie"`,
		`href="/schema/catalog/Movie"`,
		`action="/schema/catalog/install"`,
		`name="type" value="Movie"`,
		"Installed",
		"tabler--movie",
		"film",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("catalog page missing %q", want)
		}
	}
	// Person is installed: no install form for it.
	if strings.Contains(html, `name="type" value="Person"`) {
		t.Error("installed type must not render an install form")
	}
}

func TestRenderSchemaCatalogPageEmpty(t *testing.T) {
	html := renderHTML(t, SchemaCatalogPage(&SchemaCatalogView{Catalog: &CatalogList{}}, nil))
	if !strings.Contains(html, "No catalog types match") {
		t.Error("empty catalog state missing")
	}
}

func TestRenderSchemaCatalogTypePage(t *testing.T) {
	detail := &CatalogTypeDetail{
		Type: CatalogType{
			Name:        "Movie",
			Label:       "Movie",
			Description: "A feature film.",
			Category:    "media",
			Parent:      "CreativeWork",
			Labels:      []string{"film"},
			UI:          &CatalogUI{Icon: "tabler--movie", Color: "#DB2777"},
			Properties: map[string]CatalogProperty{
				"duration": {Type: "integer", Description: "Runtime in minutes."},
			},
		},
		Ancestors: []string{"CreativeWork", "Thing"},
		Children:  []string{"Documentary"},
		RelatedRelationships: []CatalogRelationship{
			{Name: "directed_by", SourceType: "Movie", TargetType: "Person", Description: "Directed by a person."},
		},
	}
	html := renderHTML(t, SchemaCatalogTypePage(&SchemaCatalogTypeView{Detail: detail}, nil))
	for _, want := range []string{
		`data-testid="catalog-type-detail"`,
		"Movie",
		"CreativeWork",
		"Documentary",
		"duration",
		"directed_by",
		"Movie → Person",
		`action="/schema/catalog/install"`,
		"Install into project",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("catalog type page missing %q", want)
		}
	}
}

func TestGroupCatalogTypesOrder(t *testing.T) {
	list := &CatalogList{
		Categories: []CatalogCategoryCount{{Key: "media", Count: 1}, {Key: "identity", Count: 1}},
		Types: []CatalogSummary{
			{Name: "Movie", Category: "media"},
			{Name: "Person", Category: "identity"},
			{Name: "Mystery"},
		},
	}
	groups := groupCatalogTypes(list)
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if groups[0].Key != "media" || groups[1].Key != "identity" || groups[2].Key != "other" {
		t.Fatalf("group order = %v", []string{groups[0].Key, groups[1].Key, groups[2].Key})
	}
}

func TestCatalogLinks(t *testing.T) {
	if got := catalogTypeHref("Movie"); got != "/schema/catalog/Movie" {
		t.Errorf("catalogTypeHref = %q", got)
	}
	if got := catalogCategoryHref("media", "film"); !strings.Contains(got, "category=media") || !strings.Contains(got, "q=film") {
		t.Errorf("catalogCategoryHref = %q", got)
	}
}
