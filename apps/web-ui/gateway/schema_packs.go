package main

import (
	"encoding/json"
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
}

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
	packs := buildSchemaPackSummaries(compiled, metas, s.buildProvenanceIndex(ctx))
	return s.page(c, pageTitle("Schema packs"), SchemaPacksPage(packs, nil))
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
		return s.page(c, pageTitle("Schema pack"), SchemaPackPage(nil, userFacingError(err)))
	}
	metas, merr := s.memory.ListAllSchemas(ctx)
	captureError(merr)
	packs := buildSchemaPackSummaries(compiled, metas, s.buildProvenanceIndex(ctx))
	for i := range packs {
		if packs[i].ID == id {
			return s.page(c, pageTitle(packs[i].Name), SchemaPackPage(&packs[i], nil))
		}
	}

	if id != "" {
		if pack, packErr := s.memory.GetSchema(ctx, id); packErr == nil && pack != nil && pack.ID != "" {
			return s.page(c, pageTitle(pack.Name), SchemaPackPendingPage(pendingPackFromSchema(pack), flashError(c)))
		}
	}
	return s.page(c, pageTitle("Schema pack"), SchemaPackPage(nil, nil))
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
}

// pendingPackFromSchema turns a schema definition into the review view model.
// Both type maps are keyed by name; a relationship also names its endpoints, so
// the reviewer can see which pairs it proposes.
func pendingPackFromSchema(schema *BlueprintSchema) *pendingPack {
	pack := &pendingPack{
		ID:          schema.ID,
		Name:        schema.Name,
		Version:     schema.Version,
		Description: schema.Description,
		Author:      schema.Author,
		Source:      schema.Source,
	}

	var objects map[string]struct {
		Description string `json:"description"`
	}
	if len(schema.ObjectTypeSchemas) > 0 && json.Unmarshal(schema.ObjectTypeSchemas, &objects) == nil {
		for name := range objects {
			pack.ObjectTypes = append(pack.ObjectTypes, name)
		}
		sort.Strings(pack.ObjectTypes)
	}

	var relations map[string]struct {
		SourceTypes []string `json:"sourceTypes"`
		TargetTypes []string `json:"targetTypes"`
	}
	if len(schema.RelationshipTypeSchemas) > 0 && json.Unmarshal(schema.RelationshipTypeSchemas, &relations) == nil {
		names := make([]string, 0, len(relations))
		for name := range relations {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			r := relations[name]
			switch {
			case len(r.SourceTypes) > 0 && len(r.TargetTypes) > 0:
				pack.RelationTypes = append(pack.RelationTypes, name+" ("+r.SourceTypes[0]+" → "+r.TargetTypes[0]+")")
			default:
				pack.RelationTypes = append(pack.RelationTypes, name)
			}
		}
	}

	return pack
}
