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

// catalogPackName is the project schema pack the catalog materializes into. It
// mirrors blueprints.CatalogPackOptions' default; a type under any other pack is
// a different definition that happens to share the name.
const catalogPackName = "schemaorg-catalog"

// catalogInstallState is what the project already has under a catalog type's
// name: nothing (installable), a type the catalog pack provides, or a same-named
// type another pack provides, whose definition wins and which an install would
// merge into rather than replace.
type catalogInstallState struct {
	Installed   bool
	ProvidedBy  string
	ProvidedVer string
}

// Available reports whether the catalog type can be installed into the project.
func (s catalogInstallState) Available() bool { return !s.Installed && s.ProvidedBy == "" }

// ProvidedLabel names the providing pack with its version when known.
func (s catalogInstallState) ProvidedLabel() string {
	if s.ProvidedVer != "" {
		return s.ProvidedBy + " " + s.ProvidedVer
	}
	return s.ProvidedBy
}

// catalogStates maps each compiled object type name to what provides it. The
// compiled view orders declarations by assignment and flags the losing
// same-name rows Shadowed, so only unshadowed rows are considered: the winner is
// the definition the project actually resolves.
func catalogStates(compiled *CompiledSchemaTypes) map[string]catalogInstallState {
	states := map[string]catalogInstallState{}
	if compiled == nil {
		return states
	}
	for _, t := range compiled.ObjectTypes {
		if t.Shadowed || t.Name == "" {
			continue
		}
		if t.SchemaName == catalogPackName {
			states[t.Name] = catalogInstallState{Installed: true, ProvidedBy: t.SchemaName, ProvidedVer: t.SchemaVersion}
			continue
		}
		states[t.Name] = catalogInstallState{ProvidedBy: t.SchemaName, ProvidedVer: t.SchemaVersion}
	}
	return states
}

// SchemaCatalogView is the catalog browse page model.
type SchemaCatalogView struct {
	Catalog  *CatalogList
	Query    string
	Category string
	States   map[string]catalogInstallState
	Groups   []CatalogGroup
	FlashMsg string
	FlashErr error
}

// SchemaCatalogTypeView is the catalog type detail page model.
type SchemaCatalogTypeView struct {
	Detail   *CatalogTypeDetail
	State    catalogInstallState
	FlashMsg string
	FlashErr error
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
	compiled, cerr := s.memory.GetCompiledTypes(ctx)
	if cerr != nil {
		compiled = nil
	}
	view := &SchemaCatalogView{
		Catalog:  list,
		Query:    q,
		Category: category,
		States:   catalogStates(compiled),
		Groups:   groupCatalogTypes(list),
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
	compiled, cerr := s.memory.GetCompiledTypes(ctx)
	if cerr != nil {
		compiled = nil
	}
	view := &SchemaCatalogTypeView{
		Detail: detail,
		State:  catalogStates(compiled)[detail.Type.Name],
	}
	if c.QueryParam("installed") != "" {
		view.FlashMsg = "Type installed."
	}
	view.FlashErr = flashError(c)
	return s.page(c, pageTitle(catalogTypeTitle(detail)), SchemaCatalogTypePage(view, nil))
}

// uiSchemaCatalogInstall installs the selected catalog type(s) and redirects to
// the type page (single) or the list (multiple). A selection whose name another
// pack already provides is refused by the server (the project keeps one type per
// name), so the redirect carries that message; the browser never offers one.
func (s *Server) uiSchemaCatalogInstall(c echo.Context) error {
	ctx := c.Request().Context()
	types := catalogFormTypes(c)
	if len(types) == 0 {
		return redirectWithError(c, "/schema/catalog", fmt.Errorf("select at least one type"))
	}
	if _, err := s.memory.InstallCatalog(ctx, types, false, nil); err != nil {
		return redirectWithError(c, "/schema/catalog", err)
	}
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

// objectTypeHref is the compiled-type page for a name the project already has,
// where the live definition (its pack, properties and history) is shown.
func objectTypeHref(name string) string {
	return "/schema/object-types/" + url.PathEscape(name)
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
