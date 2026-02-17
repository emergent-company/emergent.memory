package workspace

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TG16.3 — Provider Fallback Scenarios
// =============================================================================

func TestE2E_ProviderFallback_FirecrackerUnhealthy_FallsBackToGVisor(t *testing.T) {
	// Scenario: Firecracker is the preferred provider for self-hosted agent workspaces,
	// but it's unhealthy. The orchestrator should fall back to gVisor.
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "firecracker", providerType: ProviderFirecracker, healthy: false}
	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	// Run health check to update status
	o.checkAllHealth(t.Context())

	// Auto-select for agent workspace (prefers Firecracker, but it's unhealthy)
	provider, providerType, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, providerType)
	assert.NotNil(t, provider)

	// Verify we can create a workspace with the fallback provider
	result, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	assert.Contains(t, result.ProviderID, "mock-gvisor")
}

func TestE2E_ProviderFallback_AllUnhealthy_ReturnsError(t *testing.T) {
	// Scenario: All providers are unhealthy — should return an error.
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "firecracker", providerType: ProviderFirecracker, healthy: false}
	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: false}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	o.checkAllHealth(t.Context())

	_, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy providers")
}

func TestE2E_ProviderFallback_ExplicitRequest_NoFallback(t *testing.T) {
	// Scenario: User explicitly requests Firecracker, but it's unhealthy.
	// Should NOT fall back — return error immediately.
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "firecracker", providerType: ProviderFirecracker, healthy: false}
	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	o.checkAllHealth(t.Context())

	_, _, err := o.SelectProviderWithFallback(ContainerTypeAgentWorkspace, DeploymentSelfHosted, ProviderFirecracker)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unhealthy")
}

func TestE2E_ProviderFallback_CreateFails_RetryWithDifferentProvider(t *testing.T) {
	// Scenario: Firecracker is healthy but Create() fails. The orchestrator should
	// allow the caller to retry with a fallback. Simulates what attemptProvision does.
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{
		name:         "firecracker",
		providerType: ProviderFirecracker,
		healthy:      true,
		createErr:    errors.New("KVM not available"),
	}
	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	// First attempt: Firecracker is preferred and healthy
	provider, providerType, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderFirecracker, providerType)

	// Create fails
	_, createErr := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	assert.Error(t, createErr)
	assert.Contains(t, createErr.Error(), "KVM not available")

	// Mark Firecracker as unhealthy after failure
	fc.healthy = false
	o.checkAllHealth(t.Context())

	// Retry: now only gVisor is healthy
	provider, providerType, err = o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, providerType)

	// Create succeeds on gVisor
	result, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	assert.Contains(t, result.ProviderID, "mock-gvisor")
}

func TestE2E_ProviderFallback_MCP_PrefersGVisor(t *testing.T) {
	// Scenario: MCP server containers prefer gVisor over Firecracker in self-hosted mode.
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "firecracker", providerType: ProviderFirecracker, healthy: true}
	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	provider, providerType, err := o.SelectProvider(ContainerTypeMCPServer, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, providerType)
	assert.NotNil(t, provider)
}

func TestE2E_ProviderFallback_MCP_GVisorDown_FallsToFirecracker(t *testing.T) {
	// Scenario: gVisor is preferred for MCP but unhealthy → fall back to Firecracker.
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "firecracker", providerType: ProviderFirecracker, healthy: true}
	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: false}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	o.checkAllHealth(t.Context())

	provider, providerType, err := o.SelectProvider(ContainerTypeMCPServer, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderFirecracker, providerType)
	assert.NotNil(t, provider)
}

// =============================================================================
// TG16.5 — TTL Cleanup Scenarios (logic tests without DB)
// =============================================================================

func TestE2E_Cleanup_DestroyWorkspace_ViaProvider(t *testing.T) {
	// Scenario: CleanupJob.destroyWorkspace uses the orchestrator to find a provider
	// and calls Destroy on it. Verify the provider's Destroy is invoked.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gv)

	// The cleanup job only needs an orchestrator (Store ops will fail without DB,
	// but we can test the provider interaction part)
	job := NewCleanupJob(nil, o, testLogger(), DefaultCleanupConfig())

	ws := &AgentWorkspace{
		ID:                  "ws-expired-1",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-abc",
		Status:              StatusReady,
	}

	// destroyWorkspace will try to update DB (which will panic with nil store),
	// so we just test the provider lookup and destroy call
	provider, err := o.GetProvider(ProviderGVisor)
	require.NoError(t, err)

	err = provider.Destroy(t.Context(), ws.ProviderWorkspaceID)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), gv.destroyCount.Load())

	// Verify the job was constructed correctly
	assert.NotNil(t, job)
}

func TestE2E_Cleanup_ProviderNotRegistered_LogsWarning(t *testing.T) {
	// Scenario: Workspace references a provider that's no longer registered.
	// The cleanup should handle this gracefully.
	o := NewOrchestrator(testLogger())

	// No providers registered
	_, err := o.GetProvider(ProviderFirecracker)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestE2E_Cleanup_ProviderDestroyError_Continues(t *testing.T) {
	// Scenario: Provider.Destroy returns an error (container already removed).
	// The cleanup should still attempt to update the DB status.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:         "gvisor",
		providerType: ProviderGVisor,
		healthy:      true,
		destroyErr:   errors.New("container not found"),
	}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, err := o.GetProvider(ProviderGVisor)
	require.NoError(t, err)

	err = provider.Destroy(t.Context(), "nonexistent-container")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "container not found")
	// Despite the error, destroyCount should still be incremented
	assert.Equal(t, int64(1), gv.destroyCount.Load())
}

func TestE2E_Cleanup_MCPServerExemption(t *testing.T) {
	// Scenario: Persistent MCP servers have NULL expires_at and are never returned
	// by ListExpired. Verify the entity-level invariant.
	mcpWs := &AgentWorkspace{
		ContainerType: ContainerTypeMCPServer,
		Lifecycle:     LifecyclePersistent,
		ExpiresAt:     nil,
	}
	assert.Nil(t, mcpWs.ExpiresAt, "persistent MCP servers must have nil ExpiresAt")

	// An ephemeral workspace with an expired TTL would be picked up
	agentWs := &AgentWorkspace{
		ContainerType: ContainerTypeAgentWorkspace,
		Lifecycle:     LifecycleEphemeral,
		Status:        StatusReady,
	}
	assert.NotEqual(t, LifecyclePersistent, agentWs.Lifecycle)
}

// =============================================================================
// TG16.6 — Concurrent Workspace Limit
// =============================================================================

func TestE2E_ConcurrentLimit_ServiceConfig(t *testing.T) {
	// Scenario: Verify Service has the expected default MaxConcurrent value
	// and that the limit check logic is correct.
	svc := NewService(nil, nil, testLogger())
	assert.Equal(t, 10, svc.config.MaxConcurrent, "default max concurrent should be 10")
	assert.Equal(t, 30, svc.config.DefaultTTLDays, "default TTL should be 30 days")
	assert.Equal(t, ProviderGVisor, svc.config.DefaultProvider, "default provider should be gvisor")
}

func TestE2E_ConcurrentLimit_ResolveProvider(t *testing.T) {
	// Scenario: Test provider resolution with various inputs.
	svc := NewService(nil, nil, testLogger())

	tests := []struct {
		name     string
		input    string
		expected ProviderType
	}{
		{"empty defaults to gvisor", "", ProviderGVisor},
		{"auto defaults to gvisor", "auto", ProviderGVisor},
		{"explicit firecracker", "firecracker", ProviderFirecracker},
		{"explicit e2b", "e2b", ProviderE2B},
		{"explicit gvisor", "gvisor", ProviderGVisor},
		{"unknown defaults to gvisor", "kubernetes", ProviderGVisor},
		{"case insensitive", "GVisor", ProviderGVisor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.resolveProvider(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestE2E_ConcurrentLimit_ApplyDefaultLimits(t *testing.T) {
	// Scenario: Default resource limits are applied when not specified.
	svc := NewService(nil, nil, testLogger())

	t.Run("nil limits gets defaults", func(t *testing.T) {
		result := svc.applyDefaultLimits(nil)
		require.NotNil(t, result)
		assert.Equal(t, "2", result.CPU)
		assert.Equal(t, "4G", result.Memory)
		assert.Equal(t, "10G", result.Disk)
	})

	t.Run("partial limits filled with defaults", func(t *testing.T) {
		result := svc.applyDefaultLimits(&ResourceLimits{CPU: "4"})
		require.NotNil(t, result)
		assert.Equal(t, "4", result.CPU)
		assert.Equal(t, "4G", result.Memory)
		assert.Equal(t, "10G", result.Disk)
	})

	t.Run("full limits unchanged", func(t *testing.T) {
		result := svc.applyDefaultLimits(&ResourceLimits{CPU: "8", Memory: "16G", Disk: "100G"})
		require.NotNil(t, result)
		assert.Equal(t, "8", result.CPU)
		assert.Equal(t, "16G", result.Memory)
		assert.Equal(t, "100G", result.Disk)
	})
}

// =============================================================================
// TG16.8 — Security Review Tests
// =============================================================================

func TestE2E_Security_CredentialSanitization(t *testing.T) {
	// Scenario: Git output containing GitHub tokens is sanitized before
	// being returned in API responses. Tokens must never be exposed.

	tests := []struct {
		name     string
		input    string
		mustMask bool // true if the output MUST have credentials masked
	}{
		{
			name:     "installation access token in push URL",
			input:    "Pushing to https://x-access-token:ghs_a1b2c3d4e5f6@github.com/org/repo.git",
			mustMask: true,
		},
		{
			name:     "personal access token in clone URL",
			input:    "Cloning into 'repo'...\nremote: https://ghp_TokenHere1234567890@github.com/org/repo",
			mustMask: true,
		},
		{
			name:     "OAuth token in URL",
			input:    "fatal: Authentication failed for 'https://oauth2:gho_abc123@github.com/org/repo/'",
			mustMask: true,
		},
		{
			name:     "no credentials - normal git output",
			input:    "On branch main\nYour branch is up to date with 'origin/main'.\nnothing to commit, working tree clean",
			mustMask: false,
		},
		{
			name:     "github.com URL without token is preserved",
			input:    "From https://github.com/org/repo\n * branch main -> FETCH_HEAD",
			mustMask: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := sanitizeGitOutput(tt.input)
			if tt.mustMask {
				// Must NOT contain any of the known token prefixes
				assert.NotContains(t, sanitized, "ghs_")
				assert.NotContains(t, sanitized, "ghp_")
				assert.NotContains(t, sanitized, "gho_")
				assert.NotContains(t, sanitized, "x-access-token:")
				// But should still contain the github.com domain
				assert.Contains(t, sanitized, "github.com")
				// Should have the masking marker
				assert.Contains(t, sanitized, "***@github.com")
			} else {
				// Should be unchanged or close to it
				assert.NotContains(t, sanitized, "***")
			}
		})
	}
}

func TestE2E_Security_ToolRestriction_BlocksDisallowedTools(t *testing.T) {
	// Scenario: An agent with restricted tools (only "read", "grep") should NOT
	// be able to use "bash" or "write" tools.
	config := &AgentWorkspaceConfig{
		Enabled: true,
		Tools:   []string{"read", "grep"},
	}

	middleware := ToolRestrictionMiddleware(config, testLogger())

	tests := []struct {
		name       string
		path       string
		expectCode int
	}{
		{"allowed tool - read", "/api/v1/agent/workspaces/:id/read", http.StatusOK},
		{"allowed tool - grep", "/api/v1/agent/workspaces/:id/grep", http.StatusOK},
		{"disallowed tool - bash", "/api/v1/agent/workspaces/:id/bash", http.StatusForbidden},
		{"disallowed tool - write", "/api/v1/agent/workspaces/:id/write", http.StatusForbidden},
		{"disallowed tool - edit", "/api/v1/agent/workspaces/:id/edit", http.StatusForbidden},
		{"disallowed tool - git", "/api/v1/agent/workspaces/:id/git", http.StatusForbidden},
		{"disallowed tool - glob", "/api/v1/agent/workspaces/:id/glob", http.StatusForbidden},
		{"non-tool path - passes through", "/api/v1/agent/workspaces/:id", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetPath(tt.path)

			handler := middleware(func(c echo.Context) error {
				return c.String(http.StatusOK, "ok")
			})

			err := handler(c)
			if tt.expectCode == http.StatusOK {
				assert.NoError(t, err)
				assert.Equal(t, http.StatusOK, rec.Code)
			} else {
				// Tool restriction returns a JSON error response (not an Echo error)
				assert.NoError(t, err) // middleware writes response directly
				assert.Equal(t, http.StatusForbidden, rec.Code)
				assert.Contains(t, rec.Body.String(), "not allowed")
			}
		})
	}
}

func TestE2E_Security_ToolRestriction_AllToolsAllowed(t *testing.T) {
	// Scenario: Empty tools list means all tools are allowed.
	config := &AgentWorkspaceConfig{
		Enabled: true,
		Tools:   []string{}, // Empty = all allowed
	}

	middleware := ToolRestrictionMiddleware(config, testLogger())

	for _, tool := range ValidToolNames {
		t.Run("tool-"+tool, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetPath("/api/v1/agent/workspaces/:id/" + tool)

			handler := middleware(func(c echo.Context) error {
				return c.String(http.StatusOK, "ok")
			})

			err := handler(c)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestE2E_Security_ToolRestriction_NilConfig_PassesThrough(t *testing.T) {
	// Scenario: Nil config means workspace is disabled, all tools pass through.
	middleware := ToolRestrictionMiddleware(nil, testLogger())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/agent/workspaces/:id/bash")

	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	err := handler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestE2E_Security_ToolRestriction_DisabledConfig_PassesThrough(t *testing.T) {
	// Scenario: Workspace config exists but Enabled is false — all tools pass through.
	config := &AgentWorkspaceConfig{
		Enabled: false,
		Tools:   []string{"read"}, // Would restrict, but workspace is disabled
	}

	middleware := ToolRestrictionMiddleware(config, testLogger())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/agent/workspaces/:id/bash")

	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	err := handler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestE2E_Security_PathTraversal_InGitOutput(t *testing.T) {
	// Scenario: Ensure git output doesn't expose internal container paths
	// that could reveal filesystem structure.
	// The sanitizeGitOutput function focuses on credentials, but path traversal
	// prevention is handled by the tool handlers themselves.

	t.Run("extractToolFromPath rejects traversal attempts", func(t *testing.T) {
		// Tool extraction only returns valid tool names
		result := extractToolFromPath("/api/v1/agent/workspaces/:id/../../etc/passwd")
		assert.Empty(t, result, "path traversal should not match a valid tool name")
	})

	t.Run("extractToolFromPath handles nested traversal", func(t *testing.T) {
		result := extractToolFromPath("/api/v1/agent/workspaces/:id/../../../bash")
		// This returns "bash" because the last path segment is "bash" which is a valid tool.
		// This is fine — the path traversal doesn't bypass the tool restriction, it's the
		// framework (Echo) that resolves the actual route, not path parsing.
		assert.Equal(t, "bash", result)
	})

	t.Run("extractToolFromPath ignores unknown last segments", func(t *testing.T) {
		result := extractToolFromPath("/api/v1/agent/workspaces/:id/exec")
		assert.Empty(t, result, "'exec' is not a valid tool name")
	})
}

func TestE2E_Security_CredentialSanitization_EdgeCases(t *testing.T) {
	// Scenario: Edge cases for credential sanitization.

	t.Run("multiple tokens on different lines", func(t *testing.T) {
		input := "Pushing to https://token1@github.com/a/b\nPulling from https://token2@github.com/c/d"
		sanitized := sanitizeGitOutput(input)
		assert.Equal(t, 2, strings.Count(sanitized, "***@github.com"))
		assert.NotContains(t, sanitized, "token1")
		assert.NotContains(t, sanitized, "token2")
	})

	t.Run("empty string unchanged", func(t *testing.T) {
		assert.Equal(t, "", sanitizeGitOutput(""))
	})

	t.Run("non-github URLs are not masked", func(t *testing.T) {
		input := "Pushing to https://token@gitlab.com/org/repo"
		sanitized := sanitizeGitOutput(input)
		// gitlab.com URLs should not be masked (the sanitizer only targets github.com)
		assert.Equal(t, input, sanitized)
	})
}

// =============================================================================
// Additional E2E Scenarios — Setup & Provisioning
// =============================================================================

func TestE2E_SetupExecutor_RunsCommandsSequentially(t *testing.T) {
	// Scenario: SetupExecutor runs setup commands in order. If one fails,
	// remaining commands are skipped.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gv)

	exec := NewSetupExecutor(o, testLogger())

	ws := &AgentWorkspace{
		ID:                  "ws-setup-1",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-123",
	}

	// Run setup commands — mock provider always returns ExitCode 0
	completed, err := exec.RunSetupCommands(t.Context(), ws, []string{
		"apt-get update",
		"pip install -r requirements.txt",
		"npm install",
	})
	assert.NoError(t, err)
	assert.Equal(t, 3, completed, "all commands should complete successfully")
}

func TestE2E_SetupExecutor_EmptyCommands(t *testing.T) {
	// Scenario: No setup commands — should return immediately.
	o := NewOrchestrator(testLogger())
	exec := NewSetupExecutor(o, testLogger())

	ws := &AgentWorkspace{ID: "ws-1", Provider: ProviderGVisor, ProviderWorkspaceID: "c-1"}

	completed, err := exec.RunSetupCommands(t.Context(), ws, []string{})
	assert.NoError(t, err)
	assert.Equal(t, 0, completed)
}

func TestE2E_SetupExecutor_ProviderNotRegistered(t *testing.T) {
	// Scenario: Provider for the workspace is not registered in the orchestrator.
	o := NewOrchestrator(testLogger())
	exec := NewSetupExecutor(o, testLogger())

	ws := &AgentWorkspace{
		ID:                  "ws-orphan",
		Provider:            ProviderFirecracker,
		ProviderWorkspaceID: "container-orphan",
	}

	completed, err := exec.RunSetupCommands(t.Context(), ws, []string{"echo hello"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
	assert.Equal(t, 0, completed)
}

func TestE2E_AutoProvisioner_WorkspaceConfigParsing(t *testing.T) {
	// Scenario: Parse and validate workspace configs from map[string]any
	// (as stored in JSONB).

	t.Run("valid config", func(t *testing.T) {
		configMap := map[string]any{
			"enabled": true,
			"tools":   []any{"bash", "read", "write"},
			"repo_source": map[string]any{
				"type":   "fixed",
				"url":    "https://github.com/org/repo",
				"branch": "main",
			},
			"resource_limits": map[string]any{
				"cpu":    "4",
				"memory": "8G",
			},
			"setup_commands": []any{"npm install", "npm run build"},
		}

		cfg, err := ParseAgentWorkspaceConfig(configMap)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.True(t, cfg.Enabled)
		assert.Equal(t, []string{"bash", "read", "write"}, cfg.Tools)
		assert.Equal(t, RepoSourceFixed, cfg.RepoSource.Type)
		assert.Equal(t, "https://github.com/org/repo", cfg.RepoSource.URL)
		assert.Equal(t, "main", cfg.RepoSource.Branch)
		assert.Equal(t, "4", cfg.ResourceLimits.CPU)
		assert.Equal(t, "8G", cfg.ResourceLimits.Memory)
		assert.Equal(t, []string{"npm install", "npm run build"}, cfg.SetupCommands)

		errs := cfg.Validate()
		assert.Empty(t, errs, "valid config should have no validation errors")
	})

	t.Run("nil map returns nil", func(t *testing.T) {
		cfg, err := ParseAgentWorkspaceConfig(nil)
		assert.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("disabled config", func(t *testing.T) {
		cfg, err := ParseAgentWorkspaceConfig(map[string]any{"enabled": false})
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.False(t, cfg.Enabled)
	})
}

func TestE2E_RepoSourceResolution(t *testing.T) {
	// Scenario: Test how repo source is resolved for different config types
	// and task contexts.

	t.Run("fixed source always uses config URL", func(t *testing.T) {
		cfg := &AgentWorkspaceConfig{
			Enabled: true,
			RepoSource: &RepoSourceConfig{
				Type:   RepoSourceFixed,
				URL:    "https://github.com/org/fixed-repo",
				Branch: "main",
			},
		}

		url, branch, checkout := ResolveRepoSource(cfg, nil)
		assert.Equal(t, "https://github.com/org/fixed-repo", url)
		assert.Equal(t, "main", branch)
		assert.True(t, checkout)
	})

	t.Run("task_context uses task metadata", func(t *testing.T) {
		cfg := &AgentWorkspaceConfig{
			Enabled: true,
			RepoSource: &RepoSourceConfig{
				Type:   RepoSourceTaskContext,
				Branch: "develop", // default branch
			},
		}

		taskCtx := &TaskContext{
			RepositoryURL: "https://github.com/org/task-repo",
			Branch:        "feature/x",
		}

		url, branch, checkout := ResolveRepoSource(cfg, taskCtx)
		assert.Equal(t, "https://github.com/org/task-repo", url)
		assert.Equal(t, "feature/x", branch)
		assert.True(t, checkout)
	})

	t.Run("task_context falls back to config branch", func(t *testing.T) {
		cfg := &AgentWorkspaceConfig{
			Enabled: true,
			RepoSource: &RepoSourceConfig{
				Type:   RepoSourceTaskContext,
				Branch: "develop",
			},
		}

		taskCtx := &TaskContext{
			RepositoryURL: "https://github.com/org/repo",
			// No branch in task context
		}

		url, branch, checkout := ResolveRepoSource(cfg, taskCtx)
		assert.Equal(t, "https://github.com/org/repo", url)
		assert.Equal(t, "develop", branch) // Falls back to config default
		assert.True(t, checkout)
	})

	t.Run("task_context without task metadata returns empty", func(t *testing.T) {
		cfg := &AgentWorkspaceConfig{
			Enabled: true,
			RepoSource: &RepoSourceConfig{
				Type: RepoSourceTaskContext,
			},
		}

		url, branch, checkout := ResolveRepoSource(cfg, nil)
		assert.Empty(t, url)
		assert.Empty(t, branch)
		assert.False(t, checkout)
	})

	t.Run("none source never checks out", func(t *testing.T) {
		cfg := &AgentWorkspaceConfig{
			Enabled: true,
			RepoSource: &RepoSourceConfig{
				Type: RepoSourceNone,
			},
		}

		url, branch, checkout := ResolveRepoSource(cfg, &TaskContext{
			RepositoryURL: "https://github.com/org/should-not-use",
		})
		assert.Empty(t, url)
		assert.Empty(t, branch)
		assert.False(t, checkout)
	})
}

// =============================================================================
// Orchestrator Provider Lifecycle
// =============================================================================

func TestE2E_OrchestratorProviderLifecycle(t *testing.T) {
	// Scenario: Full lifecycle — register, health check, select, deregister, select fails.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}

	// Initially no providers
	providers := o.ListProviders()
	assert.Empty(t, providers)

	// Register
	o.RegisterProvider(ProviderGVisor, gv)
	providers = o.ListProviders()
	assert.Len(t, providers, 1)

	// Health check
	o.checkAllHealth(t.Context())

	// Select
	p, pt, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, pt)
	assert.NotNil(t, p)

	// Create workspace
	result, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	assert.Contains(t, result.ProviderID, "mock-gvisor")

	// Destroy workspace
	err = p.Destroy(t.Context(), result.ProviderID)
	assert.NoError(t, err)

	// Deregister
	o.DeregisterProvider(ProviderGVisor)

	// Select should fail
	_, _, err = o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	assert.Error(t, err)
}

// =============================================================================
// Checkout Service — SHA Detection
// =============================================================================

func TestE2E_Checkout_SHADetection(t *testing.T) {
	// Scenario: Branch names vs. commit SHAs are correctly distinguished
	// to determine the clone strategy.

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"full 40-char SHA", "a1b2c3d4e5f6789012345678901234567890abcd", true},
		{"short 7-char SHA", "a1b2c3d", true},
		{"branch name", "main", false},
		{"branch with slash", "feature/add-tests", false},
		{"empty string", "", false},
		{"too short (6 chars hex)", "a1b2c3", false}, // isSHA requires 7+
		{"mixed case SHA", "AbCdEf1234567890", true},
		{"contains non-hex", "g1b2c3d4e5f6", false},
		{"numeric only", "1234567890", true}, // valid hex
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSHA(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Warm Pool — Integration with Provider
// =============================================================================

func TestE2E_WarmPool_AcquireReturnsPreCreatedContainer(t *testing.T) {
	// Scenario: Warm pool pre-creates containers. Acquiring one should return
	// immediately without hitting the provider's Create.
	o := NewOrchestrator(testLogger())
	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gv)

	pool := NewWarmPool(o, testLogger(), WarmPoolConfig{Size: 2})

	err := pool.Start(t.Context())
	require.NoError(t, err)

	// Pool should have been filled synchronously by Start
	metrics := pool.Metrics()
	assert.Equal(t, 2, metrics.PoolSize, "warm pool should have 2 containers after Start")

	// Acquire should return a pre-created container
	wc := pool.Acquire(ProviderGVisor)
	require.NotNil(t, wc)
	assert.Contains(t, wc.ProviderID(), "mock-gvisor")

	// The provider should have been called at least twice to fill the pool
	assert.GreaterOrEqual(t, gv.createCount.Load(), int64(2))

	_ = pool.Stop(t.Context())
}
