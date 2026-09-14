package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- view models ---

// SchemaListView is the Schema list view model: the compiled types plus the
// resolved blueprint provenance per object type (by type name). FlashMsg/
// FlashErr carry PRG feedback from install/derive actions.
type SchemaListView struct {
	Compiled   *CompiledSchemaTypes
	Provenance map[string]*ProvenanceInfo
	FlashMsg   string
	FlashErr   error
}

// SchemaObjectTypeView is the object-type detail view model: the type, its
// inbound relationships, resolved blueprint provenance, and the schema history
// for the type's owning pack.
type SchemaObjectTypeView struct {
	Detail        *ObjectTypeDetail
	Relationships []RelationshipTypeDetail
	Icon          string
	Color         string
	Provenance    *ProvenanceInfo
	SchemaName    string
	SchemaVersion string
	History       []SchemaHistoryItem
	HistoryErr    bool
	FlashMsg      string
	FlashErr      error
}

// SchemaAddView is the "Add schema" view model: the installable pack list.
type SchemaAddView struct {
	Available []AvailableSchemaItem
	Err       error
}

// SchemaEditView is the object-type editor view model. Detail == nil means the
// type could not be resolved (unknown-type state). Icon/Color carry the
// type-level ui accent so the editor can prefill them. SaveErr is a non-fatal
// save failure rendered above the form with the user's edited values preserved;
// Err is a fatal load failure.
type SchemaEditView struct {
	Detail     *ObjectTypeDetail
	Provenance *ProvenanceInfo
	Icon       string
	Color      string
	SaveErr    error
	Err        error
}

// --- Schema browser handlers ---

// uiSchema renders the compiled schema browser: object and relationship types
// merged across active blueprint installs, with blueprint provenance resolved
// per object type.
func (s *Server) uiSchema(c echo.Context) error {
	ctx := c.Request().Context()
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return s.page(c, pageTitle("Schema"), SchemaPage(nil, err))
	}
	visible := &CompiledSchemaTypes{
		ObjectTypes:       visibleCompiledTypes(compiled.ObjectTypes),
		RelationshipTypes: visibleCompiledTypes(compiled.RelationshipTypes),
	}
	sortCompiledTypesByName(visible.ObjectTypes)
	sortCompiledTypesByName(visible.RelationshipTypes)
	index := s.buildProvenanceIndex(ctx)
	prov := map[string]*ProvenanceInfo{}
	for _, t := range visible.ObjectTypes {
		if p := provenanceFor(index, t); p != nil {
			prov[t.Name] = p
		}
	}
	var flashMsg string
	if c.QueryParam("installed") != "" {
		flashMsg = "Schema installed."
	}
	return s.page(c, pageTitle("Schema"), SchemaPage(&SchemaListView{
		Compiled:   visible,
		Provenance: prov,
		FlashMsg:   flashMsg,
		FlashErr:   flashError(c),
	}, nil))
}

// visibleCompiledTypes drops shadowed (losing) duplicates from a compiled-type
// slice, so the browser renders only the effective winner per name. Order is
// preserved; an all-shadowed input yields an empty slice.
func visibleCompiledTypes(types []CompiledType) []CompiledType {
	out := make([]CompiledType, 0, len(types))
	for _, t := range types {
		if !t.Shadowed {
			out = append(out, t)
		}
	}
	return out
}

// sortCompiledTypesByName sorts compiled types by name (case-insensitive,
// stable) so the schema browser renders a deterministic alphabetical list.
func sortCompiledTypesByName(types []CompiledType) {
	sort.SliceStable(types, func(i, j int) bool {
		return strings.ToLower(types[i].Name) < strings.ToLower(types[j].Name)
	})
}

// uiSchemaObjectType renders one compiled object type: its description,
// properties, resolved blueprint provenance, schema history, and the
// relationship types that reference it (as source or target).
func (s *Server) uiSchemaObjectType(c echo.Context) error {
	ctx := c.Request().Context()
	name := pathUnescape(c.Param("name"))
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return s.page(c, pageTitle("Object type"), SchemaObjectTypePage(nil, err))
	}
	found := findCompiledObjectType(compiled, name)
	if found == nil {
		return s.page(c, pageTitle("Object type"), SchemaObjectTypePage(nil, fmt.Errorf("object type %q not found", name)))
	}
	view := &SchemaObjectTypeView{
		Detail: &ObjectTypeDetail{
			Name:        found.Name,
			Label:       found.Label,
			Description: found.Description,
			Properties:  compiledProperties(found.Properties),
		},
		Icon:          compiledTypeIcon(*found),
		Color:         compiledTypeColor(*found),
		SchemaName:    found.SchemaName,
		SchemaVersion: found.SchemaVersion,
	}
	for _, r := range compiled.RelationshipTypes {
		if r.SourceType == name || r.TargetType == name {
			view.Relationships = append(view.Relationships, RelationshipTypeDetail{
				Name:        r.Name,
				Label:       r.Label,
				Description: r.Description,
				SourceType:  r.SourceType,
				TargetType:  r.TargetType,
			})
		}
	}
	sortRelationshipDetailsByName(view.Relationships)
	view.Provenance = provenanceFor(s.buildProvenanceIndex(ctx), *found)
	s.attachSchemaHistory(ctx, view, found.SchemaID)
	if c.QueryParam("updated") != "" {
		view.FlashMsg = "Object type saved."
	}
	view.FlashErr = flashError(c)
	return s.page(c, pageTitle(objectTypeLabel(view.Detail)), SchemaObjectTypePage(view, nil))
}

// attachSchemaHistory resolves the type's schema history (best-effort): a
// failed history call marks the view unavailable without hiding the type.
func (s *Server) attachSchemaHistory(ctx context.Context, view *SchemaObjectTypeView, schemaID string) {
	if schemaID == "" {
		return
	}
	history, err := s.memory.GetSchemaHistory(ctx)
	if err != nil {
		view.HistoryErr = true
		return
	}
	view.History = filterHistoryBySchema(history, schemaID)
}

// filterHistoryBySchema keeps only the history entries for one schema id.
func filterHistoryBySchema(items []SchemaHistoryItem, schemaID string) []SchemaHistoryItem {
	var out []SchemaHistoryItem
	for _, it := range items {
		if it.SchemaID == schemaID {
			out = append(out, it)
		}
	}
	return out
}

// findCompiledObjectType returns the named compiled object type, preferring the
// effective (non-shadowed) winner when a project-owned override pack shadows a
// blueprint pack of the same type name. Falls back to the shadowed entry only
// when no winner exists.
func findCompiledObjectType(compiled *CompiledSchemaTypes, name string) *CompiledType {
	if compiled == nil {
		return nil
	}
	var fallback *CompiledType
	for i := range compiled.ObjectTypes {
		if compiled.ObjectTypes[i].Name != name {
			continue
		}
		if !compiled.ObjectTypes[i].Shadowed {
			return &compiled.ObjectTypes[i]
		}
		if fallback == nil {
			fallback = &compiled.ObjectTypes[i]
		}
	}
	return fallback
}

// pathUnescape decodes a URL path parameter, falling back to the raw value.
func pathUnescape(v string) string {
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

// sortRelationshipDetailsByName sorts relationship type details by name
// (case-insensitive, stable) for the object-type detail page.
func sortRelationshipDetailsByName(rels []RelationshipTypeDetail) {
	sort.SliceStable(rels, func(i, j int) bool {
		return strings.ToLower(rels[i].Name) < strings.ToLower(rels[j].Name)
	})
}

// compiledTypeLabel is the display label of a compiled type, falling back to
// the machine name when no label is set.
func compiledTypeLabel(t CompiledType) string {
	if t.Label != "" {
		return t.Label
	}
	return t.Name
}

// objectTypeLabel is the display label of an object type detail, falling back
// to the machine name when no label is set.
func objectTypeLabel(d *ObjectTypeDetail) string {
	if d == nil {
		return ""
	}
	if d.Label != "" {
		return d.Label
	}
	return d.Name
}

// compiledProperties decodes a compiled type's raw properties JSON (a map of
// propName → {type, description, required, ...}) into the shared detail form.
func compiledProperties(raw json.RawMessage) []PropertyDetail {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return propertiesFromMap(m)
}

// compiledTypeCountLabel renders the total compiled type count with correct
// pluralisation ("3 types" / "1 type").
func compiledTypeCountLabel(compiled *CompiledSchemaTypes) string {
	if compiled == nil {
		return "0 types"
	}
	return countLabel(len(compiled.ObjectTypes)+len(compiled.RelationshipTypes), "type", "types")
}

// compiledTypeSchemaLabel renders a type's source schema, e.g.
// "personal-memory v1.0.0", or "—" when the schema name is unknown.
func compiledTypeSchemaLabel(t CompiledType) string {
	if t.SchemaName == "" {
		return "—"
	}
	if t.SchemaVersion != "" {
		return t.SchemaName + " v" + t.SchemaVersion
	}
	return t.SchemaName
}

// schemaOriginLabel renders a schema name/version pair for the object-type
// origin line, or "" when no schema name is known.
func schemaOriginLabel(name, version string) string {
	if name == "" {
		return ""
	}
	if version != "" {
		return name + " v" + version
	}
	return name
}

// propertyTypeOptions is the editor's fixed set of property type values. It
// mirrors the types the memory schema registry accepts and drives both the
// existing rows and the add-row template so they never drift.
func propertyTypeOptions() []string {
	return []string{"string", "number", "boolean", "date", "array", "object"}
}

// propertyWidgetOption is one choice in the editor's per-property widget select.
type propertyWidgetOption struct {
	Value string
	Label string
}

// propertyWidgetOptions is the editor's fixed set of widget hints. An empty
// value means "auto": the object editor picks the widget from the property's
// data type (see propertyInput).
func propertyWidgetOptions() []propertyWidgetOption {
	return []propertyWidgetOption{
		{Value: "", Label: "Auto"},
		{Value: "input", Label: "Single line"},
		{Value: "textarea", Label: "Text area"},
	}
}

// schemaColorPresets is the object-type editor's quick-pick accent palette for
// the color field. It seeds ui.ColorPicker with a small, tasteful spread so a
// type can be accented without typing a hex value; the paired text input still
// accepts any CSS color.
var schemaColorPresets = []string{
	"#4F46E5", // indigo
	"#7C3AED", // violet
	"#0EA5E9", // sky
	"#10B981", // emerald
	"#F59E0B", // amber
	"#EF4444", // red
	"#EC4899", // pink
	"#64748B", // slate
}

// compiledTypeIcon returns the schema-declared UI icon for a type (from its
// optional {"ui":{"icon":...}} block), or "" when none is declared. Icon
// values are free-form (iconify names, emoji, …) — consumers decide how to
// render them; empty means "use the default box icon".
func compiledTypeIcon(t CompiledType) string {
	return compiledTypeUIValue(t, "icon")
}

// compiledTypeColor returns the schema-declared UI color for a type (from its
// optional {"ui":{"color":...}} block), or "" when none is declared.
func compiledTypeColor(t CompiledType) string {
	return compiledTypeUIValue(t, "color")
}

// compiledTypeUIValue reads one key from a compiled type's raw ui block.
func compiledTypeUIValue(t CompiledType, key string) string {
	if len(t.UI) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(t.UI, &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// --- "Add schema" handlers (design D6) ---

// uiSchemaAdd renders the Schema-area "Add schema" action: the installable pack
// list (registry + bundled) with one-action install.
func (s *Server) uiSchemaAdd(c echo.Context) error {
	ctx := c.Request().Context()
	schemas, err := s.memory.ListAllSchemas(ctx)
	if err != nil {
		// A registry failure still lets bundled packs surface.
		captureError(err)
		schemas = nil
	}
	applied, err := s.memory.ListAppliedBlueprints(ctx)
	captureError(err)
	bundled, _ := s.bundledBlueprints()
	return s.page(c, pageTitle("Add schema"), SchemaAddPage(&SchemaAddView{
		Available: buildAvailableList(schemas, bundled, applied, s.existingAgents(ctx)),
		Err:       flashError(c),
	}))
}

// uiSchemaInstall installs the selected pack in place, reusing the existing
// install path (registry schema id or bundled pack name).
func (s *Server) uiSchemaInstall(c echo.Context) error {
	ctx := c.Request().Context()
	schemaID := c.FormValue("schemaId")
	name := c.FormValue("name")
	var err error
	switch {
	case schemaID != "":
		_, err = s.installRegistryBlueprint(ctx, schemaID)
	case name != "":
		_, err = s.installBundledBlueprint(ctx, name)
	default:
		return c.Redirect(http.StatusSeeOther, "/schema/add?err=1")
	}
	if err != nil {
		return redirectWithError(c, "/schema/add", err)
	}
	return c.Redirect(http.StatusSeeOther, "/schema?installed=1")
}

// --- object-type editor handlers ---

// uiSchemaObjectTypeEdit renders the object-type editor (immutable name,
// editable description and properties) prefilled from the effective type.
func (s *Server) uiSchemaObjectTypeEdit(c echo.Context) error {
	ctx := c.Request().Context()
	name := pathUnescape(c.Param("name"))
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return s.page(c, pageTitle("Edit object type"), SchemaObjectTypeEditPage(&SchemaEditView{Err: err}))
	}
	found := findCompiledObjectType(compiled, name)
	if found == nil {
		return s.page(c, pageTitle("Edit object type"), SchemaObjectTypeEditPage(&SchemaEditView{Err: fmt.Errorf("object type %q not found", name)}))
	}
	view := &SchemaEditView{
		Detail: &ObjectTypeDetail{
			Name:        found.Name,
			Label:       found.Label,
			Description: found.Description,
			Properties:  compiledProperties(found.Properties),
		},
		Icon:       compiledTypeIcon(*found),
		Color:      compiledTypeColor(*found),
		Provenance: provenanceFor(s.buildProvenanceIndex(ctx), *found),
	}
	return s.page(c, pageTitle("Edit "+name), SchemaObjectTypeEditPage(view))
}

// uiSchemaObjectTypeUpdate persists an object-type edit. A project-authored
// type writes through to its owning pack; a blueprint-derived type is copied
// into the project-owned override pack instead (design D1). The type name is
// immutable.
//
// The editor form is distinct from the JSON API: it posts _ui=1 and, on a
// validation or backend error, gets the editor back (200) with the submitted
// values preserved and an inline error — never a raw JSON body. The JSON API
// path keeps the existing status-code contract.
func (s *Server) uiSchemaObjectTypeUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	name := pathUnescape(c.Param("name"))
	if err := c.Request().ParseForm(); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid form"})
	}
	uiForm := isHTMLForm(c)

	edit := objectTypeEditFromForm(name, c)
	if err := validateObjectTypeEdit(edit, c); err != nil {
		if uiForm {
			return s.renderEditorError(c, name, err)
		}
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		if uiForm {
			return s.renderEditorError(c, name, err)
		}
		return blueprintMemoryError(c, err)
	}
	found := findCompiledObjectType(compiled, name)
	if found == nil {
		if uiForm {
			return s.page(c, pageTitle("Edit object type"), SchemaObjectTypeEditPage(&SchemaEditView{Err: fmt.Errorf("object type %q not found", name)}))
		}
		return c.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("object type %q not found", name)})
	}
	if prov := provenanceFor(s.buildProvenanceIndex(ctx), *found); prov != nil {
		if err := s.upsertOverrideType(ctx, edit); err != nil {
			if uiForm {
				return s.renderEditorError(c, name, err)
			}
			return blueprintMemoryError(c, err)
		}
	} else {
		if found.SchemaID == "" {
			err := fmt.Errorf("object type has no project-owned pack to edit")
			if uiForm {
				return s.renderEditorError(c, name, err)
			}
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := s.memory.UpdateSchemaPack(ctx, found.SchemaID, edit); err != nil {
			if uiForm {
				return s.renderEditorError(c, name, err)
			}
			return blueprintMemoryError(c, err)
		}
	}
	return c.Redirect(http.StatusSeeOther, "/schema/object-types/"+url.PathEscape(name)+"?updated=1")
}

// renderEditorError re-renders the object-type editor with the submitted values
// preserved and err shown above the form. Provenance is re-resolved so the
// derived-type confirmation gate is still armed on retry.
func (s *Server) renderEditorError(c echo.Context, name string, failure error) error {
	ctx := c.Request().Context()
	view := &SchemaEditView{
		Detail:  objectTypeDetailFromForm(name, c),
		Icon:    c.FormValue("icon"),
		Color:   c.FormValue("color"),
		SaveErr: failure,
	}
	if compiled, err := s.memory.GetCompiledTypes(ctx); err == nil {
		if found := findCompiledObjectType(compiled, name); found != nil {
			view.Provenance = provenanceFor(s.buildProvenanceIndex(ctx), *found)
		}
	}
	return s.page(c, pageTitle("Edit "+name), SchemaObjectTypeEditPage(view))
}

// objectTypeDetailFromForm rebuilds the editor's detail view from the submitted
// form, preserving the row order the user saw (unlike the map-backed compiled
// form, which sorts). Blank property names are skipped.
func objectTypeDetailFromForm(name string, c echo.Context) *ObjectTypeDetail {
	return &ObjectTypeDetail{
		Name:        name,
		Description: c.FormValue("description"),
		Properties:  objectTypePropertiesFromForm(c),
	}
}

// objectTypePropertiesFromForm reads the editor's parallel property arrays into
// order-preserving property details. Blank property names are skipped.
func objectTypePropertiesFromForm(c echo.Context) []PropertyDetail {
	form := c.Request().Form
	names := form["property_name"]
	types := form["property_type"]
	widgets := form["property_widget"]
	descriptions := form["property_description"]
	required := form["property_required"]
	out := make([]PropertyDetail, 0, len(names))
	for i, raw := range names {
		pn := strings.TrimSpace(raw)
		if pn == "" {
			continue
		}
		out = append(out, PropertyDetail{
			Name:        pn,
			Type:        formValueAt(types, i),
			Widget:      formValueAt(widgets, i),
			Description: formValueAt(descriptions, i),
			Required:    formValueAt(required, i) != "",
		})
	}
	return out
}

// validateObjectTypeEdit rejects a malformed property definition: a blank type,
// an unknown widget hint, or two properties with the same name. The message
// names the offending row so the editor can show a precise inline error.
func validateObjectTypeEdit(edit ObjectTypeEdit, c echo.Context) error {
	seen := map[string]bool{}
	for _, p := range objectTypePropertiesFromForm(c) {
		if p.Type == "" {
			return fmt.Errorf("property %q needs a type", p.Name)
		}
		if !validPropertyWidget(p.Widget) {
			return fmt.Errorf("property %q has an unknown widget %q", p.Name, p.Widget)
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate property %q", p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

// validPropertyWidget reports whether a property's widget hint is one the
// object editor understands. Empty means "auto" (the type-driven default).
func validPropertyWidget(w string) bool {
	switch w {
	case "", "input", "textarea":
		return true
	default:
		return false
	}
}

// objectTypeEditFromForm builds an ObjectTypeEdit from the editor form's
// parallel property arrays. Blank property names are skipped.
func objectTypeEditFromForm(name string, c echo.Context) ObjectTypeEdit {
	form := c.Request().Form
	props := map[string]any{}
	names := form["property_name"]
	types := form["property_type"]
	widgets := form["property_widget"]
	descriptions := form["property_description"]
	required := form["property_required"]
	for i, pn := range names {
		pn = strings.TrimSpace(pn)
		if pn == "" {
			continue
		}
		p := map[string]any{"type": formValueAt(types, i)}
		if w := formValueAt(widgets, i); w != "" {
			p["widget"] = w
		}
		if d := formValueAt(descriptions, i); d != "" {
			p["description"] = d
		}
		if formValueAt(required, i) != "" {
			p["required"] = true
		}
		props[pn] = p
	}
	return ObjectTypeEdit{
		Name:        name,
		Description: c.FormValue("description"),
		Icon:        c.FormValue("icon"),
		Color:       c.FormValue("color"),
		Properties:  props,
	}
}

// isHTMLForm reports whether the request is a UI form submission (the editor
// and derive dialogs post _ui=1) as opposed to the JSON API. Callers must have
// parsed the form first.
func isHTMLForm(c echo.Context) bool {
	return c.Request().FormValue("_ui") != ""
}

// formValueAt returns values[i], or "" when the index is out of range.
func formValueAt(values []string, i int) string {
	if i >= 0 && i < len(values) {
		return values[i]
	}
	return ""
}

// --- blueprint derivation handlers (design D5) ---

// deriveBlueprintRequest is the body for deriving a new blueprint draft.
type deriveBlueprintRequest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// deriveBlueprintVersionRequest is the body for deriving a new version.
type deriveBlueprintVersionRequest struct {
	Version     string `json:"version"`
	Description string `json:"description"`
}

// deriveSchemaBlueprint creates a draft blueprint from the project's effective
// types (POST /schema/blueprints/derive). The JSON API returns 201; the Schema
// area form (posted with _ui=1) redirects to the blueprints gallery with the
// draft visible.
func (s *Server) deriveSchemaBlueprint(c echo.Context) error {
	_ = c.Request().ParseForm()
	if isHTMLForm(c) {
		return s.deriveSchemaBlueprintUI(c)
	}
	var in deriveBlueprintRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if in.Name == "" || in.Version == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name and version required"})
	}
	rec, err := s.deriveBlueprintDraft(c.Request().Context(), in.Name, in.Version, in.Description)
	if err != nil {
		return deriveError(c, err)
	}
	return c.JSON(http.StatusCreated, rec)
}

// deriveSchemaBlueprintUI is the Schema-area form path for a new blueprint.
func (s *Server) deriveSchemaBlueprintUI(c echo.Context) error {
	name := strings.TrimSpace(c.FormValue("name"))
	version := strings.TrimSpace(c.FormValue("version"))
	description := strings.TrimSpace(c.FormValue("description"))
	if name == "" || version == "" {
		return redirectWithError(c, "/schema", fmt.Errorf("name and version are required"))
	}
	rec, err := s.deriveBlueprintDraft(c.Request().Context(), name, version, description)
	if err != nil {
		return redirectWithError(c, "/schema", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?derived="+url.QueryEscape(draftLabel(rec, name)))
}

// deriveSchemaBlueprintVersion creates a new draft version of an existing
// blueprint from the project's effective types
// (POST /schema/blueprints/:id/versions/derive). The JSON API returns 201; the
// blueprints gallery form (posted with _ui=1) redirects back to the blueprint
// detail with a flash naming the new version.
func (s *Server) deriveSchemaBlueprintVersion(c echo.Context) error {
	_ = c.Request().ParseForm()
	if isHTMLForm(c) {
		return s.deriveSchemaBlueprintVersionUI(c)
	}
	var in deriveBlueprintVersionRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if in.Version == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "version required"})
	}
	rec, err := s.deriveBlueprintVersionDraft(c.Request().Context(), c.Param("id"), in.Version, in.Description)
	if err != nil {
		return deriveError(c, err)
	}
	return c.JSON(http.StatusCreated, rec)
}

// deriveSchemaBlueprintVersionUI is the gallery form path for a new version.
func (s *Server) deriveSchemaBlueprintVersionUI(c echo.Context) error {
	id := pathUnescape(c.Param("id"))
	version := strings.TrimSpace(c.FormValue("version"))
	description := strings.TrimSpace(c.FormValue("description"))
	back := "/blueprints/" + url.PathEscape(id)
	if version == "" {
		return redirectWithError(c, back, fmt.Errorf("a version label is required"))
	}
	rec, err := s.deriveBlueprintVersionDraft(c.Request().Context(), id, version, description)
	if err != nil {
		return redirectWithError(c, back, err)
	}
	return c.Redirect(http.StatusSeeOther, back+"?derived="+url.QueryEscape(draftLabel(rec, version)))
}

// draftLabel renders the derived draft's name@version for the redirect flash,
// falling back to the submitted values when the record omits them.
func draftLabel(rec *BlueprintRecord, fallback string) string {
	if rec != nil && rec.Name != "" && rec.Version != "" {
		return rec.Name + " " + rec.Version
	}
	return fallback
}

// deriveBlueprintDraft assembles the effective-type manifest and creates a
// draft blueprint from it.
func (s *Server) deriveBlueprintDraft(ctx context.Context, name, version, description string) (*BlueprintRecord, error) {
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return nil, err
	}
	manifest, err := buildDerivedManifest(name, version, description, compiled)
	if err != nil {
		return nil, err
	}
	return s.memory.CreateBlueprint(ctx, &createBlueprintRequest{
		Name:        name,
		Version:     version,
		Description: description,
		Manifest:    manifest,
	})
}

// deriveBlueprintVersionDraft forks a new draft version of an existing
// blueprint, then replaces its manifest with the project's effective types
// (design D5: new-version then manifest-replace).
func (s *Server) deriveBlueprintVersionDraft(ctx context.Context, baseID, version, description string) (*BlueprintRecord, error) {
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return nil, err
	}
	base, err := s.memory.GetBlueprint(ctx, baseID)
	if err != nil {
		return nil, err
	}
	packName := base.Name
	var bm blueprintManifest
	if json.Unmarshal(base.Manifest, &bm) == nil && len(bm.Packs) > 0 && bm.Packs[0].Name != "" {
		packName = bm.Packs[0].Name
	}
	manifest, err := buildDerivedManifest(packName, version, description, compiled)
	if err != nil {
		return nil, err
	}
	created, err := s.memory.CreateBlueprintVersion(ctx, baseID, &CreateBlueprintVersionRequest{
		Version:     version,
		Description: description,
	})
	if err != nil {
		return nil, err
	}
	newID := baseID
	if created != nil && created.ID != "" {
		newID = created.ID
	}
	return s.memory.UpdateBlueprint(ctx, newID, &UpdateBlueprintRequest{
		Name:        base.Name,
		Version:     version,
		Description: description,
		Manifest:    manifest,
	})
}

// deriveError maps a derivation failure onto its HTTP status.
func deriveError(c echo.Context, err error) error {
	if err != nil && strings.Contains(err.Error(), errNothingToDerive.Error()) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if isMemoryConflict(err) {
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}
	return blueprintMemoryError(c, err)
}
