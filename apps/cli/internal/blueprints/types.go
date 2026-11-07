// Package blueprints implements the "memory blueprints" command: reading a
// structured directory of pack and agent definition files, then creating or
// updating those resources via the Memory API.
package blueprints

import (
	"encoding/json"
	"fmt"

	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
	"gopkg.in/yaml.v3"
)

// JSONBlob is a raw JSON value decoded from either JSON or YAML.
//
// json.RawMessage only implements json.Unmarshaler; yaml.v3 cannot decode a
// YAML mapping or sequence into it ("cannot unmarshal !!map into
// jsontext.Value"). JSONBlob adds a YAML decoder that converts the node to its
// JSON representation, so a pack file may declare uiConfigs/extractionPrompts
// as native YAML maps.
type JSONBlob json.RawMessage

// MarshalJSON emits the raw bytes, or null when unset (matching
// json.RawMessage(nil)).
func (b JSONBlob) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte("null"), nil
	}
	return []byte(b), nil
}

// UnmarshalJSON copies the raw bytes so the blob does not alias the input
// buffer.
func (b *JSONBlob) UnmarshalJSON(data []byte) error {
	if b == nil {
		return fmt.Errorf("blueprints.JSONBlob: UnmarshalJSON on nil pointer")
	}
	if len(data) == 0 {
		*b = nil
		return nil
	}
	*b = append(JSONBlob(nil), data...)
	return nil
}

// UnmarshalYAML decodes the YAML node into a generic value and stores its JSON
// encoding. A null/absent node leaves the blob unset.
func (b *JSONBlob) UnmarshalYAML(value *yaml.Node) error {
	if b == nil {
		return fmt.Errorf("blueprints.JSONBlob: UnmarshalYAML on nil pointer")
	}
	var v any
	if err := value.Decode(&v); err != nil {
		return err
	}
	v, err := normalizeYAMLValue(v)
	if err != nil {
		return err
	}
	if v == nil {
		*b = nil
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode YAML value as JSON: %w", err)
	}
	*b = JSONBlob(data)
	return nil
}

// Raw returns the blob as a json.RawMessage, or nil when unset.
func (b JSONBlob) Raw() json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

// normalizeYAMLValue recursively converts yaml.v3's generic mappings into the
// string-keyed shape json.Marshal accepts.
func normalizeYAMLValue(v any) (any, error) {
	switch m := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			norm, err := normalizeYAMLValue(val)
			if err != nil {
				return nil, err
			}
			out[k] = norm
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("YAML mapping key %v is not a string", k)
			}
			norm, err := normalizeYAMLValue(val)
			if err != nil {
				return nil, err
			}
			out[ks] = norm
		}
		return out, nil
	case []any:
		out := make([]any, len(m))
		for i, val := range m {
			norm, err := normalizeYAMLValue(val)
			if err != nil {
				return nil, err
			}
			out[i] = norm
		}
		return out, nil
	default:
		return v, nil
	}
}

// ──────────────────────────────────────────────
// ProjectFile — project.[json|yaml|yml] (blueprint root)
// ──────────────────────────────────────────────

// ProjectFile holds project-level settings read from project.yaml (or .json/.yml)
// at the blueprint root. All fields are optional — absent or empty fields are
// skipped when applying.
type ProjectFile struct {
	// Name/Version/Description/Author mirror the bundled blueprint manifest
	// header (see apps/server/domain/blueprints/builtin_packs/operator/project.yaml:
	// name/version/description/author). Name keys the per-project model-override
	// store, so a blueprint's identity survives install even when no name was
	// otherwise derivable from the source path.
	Name        string `json:"name"        yaml:"name"`
	Version     string `json:"version"     yaml:"version"`
	Description string `json:"description" yaml:"description"`
	Author      string `json:"author"      yaml:"author"`

	ProjectInfo string `json:"projectInfo" yaml:"projectInfo"`

	// SourceFile is the path from which this file was loaded (not serialised).
	SourceFile string `json:"-" yaml:"-"`
}

// ──────────────────────────────────────────────
// PackFile — schemas/<name>.[json|yaml|yml]
// ──────────────────────────────────────────────

// PackFile is the top-level structure parsed from a file in the schemas/ directory
// (or packs/ for backward compatibility).
type PackFile struct {
	Name              string                `json:"name"             yaml:"name"`
	Version           string                `json:"version"          yaml:"version"`
	Description       string                `json:"description"      yaml:"description"`
	Author            string                `json:"author"           yaml:"author"`
	License           string                `json:"license"          yaml:"license"`
	RepositoryURL     string                `json:"repositoryUrl"    yaml:"repositoryUrl"`
	DocumentationURL  string                `json:"documentationUrl" yaml:"documentationUrl"`
	ObjectTypes       []ObjectTypeDef       `json:"objectTypes"      yaml:"objectTypes"`
	RelationshipTypes []RelationshipTypeDef `json:"relationshipTypes" yaml:"relationshipTypes"`

	// Snake_case aliases accepted by the server's PackManifest.UnmarshalJSON
	// (#2317). They are promoted into the canonical fields by
	// normalizeSchemaAliases and cleared, so they never reach apply/marshal.
	ObjectTypeSchemas       []ObjectTypeDef       `json:"objectTypeSchemas,omitempty" yaml:"objectTypeSchemas,omitempty"`
	RelationshipTypeSchemas []RelationshipTypeDef `json:"relationshipTypeSchemas,omitempty" yaml:"relationshipTypeSchemas,omitempty"`
	UIConfigs               JSONBlob              `json:"uiConfigs"        yaml:"uiConfigs"`
	ExtractionPrompts       JSONBlob              `json:"extractionPrompts" yaml:"extractionPrompts"`

	// Migrations carries the pack's optional migration-hint block. The server
	// accepts and validates it; without this field the CLI silently dropped it,
	// so a blueprint's type/property renames never reached an upgrade.
	Migrations *sdkschemas.SchemaMigrationHints `json:"migrations,omitempty" yaml:"migrations,omitempty"`

	// SourceFile is the path from which this pack was loaded (not serialised).
	SourceFile string `json:"-" yaml:"-"`
}

// normalizeSchemaAliases mirrors the server: accept the snake_case
// objectTypeSchemas/relationshipTypeSchemas spellings, with the canonical
// camelCase field winning when both are present (#2317).
func (p *PackFile) normalizeSchemaAliases() {
	if len(p.ObjectTypes) == 0 && len(p.ObjectTypeSchemas) > 0 {
		p.ObjectTypes = p.ObjectTypeSchemas
	}
	if len(p.RelationshipTypes) == 0 && len(p.RelationshipTypeSchemas) > 0 {
		p.RelationshipTypes = p.RelationshipTypeSchemas
	}
	p.ObjectTypeSchemas = nil
	p.RelationshipTypeSchemas = nil
}

// ObjectTypeDef represents a single object type definition inside a pack file.
type ObjectTypeDef struct {
	Name        string         `json:"name"        yaml:"name"`
	Label       string         `json:"label"       yaml:"label"`
	Labels      []string       `json:"labels,omitempty" yaml:"labels,omitempty"`
	Description string         `json:"description" yaml:"description"`
	Properties  map[string]any `json:"properties"  yaml:"properties"`
	// Required lists the property names the type marks required. Preserved
	// through install/update so a pack authored with a required list reaches
	// the schema registry unchanged.
	Required []string `json:"required,omitempty" yaml:"required,omitempty"`
	// UI is the type-level appearance block (e.g. {"icon":"file-text",
	// "color":"#3B82F6"}). When absent the pack-level uiConfigs entry for the
	// type is used. Carried as a raw map so free-form keys round-trip.
	UI map[string]any `json:"ui,omitempty" yaml:"ui,omitempty"`
	// Embedding/Extraction are behavioural blocks (e.g. embedding.mode,
	// extraction.enabled). Carried as raw maps so they round-trip losslessly.
	Embedding  map[string]any `json:"embedding,omitempty" yaml:"embedding,omitempty"`
	Extraction map[string]any `json:"extraction,omitempty" yaml:"extraction,omitempty"`
	// ScopeKey is the effective scope-key declaration (issue #1148): the
	// property on this type that scopes it to a parent document, plus the
	// reference it points at. It is the canonical (camelCase) spelling; the
	// snake_case alias below is promoted into it on decode. omitempty keeps an
	// absent declaration out of the marshalled payload entirely (a JSON null
	// would otherwise be re-emitted for every type and rejected by the server).
	ScopeKey map[string]any `json:"scopeKey,omitempty" yaml:"scopeKey,omitempty"`

	// ScopeKeySnake is the accepted top-level snake_case alias (`scope_key`).
	// On decode it is promoted into ScopeKey and cleared, so it is never emitted.
	// Precedence: the canonical `scopeKey` wins when both are present, matching
	// the inner-property alias handling.
	ScopeKeySnake map[string]any `json:"scope_key,omitempty" yaml:"scope_key,omitempty"`

	// Object-driven work configuration (Kanban board). Mirrors the server's
	// ObjectTypeDef so memory blueprints round-trips the board-enabled fields.
	BoardEnabled      bool     `json:"boardEnabled,omitempty"      yaml:"boardEnabled,omitempty"`
	AllowedStatuses   []string `json:"allowedStatuses,omitempty"   yaml:"allowedStatuses,omitempty"`
	SkipEmbeddings    bool     `json:"skipEmbeddings,omitempty"    yaml:"skipEmbeddings,omitempty"`
	SkipExtraction    bool     `json:"skipExtraction,omitempty"    yaml:"skipExtraction,omitempty"`
	ExcludeFromSearch bool     `json:"excludeFromSearch,omitempty" yaml:"excludeFromSearch,omitempty"`
}

// normalizeScopeKey folds the top-level snake_case alias into the canonical
// ScopeKey and drops the alias, so the effective declaration has a single
// representation. Canonical `scopeKey` wins when both are present.
func (o *ObjectTypeDef) normalizeScopeKey() {
	if o.ScopeKey == nil && o.ScopeKeySnake != nil {
		o.ScopeKey = o.ScopeKeySnake
	}
	o.ScopeKeySnake = nil
}

// UnmarshalJSON decodes the canonical `scopeKey` and its `scope_key` alias and
// normalizes them (canonical wins).
func (o *ObjectTypeDef) UnmarshalJSON(data []byte) error {
	type alias ObjectTypeDef
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*o = ObjectTypeDef(a)
	o.normalizeScopeKey()
	return nil
}

// UnmarshalYAML decodes the canonical `scopeKey` and its `scope_key` alias and
// normalizes them (canonical wins).
func (o *ObjectTypeDef) UnmarshalYAML(value *yaml.Node) error {
	type alias ObjectTypeDef
	var a alias
	if err := value.Decode(&a); err != nil {
		return err
	}
	*o = ObjectTypeDef(a)
	o.normalizeScopeKey()
	return nil
}

// RelationshipTypeDef represents a single relationship type definition inside a pack file.
// It accepts both singular (sourceType/targetType) and plural (sourceTypes/targetTypes)
// field names. Plural arrays take precedence when both are present.
type RelationshipTypeDef struct {
	Name        string   `json:"name"        yaml:"name"`
	Label       string   `json:"label"       yaml:"label"`
	Description string   `json:"description" yaml:"description"`
	SourceType  string   `json:"sourceType"  yaml:"sourceType"`
	TargetType  string   `json:"targetType"  yaml:"targetType"`
	SourceTypes []string `json:"sourceTypes" yaml:"sourceTypes"`
	TargetTypes []string `json:"targetTypes" yaml:"targetTypes"`
	// Properties carries relationship-level property definitions. The schemas
	// service accepts them and the server's manifest round-trips them; the CLI
	// must preserve them so install/update does not silently drop them.
	Properties map[string]any `json:"properties,omitempty" yaml:"properties,omitempty"`
	// Required names the relationship-level properties the server enforces on
	// every write of this type. It is preserved for the same reason as
	// Properties: dropping it here makes the constraint unreachable from a
	// pack, while a schema created through the API keeps it.
	Required []string `json:"required,omitempty" yaml:"required,omitempty"`
}

// GetSourceTypes returns the effective source types, preferring the plural
// array field over the singular string.
func (r RelationshipTypeDef) GetSourceTypes() []string {
	if len(r.SourceTypes) > 0 {
		return r.SourceTypes
	}
	if r.SourceType != "" {
		return []string{r.SourceType}
	}
	return nil
}

// GetTargetTypes returns the effective target types, preferring the plural
// array field over the singular string.
func (r RelationshipTypeDef) GetTargetTypes() []string {
	if len(r.TargetTypes) > 0 {
		return r.TargetTypes
	}
	if r.TargetType != "" {
		return []string{r.TargetType}
	}
	return nil
}

// ──────────────────────────────────────────────
// AgentFile — agents/<name>.[json|yaml|yml]
// ──────────────────────────────────────────────

// AgentFile is the top-level structure parsed from a file in the agents/ directory.
type AgentFile struct {
	Name            string                     `json:"name"            yaml:"name"`
	Description     string                     `json:"description"     yaml:"description"`
	SystemPrompt    string                     `json:"systemPrompt"    yaml:"systemPrompt"`
	Model           *AgentModel                `json:"model"           yaml:"model"`
	Tools           []string                   `json:"tools"           yaml:"tools"`
	Skills          []string                   `json:"skills"          yaml:"skills"`
	IsDefault       bool                       `json:"isDefault"       yaml:"isDefault"`
	MaxSteps        *int                       `json:"maxSteps"        yaml:"maxSteps"`
	DefaultTimeout  *int                       `json:"defaultTimeout"  yaml:"defaultTimeout"`
	Visibility      string                     `json:"visibility"      yaml:"visibility"`
	DispatchMode    string                     `json:"dispatchMode"    yaml:"dispatchMode"`
	Config          map[string]any             `json:"config"          yaml:"config"`
	WorkspaceConfig map[string]any             `json:"workspaceConfig" yaml:"workspaceConfig"`
	ToolPolicies    map[string]AgentToolPolicy `json:"toolPolicies"    yaml:"toolPolicies"`
	// WorkConfig is the object-driven work configuration, sent through to the
	// server's definition workConfig.
	WorkConfig map[string]any `json:"workConfig,omitempty" yaml:"workConfig,omitempty"`
	// TriggerType/ReactionConfig/CronSchedule configure the runtime agent that
	// picks object-driven work up.
	TriggerType    string          `json:"triggerType,omitempty"    yaml:"triggerType,omitempty"`
	ReactionConfig *ReactionConfig `json:"reactionConfig,omitempty" yaml:"reactionConfig,omitempty"`
	CronSchedule   string          `json:"cronSchedule,omitempty"   yaml:"cronSchedule,omitempty"`

	// SourceFile is the path from which this agent was loaded (not serialised).
	SourceFile string `json:"-" yaml:"-"`
}

// effectiveTriggerType mirrors the server applier's defaulting
// (apps/server/domain/blueprints/apply.go applyAgent): an empty triggerType is a
// reaction trigger when a reactionConfig is present, otherwise a schedule. The
// validator and the CLI applier both branch on it, so it lives with the type.
func (a AgentFile) effectiveTriggerType() string {
	if a.TriggerType != "" {
		return a.TriggerType
	}
	if a.ReactionConfig != nil {
		return "reaction"
	}
	return "schedule"
}

// hasRuntimeTrigger reports whether the manifest configures runtime trigger
// behaviour, i.e. whether install materializes a runtime kb.agents row bound to
// the definition. Without one the agent installs as a definition only: nothing
// picks work up and nothing runs until an operator triggers it by hand (#321).
func (a AgentFile) hasRuntimeTrigger() bool {
	return a.TriggerType != "" || a.ReactionConfig != nil || a.CronSchedule != ""
}

// runtimeTriggerType returns the trigger type of the runtime agent install will
// ensure, or "" when the agent installs as a definition only. "manual" is a
// first-class value: the runtime agent is created but the trigger service
// registers no automatic trigger for it, so it only runs when invoked through
// the manual trigger endpoint (POST /api/admin/agents/:id/trigger, surfaced as
// `memory agents trigger`).
func (a AgentFile) runtimeTriggerType() string {
	if !a.hasRuntimeTrigger() {
		return ""
	}
	return a.effectiveTriggerType()
}

// ReactionConfig mirrors the server's reaction trigger configuration for a
// runtime agent, so memory blueprints round-trips the block losslessly.
type ReactionConfig struct {
	ObjectTypes          []string `json:"objectTypes"                   yaml:"objectTypes"`
	Events               []string `json:"events"                        yaml:"events"`
	ConcurrencyStrategy  string   `json:"concurrencyStrategy,omitempty" yaml:"concurrencyStrategy,omitempty"`
	IgnoreAgentTriggered *bool    `json:"ignoreAgentTriggered,omitempty" yaml:"ignoreAgentTriggered,omitempty"`
	IgnoreSelfTriggered  *bool    `json:"ignoreSelfTriggered,omitempty" yaml:"ignoreSelfTriggered,omitempty"`
}

// AgentToolPolicy controls how a specific tool is invoked by an agent.
type AgentToolPolicy struct {
	Confirm bool   `json:"confirm" yaml:"confirm"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// AgentModel holds model configuration for an agent definition file.
type AgentModel struct {
	Name string `json:"name" yaml:"name"`
	// Provider is the optional provider-instance slug that serves Name. When
	// set, the install preflight folds it into the canonical "provider/model"
	// name (matching the server's NormalizeAgentModelConfig) before applying.
	Provider       string   `json:"provider"       yaml:"provider"`
	Temperature    *float32 `json:"temperature"    yaml:"temperature"`
	MaxTokens      *int     `json:"maxTokens"      yaml:"maxTokens"`
	EnableThinking *bool    `json:"enableThinking" yaml:"enableThinking"`
}

// ──────────────────────────────────────────────
// Seed records — seed/objects/<Type>.jsonl
//                seed/relationships/<Type>.jsonl
// ──────────────────────────────────────────────

const (
	// UntypedSeedBaseName is the file-name stem used for seed records whose
	// type is empty. A bare "<Type>.jsonl" naming scheme would emit a file
	// literally named ".jsonl", which is invisible in directory listings and
	// easily mistaken for noise (issue #323).
	UntypedSeedBaseName = "_untyped"

	// UntypedTypeName is the visible placeholder assigned to records read back
	// from an UntypedSeedBaseName file. The graph API rejects an empty type on
	// create, so a dump of legacy untyped objects must restore as a real type.
	UntypedTypeName = "Untyped"
)

// SeedObjectRecord is one JSONL line from a seed/objects/<Type>.jsonl file.
// Each line represents one object to create or upsert.
type SeedObjectRecord struct {
	Type       string         `json:"type"`
	Key        string         `json:"key,omitempty"`
	Status     string         `json:"status,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Labels     []string       `json:"labels,omitempty"`
	// Assignee is the work lane for board-enabled types (the agent identity that
	// owns the item). Round-tripped on dump/seed.
	Assignee string `json:"assignee,omitempty"`

	// SourceFile is populated by the loader (not serialised).
	SourceFile string `json:"-"`
}

// SeedRelationshipRecord is one JSONL line from a seed/relationships/<Type>.jsonl file.
// Use SrcKey/DstKey to reference objects by key within the same seed directory,
// or SrcID/DstID to supply raw server-side entity IDs directly.
type SeedRelationshipRecord struct {
	Type       string         `json:"type"`
	SrcKey     string         `json:"srcKey,omitempty"`
	DstKey     string         `json:"dstKey,omitempty"`
	SrcID      string         `json:"srcId,omitempty"`
	DstID      string         `json:"dstId,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Weight     *float32       `json:"weight,omitempty"`

	// SourceFile is populated by the loader (not serialised).
	SourceFile string `json:"-"`
}

// SeedResult summarises the outcome of a seed run.
type SeedResult struct {
	ObjectsCreated int
	ObjectsUpdated int
	ObjectsSkipped int
	ObjectsFailed  int
	RelsCreated    int
	RelsFailed     int
}

// ──────────────────────────────────────────────
// SkillFile — skills/<name>.md
// ──────────────────────────────────────────────

// SkillFile is the top-level structure parsed from a file in the skills/
// directory. Each file must begin with a YAML frontmatter block delimited by
// "---", followed by the skill content (Markdown).
type SkillFile struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Version     string         `yaml:"version"`
	License     string         `yaml:"license"`
	Metadata    map[string]any `yaml:"metadata,omitempty"`

	// Content holds the Markdown body after the closing frontmatter delimiter.
	Content string `yaml:"-"`

	// SourceFile is the path from which this skill was loaded (not serialised).
	SourceFile string `yaml:"-"`
}

// ──────────────────────────────────────────────
// BlueprintsResult — outcome of processing one resource
// ──────────────────────────────────────────────

// BlueprintsAction describes what happened to a resource.
type BlueprintsAction string

const (
	BlueprintsActionCreated BlueprintsAction = "created"
	BlueprintsActionUpdated BlueprintsAction = "updated"
	BlueprintsActionSkipped BlueprintsAction = "skipped"
	BlueprintsActionError   BlueprintsAction = "error"
)

// BlueprintsResult records the outcome of applying a single resource file.
type BlueprintsResult struct {
	ResourceType string           // "project", "pack", "agent", "skill", or "seed"
	Name         string           // resource name
	SourceFile   string           // file path it was loaded from
	Action       BlueprintsAction // what happened
	Error        error            // non-nil when Action == BlueprintsActionError
	// RuntimeTrigger is the trigger type of the runtime agent ensured for an
	// agent result, or "" when the manifest installs a definition only (#321).
	// It lets the install output tell the operator which agents will actually
	// run instead of printing an identical "created agent" line for both.
	RuntimeTrigger string
}
