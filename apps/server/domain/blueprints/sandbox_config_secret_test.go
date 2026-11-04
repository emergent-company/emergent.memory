package blueprints

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/sandbox"
)

// These tests pin the write-path secret handling for the blueprint apply path
// (issue #1844 review): a blueprint workspaceConfig must never persist a
// client-supplied plaintext git token, and a redaction-placeholder round-trip
// must preserve a previously stored secret.

const dummyGitToken = "ghp_dummy_token_not_real"

func TestBuildAgentDefinition_RejectsPlaintextGitToken(t *testing.T) {
	for _, wc := range []map[string]any{
		{"enabled": true, "git_token": dummyGitToken},
		{"enabled": true, "env_vars": map[string]any{"GH_TOKEN": dummyGitToken}},
		{"enabled": true, "env_vars": map[string]any{"GITHUB_TOKEN": dummyGitToken}},
	} {
		_, err := buildAgentDefinition(&AgentManifest{Name: "x", WorkspaceConfig: wc}, "proj-1")
		require.Error(t, err, "workspaceConfig %v must be rejected", wc)
		require.Contains(t, err.Error(), "git_token_secret_ref")
	}
}

func TestBuildAgentDefinition_AllowsSecretRef(t *testing.T) {
	def, err := buildAgentDefinition(&AgentManifest{
		Name: "x",
		WorkspaceConfig: map[string]any{
			"enabled":              true,
			"git_token_secret_ref": sandbox.GitTokenSecretRefEnv,
		},
	}, "proj-1")
	require.NoError(t, err)
	require.NotNil(t, def.SandboxConfig)
	require.Equal(t, sandbox.GitTokenSecretRefEnv, def.SandboxConfig["git_token_secret_ref"])
}

func TestApplyAgentManifestToExisting_RejectsPlaintextGitToken(t *testing.T) {
	existing := &agents.AgentDefinition{SandboxConfig: map[string]any{"enabled": true}}
	err := applyAgentManifestToExisting(existing, &AgentManifest{
		Name:            "x",
		WorkspaceConfig: map[string]any{"enabled": true, "env_vars": map[string]any{"GH_TOKEN": dummyGitToken}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "git_token_secret_ref")
}

func TestApplyAgentManifestToExisting_PlaceholderRoundTripPreserves(t *testing.T) {
	existing := &agents.AgentDefinition{SandboxConfig: map[string]any{
		"enabled":  true,
		"env_vars": map[string]any{"GH_TOKEN": dummyGitToken, "FOO": "bar"},
	}}

	err := applyAgentManifestToExisting(existing, &AgentManifest{
		Name: "x",
		WorkspaceConfig: map[string]any{
			"enabled":  true,
			"env_vars": map[string]any{"GH_TOKEN": sandbox.RedactedSecretValue, "FOO": "baz"},
		},
	})
	require.NoError(t, err)

	envVars, ok := existing.SandboxConfig["env_vars"].(map[string]any)
	require.True(t, ok, "env_vars must be preserved")
	require.Equal(t, dummyGitToken, envVars["GH_TOKEN"],
		"placeholder round-trip must restore (not erase) the stored secret")
	require.Equal(t, "baz", envVars["FOO"])
}
