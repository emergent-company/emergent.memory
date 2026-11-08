package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
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

// Agent run modes.
const (
	// RunModeStructured exposes the fine-grained workspace_* tools.
	RunModeStructured = "structured"
	// RunModeMini exposes only workspace_bash plus a submit tool: one action
	// per model turn, linear history, explicit termination.
	RunModeMini = "mini"
)

// ValidRunModes lists the accepted run_mode values ("" means structured).
var ValidRunModes = []string{RunModeStructured, RunModeMini}

// IsMini reports whether the agent uses the minimal bash-only run mode.
func (c *AgentSandboxConfig) IsMini() bool {
	return c != nil && c.RunMode == RunModeMini
}

// ValidToolNames lists all workspace tools that can be allowed.
var ValidToolNames = []string{"bash", "read", "write", "edit", "glob", "grep", "git", "run_python", "run_go", "ast_grep"}

// AgentSandboxConfig defines the declarative workspace configuration for an agent definition.
// Stored as JSONB in kb.agent_definitions.workspace_config.
type AgentSandboxConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider,omitempty"` // Explicit provider: "firecracker", "gvisor", "e2b", or "" (auto)
	// RunMode selects the agent loop's tool surface. "" and "structured" keep
	// the fine-grained workspace_* tools; "mini" exposes only workspace_bash
	// plus a submit tool (one action per turn, minimal scaffold).
	RunMode        string            `json:"run_mode,omitempty"`
	RepoSource     *RepoSourceConfig `json:"repo_source,omitempty"`
	Tools          []string          `json:"tools,omitempty"`
	ResourceLimits *ResourceLimits   `json:"resource_limits,omitempty"`
	BaseImage      string            `json:"base_image,omitempty"`
	SetupCommands  []string          `json:"setup_commands,omitempty"`
	EnvVars        map[string]string `json:"env_vars,omitempty"`

	// GitTokenSecretRef names a server-side git credential. It is a reference, not
	// a secret value: the token is resolved from the server environment at
	// provision time (see ResolveGitTokenSecretRef), so the database never stores a
	// plaintext token on new writes. Only the allowlisted name
	// (GitTokenSecretRefEnv) may be referenced (issue #1844).
	GitTokenSecretRef string `json:"git_token_secret_ref,omitempty"`

	// GitToken is a legacy plaintext git token backfilled out of env_vars by
	// migration 00244. It is retained only so pre-existing rows keep cloning until
	// the operator rotates the credential to a server-side secret referenced by
	// GitTokenSecretRef (issue #1844). It is server-managed: read endpoints redact
	// it, and the write path never accepts a new value.
	GitToken string `json:"git_token,omitempty"`
}

// RepoSourceConfig defines the repository source for a workspace.
type RepoSourceConfig struct {
	Type   RepoSourceType `json:"type"`
	URL    string         `json:"url,omitempty"`    // Only for "fixed" type
	Branch string         `json:"branch,omitempty"` // Default branch; overridden by task context
}

// DefaultAgentSandboxConfig returns the default workspace config (disabled).
func DefaultAgentSandboxConfig() *AgentSandboxConfig {
	return &AgentSandboxConfig{
		Enabled: false,
	}
}

// RedactedSecretValue is the placeholder returned by read endpoints in place of a
// secret env var value (e.g. a git token). It is deliberately not a valid token
// so it can never be mistaken for — or replayed as — a real credential.
const RedactedSecretValue = "••••••••"

// GitTokenSecretRefEnv is the only server environment variable a sandbox's
// git_token_secret_ref may name. It holds the deployment-level git token. The
// allowlist is deliberately a single name: a caller must not be able to use the
// reference to exfiltrate an arbitrary server environment value (e.g. a database
// password) by naming it.
const GitTokenSecretRefEnv = "SANDBOX_GIT_TOKEN"

// IsValidGitTokenSecretRef reports whether name is an allowlisted git token
// secret reference.
func IsValidGitTokenSecretRef(name string) bool {
	return name == GitTokenSecretRefEnv
}

// ResolveGitTokenSecretRef resolves a git token secret reference from the server
// environment. It returns "" for an empty or non-allowlisted reference so an
// arbitrary name can never read an arbitrary environment variable. The resolved
// value is a credential and must never be logged.
func ResolveGitTokenSecretRef(ref string) string {
	if !IsValidGitTokenSecretRef(ref) {
		return ""
	}
	return os.Getenv(ref)
}

// IsGitTokenEnvKey reports whether an env var key is a git token key
// (GH_TOKEN / GITHUB_TOKEN), case-insensitive.
func IsGitTokenEnvKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	return k == "gh_token" || k == "github_token"
}

// IsSecretEnvKey reports whether an env var key names a credential that must
// never be echoed to a client. It uses the same credential vocabulary as the
// audit redactor (token/password/secret/api_key/credential) and additionally
// treats the two git token keys as secret even without a marker suffix.
func IsSecretEnvKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	switch {
	case IsGitTokenEnvKey(k):
		return true
	case strings.Contains(k, "token"),
		strings.Contains(k, "password"),
		strings.Contains(k, "secret"),
		strings.Contains(k, "api_key"),
		strings.Contains(k, "apikey"),
		strings.Contains(k, "credential"):
		return true
	}
	return false
}

// RedactSecrets returns a copy of the config with secret env var values replaced
// by RedactedSecretValue. Read endpoints must use this so a git token or other
// credential persisted in env_vars is never returned to a client (issue #1844).
func (c *AgentSandboxConfig) RedactSecrets() *AgentSandboxConfig {
	if c == nil {
		return nil
	}
	out := *c
	if c.GitToken != "" {
		out.GitToken = RedactedSecretValue
	}
	if len(c.EnvVars) == 0 {
		return &out
	}
	out.EnvVars = make(map[string]string, len(c.EnvVars))
	for k, v := range c.EnvVars {
		if IsSecretEnvKey(k) && v != "" {
			out.EnvVars[k] = RedactedSecretValue
		} else {
			out.EnvVars[k] = v
		}
	}
	return &out
}

// RedactSandboxConfigMap returns a copy of the raw workspace-config map (the
// JSONB shape stored on kb.agent_definitions.sandbox_config) with secret env var
// values redacted. Only the env_vars object is touched; every other key and value
// is preserved verbatim so read endpoints do not alter the config's shape.
func RedactSandboxConfigMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch k {
		case "env_vars":
			out[k] = redactEnvVarsMap(v)
			continue
		case "git_token":
			// The legacy plaintext git token (migration 00244) is a secret; redact
			// it like any other secret env var.
			if s, ok := v.(string); ok && s != "" {
				out[k] = RedactedSecretValue
				continue
			}
		}
		out[k] = v
	}
	return out
}

func redactEnvVarsMap(v any) any {
	env, ok := v.(map[string]any)
	if !ok || len(env) == 0 {
		return v
	}
	out := make(map[string]any, len(env))
	for k, val := range env {
		if s, ok := val.(string); ok && s != "" && IsSecretEnvKey(k) {
			out[k] = RedactedSecretValue
		} else {
			out[k] = val
		}
	}
	return out
}

// NormalizeSecretsForWrite applies write-path secret handling to the incoming
// typed config against the previously stored config, returning validation
// errors. It delegates to NormalizeSandboxConfigForWrite — the single map-level
// entry point — so the typed and raw-map write paths share one implementation.
func NormalizeSecretsForWrite(incoming, existing *AgentSandboxConfig) []string {
	if incoming == nil {
		return nil
	}
	inMap, err := incoming.ToMap()
	if err != nil {
		return []string{"failed to encode workspace config for write normalization"}
	}
	var exMap map[string]any
	if existing != nil {
		if exMap, err = existing.ToMap(); err != nil {
			return []string{"failed to encode existing workspace config for write normalization"}
		}
	}
	errs := NormalizeSandboxConfigForWrite(inMap, exMap)
	// Rehydrate only the two secret-bearing fields; every other field on the
	// typed config is left untouched by normalization.
	if parsed, perr := ParseAgentSandboxConfig(inMap); perr == nil && parsed != nil {
		incoming.GitToken = parsed.GitToken
		incoming.EnvVars = parsed.EnvVars
	}
	return errs
}

// mapStringValue reads a string value from a raw workspace-config map.
func mapStringValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// NormalizeSandboxConfigForWrite applies write-path secret handling to a raw
// workspace-config map (the JSONB shape persisted on kb.agent_definitions and on
// agent override settings), returning validation errors. It is the single entry
// point every write path that accepts a full sandbox-config map must funnel
// through: CreateDefinition, UpdateDefinition, SetAgentOverride, blueprint apply,
// and — via NormalizeSecretsForWrite — UpdateSandboxConfig.
//
//   - The legacy git_token field is server-managed (backfilled by migration
//     00244): a stored value is carried forward verbatim (so a placeholder echo
//     or an omitted value never erases it), and a client-supplied plaintext value
//     is rejected.
//   - A secret env var echoed as RedactedSecretValue is restored from the stored
//     value, so a placeholder round-trip never erases a stored credential (#1883).
//   - A newly-supplied plaintext git token (GH_TOKEN / GITHUB_TOKEN) is rejected:
//     new git credentials must use git_token_secret_ref (#1844).
//
// incoming is mutated in place; existing is read-only and may be nil (create
// paths). A full-replace write that omits env_vars clears any stored env vars.
func NormalizeSandboxConfigForWrite(incoming, existing map[string]any) []string {
	if incoming == nil {
		return nil
	}
	var errs []string

	// Legacy git_token: server-managed. Carry forward the stored value regardless
	// of what the client sent (it can neither set nor clear the credential), and
	// reject a plaintext value when there is none stored.
	existingGitToken := mapStringValue(existing, "git_token")
	if incomingGitToken := mapStringValue(incoming, "git_token"); existingGitToken != "" {
		incoming["git_token"] = existingGitToken
	} else if incomingGitToken != "" && incomingGitToken != RedactedSecretValue {
		errs = append(errs, "git_token must not be set directly; use git_token_secret_ref")
		// Defense in depth: strip the literal even if the caller ignores errs.
		delete(incoming, "git_token")
	} else {
		delete(incoming, "git_token")
	}

	// Secret env vars. Only normalized when the incoming map carries an env_vars
	// object; a full-replace write that omits env_vars clears stored env vars.
	if env, ok := incoming["env_vars"].(map[string]any); ok {
		existingEnv, _ := existing["env_vars"].(map[string]any)
		normalized := make(map[string]any, len(env))
		for k, v := range env {
			s, isStr := v.(string)
			if isStr && s == RedactedSecretValue {
				// Placeholder echo — restore the stored secret so a read→write
				// round-trip never overwrites a real credential with the marker.
				if old, ok := existingEnv[k]; ok {
					if oldS, oldIsStr := old.(string); oldIsStr && oldS != "" {
						normalized[k] = old
						continue
					}
				}
				// Placeholder with no stored value — drop it.
				continue
			}
			if isStr && s != "" && IsGitTokenEnvKey(k) {
				// A git token must not be persisted as a fresh literal. Accept only
				// an unchanged legacy value (already stored); reject anything new.
				if old, ok := existingEnv[k]; ok {
					if oldS, oldIsStr := old.(string); oldIsStr && oldS == s {
						normalized[k] = v
						continue
					}
				}
				errs = append(errs, fmt.Sprintf("env_vars[%q] must not contain a plaintext git token; use git_token_secret_ref", k))
				continue
			}
			normalized[k] = v
		}
		if len(normalized) > 0 {
			incoming["env_vars"] = normalized
		} else {
			delete(incoming, "env_vars")
		}
	}

	return errs
}

// Validate validates the workspace configuration and returns a list of validation errors.
func (c *AgentSandboxConfig) Validate() []string {
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

	// Validate run mode
	if c.RunMode != "" {
		valid := false
		for _, m := range ValidRunModes {
			if c.RunMode == m {
				valid = true
				break
			}
		}
		if !valid {
			errs = append(errs, fmt.Sprintf("invalid run_mode: %q (valid: structured, mini)", c.RunMode))
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

	// Warn about reserved env var keys (not an error — system keys always win at merge time)
	reservedKeys := []string{"MEMORY_ACCOUNT_API_KEY", "MEMORY_API_KEY", "MEMORY_PROJECT_ID", "MEMORY_SERVER_URL", "MEMORY_API_URL"}
	for _, rk := range reservedKeys {
		if _, ok := c.EnvVars[rk]; ok {
			errs = append(errs, fmt.Sprintf("env_vars contains reserved key %q — it will be overridden by the system at runtime", rk))
		}
	}

	// The git token secret reference must name the allowlisted server secret; any
	// other value is treated as an arbitrary-env read attempt and rejected.
	if c.GitTokenSecretRef != "" && !IsValidGitTokenSecretRef(c.GitTokenSecretRef) {
		errs = append(errs, fmt.Sprintf("git_token_secret_ref must be %q (or empty)", GitTokenSecretRefEnv))
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
func (c *AgentSandboxConfig) NormalizeTools() {
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
func (c *AgentSandboxConfig) IsToolAllowed(toolName string) bool {
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
func (c *AgentSandboxConfig) ToMap() (map[string]any, error) {
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

// ParseAgentSandboxConfig parses a map[string]any (from JSONB) into an AgentSandboxConfig.
func ParseAgentSandboxConfig(m map[string]any) (*AgentSandboxConfig, error) {
	if m == nil {
		return nil, nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal workspace config map: %w", err)
	}
	var cfg AgentSandboxConfig
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
func ResolveRepoSource(cfg *AgentSandboxConfig, taskCtx *TaskContext) (string, string, bool) {
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

// firecrackerVariants lists known Firecracker rootfs variant names.
// Any BaseImage value NOT in this set is treated as a Docker image reference.
var firecrackerVariants = map[string]bool{
	"":           true, // empty = default base
	"base":       true,
	"coder":      true,
	"researcher": true,
	"reviewer":   true,
}

// IsDockerImageRef returns true if baseImage looks like a Docker image reference
// (e.g. "python:3.12-slim", "ghcr.io/org/image:v1", "ubuntu") rather than a
// known Firecracker rootfs variant name (e.g. "coder", "reviewer").
func IsDockerImageRef(baseImage string) bool {
	if baseImage == "" {
		return false
	}
	return !firecrackerVariants[baseImage]
}

// ResolveProviderType determines which provider should handle a workspace based
// on the workspace config. Resolution order:
//  1. If Provider is explicitly set (e.g. "firecracker", "gvisor"), use that.
//  2. If BaseImage is a Docker image ref (not a known Firecracker variant),
//     route to gVisor since only gVisor can pull/run arbitrary Docker images.
//  3. Otherwise, return "" to let the orchestrator auto-select.
func ResolveProviderType(cfg *AgentSandboxConfig) ProviderType {
	if cfg == nil {
		return "" // auto-select
	}

	// Explicit provider takes priority
	if cfg.Provider != "" {
		return ProviderType(cfg.Provider)
	}

	// Docker image refs must go to gVisor (Firecracker only supports named rootfs variants)
	if IsDockerImageRef(cfg.BaseImage) {
		return ProviderGVisor
	}

	return "" // auto-select
}
