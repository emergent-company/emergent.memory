// Package blueprints implements the "memory blueprints" command: reading a
// structured directory of pack and agent definition files, then creating or
// updating those resources via the Memory API.
package blueprints

import "encoding/json"

// ──────────────────────────────────────────────
// PackFile — packs/<name>.[json|yaml|yml]
// ──────────────────────────────────────────────

// PackFile is the top-level structure parsed from a file in the packs/ directory.
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
	UIConfigs         json.RawMessage       `json:"uiConfigs"        yaml:"uiConfigs"`
	ExtractionPrompts json.RawMessage       `json:"extractionPrompts" yaml:"extractionPrompts"`

	// SourceFile is the path from which this pack was loaded (not serialised).
	SourceFile string `json:"-" yaml:"-"`
}

// ObjectTypeDef represents a single object type definition inside a pack file.
type ObjectTypeDef struct {
	Name        string         `json:"name"        yaml:"name"`
	Label       string         `json:"label"       yaml:"label"`
	Description string         `json:"description" yaml:"description"`
	Properties  map[string]any `json:"properties"  yaml:"properties"`
}

// RelationshipTypeDef represents a single relationship type definition inside a pack file.
type RelationshipTypeDef struct {
	Name        string `json:"name"        yaml:"name"`
	Label       string `json:"label"       yaml:"label"`
	Description string `json:"description" yaml:"description"`
	SourceType  string `json:"sourceType"  yaml:"sourceType"`
	TargetType  string `json:"targetType"  yaml:"targetType"`
}

// ──────────────────────────────────────────────
// AgentFile — agents/<name>.[json|yaml|yml]
// ──────────────────────────────────────────────

// AgentFile is the top-level structure parsed from a file in the agents/ directory.
type AgentFile struct {
	Name            string         `json:"name"            yaml:"name"`
	Description     string         `json:"description"     yaml:"description"`
	SystemPrompt    string         `json:"systemPrompt"    yaml:"systemPrompt"`
	Model           *AgentModel    `json:"model"           yaml:"model"`
	Tools           []string       `json:"tools"           yaml:"tools"`
	FlowType        string         `json:"flowType"        yaml:"flowType"`
	IsDefault       bool           `json:"isDefault"       yaml:"isDefault"`
	MaxSteps        *int           `json:"maxSteps"        yaml:"maxSteps"`
	DefaultTimeout  *int           `json:"defaultTimeout"  yaml:"defaultTimeout"`
	Visibility      string         `json:"visibility"      yaml:"visibility"`
	Config          map[string]any `json:"config"          yaml:"config"`
	WorkspaceConfig map[string]any `json:"workspaceConfig" yaml:"workspaceConfig"`

	// SourceFile is the path from which this agent was loaded (not serialised).
	SourceFile string `json:"-" yaml:"-"`
}

// AgentModel holds model configuration for an agent definition file.
type AgentModel struct {
	Name        string   `json:"name"        yaml:"name"`
	Temperature *float32 `json:"temperature" yaml:"temperature"`
	MaxTokens   *int     `json:"maxTokens"   yaml:"maxTokens"`
}

// ──────────────────────────────────────────────
// Seed records — seed/objects/<Type>.jsonl
//                seed/relationships/<Type>.jsonl
// ──────────────────────────────────────────────

// SeedObjectRecord is one JSONL line from a seed/objects/<Type>.jsonl file.
// Each line represents one object to create or upsert.
type SeedObjectRecord struct {
	Type       string         `json:"type"`
	Key        string         `json:"key,omitempty"`
	Status     string         `json:"status,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Labels     []string       `json:"labels,omitempty"`

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
	ResourceType string           // "pack" or "agent"
	Name         string           // resource name
	SourceFile   string           // file path it was loaded from
	Action       BlueprintsAction // what happened
	Error        error            // non-nil when Action == BlueprintsActionError
}
