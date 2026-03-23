package blueprints

import (
	"fmt"
	"strings"
)

// ──────────────────────────────────────────────
// Validation result types
// ──────────────────────────────────────────────

// ValidationSeverity indicates how serious a finding is.
type ValidationSeverity string

const (
	ValidationError   ValidationSeverity = "error"
	ValidationWarning ValidationSeverity = "warning"
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

// ──────────────────────────────────────────────
// Validate — entry point
// ──────────────────────────────────────────────

// Validate runs a full structural and cross-reference validation over a loaded
// blueprint directory. It expects the same values returned by LoadDir.
//
// It is purely offline — no API calls are made. It validates:
//   - Pack: required fields, per-type required fields, internal cross-references
//     (relationship sourceType/targetType must name an objectType in the same pack),
//     duplicate names within a pack, duplicate pack names across files.
//   - Agent: required fields, enum values (flowType, visibility, dispatchMode),
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
	r.validatePacks(packs)
	r.validateAgents(agents)
	r.validateSkills(skills)
	r.validateSeedObjects(objects, packs)
	r.validateSeedRelationships(rels, packs, objects)

	return r
}

// ──────────────────────────────────────────────
// Pack validation
// ──────────────────────────────────────────────

func (r *ValidationReport) validatePacks(packs []PackFile) {
	seen := make(map[string]string) // name → sourceFile

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

		// Build set of object type names for cross-reference.
		objTypeNames := make(map[string]bool, len(p.ObjectTypes))

		// Validate each object type.
		dupObjNames := make(map[string]bool)
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
				objTypeNames[ot.Name] = true
			}
			if ot.Label == "" {
				r.add(ValidationWarning, "pack", p.Name, p.SourceFile, field+".label",
					fmt.Sprintf("objectType %q has no label — UI will fall back to name", ot.Name))
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
					if len(objTypeNames) > 0 && !objTypeNames[src] {
						r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".sourceType",
							fmt.Sprintf("relationshipType %q: sourceType %q does not match any objectType in this pack", rt.Name, src))
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
					if len(objTypeNames) > 0 && !objTypeNames[tgt] {
						r.add(ValidationError, "pack", p.Name, p.SourceFile, field+".targetType",
							fmt.Sprintf("relationshipType %q: targetType %q does not match any objectType in this pack", rt.Name, tgt))
					}
				}
			}
		}
	}
}

// ──────────────────────────────────────────────
// Agent validation
// ──────────────────────────────────────────────

var validFlowTypes = map[string]bool{
	"":           true, // optional
	"reactive":   true,
	"workflow":   true,
	"sequential": true,
}

var validVisibilities = map[string]bool{
	"":        true, // optional
	"public":  true,
	"private": true,
	"system":  true,
}

var validDispatchModes = map[string]bool{
	"":            true, // optional
	"auto":        true,
	"manual":      true,
	"round_robin": true,
}

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

		if a.FlowType != "" && !validFlowTypes[a.FlowType] {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "flowType",
				fmt.Sprintf("invalid flowType %q (valid: reactive, workflow, sequential)", a.FlowType))
		}

		if a.Visibility != "" && !validVisibilities[a.Visibility] {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "visibility",
				fmt.Sprintf("invalid visibility %q (valid: public, private, system)", a.Visibility))
		}

		if a.DispatchMode != "" && !validDispatchModes[a.DispatchMode] {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "dispatchMode",
				fmt.Sprintf("invalid dispatchMode %q (valid: auto, manual, round_robin)", a.DispatchMode))
		}

		if a.Model != nil && a.Model.Name == "" {
			r.add(ValidationError, "agent", a.Name, a.SourceFile, "model.name",
				"model.name is required when a model block is present")
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
