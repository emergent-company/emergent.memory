package blueprints

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	sdkmodelref "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/modelref"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaicon"
	"github.com/robfig/cron/v3"
)

// ──────────────────────────────────────────────
// Validation result types
// ──────────────────────────────────────────────

// ValidationSeverity indicates how serious a finding is.
type ValidationSeverity string

const (
	ValidationError   ValidationSeverity = "error"
	ValidationWarning ValidationSeverity = "warning"
	// ValidationInfo is advisory: it flags a valid configuration whose runtime
	// consequence an operator should know about. It never fails validate, even
	// with --strict (#321).
	ValidationInfo ValidationSeverity = "info"
)

// ValidationIssue is a single finding produced by Validate.
type ValidationIssue struct {
	Severity     ValidationSeverity
	ResourceType string // "pack", "agent", "skill", "seed_object", "seed_relationship"
	Name         string // resource name (may be empty for file-level issues)
	SourceFile   string // file the issue was found in
	Field        string // field path, e.g. "objectTypes[1].name"
	Message      string
}

func (v ValidationIssue) String() string {
	loc := v.SourceFile
	if v.Field != "" {
		loc += " → " + v.Field
	}
	name := ""
	if v.Name != "" {
		name = fmt.Sprintf(" %q", v.Name)
	}
	return fmt.Sprintf("[%s] %s%s: %s (%s)", v.Severity, v.ResourceType, name, v.Message, loc)
}

// ValidationReport is the full output of Validate.
type ValidationReport struct {
	Issues []ValidationIssue
}

// HasErrors returns true when any issue has severity Error.
func (r *ValidationReport) HasErrors() bool {
	for _, i := range r.Issues {
		if i.Severity == ValidationError {
			return true
		}
	}
	return false
}

// Errors returns only error-severity issues.
func (r *ValidationReport) Errors() []ValidationIssue {
	var out []ValidationIssue
	for _, i := range r.Issues {
		if i.Severity == ValidationError {
			out = append(out, i)
		}
	}
	return out
}

// Warnings returns only warning-severity issues.
func (r *ValidationReport) Warnings() []ValidationIssue {
	var out []ValidationIssue
	for _, i := range r.Issues {
		if i.Severity == ValidationWarning {
			out = append(out, i)
		}
	}
	return out
}

// Infos returns only info-severity issues.
func (r *ValidationReport) Infos() []ValidationIssue {
	var out []ValidationIssue
	for _, i := range r.Issues {
		if i.Severity == ValidationInfo {
			out = append(out, i)
		}
	}
	return out
}

// ──────────────────────────────────────────────
// Validate — entry point
// ──────────────────────────────────────────────

// Validate runs a full structural and cross-reference validation over a loaded
// blueprint directory. It expects the same values returned by LoadDir.
//
// It is purely offline — no API calls are made. It validates:
//   - Pack: required fields, per-type required fields, cross-references
//     (a relationship's sourceType/targetType must name an objectType defined by
//     some pack in the blueprint; an endpoint may be owned by a different pack
//     than the one declaring the relationship, and the install gate
//     PreflightWithProjectTypes additionally accepts types already present in
//     the project, #1853), duplicate names within a pack, duplicate pack names
//     across files.
//   - Agent: required fields, enum values (visibility, dispatchMode),
//     model.name required when model block is present, duplicate agent names.
//   - Skill: required fields, non-empty content body, duplicate skill names.
//   - Seed objects: type field required, warns on missing key, cross-references
//     type against loaded pack object types.
//   - Seed relationships: required fields, cross-references type against loaded
//     pack relationship types, srcKey/dstKey reference known seed object keys.
//   - Cross-file: warns when the blueprint is empty (nothing found).
func Validate(
	project *ProjectFile,
	packs []PackFile,
	agents []AgentFile,
	skills []SkillFile,
	objects []SeedObjectRecord,
	rels []SeedRelationshipRecord,
	loadErrors []BlueprintsResult,
) *ValidationReport {
	return validateWithProjectTypes(nil, project, packs, agents, skills, objects, rels, loadErrors)
}

// validateWithProjectTypes is the shared implementation behind Validate (offline
// — nil knownProjectObjTypes, so only the blueprint's own packs are known) and
// PreflightWithProjectTypes (the install gate, which supplies the object types
// already present in the project). A relationship endpoint is accepted when it
// names an object type defined by any pack in the blueprint OR one already in
// the project (#1853).
func validateWithProjectTypes(
	knownProjectObjTypes map[string]bool,
	project *ProjectFile,
	packs []PackFile,
	agents []AgentFile,
	skills []SkillFile,
	objects []SeedObjectRecord,
	rels []SeedRelationshipRecord,
	loadErrors []BlueprintsResult,
) *ValidationReport {
	r := &ValidationReport{}

	// Surface load-time parse errors as validation errors.
	for _, le := range loadErrors {
		if le.Action == BlueprintsActionError {
			r.add(ValidationError, le.ResourceType, le.Name, le.SourceFile, "", le.Error.Error())
		}
	}

	// Empty blueprint warning.
	if project == nil && len(packs) == 0 && len(agents) == 0 && len(skills) == 0 && len(objects) == 0 && len(rels) == 0 {
		r.add(ValidationWarning, "blueprint", "", "", "", "no blueprint files found — directory appears to be empty")
		return r
	}

	// Validate each resource type.
	r.validatePacks(packs, knownProjectObjTypes)
	r.validateAgents(agents)
	r.validateSkills(skills)
	r.validateSeedObjects(objects, packs)
	r.validateSeedRelationships(rels, packs, objects)

	return r
}

// ──────────────────────────────────────────────
// Pack validation
// ──────────────────────────────────────────────

func (r *ValidationReport) validatePacks(packs []PackFile, knownProjectObjTypes map[string]bool) {
	seen := make(map[string]string) // name → sourceFile

	// Relationship endpoints may name an object type owned by any pack in the
	// same blueprint, not only the pack that declares the relationship type
	// (#1853), or a type already present in the project. The server accepts any
	// non-empty endpoint, so validate against the union of every loaded pack's
	// object types plus the project's known types rather than one pack in
	// isolation. Offline Validate passes a nil project set (blueprint only).
	knownObjTypes := buildKnownObjectTypes(packs)
	for name := range knownProjectObjTypes {
		if name != "" {
			knownObjTypes[name] = true
		}
	}
	// Scope phrase for the "does not match" message: the install gate knows the
	// project's types, the offline validator only knows the blueprint.
	scope := "the blueprint"
	if len(knownProjectObjTypes) > 0 {
		scope = "the blueprint or the project"
	}
	// A ui.view section names relationship types, so it is checked against every
	// relationship type the blueprint declares — not only this pack's, since a
	// pack may present a relationship another pack in the same blueprint owns.
	knownRelTypes := buildKnownRelationshipTypes(packs)

	for _, p := range packs {
		// Duplicate pack names across files.
		if prev, ok := seen[p.Name]; ok {
			r.add(ValidationError, "pack", p.Name, p.SourceFile, "name",
				fmt.Sprintf("duplicate pack name (also defined in %s)", prev))
		} else {
			seen[p.Name] = p.SourceFile
		}

		// name required (already checked by loader, but belt-and-suspenders).
		if p.Name == "" {
			r.add(ValidationError, "pack", "", p.SourceFile, "name", "name is required")
		}

		// version required.
		if p.Version == "" {
			r.add(ValidationError, "pack", p.Name, p.SourceFile, "version", "version is required")
		}

		// At least one object type.
		if len(p.ObjectTypes) == 0 {
			r.add(ValidationError, "pack", p.Name, p.SourceFile, "objectTypes", "must define at least one objectType")
		}

		// Decode the pack-level uiConfigs once. Malformed uiConfigs is a
		// warning: the server tolerates it and falls back to empty UI metadata,
		// while --strict opts in to enforcing a well-formed block.
		packUIConfigs, uiErr := decodePackUIConfigs(p)
		if uiErr != nil {
			r.add(ValidationWarning, "pack", p.Name, p.SourceFile, "uiConfigs", uiErr.Error())
		}

		// Validate each object type.
		dupObjNames := make(map[string]bool)
		declaredObjTypes := make(map[string]bool, len(p.ObjectTypes))
		for i, ot := range p.ObjectTypes {
			field := fmt.Sprintf("objectTypes[%d]", i)
			if ot.Name == "" {
				r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".name", "name is required")
			} else {
				if dupObjNames[ot.Name] {
					r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".name",
						fmt.Sprintf("duplicate objectType name %q", ot.Name))
				}
				dupObjNames[ot.Name] = true
				declaredObjTypes[ot.Name] = true
			}
			if ot.Label == "" {
				r.add(ValidationWarning, "pack", p.Name, p.SourceFile, field+".label",
					fmt.Sprintf("objectType %q has no label — UI will fall back to name", ot.Name))
			}
			if len(ot.Labels) == 0 {
				r.add(ValidationWarning, "pack", p.Name, p.SourceFile, field+".labels",
					fmt.Sprintf("objectType %q has no labels — search/list grouping may be less useful", ot.Name))
			}

			ui, uiField := effectiveObjectUI(ot, packUIConfigs, field)
			if len(ui) == 0 {
				r.add(ValidationWarning, "pack", p.Name, p.SourceFile, field+".ui",
					fmt.Sprintf("objectType %q has no ui metadata — set ui.icon and ui.color (or a pack uiConfigs entry)", ot.Name))
			} else if len(ot.UI) > 0 {
				// A pack uiConfigs fallback is validated once in the pack-level
				// loop below, so it is not reported twice.
				r.validateUIConfig(p.Name, p.SourceFile, uiField, ot.Name, ui, knownRelTypes)
			}

			r.validateProperties(p.Name, p.SourceFile, field+".properties", fmt.Sprintf("objectType %q", ot.Name), ot.Properties)

			if len(ot.Required) > 0 {
				for j, req := range ot.Required {
					if _, ok := ot.Properties[req]; !ok {
						r.add(ValidationWarning, "pack", p.Name, p.SourceFile,
							fmt.Sprintf("%s.required[%d]", field, j),
							fmt.Sprintf("objectType %q: required property %q is not declared in properties", ot.Name, req))
					}
				}
			}

			r.validateBehaviorBlock(p.Name, p.SourceFile, field+".embedding", ot.Name, "embedding", ot.Embedding)
			r.validateBehaviorBlock(p.Name, p.SourceFile, field+".extraction", ot.Name, "extraction", ot.Extraction)
		}

		// Validate pack-level uiConfigs entries: each is validated like an
		// inline ui block, and a key naming no declared object type is a
		// warning (the server otherwise stores it unused).
		if uiErr == nil {
			for _, typeName := range sortedMapKeys(packUIConfigs) {
				cfg := packUIConfigs[typeName]
				if len(cfg) == 0 {
					continue
				}
				if !declaredObjTypes[typeName] {
					r.add(ValidationWarning, "pack", p.Name, p.SourceFile, "uiConfigs."+typeName,
						fmt.Sprintf("uiConfigs entry names %q, which is not an objectType declared by this pack", typeName))
				}
				r.validateUIConfig(p.Name, p.SourceFile, "uiConfigs."+typeName, typeName, cfg, knownRelTypes)
			}
		}

		// Validate each relationship type.
		// Uniqueness key is (name, sourceType) — the same name may appear multiple times
		// when sourceTypes differ (e.g. belongs_to with sourceType=Scenario and sourceType=Module).
		// A plain duplicate (same name AND same sourceType) is still an error.
		dupRelKeys := make(map[string]bool) // key: "name\x00sourceType"
		for i, rt := range p.RelationshipTypes {
			field := fmt.Sprintf("relationshipTypes[%d]", i)
			if rt.Name == "" {
				r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".name", "name is required")
			}

			// Resolve effective source types: prefer sourceTypes[] over sourceType.
			srcTypes := rt.GetSourceTypes()
			if len(srcTypes) == 0 {
				r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".sourceType",
					fmt.Sprintf("relationshipType %q: sourceType (or sourceTypes) is required", rt.Name))
			} else {
				for _, src := range srcTypes {
					dupKey := rt.Name + "\x00" + src
					if dupRelKeys[dupKey] {
						r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".name",
							fmt.Sprintf("duplicate relationshipType name %q with sourceType %q", rt.Name, src))
					}
					dupRelKeys[dupKey] = true
					if len(knownObjTypes) > 0 && !knownObjTypes[src] {
						r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".sourceType",
							fmt.Sprintf("relationshipType %q: sourceType %q does not match any objectType in %s", rt.Name, src, scope))
					}
				}
			}

			// Resolve effective target types.
			tgtTypes := rt.GetTargetTypes()
			if len(tgtTypes) == 0 {
				r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".targetType",
					fmt.Sprintf("relationshipType %q: targetType (or targetTypes) is required", rt.Name))
			} else {
				for _, tgt := range tgtTypes {
					if len(knownObjTypes) > 0 && !knownObjTypes[tgt] {
						r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".targetType",
							fmt.Sprintf("relationshipType %q: targetType %q does not match any objectType in %s", rt.Name, tgt, scope))
					}
				}
			}

			if rt.Label == "" {
				r.add(ValidationWarning, "pack", p.Name, p.SourceFile, field+".label",
					fmt.Sprintf("relationshipType %q has no label — UI will fall back to name", rt.Name))
			}

			r.validateProperties(p.Name, p.SourceFile, field+".properties", fmt.Sprintf("relationshipType %q", rt.Name), rt.Properties)

			// Mirror the object-side check: a required name the type does not
			// declare is stored but never enforced, so the author should hear
			// about it while the pack is still being written.
			if len(rt.Required) > 0 {
				for j, req := range rt.Required {
					if _, ok := rt.Properties[req]; !ok {
						r.add(ValidationWarning, "pack", p.Name, p.SourceFile,
							fmt.Sprintf("%s.required[%d]", field, j),
							fmt.Sprintf("relationshipType %q: required property %q is not declared in properties", rt.Name, req))
					}
				}
			}
		}

		r.validatePackMigrations(p)
	}
}

// validatePackMigrations checks the pack's migration-hint block against the
// types and properties the pack itself declares. The server enforces these
// rules on install; checking them offline means an author sees a rename that
// cannot apply ("to" names a type that does not exist) before pushing, instead
// of in a 400 from the API. A rename's `from` is the previous version's name,
// so only `to`/`type_name` are checked here.
func (r *ValidationReport) validatePackMigrations(p PackFile) {
	hints := p.Migrations
	if hints == nil {
		return
	}
	if strings.TrimSpace(hints.FromVersion) == "" {
		r.add(ValidationError, "pack", p.Name, p.SourceFile, "migrations.from_version",
			"from_version is required when a migrations block is present")
	}

	objProps := make(map[string]map[string]bool, len(p.ObjectTypes))
	for _, ot := range p.ObjectTypes {
		props := make(map[string]bool, len(ot.Properties))
		for name := range ot.Properties {
			props[name] = true
		}
		objProps[ot.Name] = props
	}
	relNames := make(map[string]bool, len(p.RelationshipTypes))
	for _, rt := range p.RelationshipTypes {
		relNames[rt.Name] = true
	}
	typeExists := func(name string) bool {
		_, isObject := objProps[name]
		return isObject || relNames[name]
	}

	for i, tr := range hints.TypeRenames {
		field := fmt.Sprintf("migrations.type_renames[%d]", i)
		if tr.From == "" || tr.To == "" {
			r.add(ValidationError, "pack", p.Name, p.SourceFile, field, "from and to are both required")
			continue
		}
		if !typeExists(tr.To) {
			r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".to",
				fmt.Sprintf("type %q does not exist in this pack", tr.To))
		}
	}
	for i, pr := range hints.PropertyRenames {
		field := fmt.Sprintf("migrations.property_renames[%d]", i)
		props, ok := objProps[pr.TypeName]
		if !ok {
			r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".type_name",
				fmt.Sprintf("type %q does not exist in this pack", pr.TypeName))
			continue
		}
		if pr.From == "" || pr.To == "" {
			r.add(ValidationError, "pack", p.Name, p.SourceFile, field, "from and to are both required")
			continue
		}
		if !props[pr.To] {
			r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".to",
				fmt.Sprintf("property %q does not exist in type %q", pr.To, pr.TypeName))
		}
	}
	for i, rp := range hints.RemovedProperties {
		field := fmt.Sprintf("migrations.removed_properties[%d]", i)
		if !typeExists(rp.TypeName) {
			r.add(ValidationWarning, "pack", p.Name, p.SourceFile, field+".type_name",
				fmt.Sprintf("type %q does not exist in this pack", rp.TypeName))
		}
	}
}

// ──────────────────────────────────────────────
// Object/relationship metadata validation
// ──────────────────────────────────────────────

// validPropertyTypes is the canonical set of property types the schemas
// registry and UI understand. Unknown types are a warning rather than an error:
// the server's coercion is deliberately lenient, and install must not regress
// for packs that were accepted before this check existed. --strict opts in to
// enforcing the canonical vocabulary.
var validPropertyTypes = map[string]bool{
	"string": true, "number": true, "integer": true, "boolean": true,
	"date": true, "time": true, "array": true, "object": true,
	"url": true, "email": true, "phone": true, "country": true, "currency": true,
	"md5": true, "sha1": true, "sha256": true, "sha512": true,
}

// stringPropertyTypes are the string-backed property types. They accept string
// enum values and are the types where a string-shaped declaration makes sense.
var stringPropertyTypes = map[string]bool{
	"string": true, "date": true, "time": true, "url": true,
	"email": true, "phone": true, "country": true, "currency": true,
	"md5": true, "sha1": true, "sha256": true, "sha512": true,
}

// decodePackUIConfigs decodes a pack's uiConfigs blob (a map of type name to
// ui block). A JSON null is treated as absent, matching the server's handling.
func decodePackUIConfigs(p PackFile) (map[string]map[string]any, error) {
	raw := p.UIConfigs.Raw()
	if len(raw) == 0 {
		return nil, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("uiConfigs must be an object keyed by object type: %w", err)
	}
	out := make(map[string]map[string]any, len(entries))
	for name, entry := range entries {
		trimmedEntry := strings.TrimSpace(string(entry))
		if trimmedEntry == "" || trimmedEntry == "null" {
			continue
		}
		var cfg map[string]any
		if err := json.Unmarshal(entry, &cfg); err != nil {
			return nil, fmt.Errorf("uiConfigs[%q] must be an object with optional icon/color: %w", name, err)
		}
		out[name] = cfg
	}
	return out, nil
}

// effectiveObjectUI returns the ui block the UI will use for a type: the inline
// objectTypes[].ui block when present, otherwise the pack uiConfigs entry named
// after the type. The field label points at whichever source won so findings
// stay actionable.
func effectiveObjectUI(ot ObjectTypeDef, packUIConfigs map[string]map[string]any, field string) (map[string]any, string) {
	if len(ot.UI) > 0 {
		return ot.UI, field + ".ui"
	}
	if cfg, ok := packUIConfigs[ot.Name]; ok && len(cfg) > 0 {
		return cfg, "uiConfigs." + ot.Name
	}
	return nil, field + ".ui"
}

// validateUIConfig checks an inline or pack-level ui block. Missing or
// non-renderable style metadata is a warning, never an error: the server and UI
// both fall back to defaults.
func (r *ValidationReport) validateUIConfig(packName, sourceFile, field, typeName string, ui map[string]any, knownRelTypes map[string]bool) {
	iconRaw, hasIcon := ui["icon"]
	icon, iconIsString := iconRaw.(string)
	switch {
	case !hasIcon:
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".icon",
			fmt.Sprintf("type %q has no ui.icon — UI will use the default box", typeName))
	case !iconIsString:
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".icon",
			fmt.Sprintf("type %q ui.icon must be a string", typeName))
	case strings.TrimSpace(icon) == "":
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".icon",
			fmt.Sprintf("type %q has no ui.icon — UI will use the default box", typeName))
	case !schemaicon.IsRenderable(icon):
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".icon",
			fmt.Sprintf("type %q ui.icon %q is not in the supported icon catalog — it will render as a generic box", typeName, icon))
	}

	colorRaw, hasColor := ui["color"]
	color, colorIsString := colorRaw.(string)
	switch {
	case !hasColor:
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".color",
			fmt.Sprintf("type %q has no ui.color — UI will use the default accent", typeName))
	case !colorIsString:
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".color",
			fmt.Sprintf("type %q ui.color must be a string", typeName))
	case strings.TrimSpace(color) == "":
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".color",
			fmt.Sprintf("type %q has no ui.color — UI will use the default accent", typeName))
	case !isHexColor(color):
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".color",
			fmt.Sprintf("type %q ui.color %q is not a hex color (#RGB, #RRGGBB, or #RRGGBBAA)", typeName, color))
	}

	if view, ok := ui["view"]; ok {
		r.validateUIView(packName, sourceFile, field+".view", typeName, view, knownRelTypes)
	}
}

// uiViewKeys and uiViewSectionKeys are the closed vocabulary of a type's
// ui.view plan (the gateway parses it in apps/web-ui/gateway/ui_view.go with
// unknown fields rejected). A key outside the set is a typo that would make the
// whole view be ignored, which is silent at render time — so it is reported
// here, offline, where the author is looking.
var (
	uiViewKeys        = map[string]bool{"inline": true, "sections": true, "more": true}
	uiViewSectionKeys = map[string]bool{"title": true, "relation": true, "dir": true, "limit": true}
)

// uiViewMaxLimit mirrors the renderer's row cap; a declared limit above it is
// rejected there, so it must not pass validation here.
const uiViewMaxLimit = 50

// validateUIView checks a type's `ui.view` block: the closed key set, the
// shapes each key accepts, and the bounds the renderer enforces. Findings are
// warnings, matching the rest of the ui block — the renderer ignores a view it
// cannot parse and the page still works, so an install must not fail on it.
func (r *ValidationReport) validateUIView(packName, sourceFile, field, typeName string, raw any, knownRelTypes map[string]bool) {
	view, ok := toStringAnyMap(raw)
	if !ok {
		r.add(ValidationWarning, "pack", packName, sourceFile, field,
			fmt.Sprintf("type %q ui.view must be a mapping with inline/sections/more", typeName))
		return
	}
	for _, key := range sortedMapKeys(view) {
		if !uiViewKeys[key] {
			r.add(ValidationWarning, "pack", packName, sourceFile, field+"."+key,
				fmt.Sprintf("type %q ui.view: unknown key %q (known: inline, sections, more) — the whole view is ignored at render time", typeName, key))
		}
	}

	if inlineRaw, ok := view["inline"]; ok {
		names, ok := anySlice(inlineRaw)
		if !ok {
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".inline",
				fmt.Sprintf("type %q ui.view.inline must be a list of property names", typeName))
		} else {
			for i, name := range names {
				if _, ok := name.(string); !ok {
					r.add(ValidationWarning, "pack", packName, sourceFile, fmt.Sprintf("%s.inline[%d]", field, i),
						fmt.Sprintf("type %q ui.view.inline entries must be strings", typeName))
				}
			}
		}
	}

	if moreRaw, ok := view["more"]; ok {
		if _, ok := moreRaw.(bool); !ok {
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".more",
				fmt.Sprintf("type %q ui.view.more must be true or false", typeName))
		}
	}

	sectionsRaw, ok := view["sections"]
	if !ok {
		return
	}
	sections, ok := anySlice(sectionsRaw)
	if !ok {
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".sections",
			fmt.Sprintf("type %q ui.view.sections must be a list of sections", typeName))
		return
	}
	for i, sectionRaw := range sections {
		sectionField := fmt.Sprintf("%s.sections[%d]", field, i)
		section, ok := toStringAnyMap(sectionRaw)
		if !ok {
			r.add(ValidationWarning, "pack", packName, sourceFile, sectionField,
				fmt.Sprintf("type %q ui.view.sections[%d] must be a mapping", typeName, i))
			continue
		}
		for _, key := range sortedMapKeys(section) {
			if !uiViewSectionKeys[key] {
				r.add(ValidationWarning, "pack", packName, sourceFile, sectionField+"."+key,
					fmt.Sprintf("type %q ui.view.sections[%d]: unknown key %q (known: title, relation, dir, limit)", typeName, i, key))
			}
		}
		r.validateUIViewSection(packName, sourceFile, sectionField, typeName, i, section, knownRelTypes)
	}
}

// validateUIViewSection checks one declared section: title and relation shapes,
// relation is required, dir is a closed enum, limit is bounded, and every named
// relationship type is one the blueprint actually declares.
func (r *ValidationReport) validateUIViewSection(packName, sourceFile, field, typeName string, index int, section map[string]any, knownRelTypes map[string]bool) {
	if titleRaw, ok := section["title"]; ok {
		// The renderer decodes title as a string; an unquoted year in YAML
		// (title: 2024) decodes as a number and drops the whole view at render
		// time, which is exactly the silent failure this validator exists for.
		if _, isString := titleRaw.(string); !isString {
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".title",
				fmt.Sprintf("type %q ui.view.sections[%d]: title must be a string (quote it) — the whole view is ignored at render time", typeName, index))
		}
	}

	// relationNames collects the section's relationship types so each can be
	// checked against the blueprint's declarations below.
	var relationNames []string
	relationRaw, hasRelation := section["relation"]
	switch rel := relationRaw.(type) {
	case string:
		if strings.TrimSpace(rel) == "" {
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".relation",
				fmt.Sprintf("type %q ui.view.sections[%d]: relation is empty — the section is ignored", typeName, index))
		} else {
			relationNames = append(relationNames, strings.TrimSpace(rel))
		}
	case []any:
		if len(rel) == 0 {
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".relation",
				fmt.Sprintf("type %q ui.view.sections[%d]: relation list is empty — the section is ignored", typeName, index))
		}
		for j, name := range rel {
			relName, isString := name.(string)
			if !isString {
				r.add(ValidationWarning, "pack", packName, sourceFile, fmt.Sprintf("%s.relation[%d]", field, j),
					fmt.Sprintf("type %q ui.view.sections[%d]: relation entries must be strings", typeName, index))
				continue
			}
			if trimmed := strings.TrimSpace(relName); trimmed != "" {
				relationNames = append(relationNames, trimmed)
			}
		}
	default:
		if !hasRelation {
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".relation",
				fmt.Sprintf("type %q ui.view.sections[%d]: relation is required — the section is ignored", typeName, index))
		} else {
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".relation",
				fmt.Sprintf("type %q ui.view.sections[%d]: relation must be a name or a list of names", typeName, index))
		}
	}

	// A section can only render an edge whose type is one the graph knows about.
	// An undeclared name is a permanently empty card, and nothing else reports
	// it: the page simply shows nothing under that heading.
	if len(knownRelTypes) > 0 {
		for _, relName := range relationNames {
			if !knownRelTypes[relName] {
				r.add(ValidationWarning, "pack", packName, sourceFile, field+".relation",
					fmt.Sprintf("type %q ui.view.sections[%d]: relation %q is not a relationshipType declared by this blueprint — the section stays empty unless another installed blueprint declares it", typeName, index, relName))
			}
		}
	}

	if dirRaw, ok := section["dir"]; ok {
		dir, isString := dirRaw.(string)
		switch {
		case !isString:
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".dir",
				fmt.Sprintf("type %q ui.view.sections[%d]: dir must be a string (in, out or both)", typeName, index))
		case dir == "", dir == "in", dir == "out", dir == "both":
		default:
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".dir",
				fmt.Sprintf("type %q ui.view.sections[%d]: dir %q must be in, out or both — the section is ignored", typeName, index, dir))
		}
	}

	if limitRaw, ok := section["limit"]; ok {
		switch limit := limitRaw.(type) {
		case int:
			if limit < 0 || limit > uiViewMaxLimit {
				r.add(ValidationWarning, "pack", packName, sourceFile, field+".limit",
					fmt.Sprintf("type %q ui.view.sections[%d]: limit %d must be between 0 and %d — the section is ignored", typeName, index, limit, uiViewMaxLimit))
			}
		case float64: // a pack read back from JSON decodes numbers as float64
			if limit != float64(int(limit)) {
				r.add(ValidationWarning, "pack", packName, sourceFile, field+".limit",
					fmt.Sprintf("type %q ui.view.sections[%d]: limit must be a whole number", typeName, index))
			} else if limit < 0 || limit > uiViewMaxLimit {
				r.add(ValidationWarning, "pack", packName, sourceFile, field+".limit",
					fmt.Sprintf("type %q ui.view.sections[%d]: limit %d must be between 0 and %d — the section is ignored", typeName, index, int(limit), uiViewMaxLimit))
			}
		default:
			r.add(ValidationWarning, "pack", packName, sourceFile, field+".limit",
				fmt.Sprintf("type %q ui.view.sections[%d]: limit must be a whole number", typeName, index))
		}
	}
}

// anySlice normalizes a decoded YAML/JSON list to []any.
func anySlice(v any) ([]any, bool) {
	switch list := v.(type) {
	case []any:
		return list, true
	case nil:
		return nil, false
	}
	return nil, false
}

// isHexColor accepts #RGB, #RRGGBB, and #RRGGBBAA (case-insensitive).
func isHexColor(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 4 && len(s) != 7 && len(s) != 9 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// validateProperties checks every property definition on an object or
// relationship type. All findings are warnings: the server's coercion is
// deliberately lenient and install must not regress, while --strict opts in to
// enforcing the canonical shape. An array without items is likewise a warning
// because the element type need not be pinned.
func (r *ValidationReport) validateProperties(packName, sourceFile, field, owner string, props map[string]any) {
	for _, propName := range sortedMapKeys(props) {
		pf := field + "." + propName
		raw := props[propName]
		def, ok := toStringAnyMap(raw)
		if !ok {
			r.add(ValidationWarning, "pack", packName, sourceFile, pf,
				fmt.Sprintf("%s property %q must be a mapping (got %T)", owner, propName, raw))
			continue
		}

		typ, _ := def["type"].(string)
		typ = strings.TrimSpace(typ)
		validType := false
		switch {
		case typ == "":
			r.add(ValidationWarning, "pack", packName, sourceFile, pf+".type",
				fmt.Sprintf("%s property %q: type is not declared — the server accepts untyped properties, but --strict requires one", owner, propName))
		case !validPropertyTypes[typ]:
			r.add(ValidationWarning, "pack", packName, sourceFile, pf+".type",
				fmt.Sprintf("%s property %q: unknown type %q (canonical: string, number, integer, boolean, date, time, array, object, url, email, phone, country, currency, md5, sha1, sha256, sha512) — the server accepts it leniently, but --strict rejects it", owner, propName, typ))
			validType = true
		default:
			validType = true
		}

		if enumRaw, ok := def["enum"]; ok {
			r.validateEnum(packName, sourceFile, pf, owner, propName, typ, validType, enumRaw)
		}

		if typ == "array" {
			if items, ok := def["items"]; !ok || items == nil {
				r.add(ValidationWarning, "pack", packName, sourceFile, pf+".items",
					fmt.Sprintf("%s property %q is an array without items — element type is unspecified", owner, propName))
			}
		}

		for _, bound := range []string{"minimum", "maximum"} {
			v, ok := def[bound]
			if !ok {
				continue
			}
			if typ != "number" && typ != "integer" {
				r.add(ValidationWarning, "pack", packName, sourceFile, pf+"."+bound,
					fmt.Sprintf("%s property %q: %s is only valid on number/integer properties (type is %q)", owner, propName, bound, typ))
				continue
			}
			if !isNumericValue(v) {
				r.add(ValidationWarning, "pack", packName, sourceFile, pf+"."+bound,
					fmt.Sprintf("%s property %q: %s must be numeric", owner, propName, bound))
			}
		}

		if embRaw, ok := def["x-embedding-target"]; ok {
			enabled, isBool := embRaw.(bool)
			if !isBool || !enabled {
				r.add(ValidationWarning, "pack", packName, sourceFile, pf+".x-embedding-target",
					fmt.Sprintf("%s property %q: x-embedding-target must be boolean true when set", owner, propName))
			} else if typ != "string" {
				r.add(ValidationWarning, "pack", packName, sourceFile, pf+".x-embedding-target",
					fmt.Sprintf("%s property %q: x-embedding-target is only meaningful on string properties (type is %q)", owner, propName, typ))
			}
		}
	}
}

func (r *ValidationReport) validateEnum(packName, sourceFile, field, owner, propName, typ string, validType bool, enumRaw any) {
	if !validType {
		return
	}
	if typ != "number" && typ != "integer" && typ != "boolean" && !stringPropertyTypes[typ] {
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".enum",
			fmt.Sprintf("%s property %q: enum is only supported on primitive property types (type is %q)", owner, propName, typ))
		return
	}
	values, ok := enumRaw.([]any)
	if !ok {
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".enum",
			fmt.Sprintf("%s property %q: enum must be a non-empty list", owner, propName))
		return
	}
	if len(values) == 0 {
		r.add(ValidationWarning, "pack", packName, sourceFile, field+".enum",
			fmt.Sprintf("%s property %q: enum must be a non-empty list", owner, propName))
		return
	}
	for i, v := range values {
		if !enumValueMatches(typ, v) {
			r.add(ValidationWarning, "pack", packName, sourceFile, fmt.Sprintf("%s.enum[%d]", field, i),
				fmt.Sprintf("%s property %q: enum value %v does not match property type %q", owner, propName, v, typ))
		}
	}
}

func enumValueMatches(typ string, v any) bool {
	if stringPropertyTypes[typ] {
		_, ok := v.(string)
		return ok
	}
	switch typ {
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		return isNumericValue(v)
	case "integer":
		return isIntegerValue(v)
	}
	return false
}

func isNumericValue(v any) bool {
	switch n := v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	case json.Number:
		_, err := n.Float64()
		return err == nil
	}
	return false
}

func isIntegerValue(v any) bool {
	switch n := v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return n == float32(int64(n))
	case float64:
		return n == float64(int64(n))
	case json.Number:
		if _, err := n.Int64(); err == nil {
			return true
		}
		f, err := n.Float64()
		return err == nil && f == float64(int64(f))
	}
	return false
}

// validateBehaviorBlock checks a type-level embedding/extraction block. Known
// keys have fixed kinds; an unknown key is left to the server (free-form).
func (r *ValidationReport) validateBehaviorBlock(packName, sourceFile, field, typeName, blockName string, block map[string]any) {
	if len(block) == 0 {
		return
	}
	for _, key := range sortedMapKeys(block) {
		v := block[key]
		switch key {
		case "mode", "field":
			if _, ok := v.(string); !ok {
				r.add(ValidationWarning, "pack", packName, sourceFile, field+"."+key,
					fmt.Sprintf("type %q %s.%s must be a string", typeName, blockName, key))
			}
		case "enabled":
			if _, ok := v.(bool); !ok {
				r.add(ValidationWarning, "pack", packName, sourceFile, field+"."+key,
					fmt.Sprintf("type %q %s.enabled must be a boolean", typeName, blockName))
			}
		}
	}
}

// toStringAnyMap normalizes decoded JSON/YAML mappings to map[string]any. YAML
// may decode into map[any]any in older shapes, so accept both.
func toStringAnyMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	}
	return nil, false
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ──────────────────────────────────────────────
// Agent validation
// ──────────────────────────────────────────────

// validVisibilities is the set of agent visibility levels the server accepts.
// It mirrors the canonical AgentVisibility constants in
// apps/server/domain/agents/entity.go (VisibilityProject|External|Internal) and
// the server's NormalizeVisibility, which maps empty to "project". The CLI SDK
// exposes visibility only as a plain string (no shared enum constants), so the
// set is hardcoded here and must be kept in sync with the server.
var validVisibilities = map[string]bool{
	"":         true, // optional — server normalizes empty to "project"
	"project":  true,
	"external": true,
	"internal": true,
}

var validDispatchModes = map[string]bool{
	"":       true, // optional
	"sync":   true,
	"queued": true,
}

// agentCronParser mirrors the parser the server applies to a runtime agent's
// cronSchedule (apps/server/domain/agents/triggers.go validateCronInterval):
// standard 5-field cron, no seconds field. The internal scheduler is
// seconds-precision, but it prefixes its own zero seconds field — a blueprint
// that writes 6 fields is rejected by the agents API at install time with
// "expected exactly 5 fields, found 6" (#320), so validate it offline first.
var agentCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func (r *ValidationReport) validateAgents(agents []AgentFile) {
	seen := make(map[string]string) // name → sourceFile

	for _, a := range agents {
		if prev, ok := seen[a.Name]; ok {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "name",
				fmt.Sprintf("duplicate agent name (also defined in %s)", prev))
		} else {
			seen[a.Name] = a.SourceFile
		}

		if a.Name == "" {
			r.add(ValidationError, "agent", "", a.SourceFile, "name", "name is required")
		}

		if a.Visibility != "" && !validVisibilities[a.Visibility] {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "visibility",
				fmt.Sprintf("invalid visibility %q (valid: project, external, internal)", a.Visibility))
		}

		if a.DispatchMode != "" && !validDispatchModes[a.DispatchMode] {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "dispatchMode",
				fmt.Sprintf("invalid dispatchMode %q (valid: sync, queued)", a.DispatchMode))
		}

		// The agents API validates cronSchedule only for schedule-triggered
		// agents (apps/server/domain/agents/handler.go); a cron on a reaction or
		// manual agent is stored unused, so it must not fail validate. An empty
		// value means "server default"; anything non-empty — including a
		// whitespace-only value — must parse.
		if a.effectiveTriggerType() == "schedule" && a.CronSchedule != "" {
			if _, err := agentCronParser.Parse(a.CronSchedule); err != nil {
				r.add(ValidationError, "agent", a.Name, a.SourceFile, "cronSchedule",
					fmt.Sprintf("invalid cronSchedule %q: %v (runtime agents take standard 5-field cron: minute hour day-of-month month day-of-week — a seconds field is not accepted)", a.CronSchedule, err))
			}
		}

		// No trigger configuration means install creates an agent definition and
		// no runtime agent, so the agent silently never runs. This is valid (an
		// operator may trigger it by hand, or it may be referenced by other
		// agents), so it is an info note, never an error — including under
		// --strict (#321).
		if !a.hasRuntimeTrigger() {
			r.add(ValidationInfo, "agent", a.Name, a.SourceFile, "triggerType",
				"definition only; trigger manually or add triggerType (schedule, reaction, or manual) — without one, install creates no runtime agent and the agent never runs on its own")
		}

		if a.Model != nil {
			if a.Model.Name == "" && a.Model.Provider == "" {
				r.add(ValidationError, "agent", a.Name, a.SourceFile, "model.name",
					"model.name is required when a model block is present")
			} else if _, err := sdkmodelref.ParseWithProvider(a.Model.Name, a.Model.Provider); err != nil {
				// The server's create/update handlers reject a reference that is
				// not in "provider/model" form. Parse via the shared SDK package
				// so the CLI and server apply the exact same rule and cannot
				// drift (#1489).
				r.add(ValidationError, "agent", a.Name, a.SourceFile, "model.name",
					fmt.Sprintf("%s (got %q: %v)", agentModelRefHelp, a.Model.refDisplay(), err))
			}
		}

		if a.MaxSteps != nil && *a.MaxSteps <= 0 {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "maxSteps",
				fmt.Sprintf("maxSteps must be a positive integer, got %d", *a.MaxSteps))
		}

		if a.DefaultTimeout != nil && *a.DefaultTimeout <= 0 {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "defaultTimeout",
				fmt.Sprintf("defaultTimeout must be a positive integer (milliseconds), got %d", *a.DefaultTimeout))
		}

		if a.SystemPrompt == "" && a.Description == "" {
			r.add(ValidationWarning, "agent", a.Name, a.SourceFile, "",
				"agent has no systemPrompt and no description — it may behave unexpectedly")
		}
	}
}

// ──────────────────────────────────────────────
// Install preflight — validate + normalize models
// ──────────────────────────────────────────────

// agentModelRefHelp mirrors the server's ErrInvalidAgentModelRef
// (apps/server/domain/agents/model_ref.go) so both write paths describe the
// expected form identically.
const agentModelRefHelp = `model.name must be in 'provider/model' form (e.g. "deepseek/deepseek-v4-flash" or "google/gemini-2.5-flash")`

// refDisplay renders a model block in its most useful form for an error message:
// the canonical "provider/model" when both parts are present, otherwise
// whichever field is set.
func (m *AgentModel) refDisplay() string {
	switch {
	case m.Provider != "" && m.Name != "":
		return m.Provider + "/" + m.Name
	case m.Provider != "":
		return m.Provider
	default:
		return m.Name
	}
}

// NormalizeAgentModels validates every agent's model reference with the shared
// modelref parser and canonicalizes it in place: a structured model.provider is
// folded into the "provider/model" name and the provider field is cleared, so
// the applier sends exactly the shape the server accepts (mirroring
// agents.NormalizeAgentModelConfig). An empty reference is left empty (project
// default). The returned report collects every malformed reference with
// agent/file/field context; callers must abort when it HasErrors.
func NormalizeAgentModels(agents []AgentFile) *ValidationReport {
	r := &ValidationReport{}
	for i := range agents {
		a := &agents[i]
		if a.Model == nil {
			continue
		}
		ref, err := sdkmodelref.ParseWithProvider(a.Model.Name, a.Model.Provider)
		if err != nil {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "model.name",
				fmt.Sprintf("%s (got %q: %v)", agentModelRefHelp, a.Model.refDisplay(), err))
			continue
		}
		if ref.IsZero() {
			// No override. Drop a stray provider so the applied shape is canonical.
			a.Model.Provider = ""
			continue
		}
		a.Model.Name = ref.String()
		a.Model.Provider = ""
	}
	return r
}

// Preflight is the offline gate the CLI install path runs before applying any
// blueprint. It runs the full structural validation and, when that passes,
// canonicalizes every agent model reference in place (folding a structured
// model.provider into "provider/model"). It makes no API calls.
//
// Install MUST abort when the returned report HasErrors so a malformed
// blueprint — in particular an agent model that is not in "provider/model" form
// — applies nothing instead of leaving a partial install (packs already
// created) behind (#1586).
func Preflight(
	project *ProjectFile,
	packs []PackFile,
	agents []AgentFile,
	skills []SkillFile,
	objects []SeedObjectRecord,
	rels []SeedRelationshipRecord,
	loadErrors []BlueprintsResult,
) *ValidationReport {
	return PreflightWithProjectTypes(nil, project, packs, agents, skills, objects, rels, loadErrors)
}

// PreflightWithProjectTypes is Preflight with the object types already present in
// the project supplied (installed packs plus built-ins). A relationship endpoint
// naming one of them is accepted even when no pack in the blueprint defines it
// (#1853), so a pack may relate to a type owned by a companion/community pack or
// a built-in type. Install resolves the list with a best-effort read-only call;
// when it is unavailable (nil) the check falls back to the blueprint's own packs,
// exactly like Preflight.
//
// Like Preflight, this function makes no API calls itself — the caller supplies
// the project's types — so a malformed blueprint still aborts before any
// mutation (#1586).
func PreflightWithProjectTypes(
	projectObjectTypes []string,
	project *ProjectFile,
	packs []PackFile,
	agents []AgentFile,
	skills []SkillFile,
	objects []SeedObjectRecord,
	rels []SeedRelationshipRecord,
	loadErrors []BlueprintsResult,
) *ValidationReport {
	var known map[string]bool
	if len(projectObjectTypes) > 0 {
		known = make(map[string]bool, len(projectObjectTypes))
		for _, name := range projectObjectTypes {
			if name != "" {
				known[name] = true
			}
		}
	}

	report := validateWithProjectTypes(known, project, packs, agents, skills, objects, rels, loadErrors)
	if !report.HasErrors() {
		// Validate just proved every model reference parses, so normalization
		// cannot add errors; it only rewrites the references the applier sends.
		NormalizeAgentModels(agents)
	}
	return report
}

// ──────────────────────────────────────────────
// Skill validation
// ──────────────────────────────────────────────

func (r *ValidationReport) validateSkills(skills []SkillFile) {
	seen := make(map[string]string) // name → sourceFile

	for _, s := range skills {
		if prev, ok := seen[s.Name]; ok {
			r.add(ValidationError, "skill", s.Name, s.SourceFile, "name",
				fmt.Sprintf("duplicate skill name (also defined in %s)", prev))
		} else {
			seen[s.Name] = s.SourceFile
		}

		if s.Name == "" {
			r.add(ValidationError, "skill", "", s.SourceFile, "name", "name is required")
		}

		if s.Description == "" {
			r.add(ValidationError, "skill", s.Name, s.SourceFile, "description", "description is required")
		}

		if strings.TrimSpace(s.Content) == "" {
			r.add(ValidationError, "skill", s.Name, s.SourceFile, "content",
				"SKILL.md has no content body after frontmatter — skill would be empty")
		}
	}
}

// ──────────────────────────────────────────────
// Seed object validation
// ──────────────────────────────────────────────

func (r *ValidationReport) validateSeedObjects(objects []SeedObjectRecord, packs []PackFile) {
	// Build set of known object type names from all packs.
	knownObjTypes := buildKnownObjectTypes(packs)

	for _, o := range objects {
		if o.Type == "" {
			r.add(ValidationError, "seed_object", "", o.SourceFile, "type", "type is required")
			continue
		}

		if o.Key == "" {
			r.add(ValidationWarning, "seed_object", o.Type, o.SourceFile, "key",
				"object has no key — upsert will always create a new object rather than merging")
		}

		// Cross-reference type against known pack object types (only when packs are present).
		if len(knownObjTypes) > 0 && !knownObjTypes[o.Type] {
			r.add(ValidationWarning, "seed_object", o.Type, o.SourceFile, "type",
				fmt.Sprintf("type %q is not defined in any loaded pack — ensure the pack is installed", o.Type))
		}
	}
}

// ──────────────────────────────────────────────
// Seed relationship validation
// ──────────────────────────────────────────────

func (r *ValidationReport) validateSeedRelationships(rels []SeedRelationshipRecord, packs []PackFile, objects []SeedObjectRecord) {
	// Build set of known relationship type names from all packs.
	knownRelTypes := buildKnownRelationshipTypes(packs)

	// Build set of all seed object keys for cross-reference.
	seedObjectKeys := make(map[string]bool, len(objects))
	for _, o := range objects {
		if o.Key != "" {
			seedObjectKeys[o.Key] = true
		}
	}

	for _, rel := range rels {
		if rel.Type == "" {
			r.add(ValidationError, "seed_relationship", "", rel.SourceFile, "type", "type is required")
			continue
		}

		// Endpoint validation.
		hasSrcKey := rel.SrcKey != ""
		hasDstKey := rel.DstKey != ""
		hasSrcID := rel.SrcID != ""
		hasDstID := rel.DstID != ""

		if !((hasSrcKey && hasDstKey) || (hasSrcID && hasDstID)) {
			r.add(ValidationError, "seed_relationship", rel.Type, rel.SourceFile, "",
				"must provide either (srcKey + dstKey) or (srcId + dstId)")
		}

		// Cross-reference srcKey/dstKey against loaded seed objects.
		if hasSrcKey && len(seedObjectKeys) > 0 && !seedObjectKeys[rel.SrcKey] {
			r.add(ValidationWarning, "seed_relationship", rel.Type, rel.SourceFile, "srcKey",
				fmt.Sprintf("srcKey %q does not match any seed object key in this blueprint", rel.SrcKey))
		}
		if hasDstKey && len(seedObjectKeys) > 0 && !seedObjectKeys[rel.DstKey] {
			r.add(ValidationWarning, "seed_relationship", rel.Type, rel.SourceFile, "dstKey",
				fmt.Sprintf("dstKey %q does not match any seed object key in this blueprint", rel.DstKey))
		}

		// Cross-reference relationship type against known pack types.
		if len(knownRelTypes) > 0 && !knownRelTypes[rel.Type] {
			r.add(ValidationWarning, "seed_relationship", rel.Type, rel.SourceFile, "type",
				fmt.Sprintf("type %q is not defined in any loaded pack — ensure the pack is installed", rel.Type))
		}
	}
}

// ──────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────

func (r *ValidationReport) add(severity ValidationSeverity, resourceType, name, sourceFile, field, message string) {
	r.Issues = append(r.Issues, ValidationIssue{
		Severity:     severity,
		ResourceType: resourceType,
		Name:         name,
		SourceFile:   sourceFile,
		Field:        field,
		Message:      message,
	})
}

func buildKnownObjectTypes(packs []PackFile) map[string]bool {
	m := make(map[string]bool)
	for _, p := range packs {
		for _, ot := range p.ObjectTypes {
			if ot.Name != "" {
				m[ot.Name] = true
			}
		}
	}
	return m
}

// HasUnknownRelationshipEndpoints reports whether any relationship in packs
// names a source or target object type that no pack in the same set defines. The
// install path uses it to decide whether it must consult the project's existing
// object types: when every endpoint resolves locally, no read is needed, so a
// blueprint that fails offline validation still aborts with zero requests. When
// an endpoint is unresolved, a single read-only compiled-types call happens
// before the abort — the #1586 guarantee of zero mutations holds regardless. It
// is a cheap pre-check; the authoritative check remains validatePacks/Preflight.
func HasUnknownRelationshipEndpoints(packs []PackFile) bool {
	known := buildKnownObjectTypes(packs)
	if len(known) == 0 {
		// No object types at all — validatePacks skips endpoint cross-checks.
		return false
	}
	for _, p := range packs {
		for _, rt := range p.RelationshipTypes {
			for _, src := range rt.GetSourceTypes() {
				if !known[src] {
					return true
				}
			}
			for _, tgt := range rt.GetTargetTypes() {
				if !known[tgt] {
					return true
				}
			}
		}
	}
	return false
}

// buildKnownRelationshipTypes is the relationship-name sibling of
// buildKnownObjectTypes: the set of relationship types any pack in the same
// blueprint declares. A ui.view section that names a relationship outside this
// set can never match an edge, so the validator reports it instead of leaving
// the author with a silently empty section. It is only a warning: the real
// project may have the relationship type from another installed blueprint.
func buildKnownRelationshipTypes(packs []PackFile) map[string]bool {
	m := make(map[string]bool)
	for _, p := range packs {
		for _, rt := range p.RelationshipTypes {
			if rt.Name != "" {
				m[rt.Name] = true
			}
		}
	}
	return m
}
