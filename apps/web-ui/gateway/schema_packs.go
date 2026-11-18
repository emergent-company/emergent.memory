package main

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- Schema packs (view model + derivation) ---

// schemaPackSummary is one ACTIVE schema pack: its identity (from memory's
// schema catalog when known, else the compiled types' denormalised
// schemaName/schemaVersion) plus the object/relationship types it contributes
// to the compiled schema.
//
// Packs are derived from GetCompiledTypes grouped by SchemaID — the
// authoritative set of active packs, including global packs that have no
// blueprint and project-owned packs like the override pack — never from
// GetSchema, which is project-scoped and 404s on global packs.
type schemaPackSummary struct {
	ID          string
	Name        string
	Version     string
	Description string
	ProjectID   string // "" => global/built-in pack
	Source      string

	ObjectTypes       []CompiledType
	RelationshipTypes []CompiledType

	// schemaName/schemaVersion are the pack identity denormalised on its
	// compiled types (CompiledType.SchemaName/SchemaVersion). They are the
	// provenance lookup key, matching how the object-type page resolves
	// ownership — kept separate from Name/Version, which may be overwritten by
	// catalog metadata that diverges from the blueprint manifest's pack.
	schemaName    string
	schemaVersion string

	// Provenance is the applied blueprint that owns this pack, when one
	// declares it (name+version match). Nil for global and project-authored
	// packs with no owning blueprint.
	Provenance *ProvenanceInfo

	// Category buckets the pack on the Schema packs page: "manual",
	// "blueprints", "data-sources" or "built-in". assignPackCategories fills it;
	// it is presentation only and never stored.
	Category string
	// ProviderType/ProviderLabel identify the data-source provider that
	// installs this pack (set only for Category data-sources).
	ProviderType  string
	ProviderLabel string
	// LinkedSources are the installed apps (data sources) of that provider,
	// one list row each on the packs page and links on the pack detail.
	LinkedSources []packLinkedSource
}

// packLinkedSource is one installed app a data-source pack is linked to.
type packLinkedSource struct {
	ID   string
	Name string
}

// Schema-packs page categories. Fixed order: Manual, Blueprints, Data Sources,
// Built-in.
const (
	packCategoryManual     = "manual"
	packCategoryBlueprints = "blueprints"
	packCategoryDataSource = "data-sources"
	packCategoryBuiltIn    = "built-in"
)

// global reports whether the pack is global (not project-owned).
func (p schemaPackSummary) global() bool { return p.ProjectID == "" }

// ownerLabel is the plain owner label shown as a badge.
func (p schemaPackSummary) ownerLabel() string {
	if p.global() {
		return "Global"
	}
	return "Project"
}

// typeCountLabel renders the pack's type counts, e.g. "3 object types, 1
// relationship type". Only non-zero groups are shown; an empty pack reads
// "0 types".
func (p schemaPackSummary) typeCountLabel() string {
	var parts []string
	if n := len(p.ObjectTypes); n > 0 {
		parts = append(parts, countLabel(n, "object type", "object types"))
	}
	if n := len(p.RelationshipTypes); n > 0 {
		parts = append(parts, countLabel(n, "relationship type", "relationship types"))
	}
	if len(parts) == 0 {
		return "0 types"
	}
	return strings.Join(parts, ", ")
}

// buildSchemaPackSummaries groups compiled types by SchemaID and joins pack
// metadata from the schema catalog (ListAllSchemas). Types with no SchemaID
// cannot be attributed to a pack and are skipped, as are Shadowed (losing)
// duplicates — the same filter the compiled-schema list applies via
// visibleCompiledTypes — so an override pack never resurfaces the blueprint
// type it supersedes and a fully-shadowed pack is omitted entirely. When the
// catalog has no entry for a pack, its name/version fall back to the compiled
// types' denormalised schema fields, so every contributing pack still surfaces.
// Packs are sorted by name (case-insensitive) then version for a deterministic
// list. provenance indexes applied-blueprint packs by the compiled types'
// denormalised schema name+version (see schemaPackSummary.schemaName).
func buildSchemaPackSummaries(compiled *CompiledSchemaTypes, metas []SchemaInfo, provenance map[provenanceKey]*ProvenanceInfo) []schemaPackSummary {
	if compiled == nil {
		return nil
	}
	metaByID := make(map[string]SchemaInfo, len(metas))
	for _, m := range metas {
		metaByID[m.ID] = m
	}

	byID := map[string]*schemaPackSummary{}
	var order []string
	ensure := func(id string, t CompiledType) *schemaPackSummary {
		if p, ok := byID[id]; ok {
			return p
		}
		p := &schemaPackSummary{
			ID:            id,
			Name:          t.SchemaName,
			Version:       t.SchemaVersion,
			schemaName:    t.SchemaName,
			schemaVersion: t.SchemaVersion,
		}
		if m, ok := metaByID[id]; ok {
			p.Name = m.Name
			p.Version = m.Version
			p.Description = m.Description
			p.ProjectID = m.ProjectID
			p.Source = m.Source
		}
		if p.Name == "" {
			// No catalog metadata and no denormalised schema name: keep the id
			// as a last-resort label rather than rendering a blank row.
			p.Name = id
		}
		byID[id] = p
		order = append(order, id)
		return p
	}

	for _, t := range compiled.ObjectTypes {
		if t.SchemaID == "" || t.Shadowed {
			continue
		}
		p := ensure(t.SchemaID, t)
		p.ObjectTypes = append(p.ObjectTypes, t)
	}
	for _, t := range compiled.RelationshipTypes {
		if t.SchemaID == "" || t.Shadowed {
			continue
		}
		p := ensure(t.SchemaID, t)
		p.RelationshipTypes = append(p.RelationshipTypes, t)
	}

	out := make([]schemaPackSummary, 0, len(order))
	for _, id := range order {
		p := byID[id]
		keyName, keyVersion := p.schemaName, p.schemaVersion
		if keyName == "" {
			keyName, keyVersion = p.Name, p.Version
		}
		if prov, ok := provenance[provenanceKey{name: keyName, version: keyVersion}]; ok {
			cp := *prov
			p.Provenance = &cp
		}
		out = append(out, *p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if ni != nj {
			return ni < nj
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// schemaPackRow is one rendered pack row: the pack, the title it shows (the
// pack name, or an installed app's name for a data-source row), an optional
// secondary line, and where the title links ("" = not a link).
type schemaPackRow struct {
	Pack     schemaPackSummary
	Title    string
	Subtitle string
	Href     string
}

// schemaPackGroup is one group inside a section. Blueprint sections carry one
// group per applied blueprint (its heading links to the blueprint); a provider
// section carries one group per data-source provider; manual/built-in sections
// carry a single heading-less group.
type schemaPackGroup struct {
	// Key is the group's stable identity (e.g. "blueprint:<id>", "provider:github_app"
	// or the section key for a single group), exposed as a data attribute.
	Key string
	// Label is the group heading, or "" for a heading-less group.
	Label string
	// Href is the heading's link target, "" when the heading is plain.
	Href      string
	Blueprint *ProvenanceInfo
	Rows      []schemaPackRow
}

// BlueprintID returns the owning blueprint's id, or "" when the group holds no
// blueprint.
func (g schemaPackGroup) BlueprintID() string {
	if g.Blueprint == nil {
		return ""
	}
	return g.Blueprint.BlueprintID
}

// schemaPackSection is one top-level section of the packs page.
type schemaPackSection struct {
	Key    string
	Label  string
	Groups []schemaPackGroup
}

// assignPackCategories buckets each pack and, for a data-source pack, resolves
// its provider and installed apps. Precedence: an owning blueprint wins; then an
// explicit provider token or the provider catalog's declared pack name; then a
// global/builtin row; everything else is a manual, project-authored pack.
//
// The provider-catalog match is what makes an already-stored pack classify
// correctly before its row has been reconciled to the new provenance token.
func assignPackCategories(packs []schemaPackSummary, providers []DSProvider, sources []DSIntegration) {
	providerByPack := make(map[string]DSProvider, len(providers))
	providerByType := make(map[string]DSProvider, len(providers))
	for _, p := range providers {
		providerByType[p.Type] = p
		if p.SchemaPack != "" {
			providerByPack[p.SchemaPack] = p
		}
	}
	sourcesByType := make(map[string][]DSIntegration, len(providers))
	for _, src := range sources {
		sourcesByType[src.ProviderType] = append(sourcesByType[src.ProviderType], src)
	}

	for i := range packs {
		p := &packs[i]
		p.Category, p.ProviderType, p.ProviderLabel, p.LinkedSources = "", "", "", nil
		if p.Provenance != nil {
			p.Category = packCategoryBlueprints
			continue
		}
		if pt := providerTypeForPack(*p, providerByPack); pt != "" {
			p.Category = packCategoryDataSource
			p.ProviderType = pt
			prov, ok := providerByType[pt]
			if ok && prov.Name != "" {
				p.ProviderLabel = prov.Name
			} else {
				p.ProviderLabel = pt
			}
			for _, src := range sourcesByType[pt] {
				p.LinkedSources = append(p.LinkedSources, packLinkedSource{ID: src.ID, Name: dsSourceName(src)})
			}
			sort.SliceStable(p.LinkedSources, func(a, b int) bool {
				return strings.ToLower(p.LinkedSources[a].Name) < strings.ToLower(p.LinkedSources[b].Name)
			})
			continue
		}
		if p.Source == "builtin" || p.global() {
			p.Category = packCategoryBuiltIn
			continue
		}
		p.Category = packCategoryManual
	}
}

// providerTypeForPack resolves the data-source provider that owns a pack: the
// explicit provider token first, then the catalog's declared pack name (by the
// pack's denormalised schema name or its displayed name).
func providerTypeForPack(p schemaPackSummary, providerByPack map[string]DSProvider) string {
	if strings.HasPrefix(p.Source, "provider:") {
		return strings.TrimPrefix(p.Source, "provider:")
	}
	for _, name := range []string{p.schemaName, p.Name} {
		if name == "" {
			continue
		}
		if prov, ok := providerByPack[name]; ok {
			return prov.Type
		}
	}
	return ""
}

// groupSchemaPacks buckets blueprint-owned packs by owning blueprint, ordered
// case-insensitively by label. The packs page renders these inside the
// "Blueprints" section so the owning blueprint is the group heading (linked),
// exactly as before the section was introduced.
func groupSchemaPacks(packs []schemaPackSummary) []schemaPackGroup {
	index := make(map[string]int, len(packs))
	groups := make([]schemaPackGroup, 0, len(packs))
	for _, p := range packs {
		if p.Provenance == nil {
			continue
		}
		key := "blueprint:" + p.Provenance.BlueprintID
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, schemaPackGroup{
				Key:       key,
				Label:     typeProvenanceLabel(p.Provenance),
				Href:      "/blueprints/" + url.PathEscape(p.Provenance.BlueprintID),
				Blueprint: p.Provenance,
			})
		}
		groups[i].Rows = append(groups[i].Rows, schemaPackRow{Pack: p, Title: p.Name})
	}
	sort.SliceStable(groups, func(a, b int) bool {
		return strings.ToLower(groups[a].Label) < strings.ToLower(groups[b].Label)
	})
	return groups
}

// schemaPackSections builds the packs page's four fixed sections from
// categorised summaries, omitting an empty section. Data-source rows are one per
// installed app; a provider pack with no installed app still gets one plain row
// so the pack is never hidden.
func schemaPackSections(packs []schemaPackSummary) []schemaPackSection {
	manual := make([]schemaPackRow, 0)
	builtin := make([]schemaPackRow, 0)
	for _, p := range packs {
		switch p.Category {
		case packCategoryManual:
			manual = append(manual, schemaPackRow{Pack: p, Title: p.Name})
		case packCategoryBuiltIn:
			builtin = append(builtin, schemaPackRow{Pack: p, Title: p.Name})
		}
	}

	providerIndex := map[string]int{}
	providerGroups := make([]schemaPackGroup, 0)
	for _, p := range packs {
		if p.Category != packCategoryDataSource {
			continue
		}
		i, ok := providerIndex[p.ProviderType]
		if !ok {
			i = len(providerGroups)
			providerIndex[p.ProviderType] = i
			providerGroups = append(providerGroups, schemaPackGroup{
				Key:   "provider:" + p.ProviderType,
				Label: p.ProviderLabel,
			})
		}
		if len(p.LinkedSources) == 0 {
			providerGroups[i].Rows = append(providerGroups[i].Rows, schemaPackRow{
				Pack:     p,
				Title:    p.ProviderLabel,
				Subtitle: p.Name + " v" + p.Version,
			})
			continue
		}
		for _, src := range p.LinkedSources {
			providerGroups[i].Rows = append(providerGroups[i].Rows, schemaPackRow{
				Pack:     p,
				Title:    src.Name,
				Subtitle: p.Name + " v" + p.Version,
				Href:     "/data-sources/" + url.PathEscape(src.ID),
			})
		}
	}
	sort.SliceStable(providerGroups, func(a, b int) bool {
		return strings.ToLower(providerGroups[a].Label) < strings.ToLower(providerGroups[b].Label)
	})

	blueprintGroups := groupSchemaPacks(packs)

	sections := make([]schemaPackSection, 0, 4)
	appendSection := func(key, label string, groups []schemaPackGroup) {
		if len(groups) == 0 {
			return
		}
		sections = append(sections, schemaPackSection{Key: key, Label: label, Groups: groups})
	}
	if len(manual) > 0 {
		appendSection(packCategoryManual, "Manual", []schemaPackGroup{{Key: packCategoryManual, Rows: manual}})
	}
	if len(blueprintGroups) > 0 {
		appendSection(packCategoryBlueprints, "Blueprints", blueprintGroups)
	}
	if len(providerGroups) > 0 {
		appendSection(packCategoryDataSource, "Data Sources", providerGroups)
	}
	if len(builtin) > 0 {
		appendSection(packCategoryBuiltIn, "Built-in", []schemaPackGroup{{Key: packCategoryBuiltIn, Rows: builtin}})
	}
	return sections
}

// packCount is the number of distinct packs in a section (a data-source pack
// can contribute one row per installed app, so counting rows would overstate
// it).
func (s schemaPackSection) packCount() int {
	seen := make(map[string]struct{}, len(s.Groups))
	for _, g := range s.Groups {
		for _, r := range g.Rows {
			seen[r.Pack.ID] = struct{}{}
		}
	}
	return len(seen)
}

// packSourceDisplay renders a stored provenance token for a surface that has no
// category context (the pending-pack review page).
func packSourceDisplay(source string) string {
	switch {
	case source == "" || source == "manual":
		return "Manual"
	case source == "builtin":
		return "Built-in"
	case strings.HasPrefix(source, "provider:"):
		return strings.TrimPrefix(source, "provider:")
	default:
		return source
	}
}

// sourceLabel is the pack detail's provenance line: the category in words, never
// the stored token. A blueprint-owned pack returns "" because the detail page
// already links the owning blueprint in its own meta row.
func (p schemaPackSummary) sourceLabel() string {
	switch p.Category {
	case packCategoryBlueprints:
		return ""
	case packCategoryDataSource:
		return p.ProviderLabel
	case packCategoryBuiltIn:
		return "Built-in"
	default:
		return "Manual"
	}
}

// --- handlers ---
// uiSchemaPacks renders the Schema-area "Schema packs" list: the project's
// active packs derived from the compiled types, with metadata joined from the
// schema catalog. A compiled-types failure renders the whole-page error state.
func (s *Server) uiSchemaPacks(c echo.Context) error {
	ctx := c.Request().Context()
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return s.page(c, pageTitle("Schema packs"), SchemaPacksPage(nil, userFacingError(err)))
	}
	metas, merr := s.memory.ListAllSchemas(ctx)
	captureError(merr)
	providers, perr := s.memory.ListDSProviders(ctx)
	captureError(perr)
	sources, serr := s.memory.ListDSIntegrations(ctx)
	captureError(serr)
	packs := buildSchemaPackSummaries(compiled, metas, s.buildProvenanceIndex(ctx))
	assignPackCategories(packs, providers, sources)
	return s.page(c, pageTitle("Schema packs"), SchemaPacksPage(schemaPackSections(packs), nil))
}

// uiSchemaPack renders one active pack's read-only detail: metadata plus the
// object and relationship types it contributes. A pack that exists but is not
// installed — the state a discovery run leaves its result in — renders the
// pending-review page instead, because it contributes no compiled types and is
// therefore absent from the packs derivation. Anything else renders the
// not-found state (no panic).
func (s *Server) uiSchemaPack(c echo.Context) error {
	ctx := c.Request().Context()
	id := pathUnescape(c.Param("id"))
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return s.page(c, pageTitle("Schema pack"), SchemaPackPage(nil, userFacingError(err), ""))
	}
	metas, merr := s.memory.ListAllSchemas(ctx)
	captureError(merr)
	providers, perr := s.memory.ListDSProviders(ctx)
	captureError(perr)
	sources, serr := s.memory.ListDSIntegrations(ctx)
	captureError(serr)
	packs := buildSchemaPackSummaries(compiled, metas, s.buildProvenanceIndex(ctx))
	assignPackCategories(packs, providers, sources)
	for i := range packs {
		if packs[i].ID == id {
			notice := flashFromQuery(c, []flashParam{{key: "installed", msg: packInstalledNotice}})
			return s.page(c, pageTitle(packs[i].Name), SchemaPackPage(&packs[i], nil, notice))
		}
	}

	if id != "" {
		if pack, packErr := s.memory.GetSchema(ctx, id); packErr == nil && pack != nil && pack.ID != "" {
			return s.page(c, pageTitle(pack.Name), SchemaPackPendingPage(pendingPackFromSchema(pack), flashError(c)))
		}
	}
	return s.page(c, pageTitle("Schema pack"), SchemaPackPage(nil, nil, ""))
}

// uiSchemaPackInstall handles POST /schema/packs/:id/install: it installs a
// reviewed pack, which is what turns a discovery result into compiled types.
// Installing is a deliberate, separate step from discovering — the run never
// installs anything by itself.
func (s *Server) uiSchemaPackInstall(c echo.Context) error {
	ctx := c.Request().Context()
	id := pathUnescape(c.Param("id"))
	back := "/schema/packs"
	if id != "" {
		back = "/schema/packs/" + url.PathEscape(id)
	}
	if id == "" {
		return redirectWithError(c, back, errors.New("schema pack id is required"))
	}
	if err := s.memory.AssignSchemaPack(ctx, id); err != nil {
		return redirectWithError(c, back, err)
	}
	return c.Redirect(http.StatusSeeOther, back+"?installed=1")
}

// packInstalledNotice is the confirmation shown after installing a reviewed
// pack. Reading it from a query flag rather than a cookie keeps the redirect
// stateless and means a reload does not repeat it.
const packInstalledNotice = "Pack installed. Its object and relationship types are now part of this project's compiled types."

// pendingPack is a pack that exists in the project's catalog but is not
// installed yet: an un-reviewed discovery result, or a pack whose assignment
// was removed. It contributes no compiled types, so it cannot be rendered by
// the pack-summary path; it is read through GetSchema and installed through the
// assign endpoint.
type pendingPack struct {
	ID          string
	Name        string
	Version     string
	Description string
	Author      string
	Source      string

	ObjectTypes   []string
	RelationTypes []string

	// PendingReview distinguishes a pack a discovery run proposed (the flag the
	// server sets) from a pack that is merely unassigned.
	PendingReview bool
}

// pendingPackFromSchema turns a schema definition into the review view model.
// Both type lists are read through the catalog parsers, so the keyed-map form a
// discovered pack uses and the registry's array form both render; a relationship
// also names its endpoints, so the reviewer can see which pairs it proposes.
func pendingPackFromSchema(schema *BlueprintSchema) *pendingPack {
	pack := &pendingPack{
		ID:            schema.ID,
		Name:          schema.Name,
		Version:       schema.Version,
		Description:   schema.Description,
		Author:        schema.Author,
		Source:        schema.Source,
		PendingReview: schema.PendingReview,
	}

	// Reuse the catalog's parsers so both stored shapes render: the registry's
	// array form and the keyed-map form a discovered pack uses. A pack that
	// parsed as empty would show a review page with nothing to review and an
	// install button that installs nothing visible.
	for _, t := range objectTypesFromRaw(schema.ObjectTypeSchemas) {
		if t.Name != "" {
			pack.ObjectTypes = append(pack.ObjectTypes, t.Name)
		}
	}
	sort.Strings(pack.ObjectTypes)

	relations := relationshipTypesFromRaw(schema.RelationshipTypeSchemas)
	sort.SliceStable(relations, func(i, j int) bool { return relations[i].Name < relations[j].Name })
	for _, r := range relations {
		if r.Name == "" {
			continue
		}
		if r.SourceType != "" && r.TargetType != "" {
			pack.RelationTypes = append(pack.RelationTypes, r.Name+" ("+r.SourceType+" \u2192 "+r.TargetType+")")
			continue
		}
		pack.RelationTypes = append(pack.RelationTypes, r.Name)
	}

	return pack
}
