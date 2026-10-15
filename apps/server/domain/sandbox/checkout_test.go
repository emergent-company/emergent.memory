package sandbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsSHA(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"full SHA", "abc123def456789012345678901234567890abcd", true},
		{"short SHA (7 chars)", "abc123d", true},
		{"short SHA (8 chars)", "abc123de", true},
		{"branch name", "main", false},
		{"feature branch", "feature/auth", false},
		{"too short (6 chars)", "abc123", false},
		{"mixed case SHA", "aBcDeFg", false}, // git SHAs are lowercase hex
		{"not hex", "zzzzzzzz", false},
		{"empty", "", false},
		{"tag name", "v1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isSHA(tt.input))
		})
	}
}

func TestCloneConstants(t *testing.T) {
	assert.Equal(t, 3, maxCloneRetries)
	assert.Equal(t, 2*time.Second, initialRetryDelay)
	assert.Equal(t, 300000, cloneTimeoutMs)
	assert.Equal(t, 120000, pushPullTimeoutMs)
}

func TestCheckoutService_BuildCloneURL_NoProvider(t *testing.T) {
	cs := &CheckoutService{credProvider: nil}

	url, err := cs.buildCloneURL(context.TODO(), "https://github.com/org/repo", "")
	assert.NoError(t, err)
	assert.Equal(t, "https://github.com/org/repo", url)
}

func TestCheckoutService_BuildCloneURL_WithToken(t *testing.T) {
	cs := &CheckoutService{credProvider: nil}

	url, err := cs.buildCloneURL(context.TODO(), "https://github.com/org/repo", "ghp_testtoken")
	assert.NoError(t, err)
	assert.Equal(t, "https://x-access-token:ghp_testtoken@github.com/org/repo", url)
}

func TestCheckoutService_CloneRepository_EmptyURL(t *testing.T) {
	cs := &CheckoutService{}
	err := cs.CloneRepository(context.TODO(), nil, "", "", "", "", "")
	assert.NoError(t, err) // No-op for empty URL
}

func TestResolveGitToken_Precedence(t *testing.T) {
	tests := []struct {
		name     string
		envs     []map[string]string
		expected string
	}{
		{
			name:     "no env vars",
			envs:     nil,
			expected: "",
		},
		{
			name:     "GH_TOKEN wins over GITHUB_TOKEN within a map",
			envs:     []map[string]string{{"GH_TOKEN": "ghp_a", "GITHUB_TOKEN": "ghp_b"}},
			expected: "ghp_a",
		},
		{
			name:     "first map wins over later map",
			envs:     []map[string]string{{"GH_TOKEN": "ghp_cfg"}, {"GH_TOKEN": "ghp_rt"}},
			expected: "ghp_cfg",
		},
		{
			name:     "GITHUB_TOKEN used when GH_TOKEN absent",
			envs:     []map[string]string{{"GITHUB_TOKEN": "ghp_b"}},
			expected: "ghp_b",
		},
		{
			name:     "cfg GITHUB_TOKEN beats runtime GH_TOKEN",
			envs:     []map[string]string{{"GITHUB_TOKEN": "ghp_cfg_gh"}, {"GH_TOKEN": "ghp_rt_gh"}},
			expected: "ghp_cfg_gh",
		},
		{
			name:     "empty GH_TOKEN falls through to GITHUB_TOKEN",
			envs:     []map[string]string{{"GH_TOKEN": "", "GITHUB_TOKEN": "ghp_b"}},
			expected: "ghp_b",
		},
		{
			name:     "empty cfg falls through to runtime",
			envs:     []map[string]string{{"GH_TOKEN": ""}, {"GH_TOKEN": "ghp_rt"}},
			expected: "ghp_rt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ResolveGitToken(tt.envs...))
		})
	}
}

func TestResolveGitTokenForConfig_Precedence(t *testing.T) {
	// The secret ref wins over everything else, and resolves from the server env.
	t.Setenv(GitTokenSecretRefEnv, "ghp_server")
	cfg := &AgentSandboxConfig{
		GitTokenSecretRef: GitTokenSecretRefEnv,
		GitToken:          "ghp_legacy",
		EnvVars:           map[string]string{"GH_TOKEN": "ghp_env"},
	}
	assert.Equal(t, "ghp_server", ResolveGitTokenForConfig(cfg, map[string]string{"GH_TOKEN": "ghp_runtime"}))

	// Without a ref, the legacy literal wins over env_vars and runtime.
	t.Setenv(GitTokenSecretRefEnv, "")
	cfg = &AgentSandboxConfig{
		GitToken: "ghp_legacy",
		EnvVars:  map[string]string{"GH_TOKEN": "ghp_env"},
	}
	assert.Equal(t, "ghp_legacy", ResolveGitTokenForConfig(cfg, map[string]string{"GH_TOKEN": "ghp_runtime"}))

	// Without a ref or legacy literal, env_vars wins over runtime.
	cfg = &AgentSandboxConfig{EnvVars: map[string]string{"GITHUB_TOKEN": "ghp_env"}}
	assert.Equal(t, "ghp_env", ResolveGitTokenForConfig(cfg, map[string]string{"GH_TOKEN": "ghp_runtime"}))

	// Without any config token, runtime is used.
	assert.Equal(t, "ghp_runtime", ResolveGitTokenForConfig(nil, map[string]string{"GH_TOKEN": "ghp_runtime"}))

	// A non-allowlisted ref is ignored entirely (no arbitrary env read).
	t.Setenv("DATABASE_PASSWORD", "db-secret")
	cfg = &AgentSandboxConfig{GitTokenSecretRef: "DATABASE_PASSWORD", GitToken: "ghp_legacy"}
	assert.Equal(t, "ghp_legacy", ResolveGitTokenForConfig(cfg))
}
