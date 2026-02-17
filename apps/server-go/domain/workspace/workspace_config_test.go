package workspace

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AgentWorkspaceConfig Validation ---

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &AgentWorkspaceConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceFixed,
			URL:    "https://github.com/org/repo",
			Branch: "main",
		},
		Tools:           []string{"bash", "read", "write"},
		CheckoutOnStart: true,
	}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_DisabledConfig(t *testing.T) {
	cfg := &AgentWorkspaceConfig{Enabled: false}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_InvalidToolName(t *testing.T) {
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
		Enabled: true,
		Tools:   []string{"bash", "read", "bash"},
	}
	errs := cfg.Validate()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "duplicate tool")
}

func TestValidate_InvalidRepoSourceType(t *testing.T) {
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
		Enabled: true,
		Tools:   []string{"bash", "read", "write", "edit", "glob", "grep", "git"},
	}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_TaskContextRepoSourceValid(t *testing.T) {
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
		Tools: []string{"BASH", "Read", "bash", "WRITE", "  glob  "},
	}
	cfg.NormalizeTools()
	assert.Equal(t, []string{"bash", "read", "write", "glob"}, cfg.Tools)
}

func TestNormalizeTools_EmptyList(t *testing.T) {
	cfg := &AgentWorkspaceConfig{Tools: []string{}}
	cfg.NormalizeTools()
	assert.Empty(t, cfg.Tools)
}

func TestNormalizeTools_NilList(t *testing.T) {
	cfg := &AgentWorkspaceConfig{Tools: nil}
	cfg.NormalizeTools()
	assert.Nil(t, cfg.Tools)
}

func TestNormalizeTools_BlankEntries(t *testing.T) {
	cfg := &AgentWorkspaceConfig{
		Tools: []string{"", "  ", "bash", ""},
	}
	cfg.NormalizeTools()
	assert.Equal(t, []string{"bash"}, cfg.Tools)
}

// --- IsToolAllowed ---

func TestIsToolAllowed_NoRestriction(t *testing.T) {
	cfg := &AgentWorkspaceConfig{Tools: nil}
	assert.True(t, cfg.IsToolAllowed("bash"))
	assert.True(t, cfg.IsToolAllowed("anything"))
}

func TestIsToolAllowed_EmptySlice(t *testing.T) {
	cfg := &AgentWorkspaceConfig{Tools: []string{}}
	assert.True(t, cfg.IsToolAllowed("bash"))
}

func TestIsToolAllowed_Allowed(t *testing.T) {
	cfg := &AgentWorkspaceConfig{Tools: []string{"bash", "read", "write"}}
	assert.True(t, cfg.IsToolAllowed("bash"))
	assert.True(t, cfg.IsToolAllowed("read"))
	assert.True(t, cfg.IsToolAllowed("write"))
}

func TestIsToolAllowed_NotAllowed(t *testing.T) {
	cfg := &AgentWorkspaceConfig{Tools: []string{"bash", "read"}}
	assert.False(t, cfg.IsToolAllowed("write"))
	assert.False(t, cfg.IsToolAllowed("git"))
	assert.False(t, cfg.IsToolAllowed("grep"))
}

func TestIsToolAllowed_CaseInsensitive(t *testing.T) {
	cfg := &AgentWorkspaceConfig{Tools: []string{"bash", "read"}}
	assert.True(t, cfg.IsToolAllowed("BASH"))
	assert.True(t, cfg.IsToolAllowed("Read"))
}

// --- ParseAgentWorkspaceConfig / ToMap roundtrip ---

func TestParseAndToMap_Roundtrip(t *testing.T) {
	original := &AgentWorkspaceConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceFixed,
			URL:    "https://github.com/org/repo",
			Branch: "main",
		},
		Tools:           []string{"bash", "read", "write"},
		CheckoutOnStart: true,
		BaseImage:       "ubuntu:22.04",
		SetupCommands:   []string{"apt-get update", "apt-get install -y curl"},
		ResourceLimits:  &ResourceLimits{CPU: "2", Memory: "4G", Disk: "20G"},
	}

	m, err := original.ToMap()
	require.NoError(t, err)
	require.NotNil(t, m)

	parsed, err := ParseAgentWorkspaceConfig(m)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	assert.Equal(t, original.Enabled, parsed.Enabled)
	assert.Equal(t, original.RepoSource.Type, parsed.RepoSource.Type)
	assert.Equal(t, original.RepoSource.URL, parsed.RepoSource.URL)
	assert.Equal(t, original.RepoSource.Branch, parsed.RepoSource.Branch)
	assert.Equal(t, original.Tools, parsed.Tools)
	assert.Equal(t, original.CheckoutOnStart, parsed.CheckoutOnStart)
	assert.Equal(t, original.BaseImage, parsed.BaseImage)
	assert.Equal(t, original.SetupCommands, parsed.SetupCommands)
	assert.Equal(t, original.ResourceLimits.CPU, parsed.ResourceLimits.CPU)
	assert.Equal(t, original.ResourceLimits.Memory, parsed.ResourceLimits.Memory)
	assert.Equal(t, original.ResourceLimits.Disk, parsed.ResourceLimits.Disk)
}

func TestParseAgentWorkspaceConfig_NilMap(t *testing.T) {
	cfg, err := ParseAgentWorkspaceConfig(nil)
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestParseAgentWorkspaceConfig_EmptyMap(t *testing.T) {
	cfg, err := ParseAgentWorkspaceConfig(map[string]any{})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.False(t, cfg.Enabled)
}

func TestDefaultAgentWorkspaceConfig(t *testing.T) {
	cfg := DefaultAgentWorkspaceConfig()
	require.NotNil(t, cfg)
	assert.False(t, cfg.Enabled)
	assert.Nil(t, cfg.RepoSource)
	assert.Nil(t, cfg.Tools)
	assert.Nil(t, cfg.ResourceLimits)
	assert.False(t, cfg.CheckoutOnStart)
	assert.Empty(t, cfg.BaseImage)
	assert.Nil(t, cfg.SetupCommands)
}

// --- ResolveRepoSource ---

func TestResolveRepoSource_FixedConfig(t *testing.T) {
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{Enabled: true}
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
	cfg := &AgentWorkspaceConfig{
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
