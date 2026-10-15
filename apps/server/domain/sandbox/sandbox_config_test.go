package sandbox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AgentSandboxConfig Validation ---

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceFixed,
			URL:    "https://github.com/org/repo",
			Branch: "main",
		},
		Tools: []string{"bash", "read", "write"},
	}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_DisabledConfig(t *testing.T) {
	cfg := &AgentSandboxConfig{Enabled: false}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_InvalidToolName(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		Tools:   []string{"bash", "invalid_tool", "hack"},
	}
	errs := cfg.Validate()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "invalid tool names")
	assert.Contains(t, errs[0], "invalid_tool")
	assert.Contains(t, errs[0], "hack")
}

func TestValidate_DuplicateTools(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		Tools:   []string{"bash", "read", "bash"},
	}
	errs := cfg.Validate()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "duplicate tool")
}

func TestValidate_InvalidRepoSourceType(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: "invalid_type",
		},
	}
	errs := cfg.Validate()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "invalid repo_source.type")
}

func TestValidate_FixedRepoSourceMissingURL(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceFixed,
			// Missing URL
		},
	}
	errs := cfg.Validate()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "repo_source.url is required")
}

func TestValidate_NonFixedRepoSourceWithURL(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceTaskContext,
			URL:  "https://github.com/org/repo",
		},
	}
	errs := cfg.Validate()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "repo_source.url should not be set")
}

func TestValidate_NoneRepoSourceWithURL(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceNone,
			URL:  "https://github.com/org/repo",
		},
	}
	errs := cfg.Validate()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "repo_source.url should not be set")
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceFixed,
			// Missing URL
		},
		Tools: []string{"bash", "bash", "not_a_tool"},
	}
	errs := cfg.Validate()
	assert.GreaterOrEqual(t, len(errs), 3) // dup tool, invalid tool, missing URL
}

func TestValidate_EmptyResourceLimits(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		ResourceLimits: &ResourceLimits{
			CPU:    "",
			Memory: "",
			Disk:   "",
		},
	}
	// Empty strings are allowed (means "use defaults")
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_AllValidTools(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		Tools:   []string{"bash", "read", "write", "edit", "glob", "grep", "git"},
	}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_TaskContextRepoSourceValid(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceTaskContext,
			Branch: "develop",
		},
	}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_NoneRepoSourceValid(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceNone,
		},
	}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

// --- NormalizeTools ---

func TestNormalizeTools_LowercaseAndDedup(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Tools: []string{"BASH", "Read", "bash", "WRITE", "  glob  "},
	}
	cfg.NormalizeTools()
	assert.Equal(t, []string{"bash", "read", "write", "glob"}, cfg.Tools)
}

func TestNormalizeTools_EmptyList(t *testing.T) {
	cfg := &AgentSandboxConfig{Tools: []string{}}
	cfg.NormalizeTools()
	assert.Empty(t, cfg.Tools)
}

func TestNormalizeTools_NilList(t *testing.T) {
	cfg := &AgentSandboxConfig{Tools: nil}
	cfg.NormalizeTools()
	assert.Nil(t, cfg.Tools)
}

func TestNormalizeTools_BlankEntries(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Tools: []string{"", "  ", "bash", ""},
	}
	cfg.NormalizeTools()
	assert.Equal(t, []string{"bash"}, cfg.Tools)
}

// --- IsToolAllowed ---

func TestIsToolAllowed_NoRestriction(t *testing.T) {
	cfg := &AgentSandboxConfig{Tools: nil}
	assert.True(t, cfg.IsToolAllowed("bash"))
	assert.True(t, cfg.IsToolAllowed("anything"))
}

func TestIsToolAllowed_EmptySlice(t *testing.T) {
	cfg := &AgentSandboxConfig{Tools: []string{}}
	assert.True(t, cfg.IsToolAllowed("bash"))
}

func TestIsToolAllowed_Allowed(t *testing.T) {
	cfg := &AgentSandboxConfig{Tools: []string{"bash", "read", "write"}}
	assert.True(t, cfg.IsToolAllowed("bash"))
	assert.True(t, cfg.IsToolAllowed("read"))
	assert.True(t, cfg.IsToolAllowed("write"))
}

func TestIsToolAllowed_NotAllowed(t *testing.T) {
	cfg := &AgentSandboxConfig{Tools: []string{"bash", "read"}}
	assert.False(t, cfg.IsToolAllowed("write"))
	assert.False(t, cfg.IsToolAllowed("git"))
	assert.False(t, cfg.IsToolAllowed("grep"))
}

func TestIsToolAllowed_CaseInsensitive(t *testing.T) {
	cfg := &AgentSandboxConfig{Tools: []string{"bash", "read"}}
	assert.True(t, cfg.IsToolAllowed("BASH"))
	assert.True(t, cfg.IsToolAllowed("Read"))
}

// --- ParseAgentSandboxConfig / ToMap roundtrip ---

func TestParseAndToMap_Roundtrip(t *testing.T) {
	original := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceFixed,
			URL:    "https://github.com/org/repo",
			Branch: "main",
		},
		Tools:          []string{"bash", "read", "write"},
		BaseImage:      "ubuntu:22.04",
		SetupCommands:  []string{"apt-get update", "apt-get install -y curl"},
		ResourceLimits: &ResourceLimits{CPU: "2", Memory: "4G", Disk: "20G"},
	}

	m, err := original.ToMap()
	require.NoError(t, err)
	require.NotNil(t, m)

	parsed, err := ParseAgentSandboxConfig(m)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	assert.Equal(t, original.Enabled, parsed.Enabled)
	assert.Equal(t, original.RepoSource.Type, parsed.RepoSource.Type)
	assert.Equal(t, original.RepoSource.URL, parsed.RepoSource.URL)
	assert.Equal(t, original.RepoSource.Branch, parsed.RepoSource.Branch)
	assert.Equal(t, original.Tools, parsed.Tools)
	assert.Equal(t, original.BaseImage, parsed.BaseImage)
	assert.Equal(t, original.SetupCommands, parsed.SetupCommands)
	assert.Equal(t, original.ResourceLimits.CPU, parsed.ResourceLimits.CPU)
	assert.Equal(t, original.ResourceLimits.Memory, parsed.ResourceLimits.Memory)
	assert.Equal(t, original.ResourceLimits.Disk, parsed.ResourceLimits.Disk)
}

func TestParseAgentSandboxConfig_NilMap(t *testing.T) {
	cfg, err := ParseAgentSandboxConfig(nil)
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestParseAgentSandboxConfig_EmptyMap(t *testing.T) {
	cfg, err := ParseAgentSandboxConfig(map[string]any{})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.False(t, cfg.Enabled)
}

func TestDefaultAgentSandboxConfig(t *testing.T) {
	cfg := DefaultAgentSandboxConfig()
	require.NotNil(t, cfg)
	assert.False(t, cfg.Enabled)
	assert.Nil(t, cfg.RepoSource)
	assert.Nil(t, cfg.Tools)
	assert.Nil(t, cfg.ResourceLimits)
	assert.Empty(t, cfg.BaseImage)
	assert.Nil(t, cfg.SetupCommands)
}

// --- Secret redaction (issue #1844) ---

func TestIsSecretEnvKey(t *testing.T) {
	secret := []string{"GH_TOKEN", "GITHUB_TOKEN", "gh_token", "MY_TOKEN", "API_KEY", "CLIENT_SECRET", "DB_PASSWORD", "SOME_CREDENTIAL", "apikey"}
	for _, k := range secret {
		assert.True(t, IsSecretEnvKey(k), "expected secret: %q", k)
	}
	notSecret := []string{"PATH", "HOME", "REPO_URL", "BRANCH", "NODE_ENV", "GITHUB_REPOSITORY", "FOO"}
	for _, k := range notSecret {
		assert.False(t, IsSecretEnvKey(k), "expected not secret: %q", k)
	}
}

func TestAgentSandboxConfig_RedactSecrets(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		EnvVars: map[string]string{
			"GH_TOKEN":     "ghp_realTokenValue",
			"GITHUB_TOKEN": "gho_anotherRealToken",
			"REPO_URL":     "https://github.com/org/repo",
			"NODE_ENV":     "production",
		},
	}

	out := cfg.RedactSecrets()
	require.NotNil(t, out)
	// Original is untouched.
	assert.Equal(t, "ghp_realTokenValue", cfg.EnvVars["GH_TOKEN"])
	// Secret keys are redacted.
	assert.Equal(t, RedactedSecretValue, out.EnvVars["GH_TOKEN"])
	assert.Equal(t, RedactedSecretValue, out.EnvVars["GITHUB_TOKEN"])
	// Non-secret keys are preserved.
	assert.Equal(t, "https://github.com/org/repo", out.EnvVars["REPO_URL"])
	assert.Equal(t, "production", out.EnvVars["NODE_ENV"])
}

func TestAgentSandboxConfig_RedactSecrets_NoEnvVars(t *testing.T) {
	cfg := &AgentSandboxConfig{Enabled: true}
	out := cfg.RedactSecrets()
	require.NotNil(t, out)
	assert.True(t, out.Enabled)
	assert.Nil(t, out.EnvVars)
}

func TestRedactSandboxConfigMap_RedactsEnvVarsOnly(t *testing.T) {
	in := map[string]any{
		"enabled":     true,
		"base_image":  "ubuntu:22.04",
		"repo_source": map[string]any{"type": "fixed", "url": "https://github.com/org/repo"},
		"env_vars": map[string]any{
			"GH_TOKEN":   "ghp_realTokenValue",
			"NODE_ENV":   "production",
			"SOME_TOKEN": "plain-secret",
		},
		"unknown_key": "preserved",
	}

	out := RedactSandboxConfigMap(in)
	require.NotNil(t, out)
	// Non-env_vars entries are preserved verbatim (including an unknown key).
	assert.Equal(t, in["base_image"], out["base_image"])
	assert.Equal(t, in["repo_source"], out["repo_source"])
	assert.Equal(t, "preserved", out["unknown_key"])

	env, ok := out["env_vars"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, RedactedSecretValue, env["GH_TOKEN"])
	assert.Equal(t, RedactedSecretValue, env["SOME_TOKEN"])
	assert.Equal(t, "production", env["NODE_ENV"])

	// The input map's env_vars must not be mutated.
	assert.Equal(t, "ghp_realTokenValue", in["env_vars"].(map[string]any)["GH_TOKEN"])
}

func TestRedactSandboxConfigMap_Nil(t *testing.T) {
	assert.Nil(t, RedactSandboxConfigMap(nil))
}

// --- Git token secret reference + legacy literal (issue #1844) ---

func TestIsValidGitTokenSecretRef(t *testing.T) {
	assert.True(t, IsValidGitTokenSecretRef(GitTokenSecretRefEnv))
	assert.False(t, IsValidGitTokenSecretRef(""))
	assert.False(t, IsValidGitTokenSecretRef("GH_TOKEN"))
	assert.False(t, IsValidGitTokenSecretRef("DATABASE_PASSWORD"))
}

func TestResolveGitTokenSecretRef(t *testing.T) {
	// Non-allowlisted names can never read an arbitrary env var.
	t.Setenv("DATABASE_PASSWORD", "super-secret")
	assert.Equal(t, "", ResolveGitTokenSecretRef("DATABASE_PASSWORD"))
	assert.Equal(t, "", ResolveGitTokenSecretRef(""))
	assert.Equal(t, "", ResolveGitTokenSecretRef("GH_TOKEN"))

	t.Setenv(GitTokenSecretRefEnv, "ghp_servertoken")
	assert.Equal(t, "ghp_servertoken", ResolveGitTokenSecretRef(GitTokenSecretRefEnv))
}

func TestIsGitTokenEnvKey(t *testing.T) {
	assert.True(t, IsGitTokenEnvKey("GH_TOKEN"))
	assert.True(t, IsGitTokenEnvKey("GITHUB_TOKEN"))
	assert.True(t, IsGitTokenEnvKey("gh_token"))
	assert.True(t, IsGitTokenEnvKey("github_token"))
	assert.False(t, IsGitTokenEnvKey("SOME_TOKEN"))
	assert.False(t, IsGitTokenEnvKey("GH_TOKEN_X"))
	assert.False(t, IsGitTokenEnvKey(""))
}

func TestValidate_GitTokenSecretRef(t *testing.T) {
	ok := &AgentSandboxConfig{Enabled: true, GitTokenSecretRef: GitTokenSecretRefEnv}
	assert.Empty(t, ok.Validate())

	bad := &AgentSandboxConfig{Enabled: true, GitTokenSecretRef: "DATABASE_PASSWORD"}
	errs := bad.Validate()
	assert.NotEmpty(t, errs)
}

func TestAgentSandboxConfig_RedactSecrets_RedactsGitToken(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled:  true,
		GitToken: "ghp_legacyLiteral",
		EnvVars:  map[string]string{"FOO": "bar"},
	}
	out := cfg.RedactSecrets()
	require.NotNil(t, out)
	assert.Equal(t, RedactedSecretValue, out.GitToken)
	// Original untouched.
	assert.Equal(t, "ghp_legacyLiteral", cfg.GitToken)
	// Secret ref is not a secret value — it must pass through untouched.
}

func TestAgentSandboxConfig_RedactSecrets_KeepsSecretRef(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled:           true,
		GitTokenSecretRef: GitTokenSecretRefEnv,
	}
	out := cfg.RedactSecrets()
	require.NotNil(t, out)
	assert.Equal(t, GitTokenSecretRefEnv, out.GitTokenSecretRef)
}

func TestRedactSandboxConfigMap_RedactsGitToken(t *testing.T) {
	in := map[string]any{
		"enabled":              true,
		"git_token":            "ghp_legacyLiteral",
		"git_token_secret_ref": GitTokenSecretRefEnv,
		"env_vars":             map[string]any{"NODE_ENV": "production"},
	}
	out := RedactSandboxConfigMap(in)
	require.NotNil(t, out)
	assert.Equal(t, RedactedSecretValue, out["git_token"])
	// The reference name is not a secret.
	assert.Equal(t, GitTokenSecretRefEnv, out["git_token_secret_ref"])
	// Original map untouched.
	assert.Equal(t, "ghp_legacyLiteral", in["git_token"])
}

func TestNormalizeSecretsForWrite_PlaceholderRoundTrip(t *testing.T) {
	existing := &AgentSandboxConfig{
		EnvVars:  map[string]string{"GH_TOKEN": "ghp_stored", "FOO": "bar"},
		GitToken: "ghp_legacy",
	}
	incoming := &AgentSandboxConfig{
		EnvVars:  map[string]string{"GH_TOKEN": RedactedSecretValue, "FOO": "baz"},
		GitToken: RedactedSecretValue,
	}
	errs := NormalizeSecretsForWrite(incoming, existing)
	assert.Empty(t, errs)
	assert.Equal(t, "ghp_stored", incoming.EnvVars["GH_TOKEN"])
	assert.Equal(t, "ghp_legacy", incoming.GitToken)
	assert.Equal(t, "baz", incoming.EnvVars["FOO"])
}

func TestNormalizeSecretsForWrite_RejectsNewGitTokenLiteral(t *testing.T) {
	incoming := &AgentSandboxConfig{
		EnvVars: map[string]string{"GH_TOKEN": "ghp_brandnew"},
	}
	errs := NormalizeSecretsForWrite(incoming, nil)
	assert.NotEmpty(t, errs)

	// GITHUB_TOKEN too.
	incoming = &AgentSandboxConfig{
		EnvVars: map[string]string{"GITHUB_TOKEN": "ghp_brandnew"},
	}
	assert.NotEmpty(t, NormalizeSecretsForWrite(incoming, nil))
}

func TestNormalizeSecretsForWrite_AllowsUnchangedLegacyLiteral(t *testing.T) {
	existing := &AgentSandboxConfig{EnvVars: map[string]string{"GH_TOKEN": "ghp_stored"}}
	incoming := &AgentSandboxConfig{EnvVars: map[string]string{"GH_TOKEN": "ghp_stored"}}
	assert.Empty(t, NormalizeSecretsForWrite(incoming, existing))
	assert.Equal(t, "ghp_stored", incoming.EnvVars["GH_TOKEN"])
}

func TestNormalizeSecretsForWrite_DropsPlaceholderWithNoStored(t *testing.T) {
	incoming := &AgentSandboxConfig{
		EnvVars: map[string]string{"GH_TOKEN": RedactedSecretValue, "FOO": "bar"},
	}
	assert.Empty(t, NormalizeSecretsForWrite(incoming, nil))
	_, ok := incoming.EnvVars["GH_TOKEN"]
	assert.False(t, ok)
	assert.Equal(t, "bar", incoming.EnvVars["FOO"])
}

func TestNormalizeSecretsForWrite_NonSecretEnvVarUntouched(t *testing.T) {
	// A non-git-token secret env var (e.g. SOME_TOKEN) is not subject to the
	// git-token literal rejection — it round-trips like #1883 but is otherwise left
	// as-is (out of scope for the git-token secret ref).
	incoming := &AgentSandboxConfig{EnvVars: map[string]string{"SOME_TOKEN": "plain"}}
	assert.Empty(t, NormalizeSecretsForWrite(incoming, nil))
	assert.Equal(t, "plain", incoming.EnvVars["SOME_TOKEN"])
}

func TestNormalizeSecretsForWrite_RejectsClientGitTokenField(t *testing.T) {
	incoming := &AgentSandboxConfig{GitToken: "ghp_attacker"}
	assert.NotEmpty(t, NormalizeSecretsForWrite(incoming, nil))
}

// --- NormalizeSandboxConfigForWrite (map-level write path, #1844) ---

func TestNormalizeSandboxConfigForWrite_RejectsClientGitTokenLiteral(t *testing.T) {
	in := map[string]any{"enabled": true, "git_token": "ghp_attacker"}
	errs := NormalizeSandboxConfigForWrite(in, nil)
	assert.NotEmpty(t, errs)
	_, ok := in["git_token"]
	assert.False(t, ok, "rejected git_token literal must not be persisted")
}

func TestNormalizeSandboxConfigForWrite_RejectsEnvGitTokenLiteral(t *testing.T) {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		in := map[string]any{
			"enabled":  true,
			"env_vars": map[string]any{key: "ghp_brandnew", "FOO": "bar"},
		}
		errs := NormalizeSandboxConfigForWrite(in, nil)
		require.NotEmpty(t, errs, "key %q must be rejected", key)
		env := in["env_vars"].(map[string]any)
		_, hasToken := env[key]
		assert.False(t, hasToken, "rejected literal for %q must not be persisted", key)
		assert.Equal(t, "bar", env["FOO"], "non-secret env var must survive rejection of a sibling")
	}
}

func TestNormalizeSandboxConfigForWrite_PlaceholderRoundTripPreserves(t *testing.T) {
	existing := map[string]any{
		"git_token": "ghp_legacy",
		"env_vars":  map[string]any{"GH_TOKEN": "ghp_stored", "FOO": "bar"},
	}
	in := map[string]any{
		"git_token": RedactedSecretValue,
		"env_vars":  map[string]any{"GH_TOKEN": RedactedSecretValue, "FOO": "baz"},
	}
	assert.Empty(t, NormalizeSandboxConfigForWrite(in, existing))
	assert.Equal(t, "ghp_legacy", in["git_token"])
	env := in["env_vars"].(map[string]any)
	assert.Equal(t, "ghp_stored", env["GH_TOKEN"])
	assert.Equal(t, "baz", env["FOO"])
}

func TestNormalizeSandboxConfigForWrite_CarriesForwardLegacyGitToken(t *testing.T) {
	existing := map[string]any{"git_token": "ghp_legacy"}
	in := map[string]any{"enabled": true}
	assert.Empty(t, NormalizeSandboxConfigForWrite(in, existing))
	assert.Equal(t, "ghp_legacy", in["git_token"], "stored legacy token must be carried forward verbatim")
}

func TestNormalizeSandboxConfigForWrite_DropsPlaceholderWithNoStored(t *testing.T) {
	in := map[string]any{"env_vars": map[string]any{"GH_TOKEN": RedactedSecretValue, "FOO": "bar"}}
	assert.Empty(t, NormalizeSandboxConfigForWrite(in, nil))
	env := in["env_vars"].(map[string]any)
	_, hasToken := env["GH_TOKEN"]
	assert.False(t, hasToken, "placeholder with no stored value must be dropped")
	assert.Equal(t, "bar", env["FOO"])
}

func TestNormalizeSandboxConfigForWrite_PreservesUnknownAndNonSecretKeys(t *testing.T) {
	in := map[string]any{
		"enabled":              true,
		"base_image":           "ubuntu:22.04",
		"unknown_key":          "preserved",
		"env_vars":             map[string]any{"SOME_TOKEN": "plain", "NODE_ENV": "prod"},
		"git_token_secret_ref": GitTokenSecretRefEnv,
	}
	assert.Empty(t, NormalizeSandboxConfigForWrite(in, nil))
	assert.Equal(t, "ubuntu:22.04", in["base_image"])
	assert.Equal(t, "preserved", in["unknown_key"])
	assert.Equal(t, GitTokenSecretRefEnv, in["git_token_secret_ref"])
	env := in["env_vars"].(map[string]any)
	assert.Equal(t, "plain", env["SOME_TOKEN"], "non-git-token secret env vars are out of scope")
	assert.Equal(t, "prod", env["NODE_ENV"])
}

func TestNormalizeSandboxConfigForWrite_AllowsUnchangedLegacyEnvLiteral(t *testing.T) {
	existing := map[string]any{"env_vars": map[string]any{"GH_TOKEN": "ghp_stored"}}
	in := map[string]any{"env_vars": map[string]any{"GH_TOKEN": "ghp_stored"}}
	assert.Empty(t, NormalizeSandboxConfigForWrite(in, existing))
	assert.Equal(t, "ghp_stored", in["env_vars"].(map[string]any)["GH_TOKEN"])
}

// --- ResolveRepoSource ---

func TestResolveRepoSource_FixedConfig(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceFixed,
			URL:    "https://github.com/org/repo",
			Branch: "main",
		},
	}
	url, branch, shouldCheckout := ResolveRepoSource(cfg, nil)
	assert.Equal(t, "https://github.com/org/repo", url)
	assert.Equal(t, "main", branch)
	assert.True(t, shouldCheckout)
}

func TestResolveRepoSource_FixedNoBranch(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceFixed,
			URL:  "https://github.com/org/repo",
		},
	}
	url, branch, shouldCheckout := ResolveRepoSource(cfg, nil)
	assert.Equal(t, "https://github.com/org/repo", url)
	assert.Empty(t, branch)
	assert.True(t, shouldCheckout)
}

func TestResolveRepoSource_TaskContextWithContext(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceTaskContext,
			Branch: "fallback-branch",
		},
	}
	taskCtx := &TaskContext{
		RepositoryURL: "https://github.com/task/repo",
		Branch:        "feature-branch",
	}
	url, branch, shouldCheckout := ResolveRepoSource(cfg, taskCtx)
	assert.Equal(t, "https://github.com/task/repo", url)
	assert.Equal(t, "feature-branch", branch)
	assert.True(t, shouldCheckout)
}

func TestResolveRepoSource_TaskContextFallbackBranch(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceTaskContext,
			Branch: "fallback-branch",
		},
	}
	taskCtx := &TaskContext{
		RepositoryURL: "https://github.com/task/repo",
		// No branch — should fall back to config branch
	}
	url, branch, shouldCheckout := ResolveRepoSource(cfg, taskCtx)
	assert.Equal(t, "https://github.com/task/repo", url)
	assert.Equal(t, "fallback-branch", branch)
	assert.True(t, shouldCheckout)
}

func TestResolveRepoSource_TaskContextNoTaskCtx(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceTaskContext,
		},
	}
	url, branch, shouldCheckout := ResolveRepoSource(cfg, nil)
	assert.Empty(t, url)
	assert.Empty(t, branch)
	assert.False(t, shouldCheckout)
}

func TestResolveRepoSource_TaskContextEmptyRepoURL(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceTaskContext,
		},
	}
	taskCtx := &TaskContext{
		RepositoryURL: "",
		Branch:        "some-branch",
	}
	url, branch, shouldCheckout := ResolveRepoSource(cfg, taskCtx)
	assert.Empty(t, url)
	assert.Empty(t, branch)
	assert.False(t, shouldCheckout)
}

func TestResolveRepoSource_None(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceNone,
		},
	}
	url, branch, shouldCheckout := ResolveRepoSource(cfg, nil)
	assert.Empty(t, url)
	assert.Empty(t, branch)
	assert.False(t, shouldCheckout)
}

func TestResolveRepoSource_NilConfig(t *testing.T) {
	url, branch, shouldCheckout := ResolveRepoSource(nil, nil)
	assert.Empty(t, url)
	assert.Empty(t, branch)
	assert.False(t, shouldCheckout)
}

func TestResolveRepoSource_NilRepoSource(t *testing.T) {
	cfg := &AgentSandboxConfig{Enabled: true}
	url, branch, shouldCheckout := ResolveRepoSource(cfg, nil)
	assert.Empty(t, url)
	assert.Empty(t, branch)
	assert.False(t, shouldCheckout)
}

// --- ExtractTaskContext ---

func TestExtractTaskContext_AllFields(t *testing.T) {
	metadata := map[string]any{
		"repository_url":      "https://github.com/org/repo",
		"branch":              "feature-x",
		"pull_request_number": float64(42),
		"base_branch":         "main",
	}
	tc := ExtractTaskContext(metadata)
	require.NotNil(t, tc)
	assert.Equal(t, "https://github.com/org/repo", tc.RepositoryURL)
	assert.Equal(t, "feature-x", tc.Branch)
	assert.Equal(t, 42, tc.PullRequestNum)
	assert.Equal(t, "main", tc.BaseBranch)
}

func TestExtractTaskContext_PartialFields(t *testing.T) {
	metadata := map[string]any{
		"repository_url": "https://github.com/org/repo",
	}
	tc := ExtractTaskContext(metadata)
	require.NotNil(t, tc)
	assert.Equal(t, "https://github.com/org/repo", tc.RepositoryURL)
	assert.Empty(t, tc.Branch)
	assert.Zero(t, tc.PullRequestNum)
}

func TestExtractTaskContext_NilMetadata(t *testing.T) {
	tc := ExtractTaskContext(nil)
	assert.Nil(t, tc)
}

func TestExtractTaskContext_EmptyMetadata(t *testing.T) {
	tc := ExtractTaskContext(map[string]any{})
	assert.Nil(t, tc)
}

func TestExtractTaskContext_IrrelevantKeys(t *testing.T) {
	metadata := map[string]any{
		"unrelated_key": "some_value",
		"another":       123,
	}
	tc := ExtractTaskContext(metadata)
	assert.Nil(t, tc)
}

// --- ToolRestrictionMiddleware ---

func TestToolRestrictionMiddleware_AllowedTool(t *testing.T) {
	e := echo.New()
	cfg := &AgentSandboxConfig{
		Enabled: true,
		Tools:   []string{"bash", "read"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-1/bash", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/workspaces/:id/bash")
	c.SetParamNames("id")
	c.SetParamValues("ws-1")

	middleware := ToolRestrictionMiddleware(cfg, testLogger())
	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	err := handler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestToolRestrictionMiddleware_DisallowedTool(t *testing.T) {
	e := echo.New()
	cfg := &AgentSandboxConfig{
		Enabled: true,
		Tools:   []string{"bash", "read"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-1/write", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/workspaces/:id/write")
	c.SetParamNames("id")
	c.SetParamValues("ws-1")

	middleware := ToolRestrictionMiddleware(cfg, testLogger())
	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	err := handler(c)
	// Middleware writes 403 directly, no error returned
	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestToolRestrictionMiddleware_NilConfig(t *testing.T) {
	e := echo.New()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-1/bash", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/workspaces/:id/bash")
	c.SetParamNames("id")
	c.SetParamValues("ws-1")

	middleware := ToolRestrictionMiddleware(nil, testLogger())
	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	err := handler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestToolRestrictionMiddleware_DisabledConfig(t *testing.T) {
	e := echo.New()
	cfg := &AgentSandboxConfig{
		Enabled: false,
		Tools:   []string{"bash"}, // has restrictions but disabled
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-1/write", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/workspaces/:id/write")
	c.SetParamNames("id")
	c.SetParamValues("ws-1")

	middleware := ToolRestrictionMiddleware(cfg, testLogger())
	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	err := handler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestToolRestrictionMiddleware_EmptyToolsList(t *testing.T) {
	e := echo.New()
	cfg := &AgentSandboxConfig{
		Enabled: true,
		Tools:   []string{}, // empty = all allowed
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-1/bash", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/workspaces/:id/bash")
	c.SetParamNames("id")
	c.SetParamValues("ws-1")

	middleware := ToolRestrictionMiddleware(cfg, testLogger())
	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	err := handler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestToolRestrictionMiddleware_NonToolPath(t *testing.T) {
	e := echo.New()
	cfg := &AgentSandboxConfig{
		Enabled: true,
		Tools:   []string{"bash"},
	}

	// A path that doesn't end with a tool name should pass through
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/workspaces/:id")
	c.SetParamNames("id")
	c.SetParamValues("ws-1")

	middleware := ToolRestrictionMiddleware(cfg, testLogger())
	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	err := handler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- IsDockerImageRef ---

func TestIsDockerImageRef(t *testing.T) {
	tests := []struct {
		name      string
		baseImage string
		expected  bool
	}{
		// Firecracker variants (NOT Docker refs)
		{"empty string", "", false},
		{"base variant", "base", false},
		{"coder variant", "coder", false},
		{"researcher variant", "researcher", false},
		{"reviewer variant", "reviewer", false},

		// Docker image references
		{"docker hub with tag", "python:3.12-slim", true},
		{"docker hub no tag", "ubuntu", true},
		{"docker hub latest", "node:latest", true},
		{"ghcr registry", "ghcr.io/my-org/my-image:v1", true},
		{"gcr registry", "gcr.io/project/image:latest", true},
		{"docker hub with org", "library/python:3.12", true},
		{"custom registry", "registry.example.com/team/image:v2", true},
		{"multi-level path", "my-registry.io/org/sub/image:tag", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDockerImageRef(tt.baseImage)
			assert.Equal(t, tt.expected, result, "IsDockerImageRef(%q)", tt.baseImage)
		})
	}
}

// --- ResolveProviderType ---

func TestResolveProviderType(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *AgentSandboxConfig
		expected ProviderType
	}{
		{
			name:     "nil config auto-selects",
			cfg:      nil,
			expected: "",
		},
		{
			name:     "empty config auto-selects",
			cfg:      &AgentSandboxConfig{},
			expected: "",
		},
		{
			name:     "explicit provider takes priority",
			cfg:      &AgentSandboxConfig{Provider: "firecracker", BaseImage: "python:3.12"},
			expected: ProviderFirecracker,
		},
		{
			name:     "explicit gvisor provider",
			cfg:      &AgentSandboxConfig{Provider: "gvisor"},
			expected: ProviderGVisor,
		},
		{
			name:     "firecracker variant auto-selects",
			cfg:      &AgentSandboxConfig{BaseImage: "coder"},
			expected: "",
		},
		{
			name:     "base variant auto-selects",
			cfg:      &AgentSandboxConfig{BaseImage: "base"},
			expected: "",
		},
		{
			name:     "empty base_image auto-selects",
			cfg:      &AgentSandboxConfig{BaseImage: ""},
			expected: "",
		},
		{
			name:     "docker image routes to gvisor",
			cfg:      &AgentSandboxConfig{BaseImage: "python:3.12-slim"},
			expected: ProviderGVisor,
		},
		{
			name:     "docker hub image routes to gvisor",
			cfg:      &AgentSandboxConfig{BaseImage: "ubuntu"},
			expected: ProviderGVisor,
		},
		{
			name:     "ghcr image routes to gvisor",
			cfg:      &AgentSandboxConfig{BaseImage: "ghcr.io/org/image:v1"},
			expected: ProviderGVisor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveProviderType(tt.cfg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Provider field roundtrip ---

func TestParseAndToMap_ProviderField(t *testing.T) {
	original := &AgentSandboxConfig{
		Enabled:   true,
		Provider:  "firecracker",
		BaseImage: "coder",
	}

	m, err := original.ToMap()
	require.NoError(t, err)
	require.NotNil(t, m)

	parsed, err := ParseAgentSandboxConfig(m)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	assert.Equal(t, "firecracker", parsed.Provider)
	assert.Equal(t, "coder", parsed.BaseImage)
}

// --- extractToolFromPath ---

func TestExtractToolFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"standard tool path", "/api/v1/workspaces/ws-1/bash", "bash"},
		{"read tool", "/api/v1/workspaces/ws-1/read", "read"},
		{"short path", "/bash", "bash"},
		{"empty path", "", ""},
		{"root", "/", ""},
		{"trailing slash", "/api/v1/workspaces/ws-1/bash/", "bash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractToolFromPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
