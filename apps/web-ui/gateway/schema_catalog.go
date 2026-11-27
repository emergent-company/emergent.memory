package main

import (
	"context"
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

// catalogPackName is the default project schema pack the catalog materializes
// into. It mirrors blueprints.CatalogPackOptions' default; a project may install
// the catalog under a custom --name, so badge decisions use catalogPackFor,
// which follows the pack the project actually owns its catalog types under.
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

// catalogTypeNames is the set of object-type names the built-in catalog
// declares. It identifies a project's catalog pack (see catalogPackFor), so it
// must be the whole manifest — a filtered browse page is not enough.
func catalogTypeNames(list *CatalogList) map[string]bool {
	names := map[string]bool{}
	if list == nil {
		return names
	}
	for _, t := range list.Types {
		if t.Name != "" {
			names[t.Name] = true
		}
	}
	return names
}

// catalogTypeNameSet fetches the unfiltered catalog so the manifest's whole
// name set is available on pages whose own listing is filtered or absent. A
// failed fetch yields an empty set, which makes catalogPackFor fall back to the
// default pack name rather than guess.
func (s *Server) catalogTypeNameSet(ctx context.Context) map[string]bool {
	list, err := s.memory.ListCatalog(ctx, "", "", "")
	if err != nil {
		captureError(err)
		return map[string]bool{}
	}
	return catalogTypeNames(list)
}

// catalogPackFor names the project pack the built-in catalog was installed
// under. Install uses the default name unless the installer passed a custom
// --name, so:
//
//   - a project that owns types under the default name installed the catalog
//     there — that pack is the catalog pack, whatever else is present;
//   - otherwise the catalog pack is the pack whose whole set of owned object
//     types are catalog types (InstallCatalog refuses to write into a pack
//     holding any other type), preferring the pack owning the most catalog
//     types so the largest catalog install wins;
//   - a project with no catalog types installed falls back to the default name.
//
// Nothing records the effective pack name (see #1149), so the compiled view is
// the only project-scoped signal the browser has; a pack that happens to own
// nothing but catalog-named types is the catalog pack as far as this heuristic
// can tell.
func catalogPackFor(compiled *CompiledSchemaTypes, catalogNames map[string]bool) string {
	if compiled == nil || len(catalogNames) == 0 {
		return catalogPackName
	}
	owned := make(map[string][]string)
	for _, t := range compiled.ObjectTypes {
		if t.Shadowed || t.Name == "" || t.SchemaName == "" {
			continue
		}
		owned[t.SchemaName] = append(owned[t.SchemaName], t.Name)
	}
	if len(owned[catalogPackName]) > 0 {
		return catalogPackName
	}
	best, bestCount := "", 0
	for pack, names := range owned {
		if !allCatalogTypes(names, catalogNames) {
			continue
		}
		better := len(names) > bestCount || (len(names) == bestCount && pack < best)
		if better {
			best, bestCount = pack, len(names)
		}
	}
	if best == "" {
		return catalogPackName
	}
	return best
}

// allCatalogTypes reports whether every named type is a catalog type and there
// is at least one. A pack with any non-catalog type is not a catalog pack.
func allCatalogTypes(names []string, catalogNames map[string]bool) bool {
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		if !catalogNames[n] {
			return false
		}
	}
	return len(seen) > 0
}

// catalogStates maps each compiled object type name to what provides it. The
// compiled view orders declarations by assignment and flags the losing
// same-name rows Shadowed, so only unshadowed rows are considered: the winner is
// the definition the project actually resolves. A row with no schema name has no
// providing pack — it is skipped like the server skips it, so it stays unowned
// rather than being recorded as installable-catalog state.
func catalogStates(compiled *CompiledSchemaTypes, catalogNames map[string]bool) map[string]catalogInstallState {
	states := map[string]catalogInstallState{}
	if compiled == nil {
		return states
	}
	packName := catalogPackFor(compiled, catalogNames)
	for _, t := range compiled.ObjectTypes {
		if t.Shadowed || t.Name == "" || t.SchemaName == "" {
			continue
		}
		if t.SchemaName == packName {
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
	// The catalog pack is identified from the manifest's whole name set, which a
	// filtered page does not carry, so ask for the unfiltered list when needed.
	catalogNames := catalogTypeNames(list)
	if q != "" || category != "" {
		catalogNames = s.catalogTypeNameSet(ctx)
	}
	compiled, cerr := s.memory.GetCompiledTypes(ctx)
	if cerr != nil {
		compiled = nil
	}
	view := &SchemaCatalogView{
		Catalog:  list,
		Query:    q,
		Category: category,
		States:   catalogStates(compiled, catalogNames),
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
	catalogNames := s.catalogTypeNameSet(ctx)
	view := &SchemaCatalogTypeView{
		Detail: detail,
		State:  catalogStates(compiled, catalogNames)[detail.Type.Name],
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
