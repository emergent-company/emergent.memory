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
	Name        string          `json:"name"        yaml:"name"`
	Label       string          `json:"label"       yaml:"label"`
	Description string          `json:"description" yaml:"description"`
	Properties  json.RawMessage `json:"properties"  yaml:"properties"`
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
