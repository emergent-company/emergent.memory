package main

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

// CatalogGroup is one category group on the browse page.
type CatalogGroup struct {
	Key   string
	Types []CatalogSummary
}

// SchemaCatalogView is the catalog browse page model.
type SchemaCatalogView struct {
	Catalog   *CatalogList
	Query     string
	Category  string
	Installed map[string]bool
	Groups    []CatalogGroup
	FlashMsg  string
	FlashErr  error
}

// SchemaCatalogTypeView is the catalog type detail page model.
type SchemaCatalogTypeView struct {
	Detail    *CatalogTypeDetail
	Installed bool
	FlashMsg  string
	FlashErr  error
}

// uiSchemaCatalog renders the catalog browser: category facets, a lexical
// filter, and one card per type with an install action for types the project
// does not have yet.
func (s *Server) uiSchemaCatalog(c echo.Context) error {
	ctx := c.Request().Context()
	q := strings.TrimSpace(c.QueryParam("q"))
	category := strings.TrimSpace(c.QueryParam("category"))

	list, err := s.memory.ListCatalog(ctx, q, category, "")
	if err != nil {
		return s.page(c, pageTitle("Catalog"), SchemaCatalogPage(nil, userFacingError(err)))
	}
	installed := map[string]bool{}
	if compiled, cerr := s.memory.GetCompiledTypes(ctx); cerr == nil {
		for _, t := range compiled.ObjectTypes {
			installed[t.Name] = true
		}
	}
	view := &SchemaCatalogView{
		Catalog:   list,
		Query:     q,
		Category:  category,
		Installed: installed,
		Groups:    groupCatalogTypes(list),
	}
	if c.QueryParam("installed") != "" {
		view.FlashMsg = "Catalog types installed."
	}
	view.FlashErr = flashError(c)
	return s.page(c, pageTitle("Catalog"), SchemaCatalogPage(view, nil))
}

// uiSchemaCatalogType renders one catalog type: hierarchy, properties, related
// relationships, and an install action.
func (s *Server) uiSchemaCatalogType(c echo.Context) error {
	ctx := c.Request().Context()
	name := pathUnescape(c.Param("name"))
	detail, err := s.memory.GetCatalogType(ctx, name)
	if err != nil {
		return s.page(c, pageTitle("Catalog type"), SchemaCatalogTypePage(nil, userFacingError(err)))
	}
	installed := false
	if compiled, cerr := s.memory.GetCompiledTypes(ctx); cerr == nil {
		for _, t := range compiled.ObjectTypes {
			if t.Name == detail.Type.Name {
				installed = true
				break
			}
		}
	}
	view := &SchemaCatalogTypeView{Detail: detail, Installed: installed}
	if c.QueryParam("installed") != "" {
		view.FlashMsg = "Type installed."
	}
	view.FlashErr = flashError(c)
	return s.page(c, pageTitle(catalogTypeTitle(detail)), SchemaCatalogTypePage(view, nil))
}

// uiSchemaCatalogInstall installs the selected catalog type(s) and redirects to
// the type page (single) or the list (multiple).
func (s *Server) uiSchemaCatalogInstall(c echo.Context) error {
	ctx := c.Request().Context()
	types := catalogFormTypes(c)
	if len(types) == 0 {
		return redirectWithError(c, "/schema/catalog", fmt.Errorf("select at least one type"))
	}
	res, err := s.memory.InstallCatalog(ctx, types, false)
	if err != nil {
		return redirectWithError(c, "/schema/catalog", err)
	}
	_ = res
	target := "/schema/catalog?installed=1"
	if len(types) == 1 {
		target = "/schema/catalog/" + url.PathEscape(types[0]) + "?installed=1"
	}
	return c.Redirect(http.StatusSeeOther, target)
}

func catalogFormTypes(c echo.Context) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range c.Request().Form["type"] {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// groupCatalogTypes groups types by category, honoring the facet order the
// server returned (alphabetical), with the uncategorised bucket last.
func groupCatalogTypes(list *CatalogList) []CatalogGroup {
	if list == nil {
		return nil
	}
	byCat := map[string][]CatalogSummary{}
	for _, t := range list.Types {
		key := t.Category
		if key == "" {
			key = "other"
		}
		byCat[key] = append(byCat[key], t)
	}
	var keys []string
	for _, c := range list.Categories {
		if _, ok := byCat[c.Key]; ok {
			keys = append(keys, c.Key)
		}
	}
	if _, ok := byCat["other"]; ok {
		keys = append(keys, "other")
	}
	groups := make([]CatalogGroup, 0, len(keys))
	for _, k := range keys {
		groups = append(groups, CatalogGroup{Key: k, Types: byCat[k]})
	}
	return groups
}

func catalogTypeTitle(d *CatalogTypeDetail) string {
	if d == nil {
		return "Catalog type"
	}
	if d.Type.Label != "" {
		return d.Type.Label
	}
	return d.Type.Name
}

// sortedCatalogPropertyKeys returns property keys in stable order for a table.
func sortedCatalogPropertyKeys(props map[string]CatalogProperty) []string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// filterQuery keeps q on category links.
func catalogCategoryHref(category, q string) string {
	vals := url.Values{}
	if category != "" {
		vals.Set("category", category)
	}
	if q != "" {
		vals.Set("q", q)
	}
	if len(vals) == 0 {
		return "/schema/catalog"
	}
	return "/schema/catalog?" + vals.Encode()
}

// catalogTypeHref is the detail page link for one catalog type.
func catalogTypeHref(name string) string {
	return "/schema/catalog/" + url.PathEscape(name)
}

func catalogSummaryLabel(t CatalogSummary) string {
	if t.Label != "" {
		return t.Label
	}
	return t.Name
}

func catalogTypeLabel(t CatalogType) string {
	if t.Label != "" {
		return t.Label
	}
	return t.Name
}

func catalogPropertyDescription(p CatalogProperty) string {
	if p.Description != "" {
		return p.Description
	}
	if p.Title != "" {
		return p.Title
	}
	return p.Type
}
