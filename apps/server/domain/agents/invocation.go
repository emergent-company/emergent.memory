package agents

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// AgentInvocation is the invocation-specific configuration shared by a source
// trigger and an agent work trigger. It is deliberately separate from the
// agent definition: the same agent can run with different instructions,
// credentials, model or step budget depending on the event that woke it.
//
// The type is persisted as JSONB (kb.source_triggers.invocation and
// kb.agent_runs.run_invocation). Secrets are never stored here: Credential
// names a source-backed credential that is minted at execution time, and
// EnvVars is limited to a small non-secret allowlist.
type AgentInvocation struct {
	// Context is static metadata supplied by the trigger. It is exposed to the
	// model under the reserved "trigger_context" metadata key so it cannot
	// overwrite dispatcher-owned event fields.
	Context map[string]any `json:"context,omitempty"`
	// EnvVars carries non-secret, allowlisted environment variables. Secret
	// values belong in Credential, never here.
	EnvVars map[string]string `json:"env_vars,omitempty"`
	// Model overrides the agent definition's model for this invocation.
	Model string `json:"model,omitempty"`
	// MaxSteps overrides the agent definition's step budget for this invocation.
	MaxSteps *int `json:"max_steps,omitempty"`
	// Priority orders queued runs inside their queue (lower runs first).
	Priority int `json:"priority,omitempty"`
	// Credential asks the executor to mint a short-lived source credential and
	// inject it into the sandbox.
	Credential *AgentCredentialRef `json:"credential,omitempty"`
}

// AgentCredentialRef asks the server to mint a credential for a source. The
// token itself is never part of the persisted invocation.
type AgentCredentialRef struct {
	// Kind is the credential provider. github_app is the only supported kind.
	Kind string `json:"kind"`
	// SourceID optionally pins the source. Empty means use the run's verified
	// source subject, falling back to the sourceId in trigger metadata.
	SourceID string `json:"source_id,omitempty"`
	// Permissions is an allowlisted GitHub App permission map, e.g.
	// {"contents":"write","pull_requests":"write"}.
	Permissions map[string]string `json:"permissions,omitempty"`
}

const (
	CredentialKindGitHubApp = "github_app"

	PermissionContents     = "contents"
	PermissionPullRequests = "pull_requests"
	PermissionIssues       = "issues"
	PermissionChecks       = "checks"
)

// AllowedInvocationEnvKeys is the complete set of non-secret environment keys
// a trigger may set. The list is intentionally tiny: an arbitrary env map can
// override PATH, git config, proxy settings or library loading, which would
// turn a trigger into a sandbox escape hatch.
var AllowedInvocationEnvKeys = map[string]struct{}{
	"CI":         {},
	"LANG":       {},
	"LC_ALL":     {},
	"TERM":       {},
	"TZ":         {},
	"GOMAXPROCS": {},
}

var allowedCredentialPermissions = map[string]struct{}{
	PermissionContents:     {},
	PermissionPullRequests: {},
	PermissionIssues:       {},
	PermissionChecks:       {},
}

// NormalizeInvocation returns a copy with deterministic defaults. It does not
// validate; call ValidateInvocation at the trust boundary.
func NormalizeInvocation(in *AgentInvocation) *AgentInvocation {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.Context) > 0 {
		out.Context = cloneAnyMap(in.Context)
	}
	if len(in.EnvVars) > 0 {
		out.EnvVars = make(map[string]string, len(in.EnvVars))
		for k, v := range in.EnvVars {
			out.EnvVars[k] = v
		}
	}
	if in.MaxSteps != nil {
		v := *in.MaxSteps
		out.MaxSteps = &v
	}
	if in.Credential != nil {
		cred := *in.Credential
		if len(in.Credential.Permissions) > 0 {
			cred.Permissions = make(map[string]string, len(in.Credential.Permissions))
			for k, v := range in.Credential.Permissions {
				cred.Permissions[k] = v
			}
		}
		if cred.Kind == "" {
			cred.Kind = CredentialKindGitHubApp
		}
		if len(cred.Permissions) == 0 {
			cred.Permissions = map[string]string{PermissionContents: "read"}
		}
		out.Credential = &cred
	}
	return &out
}

// ValidateInvocation validates the persisted trigger shape. It is safe to call
// on nil (no invocation configured).
func ValidateInvocation(in *AgentInvocation) error {
	if in == nil {
		return nil
	}
	for key := range in.EnvVars {
		if _, ok := AllowedInvocationEnvKeys[key]; !ok {
			return fmt.Errorf("invocation env var %q is not allowed", key)
		}
	}
	if in.MaxSteps != nil && *in.MaxSteps <= 0 {
		return fmt.Errorf("invocation max_steps must be positive")
	}
	if in.Priority < 0 {
		return fmt.Errorf("invocation priority must not be negative")
	}
	if in.Credential == nil {
		return nil
	}
	cred := in.Credential
	if cred.Kind != "" && cred.Kind != CredentialKindGitHubApp {
		return fmt.Errorf("invocation credential kind %q is not supported", cred.Kind)
	}
	if cred.SourceID != "" {
		if _, err := uuid.Parse(strings.TrimSpace(cred.SourceID)); err != nil {
			return fmt.Errorf("invocation credential source_id must be a UUID")
		}
	}
	for name, value := range cred.Permissions {
		if _, ok := allowedCredentialPermissions[name]; !ok {
			return fmt.Errorf("invocation credential permission %q is not allowed", name)
		}
		switch value {
		case "read", "write":
		default:
			return fmt.Errorf("invocation credential permission %q must be read or write", name)
		}
	}
	return nil
}

// CredentialPermissionNames returns the allowlisted permission names in sorted
// order. Used by tests and UI serialization so the contract is one list.
func CredentialPermissionNames() []string {
	out := make([]string, 0, len(allowedCredentialPermissions))
	for name := range allowedCredentialPermissions {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
