package main

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// bundledFS embeds the bundled blueprint packs (gateway/blueprints/) into the
// binary, so the gallery can offer them in any deployment without a runtime
// filesystem dependency.
//
//go:embed blueprints
var bundledFS embed.FS

// BundledBlueprint is one pack discovered under the blueprints/ directory. It
// carries enough metadata to list it and enough schema to register it with
// memory. A pack may also carry agent definitions (agents/) — a pure-agent
// blueprint (like "operator") has no schema at all.
type BundledBlueprint struct {
	ID                      string // directory name
	Name                    string
	Version                 string
	Description             string
	Author                  string
	ObjectTypeSchemas       []map[string]any
	RelationshipTypeSchemas []map[string]any
	Migrations              *SchemaMigrationHints
	Agents                  []BundledAgent
	HasSchema               bool // true when a schema pack file was parsed
}

// BundledAgent is one agent definition carried by a bundled blueprint
// (agents/*.yaml). It maps onto the gateway's AgentDefinition create payload.
type BundledAgent struct {
	Name         string         `yaml:"name"`
	Description  string         `yaml:"description"`
	SystemPrompt string         `yaml:"systemPrompt"`
	Model        string         `yaml:"model"`
	Tools        []string       `yaml:"tools"`
	BannedTools  []string       `yaml:"bannedTools"`
	Skills       []string       `yaml:"skills"`
	Config       map[string]any `yaml:"config"`
	FlowType     string         `yaml:"flowType"`
	Visibility   string         `yaml:"visibility"`
}

// bundledPackFile is the union shape of a pack/schema YAML file. Both the
// legacy field names (objectTypes/relationshipTypes) and the current ones
// (objectTypeSchemas/relationshipTypeSchemas) are accepted.
type bundledPackFile struct {
	Name                    string                `yaml:"name"`
	Version                 string                `yaml:"version"`
	Description             string                `yaml:"description"`
	Author                  string                `yaml:"author"`
	ObjectTypes             []map[string]any      `yaml:"objectTypes"`
	RelationshipTypes       []map[string]any      `yaml:"relationshipTypes"`
	ObjectTypeSchemas       []map[string]any      `yaml:"objectTypeSchemas"`
	RelationshipTypeSchemas []map[string]any      `yaml:"relationshipTypeSchemas"`
	Migrations              *SchemaMigrationHints `yaml:"migrations"`
}

// bundledProjectFile is the optional metadata block of a bundled pack
// (project.yaml). It supplies name/version/description/author for packs that
// carry no schema file (pure-agent blueprints).
type bundledProjectFile struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
	Author      string `yaml:"author"`
}

// ListBundledBlueprints scans a directory of blueprint packs on disk and
// returns the packs it finds as available items (Source "bundled"). Used when
// BLUEPRINTS_DIR is configured and by tests.
func ListBundledBlueprints(dir string) ([]AvailableSchemaItem, error) {
	return listBundledFromFS(os.DirFS(dir), ".")
}

// listEmbeddedBlueprints returns the packs compiled into the binary.
func listEmbeddedBlueprints() ([]AvailableSchemaItem, error) {
	return listBundledFromFS(bundledFS, "blueprints")
}

// listBundledFromFS walks one pack-directory level under root within fsys and
// returns the packs it can parse, sorted by name. Unparseable subdirectories
// are skipped rather than failing the whole list.
func listBundledFromFS(fsys fs.FS, root string) ([]AvailableSchemaItem, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}
	var out []AvailableSchemaItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		bp, err := loadBundledFromFS(fsys, path.Join(root, e.Name()))
		if err != nil || bp.Name == "" {
			continue
		}
		out = append(out, AvailableSchemaItem{
			ID:          bp.ID,
			Name:        bp.Name,
			Version:     bp.Version,
			Description: bp.Description,
			Author:      bp.Author,
			Source:      "bundled",
			Agents:      agentNames(bp.Agents),
			HasSchema:   bp.HasSchema,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadBundledBlueprint reads one pack directory from disk (used for the
// BLUEPRINTS_DIR override and tests).
func loadBundledBlueprint(root, id string) (*BundledBlueprint, error) {
	return loadBundledFromFS(os.DirFS(root), id)
}

// loadBundledFromFS reads a pack directory and returns the normalized pack.
// It reads optional project.yaml metadata, then any schema YAML (packs/ or
// schemas/), then any agent definitions (agents/). Name is resolved from
// project.yaml → schema file → agent file; it returns an error when no name
// can be determined.
func loadBundledFromFS(fsys fs.FS, dir string) (*BundledBlueprint, error) {
	bp := &BundledBlueprint{ID: path.Base(dir)}

	// 1. project.yaml metadata (optional)
	if b, err := fs.ReadFile(fsys, path.Join(dir, "project.yaml")); err == nil {
		var pf bundledProjectFile
		if yaml.Unmarshal(b, &pf) == nil {
			bp.Name = pf.Name
			bp.Version = pf.Version
			bp.Description = pf.Description
			bp.Author = pf.Author
		}
	}

	// 2. schema files (packs/ + schemas/) — first parseable wins
	var files []string
	for _, sub := range []string{"packs", "schemas"} {
		entries, _ := fs.ReadDir(fsys, path.Join(dir, sub))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if ext := path.Ext(e.Name()); ext == ".yaml" || ext == ".yml" {
				files = append(files, path.Join(dir, sub, e.Name()))
			}
		}
	}
	sort.Strings(files)
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			continue
		}
		var pf bundledPackFile
		if err := yaml.Unmarshal(b, &pf); err != nil || pf.Name == "" {
			continue
		}
		bp.HasSchema = true
		if bp.Name == "" {
			bp.Name = pf.Name
		}
		if bp.Version == "" {
			bp.Version = pf.Version
		}
		if bp.Description == "" {
			bp.Description = pf.Description
		}
		if bp.Author == "" {
			bp.Author = pf.Author
		}
		bp.ObjectTypeSchemas = pf.ObjectTypeSchemas
		if len(bp.ObjectTypeSchemas) == 0 {
			bp.ObjectTypeSchemas = pf.ObjectTypes
		}
		bp.RelationshipTypeSchemas = pf.RelationshipTypeSchemas
		if len(bp.RelationshipTypeSchemas) == 0 {
			bp.RelationshipTypeSchemas = pf.RelationshipTypes
		}
		bp.Migrations = pf.Migrations
		break
	}

	// 3. agent definitions (agents/) — one agent per YAML file
	agents, _ := bundledAgentsFromFS(fsys, path.Join(dir, "agents"))
	bp.Agents = agents

	if bp.Name == "" {
		return nil, fmt.Errorf("no parseable pack yaml in %s", dir)
	}
	return bp, nil
}

// bundledAgentsFromFS reads agent definitions from an agents/ directory. It
// returns nil (no error) when the directory is absent — a blueprint need not
// define any agents.
func bundledAgentsFromFS(fsys fs.FS, dir string) ([]BundledAgent, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, nil
	}
	var out []BundledAgent
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := path.Ext(e.Name()); ext != ".yaml" && ext != ".yml" {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var a BundledAgent
		if yaml.Unmarshal(b, &a) != nil || a.Name == "" {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// bundledBlueprints resolves the bundled packs from the configured source: the
// embedded FS by default, or BLUEPRINTS_DIR when set.
func (s *Server) bundledBlueprints() ([]AvailableSchemaItem, error) {
	if s.cfg.BlueprintsDir != "" {
		return ListBundledBlueprints(s.cfg.BlueprintsDir)
	}
	return listEmbeddedBlueprints()
}

// loadBundled resolves a single bundled pack by name from the configured source.
func (s *Server) loadBundled(name string) (*BundledBlueprint, error) {
	if s.cfg.BlueprintsDir != "" {
		return loadBundledBlueprint(s.cfg.BlueprintsDir, name)
	}
	return loadBundledFromFS(bundledFS, path.Join("blueprints", name))
}

// buildAvailableList merges the global registry schemas (built-in + project)
// with the bundled packs into one available list, excluding applied blueprints
// and deduplicating by name. A bundled pack is "installed" when its name+version
// is in applied, or — as a pre-cutover fallback — when it is agent-only and all
// its agents exist. Project-scoped registry schemas collapse to the highest
// version per name; an applied registry pack surfaces (Upgrade=true) only when
// that latest version is strictly newer than the applied one.
func buildAvailableList(schemas []SchemaInfo, bundled []AvailableSchemaItem, applied []AppliedBlueprint, existingAgents map[string]bool) []AvailableSchemaItem {
	appliedVersion := make(map[string]string, len(applied))
	for _, ap := range applied {
		appliedVersion[ap.Name] = ap.Version
	}

	// Collapse project-scoped registry schemas to the highest version per name.
	// Global schemas (project_id NULL) are listed by memory's schema-list, but
	// its GetPack endpoint is project-scoped (repository.go GetPack filters
	// `project_id = ?`) and 404s on them, so they cannot be inspected or
	// installed from here. Skip them.
	latest := map[string]SchemaInfo{}
	for _, s := range schemas {
		if s.ProjectID == "" {
			continue
		}
		// The project-owned override pack is an internal copy-on-write artifact,
		// not a user-installable pack — never surface it in "Available schemas".
		if s.Name == overridePackName {
			continue
		}
		if cur, ok := latest[s.Name]; !ok || versionGreater(s.Version, cur.Version) {
			latest[s.Name] = s
		}
	}

	seen := map[string]bool{}
	var out []AvailableSchemaItem
	for _, s := range latest {
		// An applied registry pack stays hidden unless the registry carries a
		// strictly newer version; at the same or an older version it must not
		// regress into the available list.
		upgrade := false
		if iv, ok := appliedVersion[s.Name]; ok {
			if !versionGreater(s.Version, iv) {
				continue
			}
			upgrade = true
		}
		seen[s.Name] = true
		out = append(out, AvailableSchemaItem{
			ID:          s.ID,
			Name:        s.Name,
			Version:     s.Version,
			Description: s.Description,
			Source:      "registry",
			Upgrade:     upgrade,
		})
	}
	for _, b := range bundled {
		if seen[b.Name] {
			continue
		}
		if iv, ok := appliedVersion[b.Name]; ok {
			if !versionGreater(b.Version, iv) {
				continue // applied at same or newer version — not an upgrade
			}
			b.Upgrade = true
		} else if !b.HasSchema && len(b.Agents) > 0 && allAgentsExist(b.Agents, existingAgents) {
			continue // agent-only blueprint already installed via its agents (pre-cutover)
		}
		seen[b.Name] = true
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// upgradeByName indexes upgrade items by blueprint name for the installed
// rows' "update available" lookup. Returns nil when there are no upgrades.
func upgradeByName(upgrades []AvailableSchemaItem) map[string]AvailableSchemaItem {
	if len(upgrades) == 0 {
		return nil
	}
	out := make(map[string]AvailableSchemaItem, len(upgrades))
	for _, u := range upgrades {
		out[u.Name] = u
	}
	return out
}

// agentNames extracts the sorted names of a blueprint's agent definitions.
func agentNames(agents []BundledAgent) []string {
	if len(agents) == 0 {
		return nil
	}
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return names
}

// allAgentsExist reports whether every named agent is present in existing.
func allAgentsExist(names []string, existing map[string]bool) bool {
	if len(names) == 0 {
		return false
	}
	for _, n := range names {
		if !existing[n] {
			return false
		}
	}
	return true
}

// existingAgents returns the set of agent names already defined in memory. A
// failure to list agents yields an empty set (best-effort).
func (s *Server) existingAgents(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return out
	}
	for _, a := range agents {
		out[a.Name] = true
	}
	return out
}

// versionGreater compares two dotted version strings numerically
// (e.g. "2.0.0" > "1.9.0"). Missing segments compare as zero.
func versionGreater(a, b string) bool {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(pb[i])
		}
		if na != nb {
			return na > nb
		}
	}
	return false
}

// nextVersion returns the semver patch bump of v. It parses the leading numeric
// dotted components, stopping at the first component without a leading digit
// (which strips pre-release/build suffixes like "-beta.1"). Missing trailing
// components count as zero and the output is always three components:
//
//	1.2.3         -> 1.2.4
//	1.2           -> 1.2.1
//	1             -> 1.0.1
//	1.2.3-beta.1  -> 1.2.4
//	""/"abc"      -> 1.0.0
func nextVersion(v string) string {
	nums := make([]int, 0, 3)
	for _, part := range strings.Split(strings.TrimSpace(v), ".") {
		i := 0
		for i < len(part) && part[i] >= '0' && part[i] <= '9' {
			i++
		}
		if i == 0 {
			break
		}
		n, err := strconv.Atoi(part[:i])
		if err != nil {
			break
		}
		nums = append(nums, n)
	}
	if len(nums) == 0 {
		return "1.0.0"
	}
	for len(nums) < 3 {
		nums = append(nums, 0)
	}
	nums[2]++
	return fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2])
}

// sortVersionsNewestFirst orders versions by semver descending (numeric, not
// lexicographic), tie-breaking on CreatedAt descending and then ID ascending so
// the result is deterministic.
func sortVersionsNewestFirst(vs []BlueprintVersionView) {
	slices.SortFunc(vs, func(a, b BlueprintVersionView) int {
		switch {
		case versionGreater(a.Version, b.Version):
			return -1
		case versionGreater(b.Version, a.Version):
			return 1
		}
		if a.CreatedAt != b.CreatedAt {
			return cmp.Compare(b.CreatedAt, a.CreatedAt)
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// --- blueprint details ---

// BlueprintDetail is the unified view shown on the details page: schema
// metadata plus object/relationship type definitions and any agent definitions
// the blueprint carries.
type BlueprintDetail struct {
	ID                string
	Name              string
	Version           string
	Description       string
	Author            string
	Source            string
	License           string
	RepositoryURL     string
	DocumentationURL  string
	ObjectTypes       []ObjectTypeDetail
	RelationshipTypes []RelationshipTypeDetail
	Agents            []BundledAgent

	// Versions lists a blueprint record's version history (newest first), shown
	// on the detail page so a derived new version is visible and selectable for
	// comparison. Empty for bundled packs and registry schemas.
	Versions []BlueprintVersionView

	// Status is the blueprint record's lifecycle status ("draft" | "published");
	// empty for bundled packs and registry schemas.
	Status string

	// IsDraft reports whether the viewed blueprint record is a draft — the only
	// state eligible for version comparison.
	IsDraft bool

	// Installed reports whether the viewed blueprint/version is applied to the
	// project.
	Installed bool

	// CanonicalID is the id of the highest version (the breadcrumb "name" crumb
	// target); falls back to ID when no history is available.
	CanonicalID string

	// SuggestedVersion is a semver patch bump of the highest existing version.
	SuggestedVersion string

	// CompareOptions holds the versions selectable as a comparison base
	// (excludes the version currently being viewed), newest first.
	CompareOptions []BlueprintVersionView

	// CompareVersion is the version label of the active comparison base ("" when
	// no comparison is active).
	CompareVersion string

	// Diff is the field-level comparison against CompareVersion; non-nil only
	// when a comparison is active.
	Diff *BlueprintDiff

	// CanDeriveVersion enables the "Save as new version" action: only a
	// blueprint record (not a bundled pack or registry schema) can be forked.
	CanDeriveVersion bool

	// FlashMsg/FlashErr carry PRG feedback from the "Save as new version"
	// derive action.
	FlashMsg string
	FlashErr error

	// ProviderNames holds the configured provider keys of the project (e.g.
	// "openai"), attached best-effort by the uiBlueprint handler so agent rows
	// can warn when a pinned model's provider isn't configured.
	ProviderNames []string
}

// ObjectTypeDetail is one object type with its property definitions.
type ObjectTypeDetail struct {
	Name        string
	Label       string
	Description string
	Properties  []PropertyDetail
}

// PropertyDetail is one property of an object type.
type PropertyDetail struct {
	Name        string
	Type        string
	Widget      string
	Description string
	Required    bool
}

// RelationshipTypeDetail is one relationship type definition.
type RelationshipTypeDetail struct {
	Name        string
	Label       string
	Description string
	SourceType  string
	TargetType  string
}

// BlueprintVersionView is one row in a blueprint's version history.
type BlueprintVersionView struct {
	ID        string
	Version   string
	Status    string // "draft" | "published"
	CreatedAt string
	Installed bool // this exact version is applied to the project
	Current   bool // the version currently being viewed
}

// DiffChange is the kind of change for a diffed field or entity.
type DiffChange string

const (
	DiffChangeNone    DiffChange = ""
	DiffChangeAdded   DiffChange = "added"
	DiffChangeRemoved DiffChange = "removed"
	DiffChangeChanged DiffChange = "changed"
)

// FieldDiff is one text field's before/after value with its change kind.
type FieldDiff struct {
	Before string
	After  string
	Change DiffChange
}

// PropertyDiff is one property of an object type in a diff.
type PropertyDiff struct {
	Name        string
	Change      DiffChange
	Type        FieldDiff
	Widget      FieldDiff
	Description FieldDiff
	Required    FieldDiff
}

// ObjectTypeDiff is one object type in a diff.
type ObjectTypeDiff struct {
	Name        string
	Change      DiffChange
	Label       FieldDiff
	Description FieldDiff
	Properties  []PropertyDiff
}

// RelationshipTypeDiff is one relationship type in a diff.
type RelationshipTypeDiff struct {
	Name        string
	Change      DiffChange
	Label       FieldDiff
	Description FieldDiff
	SourceType  FieldDiff
	TargetType  FieldDiff
}

// ChipDiff is an added/removed set difference (e.g. agent tools/skills).
type ChipDiff struct {
	Added   []string
	Removed []string
}

// AgentDiff is one agent definition in a diff.
type AgentDiff struct {
	Name         string
	Change       DiffChange
	Description  FieldDiff
	Model        FieldDiff
	SystemPrompt FieldDiff
	FlowType     FieldDiff
	Visibility   FieldDiff
	Tools        ChipDiff
	BannedTools  ChipDiff
	Skills       ChipDiff
}

// BlueprintDiff is the field-level comparison of two blueprint versions.
type BlueprintDiff struct {
	BaseVersion       string
	TargetVersion     string
	ObjectTypes       []ObjectTypeDiff
	RelationshipTypes []RelationshipTypeDiff
	Agents            []AgentDiff
}

// buildBundledDetail converts a bundled pack (embedded YAML) to the detail view.
func buildBundledDetail(bp *BundledBlueprint) *BlueprintDetail {
	return &BlueprintDetail{
		ID:                bp.ID,
		Name:              bp.Name,
		Version:           bp.Version,
		Description:       bp.Description,
		Author:            bp.Author,
		Source:            "bundled",
		ObjectTypes:       objectTypesFromMaps(bp.ObjectTypeSchemas),
		RelationshipTypes: relationshipTypesFromMaps(bp.RelationshipTypeSchemas),
		Agents:            bp.Agents,
	}
}

// buildBlueprintRecordDetail converts an applied blueprint definition
// (GetBlueprint) to the detail view by parsing its manifest: pack object and
// relationship types plus any agents.
func buildBlueprintRecordDetail(rec *BlueprintRecord) *BlueprintDetail {
	d := &BlueprintDetail{
		ID:          rec.ID,
		Name:        rec.Name,
		Version:     rec.Version,
		Description: rec.Description,
		Author:      rec.Author,
		Source:      "blueprint",
		Status:      rec.Status,
	}
	var m blueprintManifest
	if json.Unmarshal(rec.Manifest, &m) == nil {
		if len(m.Packs) > 0 {
			d.ObjectTypes = objectTypesFromMaps(m.Packs[0].ObjectTypes)
			d.RelationshipTypes = relationshipTypesFromMaps(m.Packs[0].RelationshipTypes)
		}
		d.Agents = bundledAgentsFromManifest(m.Agents)
	}
	return d
}

// bundledAgentsFromManifest converts blueprint-manifest agents to the detail
// view's BundledAgent shape (model name unwrapped from the {name} struct).
func bundledAgentsFromManifest(agents []blueprintAgent) []BundledAgent {
	out := make([]BundledAgent, 0, len(agents))
	for _, a := range agents {
		model := ""
		if a.Model != nil {
			model = a.Model.Name
		}
		out = append(out, BundledAgent{
			Name:         a.Name,
			Description:  a.Description,
			SystemPrompt: a.SystemPrompt,
			Model:        model,
			Tools:        a.Tools,
			BannedTools:  a.BannedTools,
			Skills:       a.Skills,
			FlowType:     a.FlowType,
			Visibility:   a.Visibility,
			Config:       a.Config,
		})
	}
	return out
}

// buildSchemaDetail converts a registry schema (GetPack) to the detail view.
func buildSchemaDetail(s *BlueprintSchema) *BlueprintDetail {
	return &BlueprintDetail{
		ID:                s.ID,
		Name:              s.Name,
		Version:           s.Version,
		Description:       s.Description,
		Author:            s.Author,
		Source:            s.Source,
		License:           s.License,
		RepositoryURL:     s.RepositoryURL,
		DocumentationURL:  s.DocumentationURL,
		ObjectTypes:       objectTypesFromRaw(s.ObjectTypeSchemas),
		RelationshipTypes: relationshipTypesFromRaw(s.RelationshipTypeSchemas),
	}
}

func objectTypesFromMaps(types []map[string]any) []ObjectTypeDetail {
	out := make([]ObjectTypeDetail, 0, len(types))
	for _, t := range types {
		out = append(out, ObjectTypeDetail{
			Name:        strAny(t["name"]),
			Label:       strAny(t["label"]),
			Description: strAny(t["description"]),
			Properties:  propertiesFromMap(t["properties"]),
		})
	}
	return out
}

func propertiesFromMap(v any) []PropertyDetail {
	pm, _ := v.(map[string]any)
	var out []PropertyDetail
	for name, pv := range pm {
		p, _ := pv.(map[string]any)
		out = append(out, PropertyDetail{
			Name:        name,
			Type:        strAny(p["type"]),
			Widget:      strAny(p["widget"]),
			Description: strAny(p["description"]),
			Required:    boolAny(p["required"]),
		})
	}
	// Required properties first, then the rest; each group alphabetical.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func relationshipTypesFromMaps(types []map[string]any) []RelationshipTypeDetail {
	out := make([]RelationshipTypeDetail, 0, len(types))
	for _, t := range types {
		out = append(out, RelationshipTypeDetail{
			Name:        strAny(t["name"]),
			Label:       strAny(t["label"]),
			Description: strAny(t["description"]),
			SourceType:  strAny(t["sourceType"]),
			TargetType:  strAny(t["targetType"]),
		})
	}
	return out
}

// objectTypesFromRaw parses the raw objectTypeSchemas jsonb (array form, with a
// map-form fallback) into the detail view.
func objectTypesFromRaw(raw json.RawMessage) []ObjectTypeDetail {
	if len(raw) == 0 {
		return nil
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil {
		return objectTypesFromMaps(arr)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		arr = arr[:0]
		for name, v := range m {
			vm, _ := v.(map[string]any)
			if vm == nil {
				vm = map[string]any{}
			}
			vm["name"] = name
			arr = append(arr, vm)
		}
		sort.Slice(arr, func(i, j int) bool { return strAny(arr[i]["name"]) < strAny(arr[j]["name"]) })
		return objectTypesFromMaps(arr)
	}
	return nil
}

func relationshipTypesFromRaw(raw json.RawMessage) []RelationshipTypeDetail {
	if len(raw) == 0 {
		return nil
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil {
		return relationshipTypesFromMaps(arr)
	}
	return nil
}

func boolAny(v any) bool {
	b, _ := v.(bool)
	return b
}

// configValueString renders one agent config value for display: plain strings
// are returned as-is, everything else is JSON-encoded (config values may be
// nested maps/slices).
func configValueString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// --- blueprint version diff ---

// fieldDiff compares two same-entity field values.
func fieldDiff(before, after string) FieldDiff {
	if before == after {
		return FieldDiff{Before: before, After: after, Change: DiffChangeNone}
	}
	return FieldDiff{Before: before, After: after, Change: DiffChangeChanged}
}

// presenceField renders a text field for one entity, honoring whether the
// entity exists only in the base (removed) or only in the target (added).
func presenceField(before, after string, inBase, inTarget bool) FieldDiff {
	switch {
	case !inBase && inTarget:
		return FieldDiff{After: after, Change: DiffChangeAdded}
	case inBase && !inTarget:
		return FieldDiff{Before: before, Change: DiffChangeRemoved}
	default:
		return fieldDiff(before, after)
	}
}

// boolChange renders a boolean field as a change kind only (no Before/After
// text), honoring entity presence.
func boolChange(before, after bool, inBase, inTarget bool) FieldDiff {
	switch {
	case !inBase && inTarget:
		return FieldDiff{Change: DiffChangeAdded}
	case inBase && !inTarget:
		return FieldDiff{Change: DiffChangeRemoved}
	case before != after:
		return FieldDiff{Change: DiffChangeChanged}
	default:
		return FieldDiff{}
	}
}

// entityChange classifies an entity present only in the base, only in the
// target, or in both (the latter refined to Changed/None by the caller).
func entityChange(inBase, inTarget bool) DiffChange {
	switch {
	case !inBase && inTarget:
		return DiffChangeAdded
	case inBase && !inTarget:
		return DiffChangeRemoved
	default:
		return DiffChangeNone
	}
}

// unionNames returns the sorted, deduplicated union of entity names.
func unionNames[T any](base, target []T, name func(T) string) []string {
	set := make(map[string]bool, len(base)+len(target))
	for _, e := range base {
		set[name(e)] = true
	}
	for _, e := range target {
		set[name(e)] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// propertyFieldsChanged reports whether any diffed property field differs.
func propertyFieldsChanged(p PropertyDiff) bool {
	return p.Type.Change != DiffChangeNone ||
		p.Widget.Change != DiffChangeNone ||
		p.Description.Change != DiffChangeNone ||
		p.Required.Change != DiffChangeNone
}

// propertyDiffers reports whether a property row carries any change.
func propertyDiffers(p PropertyDiff) bool {
	return p.Change != DiffChangeNone || propertyFieldsChanged(p)
}

// diffProperties unions properties by name and diffs their fields.
func diffProperties(base, target []PropertyDetail) []PropertyDiff {
	baseByName := make(map[string]PropertyDetail, len(base))
	for _, p := range base {
		baseByName[p.Name] = p
	}
	targetByName := make(map[string]PropertyDetail, len(target))
	for _, p := range target {
		targetByName[p.Name] = p
	}
	names := unionNames(base, target, func(p PropertyDetail) string { return p.Name })
	out := make([]PropertyDiff, 0, len(names))
	for _, name := range names {
		b, inBase := baseByName[name]
		t, inTarget := targetByName[name]
		p := PropertyDiff{
			Name:        name,
			Change:      entityChange(inBase, inTarget),
			Type:        presenceField(b.Type, t.Type, inBase, inTarget),
			Widget:      FieldDiff{Change: entityChange(inBase, inTarget)},
			Description: presenceField(b.Description, t.Description, inBase, inTarget),
			Required:    boolChange(b.Required, t.Required, inBase, inTarget),
		}
		if p.Change == DiffChangeNone && propertyFieldsChanged(p) {
			p.Change = DiffChangeChanged
		}
		out = append(out, p)
	}
	return out
}

// diffObjectTypes unions object types by name and diffs their fields.
func diffObjectTypes(base, target []ObjectTypeDetail) []ObjectTypeDiff {
	baseByName := make(map[string]ObjectTypeDetail, len(base))
	for _, t := range base {
		baseByName[t.Name] = t
	}
	targetByName := make(map[string]ObjectTypeDetail, len(target))
	for _, t := range target {
		targetByName[t.Name] = t
	}
	names := unionNames(base, target, func(t ObjectTypeDetail) string { return t.Name })
	out := make([]ObjectTypeDiff, 0, len(names))
	for _, name := range names {
		b, inBase := baseByName[name]
		t, inTarget := targetByName[name]
		o := ObjectTypeDiff{
			Name:        name,
			Change:      entityChange(inBase, inTarget),
			Label:       presenceField(b.Label, t.Label, inBase, inTarget),
			Description: presenceField(b.Description, t.Description, inBase, inTarget),
			Properties:  diffProperties(b.Properties, t.Properties),
		}
		if o.Change == DiffChangeNone &&
			(o.Label.Change != DiffChangeNone || o.Description.Change != DiffChangeNone ||
				slices.ContainsFunc(o.Properties, propertyDiffers)) {
			o.Change = DiffChangeChanged
		}
		out = append(out, o)
	}
	return out
}

// relationshipFieldsChanged reports whether any diffed relationship field differs.
func relationshipFieldsChanged(r RelationshipTypeDiff) bool {
	return r.Label.Change != DiffChangeNone ||
		r.Description.Change != DiffChangeNone ||
		r.SourceType.Change != DiffChangeNone ||
		r.TargetType.Change != DiffChangeNone
}

// diffRelationshipTypes unions relationship types by name and diffs their fields.
func diffRelationshipTypes(base, target []RelationshipTypeDetail) []RelationshipTypeDiff {
	baseByName := make(map[string]RelationshipTypeDetail, len(base))
	for _, t := range base {
		baseByName[t.Name] = t
	}
	targetByName := make(map[string]RelationshipTypeDetail, len(target))
	for _, t := range target {
		targetByName[t.Name] = t
	}
	names := unionNames(base, target, func(t RelationshipTypeDetail) string { return t.Name })
	out := make([]RelationshipTypeDiff, 0, len(names))
	for _, name := range names {
		b, inBase := baseByName[name]
		t, inTarget := targetByName[name]
		r := RelationshipTypeDiff{
			Name:        name,
			Change:      entityChange(inBase, inTarget),
			Label:       presenceField(b.Label, t.Label, inBase, inTarget),
			Description: presenceField(b.Description, t.Description, inBase, inTarget),
			SourceType:  presenceField(b.SourceType, t.SourceType, inBase, inTarget),
			TargetType:  presenceField(b.TargetType, t.TargetType, inBase, inTarget),
		}
		if r.Change == DiffChangeNone && relationshipFieldsChanged(r) {
			r.Change = DiffChangeChanged
		}
		out = append(out, r)
	}
	return out
}

// chipDiff computes the added/removed set difference between two string sets.
func chipDiff(before, after []string) ChipDiff {
	beforeSet := make(map[string]bool, len(before))
	for _, s := range before {
		beforeSet[s] = true
	}
	afterSet := make(map[string]bool, len(after))
	for _, s := range after {
		afterSet[s] = true
	}
	var added, removed []string
	for _, s := range after {
		if !beforeSet[s] {
			added = append(added, s)
		}
	}
	for _, s := range before {
		if !afterSet[s] {
			removed = append(removed, s)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return ChipDiff{Added: added, Removed: removed}
}

// agentFieldsChanged reports whether any diffed agent field or chip set differs.
func agentFieldsChanged(a AgentDiff) bool {
	return a.Description.Change != DiffChangeNone ||
		a.Model.Change != DiffChangeNone ||
		a.SystemPrompt.Change != DiffChangeNone ||
		a.FlowType.Change != DiffChangeNone ||
		a.Visibility.Change != DiffChangeNone ||
		len(a.Tools.Added) > 0 || len(a.Tools.Removed) > 0 ||
		len(a.BannedTools.Added) > 0 || len(a.BannedTools.Removed) > 0 ||
		len(a.Skills.Added) > 0 || len(a.Skills.Removed) > 0
}

// diffAgents unions agents by name and diffs their fields and chip sets.
func diffAgents(base, target []BundledAgent) []AgentDiff {
	baseByName := make(map[string]BundledAgent, len(base))
	for _, a := range base {
		baseByName[a.Name] = a
	}
	targetByName := make(map[string]BundledAgent, len(target))
	for _, a := range target {
		targetByName[a.Name] = a
	}
	names := unionNames(base, target, func(a BundledAgent) string { return a.Name })
	out := make([]AgentDiff, 0, len(names))
	for _, name := range names {
		b, inBase := baseByName[name]
		t, inTarget := targetByName[name]
		a := AgentDiff{
			Name:         name,
			Change:       entityChange(inBase, inTarget),
			Description:  presenceField(b.Description, t.Description, inBase, inTarget),
			Model:        presenceField(b.Model, t.Model, inBase, inTarget),
			SystemPrompt: presenceField(b.SystemPrompt, t.SystemPrompt, inBase, inTarget),
			FlowType:     presenceField(b.FlowType, t.FlowType, inBase, inTarget),
			Visibility:   presenceField(b.Visibility, t.Visibility, inBase, inTarget),
			Tools:        chipDiff(b.Tools, t.Tools),
			BannedTools:  chipDiff(b.BannedTools, t.BannedTools),
			Skills:       chipDiff(b.Skills, t.Skills),
		}
		if a.Change == DiffChangeNone && agentFieldsChanged(a) {
			a.Change = DiffChangeChanged
		}
		out = append(out, a)
	}
	return out
}

// diffBlueprintDetails compares two blueprint versions field by field. It
// always returns a non-nil diff; nil inputs are treated as empty blueprints.
// Each section is the union of entities by name, sorted alphabetically.
func diffBlueprintDetails(base, target *BlueprintDetail) *BlueprintDiff {
	if base == nil {
		base = &BlueprintDetail{}
	}
	if target == nil {
		target = &BlueprintDetail{}
	}
	return &BlueprintDiff{
		BaseVersion:       base.Version,
		TargetVersion:     target.Version,
		ObjectTypes:       diffObjectTypes(base.ObjectTypes, target.ObjectTypes),
		RelationshipTypes: diffRelationshipTypes(base.RelationshipTypes, target.RelationshipTypes),
		Agents:            diffAgents(base.Agents, target.Agents),
	}
}
