package sandbox

import (
	"context"
	"io"
	"log/slog"
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

// recordingProvider records Exec calls. The embedded Provider interface panics
// on any other method, which is the point: the git-auth setup step must need
// nothing but Exec, so it works on a warm-pool container.
type recordingProvider struct {
	Provider
	execs []*ExecRequest
	res   *ExecResult
	err   error
}

func (p *recordingProvider) Exec(_ context.Context, _ string, req *ExecRequest) (*ExecResult, error) {
	p.execs = append(p.execs, req)
	if p.res != nil || p.err != nil {
		return p.res, p.err
	}
	return &ExecResult{ExitCode: 0}, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestConfigureGitAuth_ConfiguresGitAndGhWithoutLeakingToken(t *testing.T) {
	const token = "github_pat_test_value_must_not_reach_the_command"

	prov := &recordingProvider{}
	cs := NewCheckoutService(nil, discardLogger())

	if err := cs.ConfigureGitAuth(context.Background(), prov, "ws-1", token); err != nil {
		t.Fatalf("ConfigureGitAuth returned error: %v", err)
	}
	if len(prov.execs) != 1 {
		t.Fatalf("expected exactly one exec, got %d", len(prov.execs))
	}

	req := prov.execs[0]
	// The token travels in the exec environment, never in the command string:
	// providers may log the command or expose it as a process argument.
	assert.NotContains(t, req.Command, token, "token must not appear in the command text")
	assert.NotContains(t, req.Command, "github_pat_", "command must not carry a token literal")
	assert.Equal(t, token, req.Env["GH_TOKEN"])

	// Both consumers must be set up: git (credential store entry) and the GitHub
	// CLI (hosts.yml) — the agent needs push *and* `gh pr create`.
	assert.Contains(t, req.Command, ".git-credentials")
	assert.Contains(t, req.Command, "credential.helper store")
	assert.Contains(t, req.Command, ".config/gh/hosts.yml")
	assert.Equal(t, "/workspace", req.Workdir)
}

func TestConfigureGitAuth_EmptyTokenIsNoop(t *testing.T) {
	prov := &recordingProvider{}
	cs := NewCheckoutService(nil, discardLogger())

	if err := cs.ConfigureGitAuth(context.Background(), prov, "ws-1", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assert.Empty(t, prov.execs, "no token means the sandbox is left untouched")
}

func TestConfigureGitAuth_SurfacesNonZeroExit(t *testing.T) {
	prov := &recordingProvider{res: &ExecResult{ExitCode: 1, Stderr: "permission denied"}}
	cs := NewCheckoutService(nil, discardLogger())

	err := cs.ConfigureGitAuth(context.Background(), prov, "ws-1", "token")
	if err == nil {
		t.Fatal("expected an error when the setup command fails")
	}
	assert.Contains(t, err.Error(), "permission denied")
}
