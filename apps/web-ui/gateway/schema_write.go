package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- schema pack write model ---

// SchemaPackWriteRequest is the partial schema-pack body for POST /api/schemas
// (create) and PUT /api/schemas/:packId (update). Snake_case keys match memory's
// canonical write contract; omitted fields are left unchanged on update.
type SchemaPackWriteRequest struct {
	Name                    string                `json:"name,omitempty"`
	Version                 string                `json:"version,omitempty"`
	Description             string                `json:"description,omitempty"`
	Author                  string                `json:"author,omitempty"`
	License                 string                `json:"license,omitempty"`
	RepositoryURL           string                `json:"repository_url,omitempty"`
	DocumentationURL        string                `json:"documentation_url,omitempty"`
	ObjectTypeSchemas       json.RawMessage       `json:"object_type_schemas,omitempty"`
	RelationshipTypeSchemas json.RawMessage       `json:"relationship_type_schemas,omitempty"`
	Migrations              *SchemaMigrationHints `json:"migrations,omitempty"`
}

// ObjectTypeEdit is one object-type mutation: the type's identity plus the
// editable description, type-level ui accent (icon/color), and the full
// (replacement) property set. Name is immutable on edit; other type-level keys
// (label, embedding, …) on an existing type are preserved.
type ObjectTypeEdit struct {
	Name        string
	Label       string
	Description string
	Icon        string
	Color       string
	Properties  map[string]any
}

// objectTypeMapFromEdit renders the new-type form of an edit (used when the
// override pack is created).
func objectTypeMapFromEdit(edit ObjectTypeEdit) map[string]any {
	m := map[string]any{"name": edit.Name}
	if edit.Label != "" {
		m["label"] = edit.Label
	}
	if edit.Description != "" {
		m["description"] = edit.Description
	}
	if ui := editUIBlock(edit.Icon, edit.Color); ui != nil {
		m["ui"] = ui
	}
	if edit.Properties != nil {
		m["properties"] = edit.Properties
	}
	return m
}

// editUIBlock builds the type-level ui block from the editor's icon/color
// values, or nil when both are empty (no ui declaration).
func editUIBlock(icon, color string) map[string]any {
	ui := map[string]any{}
	if strings.TrimSpace(icon) != "" {
		ui["icon"] = strings.TrimSpace(icon)
	}
	if strings.TrimSpace(color) != "" {
		ui["color"] = strings.TrimSpace(color)
	}
	if len(ui) == 0 {
		return nil
	}
	return ui
}

// applyObjectTypeEdit upserts edit into types (name-keyed), preserving every
// unrelated type and any extra keys on the edited type. A missing type is
// appended. The input slice is not mutated.
func applyObjectTypeEdit(types []map[string]any, edit ObjectTypeEdit) []map[string]any {
	out := make([]map[string]any, 0, len(types)+1)
	replaced := false
	for _, t := range types {
		if strAny(t["name"]) == edit.Name {
			t["name"] = edit.Name
			if edit.Label != "" {
				t["label"] = edit.Label
			}
			t["description"] = edit.Description
			applyEditUI(t, edit.Icon, edit.Color)
			if edit.Properties != nil {
				t["properties"] = edit.Properties
			}
			out = append(out, t)
			replaced = true
			continue
		}
		out = append(out, t)
	}
	if !replaced {
		out = append(out, objectTypeMapFromEdit(edit))
	}
	return out
}

// applyEditUI merges the editor's icon/color into an existing type's ui block,
// preserving any unrelated ui keys. An empty value clears that key; an emptied
// block is dropped entirely.
func applyEditUI(t map[string]any, icon, color string) {
	ui, _ := t["ui"].(map[string]any)
	if ui == nil {
		ui = map[string]any{}
	}
	if strings.TrimSpace(icon) != "" {
		ui["icon"] = strings.TrimSpace(icon)
	} else {
		delete(ui, "icon")
	}
	if strings.TrimSpace(color) != "" {
		ui["color"] = strings.TrimSpace(color)
	} else {
		delete(ui, "color")
	}
	if len(ui) == 0 {
		delete(t, "ui")
		return
	}
	t["ui"] = ui
}

// --- MemoryClient pack mutations ---

// CreateSchemaPack creates a project-owned schema pack (POST /api/schemas) and
// returns the created pack. Memory scopes the pack to the caller's project.
func (m *MemoryClient) CreateSchemaPack(ctx context.Context, req *SchemaPackWriteRequest) (*BlueprintSchema, error) {
	var out BlueprintSchema
	if err := m.do(ctx, http.MethodPost, "/api/schemas", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSchemaPack performs the read-modify-write of one object type in a pack:
// fetch the pack, upsert the edited type, then write the whole object-type blob
// back. The pack's relationship types and all unrelated object types survive.
func (m *MemoryClient) UpdateSchemaPack(ctx context.Context, packID string, edit ObjectTypeEdit) error {
	pack, err := m.GetSchema(ctx, packID)
	if err != nil {
		return err
	}
	merged := applyObjectTypeEdit(schemaTypesToMaps(pack.ObjectTypeSchemas), edit)
	raw, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	return m.do(ctx, http.MethodPut, "/api/schemas/"+url.PathEscape(packID), &SchemaPackWriteRequest{ObjectTypeSchemas: raw}, nil)
}

// AssignSchemaPack assigns a project-owned pack to the project (POST
// /api/schemas/projects/:pid/assign), making it part of the compiled schema.
func (m *MemoryClient) AssignSchemaPack(ctx context.Context, schemaID string) error {
	body := map[string]any{"schema_id": schemaID}
	return m.do(ctx, http.MethodPost, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/assign", body, nil)
}

// --- copy-on-write override pack (design D1) ---

const (
	// overridePackName is the project-owned pack that shadows blueprint-derived
	// types when the user edits them. Reused across edits; never the blueprint's
	// own pack.
	overridePackName = "project-schema-overrides"
	// overridePackVersion gives the override pack a stable identity so it can be
	// found and updated on repeat edits.
	overridePackVersion     = "1.0.0"
	overridePackDescription = "Project-local edits to blueprint-derived object types."
)

// findOverridePack returns the project-owned override pack id, or "" when it
// does not exist yet.
func (s *Server) findOverridePack(ctx context.Context) (string, error) {
	schemas, err := s.memory.ListAllSchemas(ctx)
	if err != nil {
		return "", err
	}
	for _, sc := range schemas {
		if sc.Name == overridePackName && sc.Version == overridePackVersion && sc.ProjectID != "" {
			return sc.ID, nil
		}
	}
	return "", nil
}

// upsertOverrideType writes an edited blueprint-derived type into the
// project-owned override pack: it creates + assigns the pack on first use and
// updates its object-type blob on repeat. It never touches the blueprint's own
// pack. Assigning after the blueprint pack makes the override the later
// installed_at winner in the compiled-types merge.
func (s *Server) upsertOverrideType(ctx context.Context, edit ObjectTypeEdit) error {
	packID, err := s.findOverridePack(ctx)
	if err != nil {
		return err
	}
	if packID != "" {
		return s.memory.UpdateSchemaPack(ctx, packID, edit)
	}
	raw, err := json.Marshal([]map[string]any{objectTypeMapFromEdit(edit)})
	if err != nil {
		return err
	}
	created, err := s.memory.CreateSchemaPack(ctx, &SchemaPackWriteRequest{
		Name:              overridePackName,
		Version:           overridePackVersion,
		Description:       overridePackDescription,
		ObjectTypeSchemas: raw,
	})
	if err != nil {
		return err
	}
	return s.memory.AssignSchemaPack(ctx, created.ID)
}

// --- blueprint derivation client methods ---

// UpdateBlueprintRequest is the partial body for PUT /api/blueprints/:id. Memory
// only allows updating draft blueprints.
type UpdateBlueprintRequest struct {
	Name        string          `json:"name,omitempty"`
	Version     string          `json:"version,omitempty"`
	Description string          `json:"description,omitempty"`
	Author      string          `json:"author,omitempty"`
	Manifest    json.RawMessage `json:"manifest,omitempty"`
}

// CreateBlueprintVersionRequest is the body for POST
// /api/blueprints/:id/versions (fork a new draft version).
type CreateBlueprintVersionRequest struct {
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// UpdateBlueprint replaces a draft blueprint's fields (PUT /api/blueprints/:id).
func (m *MemoryClient) UpdateBlueprint(ctx context.Context, id string, req *UpdateBlueprintRequest) (*BlueprintRecord, error) {
	var out BlueprintRecord
	if err := m.do(ctx, http.MethodPut, "/api/blueprints/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateBlueprintVersion forks a new draft version of a blueprint (POST
// /api/blueprints/:id/versions) and returns the new record.
func (m *MemoryClient) CreateBlueprintVersion(ctx context.Context, id string, req *CreateBlueprintVersionRequest) (*BlueprintRecord, error) {
	var out BlueprintRecord
	if err := m.do(ctx, http.MethodPost, "/api/blueprints/"+url.PathEscape(id)+"/versions", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- effective-type manifest assembly ---

// errNothingToDerive is returned when the project has no compiled types to
// capture in a blueprint.
var errNothingToDerive = errors.New("no compiled types to derive from")

// buildDerivedManifest assembles a blueprint manifest from the project's
// effective compiled types (post-override). Shadowed (losing) duplicates are
// excluded, so exactly the merged winner per name is emitted. Object and
// relationship behavioural keys (label, description, properties, ui,
// source/target) are preserved.
func buildDerivedManifest(name, version, description string, compiled *CompiledSchemaTypes) (json.RawMessage, error) {
	if compiled == nil {
		return nil, errNothingToDerive
	}
	objects := make([]map[string]any, 0, len(compiled.ObjectTypes))
	for _, t := range compiled.ObjectTypes {
		if t.Shadowed {
			continue
		}
		objects = append(objects, compiledTypeMap(t, false))
	}
	relationships := make([]map[string]any, 0, len(compiled.RelationshipTypes))
	for _, t := range compiled.RelationshipTypes {
		if t.Shadowed {
			continue
		}
		relationships = append(relationships, compiledTypeMap(t, true))
	}
	if len(objects) == 0 && len(relationships) == 0 {
		return nil, errNothingToDerive
	}
	m := blueprintManifest{Packs: []blueprintPack{{
		Name:              name,
		Version:           version,
		Description:       description,
		ObjectTypes:       objects,
		RelationshipTypes: relationships,
	}}}
	return json.Marshal(m)
}

// compiledTypeMap converts one compiled type into the raw-map form a blueprint
// manifest expects. relationship selects source/target emission.
func compiledTypeMap(t CompiledType, relationship bool) map[string]any {
	out := map[string]any{"name": t.Name}
	if t.Label != "" {
		out["label"] = t.Label
	}
	if t.Description != "" {
		out["description"] = t.Description
	}
	if v, ok := rawJSONValue(t.Properties); ok {
		out["properties"] = v
	}
	if v, ok := rawJSONValue(t.UI); ok {
		out["ui"] = v
	}
	if relationship {
		if t.SourceType != "" {
			out["sourceType"] = t.SourceType
		}
		if t.TargetType != "" {
			out["targetType"] = t.TargetType
		}
	}
	return out
}

// rawJSONValue decodes a non-empty raw JSON payload into a generic value.
func rawJSONValue(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	return v, true
}

// --- blueprint provenance resolver (design D2) ---

// provenanceKey identifies a schema pack declared by an applied blueprint.
type provenanceKey struct{ name, version string }

// ProvenanceInfo describes the applied blueprint that owns a compiled type.
type ProvenanceInfo struct {
	BlueprintID      string
	BlueprintName    string
	BlueprintVersion string
	PackName         string
	PackVersion      string
	// Ambiguous is true when more than one applied blueprint claims the pack;
	// the first is shown and the collision logged.
	Ambiguous bool
}

// buildProvenanceIndex maps each pack (name+version) declared by an applied
// blueprint to that blueprint. Resolution is best-effort: an unreachable
// blueprint manifest simply contributes nothing. Two blueprints claiming the
// same pack keep the first and mark it ambiguous.
func (s *Server) buildProvenanceIndex(ctx context.Context) map[provenanceKey]*ProvenanceInfo {
	index := map[provenanceKey]*ProvenanceInfo{}
	applied, err := s.memory.ListAppliedBlueprints(ctx)
	if err != nil {
		return index
	}
	for _, ap := range applied {
		rec, err := s.memory.GetBlueprint(ctx, ap.BlueprintID)
		if err != nil || rec == nil {
			continue
		}
		var m blueprintManifest
		if json.Unmarshal(rec.Manifest, &m) != nil {
			continue
		}
		for _, p := range m.Packs {
			if p.Name == "" {
				continue
			}
			k := provenanceKey{name: p.Name, version: p.Version}
			if existing, ok := index[k]; ok {
				if existing.BlueprintID != rec.ID {
					existing.Ambiguous = true
					log.Printf("schema provenance: pack %s@%s claimed by blueprints %s and %s; showing %s",
						p.Name, p.Version, existing.BlueprintID, rec.ID, existing.BlueprintID)
				}
				continue
			}
			index[k] = &ProvenanceInfo{
				BlueprintID:      rec.ID,
				BlueprintName:    rec.Name,
				BlueprintVersion: rec.Version,
				PackName:         p.Name,
				PackVersion:      p.Version,
			}
		}
	}
	return index
}

// provenanceFor returns the owning blueprint for a compiled type, or nil when
// the type is project-authored or its schema name/version matches no applied
// blueprint (including a version mismatch).
func provenanceFor(index map[provenanceKey]*ProvenanceInfo, t CompiledType) *ProvenanceInfo {
	if t.SchemaName == "" {
		return nil
	}
	p, ok := index[provenanceKey{name: t.SchemaName, version: t.SchemaVersion}]
	if !ok {
		return nil
	}
	cp := *p
	return &cp
}

// provenanceLabel renders the owning-blueprint reference for a derived type.
func typeProvenanceLabel(p *ProvenanceInfo) string {
	if p == nil {
		return ""
	}
	label := p.BlueprintName
	if p.BlueprintVersion != "" {
		label += " v" + p.BlueprintVersion
	}
	if p.Ambiguous {
		label += " (ambiguous)"
	}
	return label
}

// --- schema write capability (task 2.4) ---

// requireSchemaWrite gates schema mutation routes. The default policy allows
// every gateway-authenticated caller because memory enforces the token's write
// scopes on the mutation endpoints; a deployment or test can set
// Server.schemaWritePolicy to reject a caller before any backend call is made.
func (s *Server) requireSchemaWrite(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.schemaWritePolicy != nil && !s.schemaWritePolicy(c) {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "schema write capability required"})
		}
		return next(c)
	}
}

// isMemoryConflict reports whether a memory client error is a 409-style
// duplicate/conflict response.
func isMemoryConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "409") || strings.Contains(strings.ToLower(err.Error()), "conflict")
}
