package workspace

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RepoSourceType defines how a workspace obtains its repository content.
type RepoSourceType string

const (
	RepoSourceTaskContext RepoSourceType = "task_context" // Use repo URL from task metadata
	RepoSourceFixed       RepoSourceType = "fixed"        // Use a fixed repo URL from config
	RepoSourceNone        RepoSourceType = "none"         // Empty workspace, no repo checkout
)

// ValidRepoSourceTypes lists all valid repo source types.
var ValidRepoSourceTypes = []RepoSourceType{RepoSourceTaskContext, RepoSourceFixed, RepoSourceNone}

// ValidToolNames lists all workspace tools that can be allowed.
var ValidToolNames = []string{"bash", "read", "write", "edit", "glob", "grep", "git"}

// AgentWorkspaceConfig defines the declarative workspace configuration for an agent definition.
// Stored as JSONB in kb.agent_definitions.workspace_config.
type AgentWorkspaceConfig struct {
	Enabled         bool              `json:"enabled"`
	RepoSource      *RepoSourceConfig `json:"repo_source,omitempty"`
	Tools           []string          `json:"tools,omitempty"`
	ResourceLimits  *ResourceLimits   `json:"resource_limits,omitempty"`
	CheckoutOnStart bool              `json:"checkout_on_start,omitempty"`
	BaseImage       string            `json:"base_image,omitempty"`
	SetupCommands   []string          `json:"setup_commands,omitempty"`
}

// RepoSourceConfig defines the repository source for a workspace.
type RepoSourceConfig struct {
	Type   RepoSourceType `json:"type"`
	URL    string         `json:"url,omitempty"`    // Only for "fixed" type
	Branch string         `json:"branch,omitempty"` // Default branch; overridden by task context
}

// DefaultAgentWorkspaceConfig returns the default workspace config (disabled).
func DefaultAgentWorkspaceConfig() *AgentWorkspaceConfig {
	return &AgentWorkspaceConfig{
		Enabled: false,
	}
}

// Validate validates the workspace configuration and returns a list of validation errors.
func (c *AgentWorkspaceConfig) Validate() []string {
	var errs []string

	// Validate tools
	if len(c.Tools) > 0 {
		validToolSet := make(map[string]bool, len(ValidToolNames))
		for _, t := range ValidToolNames {
			validToolSet[t] = true
		}

		var invalidTools []string
		seen := make(map[string]bool)
		for _, tool := range c.Tools {
			tool = strings.TrimSpace(strings.ToLower(tool))
			if tool == "" {
				continue
			}
			if !validToolSet[tool] {
				invalidTools = append(invalidTools, tool)
			}
			if seen[tool] {
				errs = append(errs, fmt.Sprintf("duplicate tool: %q", tool))
			}
			seen[tool] = true
		}
		if len(invalidTools) > 0 {
			errs = append(errs, fmt.Sprintf("invalid tool names: %s (valid: %s)",
				strings.Join(invalidTools, ", "),
				strings.Join(ValidToolNames, ", ")))
		}
	}

	// Validate repo source
	if c.RepoSource != nil {
		validType := false
		for _, vt := range ValidRepoSourceTypes {
			if c.RepoSource.Type == vt {
				validType = true
				break
			}
		}
		if !validType {
			errs = append(errs, fmt.Sprintf("invalid repo_source.type: %q (valid: task_context, fixed, none)", c.RepoSource.Type))
		}

		// Fixed type requires a URL
		if c.RepoSource.Type == RepoSourceFixed && c.RepoSource.URL == "" {
			errs = append(errs, "repo_source.url is required when type is 'fixed'")
		}

		// Non-fixed types should not have a URL
		if c.RepoSource.Type != RepoSourceFixed && c.RepoSource.URL != "" {
			errs = append(errs, fmt.Sprintf("repo_source.url should not be set when type is %q", c.RepoSource.Type))
		}
	}

	// Validate resource limits
	if c.ResourceLimits != nil {
		if c.ResourceLimits.CPU != "" {
			// Basic validation: should be a number optionally followed by a unit
			cpu := strings.TrimSpace(c.ResourceLimits.CPU)
			if cpu == "" {
				errs = append(errs, "resource_limits.cpu cannot be empty string")
			}
		}
		if c.ResourceLimits.Memory != "" {
			mem := strings.TrimSpace(c.ResourceLimits.Memory)
			if mem == "" {
				errs = append(errs, "resource_limits.memory cannot be empty string")
			}
		}
		if c.ResourceLimits.Disk != "" {
			disk := strings.TrimSpace(c.ResourceLimits.Disk)
			if disk == "" {
				errs = append(errs, "resource_limits.disk cannot be empty string")
			}
		}
	}

	return errs
}

// NormalizeTools normalizes tool names to lowercase and removes duplicates.
func (c *AgentWorkspaceConfig) NormalizeTools() {
	if len(c.Tools) == 0 {
		return
	}
	seen := make(map[string]bool)
	normalized := make([]string, 0, len(c.Tools))
	for _, tool := range c.Tools {
		tool = strings.TrimSpace(strings.ToLower(tool))
		if tool != "" && !seen[tool] {
			normalized = append(normalized, tool)
			seen[tool] = true
		}
	}
	c.Tools = normalized
}

// IsToolAllowed checks if a tool name is in the allowed tools list.
// If no tools are configured, all tools are allowed.
func (c *AgentWorkspaceConfig) IsToolAllowed(toolName string) bool {
	if len(c.Tools) == 0 {
		return true // No restriction = all tools allowed
	}
	toolName = strings.TrimSpace(strings.ToLower(toolName))
	for _, allowed := range c.Tools {
		if strings.TrimSpace(strings.ToLower(allowed)) == toolName {
			return true
		}
	}
	return false
}

// ToMap converts the config to a map[string]any for JSONB storage.
func (c *AgentWorkspaceConfig) ToMap() (map[string]any, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ParseAgentWorkspaceConfig parses a map[string]any (from JSONB) into an AgentWorkspaceConfig.
func ParseAgentWorkspaceConfig(m map[string]any) (*AgentWorkspaceConfig, error) {
	if m == nil {
		return nil, nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal workspace config map: %w", err)
	}
	var cfg AgentWorkspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse workspace config: %w", err)
	}
	return &cfg, nil
}

// TaskContext holds repository and branch info extracted from task metadata.
type TaskContext struct {
	RepositoryURL  string `json:"repository_url,omitempty"`
	Branch         string `json:"branch,omitempty"`
	PullRequestNum int    `json:"pull_request_number,omitempty"`
	BaseBranch     string `json:"base_branch,omitempty"`
}

// ExtractTaskContext extracts workspace-relevant repository info from task metadata.
func ExtractTaskContext(metadata map[string]any) *TaskContext {
	if metadata == nil {
		return nil
	}

	tc := &TaskContext{}
	hasData := false

	if repoURL, ok := metadata["repository_url"].(string); ok && repoURL != "" {
		tc.RepositoryURL = repoURL
		hasData = true
	}
	if branch, ok := metadata["branch"].(string); ok && branch != "" {
		tc.Branch = branch
		hasData = true
	}
	if prNum, ok := metadata["pull_request_number"].(float64); ok && prNum > 0 {
		tc.PullRequestNum = int(prNum)
		hasData = true
	}
	if baseBranch, ok := metadata["base_branch"].(string); ok && baseBranch != "" {
		tc.BaseBranch = baseBranch
		hasData = true
	}

	if !hasData {
		return nil
	}
	return tc
}

// ResolveRepoSource resolves the actual repository URL and branch based on the config and task context.
// Returns (repoURL, branch, shouldCheckout).
func ResolveRepoSource(cfg *AgentWorkspaceConfig, taskCtx *TaskContext) (string, string, bool) {
	if cfg == nil || cfg.RepoSource == nil {
		return "", "", false
	}

	switch cfg.RepoSource.Type {
	case RepoSourceFixed:
		branch := cfg.RepoSource.Branch
		return cfg.RepoSource.URL, branch, true

	case RepoSourceTaskContext:
		if taskCtx == nil || taskCtx.RepositoryURL == "" {
			// No task context — fall back to empty workspace
			return "", "", false
		}
		branch := taskCtx.Branch
		if branch == "" {
			// Fall back to config default branch
			branch = cfg.RepoSource.Branch
		}
		return taskCtx.RepositoryURL, branch, true

	case RepoSourceNone:
		return "", "", false

	default:
		return "", "", false
	}
}
