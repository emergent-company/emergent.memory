package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"slices"
)

// BundledAgent is one agent definition rendered on the blueprint detail view.
// It is produced from a blueprint record's manifest (bundledAgentsFromManifest)
// and doubles as the diff unit for agent comparisons.
type BundledAgent struct {
	Name         string          `yaml:"name"`
	Description  string          `yaml:"description"`
	SystemPrompt string          `yaml:"systemPrompt"`
	Model        string          `yaml:"model"`
	Tools        []string        `yaml:"tools"`
	BannedTools  []string        `yaml:"bannedTools"`
	Skills       []string        `yaml:"skills"`
	Config       map[string]any  `yaml:"config"`
	Visibility   string          `yaml:"visibility"`
	UI           *BundledAgentUI `json:"ui,omitempty" yaml:"ui,omitempty"`
	// WorkConfig is the object-driven work configuration, carried through to
	// the server's definition workConfig at apply time.
	WorkConfig map[string]any `yaml:"workConfig"`
	// TriggerType/ReactionConfig/CronSchedule configure the runtime agent that
	// picks object-driven work up.
	TriggerType    string                 `yaml:"triggerType"`
	ReactionConfig *BundledReactionConfig `yaml:"reactionConfig"`
	CronSchedule   string                 `yaml:"cronSchedule"`
	// DispatchMode/DefaultQueue/MaxSteps are the runtime scheduling knobs a
	// board worker declares (queued dispatch, a named queue, a step budget).
	DispatchMode string `yaml:"dispatchMode"`
	DefaultQueue string `yaml:"defaultQueue"`
	MaxSteps     *int   `yaml:"maxSteps"`
}

// BundledAgentUI is an agent's inline appearance block (icon + color) as
// declared by a blueprint agent manifest. It mirrors memory's AgentUIManifest
// and (with its json tags) doubles as the `ui` member of the manifest form
// blueprintAgent POSTs, so the field round-trips: agents/*.yaml → manifest →
// applied blueprint detail. Both fields optional; nil/empty means no
// appearance.
type BundledAgentUI struct {
	Icon  string `json:"icon,omitempty" yaml:"icon,omitempty"`
	Color string `json:"color,omitempty" yaml:"color,omitempty"`
}

// BundledReactionConfig is a board worker's reaction trigger block. It mirrors
// the server's agents.ReactionConfig (events as strings) and, with its json
// tags, doubles as the `reactionConfig` member of the manifest form the
// gateway POSTs, so the block round-trips agents/*.yaml → manifest → applied
// blueprint detail.
type BundledReactionConfig struct {
	ObjectTypes          []string `json:"objectTypes" yaml:"objectTypes"`
	Events               []string `json:"events" yaml:"events"`
	ConcurrencyStrategy  string   `json:"concurrencyStrategy,omitempty" yaml:"concurrencyStrategy,omitempty"`
	IgnoreAgentTriggered *bool    `json:"ignoreAgentTriggered,omitempty" yaml:"ignoreAgentTriggered,omitempty"`
	IgnoreSelfTriggered  *bool    `json:"ignoreSelfTriggered,omitempty" yaml:"ignoreSelfTriggered,omitempty"`
}

// blueprintSource maps a blueprint record's origin onto the available-list
// source label: built-in blueprints are "builtin", every other published
// blueprint is "registry".
func blueprintSource(origin string) string {
	if origin == "builtin" {
		return "builtin"
	}
	return "registry"
}

// buildAvailableList maps the published blueprint records in scope (global +
// project-private) into the available list: the latest published version per
// name, excluding those already applied (matched by blueprint id or
// name+version). Built-in blueprints surface with Source "builtin"; every other
// blueprint surfaces as "registry". A published version strictly newer than the
// applied one surfaces with Upgrade=true.
func buildAvailableList(records []BlueprintRecord, applied []AppliedBlueprint) []AvailableSchemaItem {
	appliedVersion := make(map[string]string, len(applied))
	appliedID := make(map[string]bool, len(applied))
	for _, ap := range applied {
		appliedVersion[ap.Name] = ap.Version
		appliedID[ap.BlueprintID] = true
	}

	// Collapse to the highest published version per name. Drafts surface in the
	// drafts section, never here.
	latest := map[string]BlueprintRecord{}
	for _, r := range records {
		if r.Status == "draft" {
			continue
		}
		if cur, ok := latest[r.Name]; !ok || versionGreater(r.Version, cur.Version) {
			latest[r.Name] = r
		}
	}

	var out []AvailableSchemaItem
	for name, r := range latest {
		upgrade := false
		if iv, ok := appliedVersion[name]; ok {
			if !versionGreater(r.Version, iv) {
				continue
			}
			upgrade = true
		} else if appliedID[r.ID] {
			continue
		}
		out = append(out, AvailableSchemaItem{
			ID:          r.ID,
			Name:        r.Name,
			Version:     r.Version,
			Description: r.Description,
			Author:      r.Author,
			Source:      blueprintSource(r.Origin),
			Origin:      r.Origin,
			Upgrade:     upgrade,
		})
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

// blueprintSourceLabel renders a GitHub provenance URL as a compact
// "host/owner/repo" label (scheme and trailing .git removed) for the installed
// row's source line. The full URL stays the link href; this is display only.
func blueprintSourceLabel(sourceURL string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(sourceURL, "https://"), "http://")
	s = strings.TrimSuffix(s, ".git")
	return strings.TrimSuffix(s, "/")
}

// blueprintUpdateAction is the rendered state of the Update control on one
// installed, repo-imported blueprint.
type blueprintUpdateAction int

const (
	// blueprintUpdateActionHidden shows no Update control (not repo-imported, or
	// no update available and no relevant job).
	blueprintUpdateActionHidden blueprintUpdateAction = iota
	// blueprintUpdateActionAvailable offers the live Update action.
	blueprintUpdateActionAvailable
	// blueprintUpdateActionQueued / Running show a disabled pending control.
	blueprintUpdateActionQueued
	blueprintUpdateActionRunning
	// blueprintUpdateActionSucceeded shows the success state and clears the
	// available-update badge.
	blueprintUpdateActionSucceeded
	// blueprintUpdateActionFailed shows the error and offers a retry.
	blueprintUpdateActionFailed
)

// UpdateAction derives the row's Update-control state from the available-update
// signal and the latest update job (which the page handler loads from the
// update-status endpoint, so the state survives reloads). Only an update job
// drives the control; a check job never does.
func (b AppliedBlueprint) UpdateAction() blueprintUpdateAction {
	if j := b.LatestJob; j != nil && j.Action == blueprintUpdateJobActionUpdate {
		switch j.Status {
		case blueprintUpdateJobStatusPending:
			return blueprintUpdateActionQueued
		case blueprintUpdateJobStatusRunning:
			return blueprintUpdateActionRunning
		case blueprintUpdateJobStatusSucceeded:
			return blueprintUpdateActionSucceeded
		case blueprintUpdateJobStatusFailed:
			return blueprintUpdateActionFailed
		}
	}
	if b.UpdateBadgeVisible() {
		return blueprintUpdateActionAvailable
	}
	return blueprintUpdateActionHidden
}

// UpdateBadgeVisible reports whether the "Update available" badge should render.
// A succeeded update job clears it: applying upstream leaves the (stale) check
// record reporting updateAvailable until the next check runs, so the job's
// terminal success is the authoritative signal that the update landed.
func (b AppliedBlueprint) UpdateBadgeVisible() bool {
	if b.SourceURL == "" || !b.UpdateAvailable || b.LatestVersion == "" {
		return false
	}
	if j := b.LatestJob; j != nil && j.Action == blueprintUpdateJobActionUpdate && j.Status == blueprintUpdateJobStatusSucceeded {
		return false
	}
	return true
}

// IsNoop reports whether a succeeded update job applied no change (upstream was
// already current, or content drifted at the same version).
func (j *BlueprintUpdateJob) IsNoop() bool {
	if j == nil || len(j.Result) == 0 {
		return false
	}
	var r struct {
		Noop bool `json:"noop"`
	}
	if err := json.Unmarshal(j.Result, &r); err != nil {
		return false
	}
	return r.Noop
}

// blueprintUpdateFailureLabel renders the failed job's error as a compact,
// single-line label (full text stays in the element's title attribute).
func blueprintUpdateFailureLabel(msg string) string {
	s := strings.Join(strings.Fields(msg), " ")
	const maxRunes = 120
	if runes := []rune(s); len(runes) > maxRunes {
		s = string(runes[:maxRunes]) + "…"
	}
	if s == "" {
		return "Update failed"
	}
	return "Update failed: " + s
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
	// comparison. Empty for registry schemas.
	Versions []BlueprintVersionView

	// Status is the blueprint record's lifecycle status ("draft" | "published");
	// empty for registry schemas.
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
	// blueprint record (not a registry schema) can be forked.
	CanDeriveVersion bool

	// FlashMsg/FlashErr carry PRG feedback from the "Save as new version"
	// derive action.
	FlashMsg string
	FlashErr error
}

// ObjectTypeDetail is one object type with its property definitions.
type ObjectTypeDetail struct {
	Name        string
	Label       string
	Description string
	Properties  []PropertyDetail

	// BoardEnabled reports whether this type is board-enabled, and
	// AllowedStatuses lists the work statuses it allows (declaration order).
	// Both come from the schema definition; zero values mean "not a board type".
	BoardEnabled    bool
	AllowedStatuses []string
}

// PropertyDetail is one property of an object type. Title is the property's
// optional human-friendly display label (JSON-Schema "title"); Name is the
// machine key. Display code shows Title when set and falls back to Name.
type PropertyDetail struct {
	Name        string
	Title       string
	Type        string
	Widget      string
	Description string
	Required    bool
}

// PropertyLabel is the property's display name: its Title when declared, else
// the machine key. The key stays visible in the UI as secondary text.
func (p PropertyDetail) PropertyLabel() string {
	if p.Title != "" {
		return p.Title
	}
	return p.Name
}

// ShowsKey reports whether the machine key should be rendered as secondary text
// — only when a distinct label makes it worth showing.
func (p PropertyDetail) ShowsKey() bool {
	return p.Title != "" && !strings.EqualFold(p.Title, p.Name)
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
	Title       FieldDiff
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
			Name:           a.Name,
			Description:    a.Description,
			SystemPrompt:   a.SystemPrompt,
			Model:          model,
			Tools:          a.Tools,
			BannedTools:    a.BannedTools,
			Skills:         a.Skills,
			Visibility:     a.Visibility,
			Config:         a.Config,
			UI:             a.UI,
			WorkConfig:     a.WorkConfig,
			TriggerType:    a.TriggerType,
			ReactionConfig: a.ReactionConfig,
			CronSchedule:   a.CronSchedule,
			DispatchMode:   a.DispatchMode,
			DefaultQueue:   a.DefaultQueue,
			MaxSteps:       a.MaxSteps,
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
			Name:            strAny(t["name"]),
			Label:           strAny(t["label"]),
			Description:     strAny(t["description"]),
			Properties:      propertiesFromMap(t["properties"]),
			BoardEnabled:    boolAny(t["boardEnabled"]),
			AllowedStatuses: strSlice(t["allowedStatuses"]),
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
			Title:       strAny(p["title"]),
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
		p.Title.Change != DiffChangeNone ||
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
			Title:       presenceField(b.Title, t.Title, inBase, inTarget),
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
