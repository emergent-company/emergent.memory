package blueprints

import "github.com/emergent-company/emergent.memory/domain/schemas"

// BlueprintManifest is the typed shape of a blueprint's opaque manifest JSON.
// Packs, agents, and skills are materialized with create-or-update (by name)
// semantics; seed objects/relationships are created idempotently.
type BlueprintManifest struct {
	Project *ProjectManifest `json:"project,omitempty"`
	Packs   []PackManifest   `json:"packs,omitempty"`
	Agents  []AgentManifest  `json:"agents,omitempty"`
	Skills  []SkillManifest  `json:"skills,omitempty"`
	Seed    *SeedManifest    `json:"seed,omitempty"`
}

// ProjectManifest carries optional project-level metadata (currently unused by apply).
type ProjectManifest struct {
	Description string `json:"description"`
}

// PackManifest describes a schema pack to create and assign.
type PackManifest struct {
	Name              string                        `json:"name"`
	Version           string                        `json:"version"`
	Description       string                        `json:"description"`
	Author            string                        `json:"author"`
	Migrations        *schemas.SchemaMigrationHints `json:"migrations,omitempty"`
	ObjectTypes       []ObjectTypeDef               `json:"objectTypes,omitempty"`
	RelationshipTypes []RelationshipTypeDef         `json:"relationshipTypes,omitempty"`
}

// ObjectTypeDef is a single object type schema in a pack manifest. Labels,
// Embedding, Extraction, and UI are carried as raw maps so behavioural keys
// (e.g. extraction.enabled:false, embedding.mode/field) survive the manifest
// round-trip losslessly rather than being dropped by a typed struct.
type ObjectTypeDef struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Properties  map[string]any `json:"properties"`
	Labels      []string       `json:"labels,omitempty"`
	Embedding   map[string]any `json:"embedding,omitempty"`
	Extraction  map[string]any `json:"extraction,omitempty"`
	UI          map[string]any `json:"ui,omitempty"`
}

// RelationshipTypeDef is a single relationship type schema in a pack manifest.
// Properties carries any relationship-level sub-block (e.g. a description
// property) as a raw map for lossless pass-through.
type RelationshipTypeDef struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	SourceType  string         `json:"sourceType"`
	TargetType  string         `json:"targetType"`
	Properties  map[string]any `json:"properties,omitempty"`
}

// AgentManifest describes an agent definition to create or update.
type AgentManifest struct {
	Name            string                     `json:"name"`
	Description     string                     `json:"description"`
	SystemPrompt    string                     `json:"systemPrompt"`
	Model           *AgentModelManifest        `json:"model,omitempty"`
	Tools           []string                   `json:"tools,omitempty"`
	BannedTools     []string                   `json:"bannedTools,omitempty"`
	Skills          []string                   `json:"skills,omitempty"`
	FlowType        string                     `json:"flowType,omitempty"`
	IsDefault       *bool                      `json:"isDefault,omitempty"`
	MaxSteps        *int                       `json:"maxSteps,omitempty"`
	DefaultTimeout  *int                       `json:"defaultTimeout,omitempty"`
	Visibility      string                     `json:"visibility,omitempty"`
	DispatchMode    string                     `json:"dispatchMode,omitempty"`
	Config          map[string]any             `json:"config,omitempty"`
	WorkspaceConfig map[string]any             `json:"workspaceConfig,omitempty"`
	ToolPolicies    map[string]AgentToolPolicy `json:"toolPolicies,omitempty"`
	UI              *AgentUIManifest           `json:"ui,omitempty"`
}

// AgentUIManifest describes an agent's inline appearance block, mirroring the
// object-type registry's inline ui block. Both fields optional.
type AgentUIManifest struct {
	Icon  string `json:"icon,omitempty" yaml:"icon,omitempty"`
	Color string `json:"color,omitempty" yaml:"color,omitempty"`
}

// AgentModelManifest describes the model config for an agent.
// Empty Name means the provider default model is used.
type AgentModelManifest struct {
	Name           string   `json:"name"`
	Temperature    *float32 `json:"temperature,omitempty"`
	MaxTokens      *int     `json:"maxTokens,omitempty"`
	EnableThinking *bool    `json:"enableThinking,omitempty"`
}

// AgentToolPolicy is a per-tool confirmation policy.
type AgentToolPolicy struct {
	Confirm bool   `json:"confirm"`
	Message string `json:"message"`
}

// SkillManifest describes a global skill to create or update.
type SkillManifest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// SeedManifest describes seed graph objects and relationships.
type SeedManifest struct {
	Objects       []SeedObjectRecord       `json:"objects,omitempty"`
	Relationships []SeedRelationshipRecord `json:"relationships,omitempty"`
}

// SeedObjectRecord is a seed graph object identified by (type, key).
type SeedObjectRecord struct {
	Type       string         `json:"type"`
	Key        string         `json:"key"`
	Status     string         `json:"status"`
	Properties map[string]any `json:"properties"`
	Labels     []string       `json:"labels,omitempty"`
}

// SeedRelationshipRecord is a seed graph relationship whose endpoints are
// referenced by the keys of seed objects. When SrcType/DstType are set they
// disambiguate the key lookup (type + key is unambiguous); otherwise the key
// is resolved alone.
type SeedRelationshipRecord struct {
	Type       string         `json:"type"`
	SrcKey     string         `json:"srcKey,omitempty"`
	DstKey     string         `json:"dstKey,omitempty"`
	SrcType    string         `json:"srcType,omitempty"`
	DstType    string         `json:"dstType,omitempty"`
	Properties map[string]any `json:"properties"`
}
