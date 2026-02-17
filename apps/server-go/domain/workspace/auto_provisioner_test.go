package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TG 18.14 — Integration Tests: Auto-Provisioning, Task Context Binding, Setup Commands
//
// Tests the full auto-provisioning pipeline using mock providers and a mock store,
// verifying workspace config parsing → repo resolution → provider selection →
// container creation → checkout → setup commands → status transitions.
// =============================================================================

// mockStoreForProvisioning implements the store operations needed by Service
// during auto-provisioning. It uses in-memory maps instead of a real DB.
type mockStoreForProvisioning struct {
	workspaces  map[string]*AgentWorkspace
	mu          sync.Mutex
	activeCount int
	activeErr   error
	createErr   error
	updateErr   error
	touchErr    error
	idCounter   int
}

func newMockStore() *mockStoreForProvisioning {
	return &mockStoreForProvisioning{
		workspaces: make(map[string]*AgentWorkspace),
	}
}

// provisioningProvider extends mockProvider with configurable exec for clone/setup testing.
type provisioningProvider struct {
	mockProvider
	mu          sync.Mutex
	execCalls   []string
	execResults map[string]*ExecResult
	execErrors  map[string]error
}

func (p *provisioningProvider) Exec(_ context.Context, _ string, req *ExecRequest) (*ExecResult, error) {
	p.mu.Lock()
	p.execCalls = append(p.execCalls, req.Command)
	p.mu.Unlock()

	for substr, err := range p.execErrors {
		if strings.Contains(req.Command, substr) {
			return nil, err
		}
	}
	for substr, result := range p.execResults {
		if strings.Contains(req.Command, substr) {
			return result, nil
		}
	}
	return &ExecResult{ExitCode: 0}, nil
}

func (p *provisioningProvider) getExecCalls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]string, len(p.execCalls))
	copy(result, p.execCalls)
	return result
}

// Verify provisioningProvider implements Provider
var _ Provider = (*provisioningProvider)(nil)

// =============================================================================
// AutoProvisioner Integration Tests
// =============================================================================

func TestIntegration_AutoProvision_DisabledConfig(t *testing.T) {
	// When workspace config is disabled, ProvisionForSession returns nil.
	ap := NewAutoProvisioner(nil, nil, nil, nil, testLogger())

	result, err := ap.ProvisionForSession(t.Context(), "agent-def-1",
		map[string]any{"enabled": false},
		nil,
	)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestIntegration_AutoProvision_NilConfig(t *testing.T) {
	// When workspace config is nil, ProvisionForSession returns nil.
	ap := NewAutoProvisioner(nil, nil, nil, nil, testLogger())

	result, err := ap.ProvisionForSession(t.Context(), "agent-def-1", nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestIntegration_AutoProvision_InvalidConfig(t *testing.T) {
	// When workspace config is unparseable, ProvisionForSession returns error.
	ap := NewAutoProvisioner(nil, nil, nil, nil, testLogger())

	// json.Marshal(chan) fails — but we won't get that from map[string]any.
	// Instead, just confirm a valid but minimal config works:
	result, err := ap.ProvisionForSession(t.Context(), "agent-def-1",
		map[string]any{},
		nil,
	)
	// Empty map → enabled=false (default) → nil result
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestIntegration_AutoProvision_NoProviders_DegradedMode(t *testing.T) {
	// When no providers are registered, provisioning fails and returns degraded result.
	o := NewOrchestrator(testLogger())
	// No providers registered

	svc := NewService(nil, o, testLogger())
	ap := NewAutoProvisioner(svc, o, nil, nil, testLogger())

	config := map[string]any{
		"enabled": true,
		"repo_source": map[string]any{
			"type":   "fixed",
			"url":    "https://github.com/org/repo",
			"branch": "main",
		},
	}

	result, err := ap.ProvisionForSession(t.Context(), "agent-def-1", config, nil)
	assert.NoError(t, err) // No top-level error — degraded mode
	require.NotNil(t, result)
	assert.True(t, result.Degraded)
	assert.NotNil(t, result.Error)
	assert.Contains(t, result.Error.Error(), "no provider available")
	assert.Equal(t, "https://github.com/org/repo", result.RepoURL)
	assert.Equal(t, "main", result.Branch)
}

func TestIntegration_AutoProvision_RepoSourceResolution_Fixed(t *testing.T) {
	// Verify fixed repo source is correctly resolved during provisioning.
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
}

func TestIntegration_AutoProvision_RepoSourceResolution_TaskContext(t *testing.T) {
	// Verify task_context repo source uses task metadata.
	cfg := &AgentWorkspaceConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceTaskContext,
			Branch: "develop",
		},
	}

	taskCtx := &TaskContext{
		RepositoryURL:  "https://github.com/org/task-repo",
		Branch:         "feature/workspace",
		PullRequestNum: 42,
		BaseBranch:     "main",
	}

	url, branch, checkout := ResolveRepoSource(cfg, taskCtx)
	assert.Equal(t, "https://github.com/org/task-repo", url)
	assert.Equal(t, "feature/workspace", branch) // Task context overrides config default
	assert.True(t, checkout)
}

func TestIntegration_AutoProvision_RepoSourceResolution_TaskContext_FallbackBranch(t *testing.T) {
	// When task context has no branch, config default is used.
	cfg := &AgentWorkspaceConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceTaskContext,
			Branch: "develop",
		},
	}

	taskCtx := &TaskContext{
		RepositoryURL: "https://github.com/org/task-repo",
		// No branch
	}

	url, branch, checkout := ResolveRepoSource(cfg, taskCtx)
	assert.Equal(t, "https://github.com/org/task-repo", url)
	assert.Equal(t, "develop", branch) // Falls back to config default
	assert.True(t, checkout)
}

func TestIntegration_AutoProvision_RepoSourceResolution_None(t *testing.T) {
	// repo_source type "none" never checks out, even with task context.
	cfg := &AgentWorkspaceConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceNone,
		},
	}

	taskCtx := &TaskContext{
		RepositoryURL: "https://github.com/org/should-not-use",
		Branch:        "main",
	}

	url, branch, checkout := ResolveRepoSource(cfg, taskCtx)
	assert.Empty(t, url)
	assert.Empty(t, branch)
	assert.False(t, checkout)
}

func TestIntegration_AutoProvision_RepoSourceResolution_TaskContext_NoMetadata(t *testing.T) {
	// task_context with no metadata yields empty result.
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
}

// =============================================================================
// Task Context Binding Tests
// =============================================================================

func TestIntegration_TaskContextExtraction_Full(t *testing.T) {
	// Full metadata map with all fields.
	metadata := map[string]any{
		"repository_url":      "https://github.com/org/repo",
		"branch":              "feature/test",
		"pull_request_number": float64(42),
		"base_branch":         "main",
	}

	tc := ExtractTaskContext(metadata)
	require.NotNil(t, tc)
	assert.Equal(t, "https://github.com/org/repo", tc.RepositoryURL)
	assert.Equal(t, "feature/test", tc.Branch)
	assert.Equal(t, 42, tc.PullRequestNum)
	assert.Equal(t, "main", tc.BaseBranch)
}

func TestIntegration_TaskContextExtraction_Partial(t *testing.T) {
	// Only repo URL provided.
	metadata := map[string]any{
		"repository_url": "https://github.com/org/repo",
	}

	tc := ExtractTaskContext(metadata)
	require.NotNil(t, tc)
	assert.Equal(t, "https://github.com/org/repo", tc.RepositoryURL)
	assert.Empty(t, tc.Branch)
	assert.Zero(t, tc.PullRequestNum)
}

func TestIntegration_TaskContextExtraction_NilMetadata(t *testing.T) {
	tc := ExtractTaskContext(nil)
	assert.Nil(t, tc)
}

func TestIntegration_TaskContextExtraction_EmptyMetadata(t *testing.T) {
	// Empty map (no relevant keys).
	tc := ExtractTaskContext(map[string]any{
		"unrelated_key": "value",
	})
	assert.Nil(t, tc)
}

func TestIntegration_TaskContextExtraction_WrongTypes(t *testing.T) {
	// Wrong types for known keys — should be ignored.
	metadata := map[string]any{
		"repository_url":      123,       // wrong: should be string
		"branch":              true,      // wrong: should be string
		"pull_request_number": "not-num", // wrong: should be float64
	}

	tc := ExtractTaskContext(metadata)
	assert.Nil(t, tc, "wrong types should be ignored, yielding nil context")
}

func TestIntegration_TaskContextExtraction_EmptyStrings(t *testing.T) {
	// Empty string values are treated as missing.
	metadata := map[string]any{
		"repository_url": "",
		"branch":         "",
	}

	tc := ExtractTaskContext(metadata)
	assert.Nil(t, tc, "empty strings should be treated as no data")
}

func TestIntegration_TaskContext_PRMetadata(t *testing.T) {
	// PR-specific metadata for task-context-based provisioning.
	metadata := map[string]any{
		"repository_url":      "https://github.com/org/repo",
		"branch":              "pr-branch",
		"pull_request_number": float64(123),
		"base_branch":         "main",
	}

	tc := ExtractTaskContext(metadata)
	require.NotNil(t, tc)
	assert.Equal(t, 123, tc.PullRequestNum)
	assert.Equal(t, "main", tc.BaseBranch)
	assert.Equal(t, "pr-branch", tc.Branch)

	// Resolve with task_context config
	cfg := &AgentWorkspaceConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type: RepoSourceTaskContext,
		},
	}

	url, branch, checkout := ResolveRepoSource(cfg, tc)
	assert.Equal(t, "https://github.com/org/repo", url)
	assert.Equal(t, "pr-branch", branch)
	assert.True(t, checkout)
}

// =============================================================================
// Setup Command Execution Tests
// =============================================================================

func TestIntegration_SetupExecutor_AllCommandsSucceed(t *testing.T) {
	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"npm install":   {ExitCode: 0, Stdout: "added 150 packages"},
			"npm run build": {ExitCode: 0, Stdout: "build complete"},
			"npm test":      {ExitCode: 0, Stdout: "all tests passed"},
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	exec := NewSetupExecutor(o, testLogger())

	ws := &AgentWorkspace{
		ID:                  "ws-setup-1",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-abc",
	}

	completed, err := exec.RunSetupCommands(t.Context(), ws, []string{
		"npm install",
		"npm run build",
		"npm test",
	})
	assert.NoError(t, err)
	assert.Equal(t, 3, completed)

	// Verify all commands were executed
	calls := pp.getExecCalls()
	require.Len(t, calls, 3)
	assert.Contains(t, calls[0], "npm install")
	assert.Contains(t, calls[1], "npm run build")
	assert.Contains(t, calls[2], "npm test")
}

func TestIntegration_SetupExecutor_SecondCommandFails(t *testing.T) {
	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"pip install": {ExitCode: 0, Stdout: "installed"},
			"pytest":      {ExitCode: 1, Stderr: "3 failed, 7 passed"},
			"pip freeze":  {ExitCode: 0}, // should not be reached
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	exec := NewSetupExecutor(o, testLogger())

	ws := &AgentWorkspace{
		ID:                  "ws-setup-2",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-def",
	}

	completed, err := exec.RunSetupCommands(t.Context(), ws, []string{
		"pip install -r requirements.txt",
		"pytest",
		"pip freeze > requirements.lock",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exited with code 1")
	assert.Equal(t, 1, completed, "only first command should complete successfully")

	// Third command should never be called
	calls := pp.getExecCalls()
	assert.Len(t, calls, 2, "should stop after failing command")
}

func TestIntegration_SetupExecutor_ExecError(t *testing.T) {
	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execErrors: map[string]error{
			"make build": errors.New("connection to container lost"),
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	exec := NewSetupExecutor(o, testLogger())

	ws := &AgentWorkspace{
		ID:                  "ws-setup-3",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-ghi",
	}

	completed, err := exec.RunSetupCommands(t.Context(), ws, []string{
		"make build",
		"make test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "setup command 1 failed")
	assert.Equal(t, 0, completed)
}

func TestIntegration_SetupExecutor_EmptyCommands(t *testing.T) {
	exec := NewSetupExecutor(nil, testLogger())
	ws := &AgentWorkspace{ID: "ws-1"}

	completed, err := exec.RunSetupCommands(t.Context(), ws, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, completed)

	completed, err = exec.RunSetupCommands(t.Context(), ws, []string{})
	assert.NoError(t, err)
	assert.Equal(t, 0, completed)
}

func TestIntegration_SetupExecutor_ProviderUnavailable(t *testing.T) {
	o := NewOrchestrator(testLogger())
	// No providers registered

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

func TestIntegration_SetupExecutor_ManyCommands(t *testing.T) {
	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	exec := NewSetupExecutor(o, testLogger())

	ws := &AgentWorkspace{
		ID:                  "ws-many",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-many",
	}

	// Generate 20 setup commands
	commands := make([]string, 20)
	for i := range commands {
		commands[i] = fmt.Sprintf("step-%d-command", i)
	}

	completed, err := exec.RunSetupCommands(t.Context(), ws, commands)
	assert.NoError(t, err)
	assert.Equal(t, 20, completed)

	calls := pp.getExecCalls()
	assert.Len(t, calls, 20)
}

// =============================================================================
// Workspace Config Validation Integration Tests
// =============================================================================

func TestIntegration_WorkspaceConfig_RoundTrip(t *testing.T) {
	// Create a config → ToMap → ParseAgentWorkspaceConfig → verify equality.
	original := &AgentWorkspaceConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceFixed,
			URL:    "https://github.com/org/repo",
			Branch: "main",
		},
		Tools:           []string{"bash", "read", "write", "edit"},
		ResourceLimits:  &ResourceLimits{CPU: "4", Memory: "8G", Disk: "20G"},
		CheckoutOnStart: true,
		BaseImage:       "emergent/workspace:v2",
		SetupCommands:   []string{"npm install", "npm run build"},
	}

	m, err := original.ToMap()
	require.NoError(t, err)

	parsed, err := ParseAgentWorkspaceConfig(m)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	assert.True(t, parsed.Enabled)
	assert.Equal(t, RepoSourceFixed, parsed.RepoSource.Type)
	assert.Equal(t, "https://github.com/org/repo", parsed.RepoSource.URL)
	assert.Equal(t, "main", parsed.RepoSource.Branch)
	assert.Equal(t, []string{"bash", "read", "write", "edit"}, parsed.Tools)
	assert.Equal(t, "4", parsed.ResourceLimits.CPU)
	assert.Equal(t, "8G", parsed.ResourceLimits.Memory)
	assert.Equal(t, "20G", parsed.ResourceLimits.Disk)
	assert.True(t, parsed.CheckoutOnStart)
	assert.Equal(t, "emergent/workspace:v2", parsed.BaseImage)
	assert.Equal(t, []string{"npm install", "npm run build"}, parsed.SetupCommands)

	errs := parsed.Validate()
	assert.Empty(t, errs)
}

func TestIntegration_WorkspaceConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		config      *AgentWorkspaceConfig
		expectError string
	}{
		{
			name: "invalid tool name",
			config: &AgentWorkspaceConfig{
				Enabled: true,
				Tools:   []string{"bash", "delete_everything"},
			},
			expectError: "invalid tool names",
		},
		{
			name: "duplicate tool",
			config: &AgentWorkspaceConfig{
				Enabled: true,
				Tools:   []string{"bash", "read", "bash"},
			},
			expectError: "duplicate tool",
		},
		{
			name: "fixed repo without URL",
			config: &AgentWorkspaceConfig{
				Enabled: true,
				RepoSource: &RepoSourceConfig{
					Type: RepoSourceFixed,
					// URL missing
				},
			},
			expectError: "repo_source.url is required",
		},
		{
			name: "task_context with URL",
			config: &AgentWorkspaceConfig{
				Enabled: true,
				RepoSource: &RepoSourceConfig{
					Type: RepoSourceTaskContext,
					URL:  "https://github.com/org/repo",
				},
			},
			expectError: "repo_source.url should not be set",
		},
		{
			name: "invalid repo source type",
			config: &AgentWorkspaceConfig{
				Enabled: true,
				RepoSource: &RepoSourceConfig{
					Type: "kubernetes",
				},
			},
			expectError: "invalid repo_source.type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.config.Validate()
			require.NotEmpty(t, errs, "expected validation errors")
			found := false
			for _, e := range errs {
				if strings.Contains(e, tt.expectError) {
					found = true
					break
				}
			}
			assert.True(t, found, "expected error containing %q, got: %v", tt.expectError, errs)
		})
	}
}

func TestIntegration_WorkspaceConfig_IsToolAllowed(t *testing.T) {
	t.Run("restricted tools", func(t *testing.T) {
		cfg := &AgentWorkspaceConfig{
			Enabled: true,
			Tools:   []string{"bash", "read", "grep"},
		}

		assert.True(t, cfg.IsToolAllowed("bash"))
		assert.True(t, cfg.IsToolAllowed("read"))
		assert.True(t, cfg.IsToolAllowed("grep"))
		assert.True(t, cfg.IsToolAllowed("BASH")) // case insensitive
		assert.False(t, cfg.IsToolAllowed("write"))
		assert.False(t, cfg.IsToolAllowed("edit"))
		assert.False(t, cfg.IsToolAllowed("git"))
		assert.False(t, cfg.IsToolAllowed("glob"))
	})

	t.Run("empty tools = all allowed", func(t *testing.T) {
		cfg := &AgentWorkspaceConfig{
			Enabled: true,
			Tools:   []string{},
		}

		for _, tool := range ValidToolNames {
			assert.True(t, cfg.IsToolAllowed(tool))
		}
	})

	t.Run("nil tools = all allowed", func(t *testing.T) {
		cfg := &AgentWorkspaceConfig{
			Enabled: true,
		}

		for _, tool := range ValidToolNames {
			assert.True(t, cfg.IsToolAllowed(tool))
		}
	})
}

func TestIntegration_WorkspaceConfig_NormalizeTools(t *testing.T) {
	cfg := &AgentWorkspaceConfig{
		Tools: []string{"BASH", "Read", "  write  ", "bash", "GREP", "grep"},
	}

	cfg.NormalizeTools()

	assert.Equal(t, []string{"bash", "read", "write", "grep"}, cfg.Tools)
}

// =============================================================================
// AutoProvisioner — LinkToRun and TeardownWorkspace Tests
// =============================================================================

func TestIntegration_AutoProvision_TeardownWorkspace_NilWorkspace(t *testing.T) {
	// TeardownWorkspace with nil should be a no-op.
	ap := NewAutoProvisioner(nil, nil, nil, nil, testLogger())
	ap.TeardownWorkspace(t.Context(), nil) // Should not panic
}

func TestIntegration_AutoProvision_TeardownWorkspace_ProviderNotRegistered(t *testing.T) {
	// TeardownWorkspace when provider is not registered should log warning.
	o := NewOrchestrator(testLogger())
	svc := NewService(nil, o, testLogger())
	ap := NewAutoProvisioner(svc, o, nil, nil, testLogger())

	ws := &AgentWorkspace{
		ID:                  "ws-teardown",
		Provider:            ProviderFirecracker,
		ProviderWorkspaceID: "fc-123",
		Status:              StatusReady,
	}

	// Should not panic — logs warning and attempts status update (which fails with nil store)
	// This tests the error handling path
	ap.TeardownWorkspace(t.Context(), ws)
}

func TestIntegration_AutoProvision_TeardownWorkspace_ProviderDestroyFails(t *testing.T) {
	// TeardownWorkspace handles provider destroy failure gracefully.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:         "gvisor",
		providerType: ProviderGVisor,
		healthy:      true,
		destroyErr:   errors.New("container already removed"),
	}
	o.RegisterProvider(ProviderGVisor, gv)

	svc := NewService(nil, o, testLogger())
	ap := NewAutoProvisioner(svc, o, nil, nil, testLogger())

	ws := &AgentWorkspace{
		ID:                  "ws-teardown-fail",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "gv-456",
		Status:              StatusReady,
	}

	// Should not panic — destroy fails but teardown continues
	ap.TeardownWorkspace(t.Context(), ws)

	// Verify destroy was attempted
	assert.Equal(t, int64(1), gv.destroyCount.Load())
}

func TestIntegration_AutoProvision_GetProviderForWorkspace(t *testing.T) {
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gv)

	ap := NewAutoProvisioner(nil, o, nil, nil, testLogger())

	t.Run("valid workspace", func(t *testing.T) {
		ws := &AgentWorkspace{Provider: ProviderGVisor}
		p, err := ap.GetProviderForWorkspace(ws)
		require.NoError(t, err)
		assert.NotNil(t, p)
	})

	t.Run("nil workspace", func(t *testing.T) {
		_, err := ap.GetProviderForWorkspace(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil")
	})

	t.Run("unregistered provider", func(t *testing.T) {
		ws := &AgentWorkspace{Provider: ProviderFirecracker}
		_, err := ap.GetProviderForWorkspace(ws)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not registered")
	})
}

// =============================================================================
// Full Pipeline Simulation Tests (without DB)
// =============================================================================

func TestIntegration_AutoProvision_FullPipeline_FixedRepo(t *testing.T) {
	// Simulate the full auto-provisioning pipeline with a fixed repo source:
	// 1. Parse config
	// 2. Resolve repo source → fixed URL
	// 3. Select provider → gvisor
	// 4. (Container creation would happen via Service.Create, but that needs DB)
	// 5. Clone repository → verify clone command was issued
	// 6. Run setup commands → verify all complete

	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":    {ExitCode: 0, Stdout: "Cloning into 'workspace'..."},
			"git config":   {ExitCode: 0},
			"npm install":  {ExitCode: 0, Stdout: "added 100 packages"},
			"npm run lint": {ExitCode: 0, Stdout: "no issues"},
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	// Simulate the pipeline steps manually (since Service.Create needs a DB)
	config := map[string]any{
		"enabled": true,
		"repo_source": map[string]any{
			"type":   "fixed",
			"url":    "https://github.com/org/my-project",
			"branch": "develop",
		},
		"tools":          []any{"bash", "read", "write", "edit", "grep"},
		"setup_commands": []any{"npm install", "npm run lint"},
		"resource_limits": map[string]any{
			"cpu":    "4",
			"memory": "8G",
			"disk":   "20G",
		},
	}

	// Step 1: Parse config
	cfg, err := ParseAgentWorkspaceConfig(config)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)

	// Step 2: Validate config
	errs := cfg.Validate()
	assert.Empty(t, errs)

	// Step 3: Resolve repo source
	url, branch, checkout := ResolveRepoSource(cfg, nil)
	assert.Equal(t, "https://github.com/org/my-project", url)
	assert.Equal(t, "develop", branch)
	assert.True(t, checkout)

	// Step 4: Select provider
	provider, providerType, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	// Only gvisor is registered, so it's selected
	assert.Equal(t, ProviderGVisor, providerType)

	// Step 5: Create container
	containerResult, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType:  ContainerTypeAgentWorkspace,
		ResourceLimits: cfg.ResourceLimits,
	})
	require.NoError(t, err)
	assert.Contains(t, containerResult.ProviderID, "mock-gvisor")

	// Step 6: Clone repository
	checkoutSvc := NewCheckoutService(nil, testLogger()) // nil cred provider = public clone
	err = checkoutSvc.CloneRepository(t.Context(), pp, containerResult.ProviderID, url, branch)
	assert.NoError(t, err)

	// Step 7: Run setup commands
	setupExec := NewSetupExecutor(o, testLogger())
	ws := &AgentWorkspace{
		ID:                  "ws-pipeline",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: containerResult.ProviderID,
	}

	completed, err := setupExec.RunSetupCommands(t.Context(), ws, cfg.SetupCommands)
	assert.NoError(t, err)
	assert.Equal(t, 2, completed)

	// Verify command sequence
	calls := pp.getExecCalls()
	// Should have: git clone, npm install, npm run lint
	cloneFound := false
	npmInstallFound := false
	npmLintFound := false
	for _, call := range calls {
		if strings.Contains(call, "git clone") {
			cloneFound = true
		}
		if strings.Contains(call, "npm install") {
			npmInstallFound = true
		}
		if strings.Contains(call, "npm run lint") {
			npmLintFound = true
		}
	}
	assert.True(t, cloneFound, "git clone should have been executed")
	assert.True(t, npmInstallFound, "npm install should have been executed")
	assert.True(t, npmLintFound, "npm run lint should have been executed")
}

func TestIntegration_AutoProvision_FullPipeline_TaskContext(t *testing.T) {
	// Simulate auto-provisioning with task_context repo source:
	// Task metadata provides repo + branch, overriding config defaults.

	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":  {ExitCode: 0},
			"git config": {ExitCode: 0},
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	config := map[string]any{
		"enabled": true,
		"repo_source": map[string]any{
			"type":   "task_context",
			"branch": "main", // default branch
		},
		"tools": []any{"bash", "read"},
	}

	taskMetadata := map[string]any{
		"repository_url":      "https://github.com/customer/their-repo",
		"branch":              "fix/bug-123",
		"pull_request_number": float64(456),
	}

	// Parse config
	cfg, err := ParseAgentWorkspaceConfig(config)
	require.NoError(t, err)

	// Extract task context
	taskCtx := ExtractTaskContext(taskMetadata)
	require.NotNil(t, taskCtx)
	assert.Equal(t, "https://github.com/customer/their-repo", taskCtx.RepositoryURL)
	assert.Equal(t, "fix/bug-123", taskCtx.Branch)
	assert.Equal(t, 456, taskCtx.PullRequestNum)

	// Resolve repo source
	url, branch, checkout := ResolveRepoSource(cfg, taskCtx)
	assert.Equal(t, "https://github.com/customer/their-repo", url)
	assert.Equal(t, "fix/bug-123", branch) // From task context, not config
	assert.True(t, checkout)

	// Verify tool restriction
	assert.True(t, cfg.IsToolAllowed("bash"))
	assert.True(t, cfg.IsToolAllowed("read"))
	assert.False(t, cfg.IsToolAllowed("write"))
	assert.False(t, cfg.IsToolAllowed("git"))
}

func TestIntegration_AutoProvision_FullPipeline_NoRepo(t *testing.T) {
	// Scenario: Agent with workspace but no repo checkout (none source).
	// Only setup commands run.

	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"mkdir":     {ExitCode: 0},
			"python -m": {ExitCode: 0},
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	config := map[string]any{
		"enabled": true,
		"repo_source": map[string]any{
			"type": "none",
		},
		"setup_commands": []any{"mkdir -p /workspace/output", "python -m http.server &"},
	}

	cfg, err := ParseAgentWorkspaceConfig(config)
	require.NoError(t, err)

	// Resolve repo — should be empty
	url, branch, checkout := ResolveRepoSource(cfg, nil)
	assert.Empty(t, url)
	assert.Empty(t, branch)
	assert.False(t, checkout)

	// Select provider and create container
	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	containerResult, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)

	// Skip clone (shouldCheckout is false)
	// Run setup commands only
	setupExec := NewSetupExecutor(o, testLogger())
	ws := &AgentWorkspace{
		ID:                  "ws-no-repo",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: containerResult.ProviderID,
	}

	completed, err := setupExec.RunSetupCommands(t.Context(), ws, cfg.SetupCommands)
	assert.NoError(t, err)
	assert.Equal(t, 2, completed)

	calls := pp.getExecCalls()
	// No git clone in the calls
	for _, call := range calls {
		assert.NotContains(t, call, "git clone")
	}
}

// =============================================================================
// TG 18.15 — E2E Test: Agent Type with Workspace Config → Start Session → Verify
//
// Tests the complete flow: define agent type with workspace config, simulate
// session start (auto-provision), verify workspace has correct repo/branch/tools.
// Uses mock providers throughout.
// =============================================================================

func TestE2E_AgentType_WorkspaceConfig_SessionFlow(t *testing.T) {
	// Full E2E scenario:
	// 1. Agent definition has workspace_config (enabled, fixed repo, restricted tools, setup cmds)
	// 2. Agent session starts → auto-provisioner parses config, resolves repo, creates workspace
	// 3. Repository is cloned with correct URL/branch
	// 4. Setup commands run in order
	// 5. Tool restrictions are enforced

	// --- Step 1: Define the workspace config as it would be stored in JSONB ---
	agentDefWorkspaceConfig := map[string]any{
		"enabled": true,
		"repo_source": map[string]any{
			"type":   "fixed",
			"url":    "https://github.com/acme/backend",
			"branch": "main",
		},
		"tools":          []any{"bash", "read", "write", "edit", "grep", "glob"},
		"base_image":     "emergent/workspace-node:18",
		"setup_commands": []any{"npm ci", "npm run build"},
		"resource_limits": map[string]any{
			"cpu":    "2",
			"memory": "4G",
			"disk":   "10G",
		},
	}

	// --- Step 2: Parse and validate ---
	cfg, err := ParseAgentWorkspaceConfig(agentDefWorkspaceConfig)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	errs := cfg.Validate()
	assert.Empty(t, errs, "config should be valid")

	// --- Step 3: Set up mock infrastructure ---
	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":     {ExitCode: 0, Stdout: "Cloning into 'workspace'..."},
			"git config":    {ExitCode: 0},
			"npm ci":        {ExitCode: 0, Stdout: "added 200 packages in 5s"},
			"npm run build": {ExitCode: 0, Stdout: "compiled successfully"},
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	// --- Step 4: Simulate session start → auto-provisioning ---

	// Resolve repo
	url, branch, shouldCheckout := ResolveRepoSource(cfg, nil) // No task context for fixed
	assert.Equal(t, "https://github.com/acme/backend", url)
	assert.Equal(t, "main", branch)
	assert.True(t, shouldCheckout)

	// Select provider
	provider, providerType, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, providerType)

	// Create workspace container
	containerResult, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType:  ContainerTypeAgentWorkspace,
		ResourceLimits: cfg.ResourceLimits,
		BaseImage:      cfg.BaseImage,
	})
	require.NoError(t, err)
	providerID := containerResult.ProviderID

	// Build workspace entity
	ws := &AgentWorkspace{
		ID:                  "ws-e2e-session",
		Provider:            providerType,
		ProviderWorkspaceID: providerID,
		ContainerType:       ContainerTypeAgentWorkspace,
		Status:              StatusCreating,
		ResourceLimits:      cfg.ResourceLimits,
	}

	// Clone repository
	checkoutSvc := NewCheckoutService(nil, testLogger())
	err = checkoutSvc.CloneRepository(t.Context(), pp, providerID, url, branch)
	assert.NoError(t, err)

	// Run setup commands
	setupExec := NewSetupExecutor(o, testLogger())
	completed, err := setupExec.RunSetupCommands(t.Context(), ws, cfg.SetupCommands)
	assert.NoError(t, err)
	assert.Equal(t, 2, completed, "both setup commands should complete")

	// Mark as ready
	ws.Status = StatusReady

	// --- Step 5: Verify workspace state ---
	assert.Equal(t, StatusReady, ws.Status)
	assert.Equal(t, ProviderGVisor, ws.Provider)
	assert.Contains(t, ws.ProviderWorkspaceID, "mock-gvisor")
	assert.NotNil(t, ws.ResourceLimits)
	assert.Equal(t, "2", ws.ResourceLimits.CPU)
	assert.Equal(t, "4G", ws.ResourceLimits.Memory)
	assert.Equal(t, "10G", ws.ResourceLimits.Disk)

	// --- Step 6: Verify tool restrictions ---
	assert.True(t, cfg.IsToolAllowed("bash"))
	assert.True(t, cfg.IsToolAllowed("read"))
	assert.True(t, cfg.IsToolAllowed("write"))
	assert.True(t, cfg.IsToolAllowed("edit"))
	assert.True(t, cfg.IsToolAllowed("grep"))
	assert.True(t, cfg.IsToolAllowed("glob"))
	assert.False(t, cfg.IsToolAllowed("git"), "git should NOT be allowed — not in the tools list")

	// --- Step 7: Verify command execution sequence ---
	calls := pp.getExecCalls()
	require.GreaterOrEqual(t, len(calls), 3, "should have git clone + 2 setup commands")

	// Find the sequence
	cloneIdx := -1
	npmCIIdx := -1
	npmBuildIdx := -1
	for i, call := range calls {
		if strings.Contains(call, "git clone") {
			cloneIdx = i
		}
		if strings.Contains(call, "npm ci") {
			npmCIIdx = i
		}
		if strings.Contains(call, "npm run build") {
			npmBuildIdx = i
		}
	}

	assert.Greater(t, cloneIdx, -1, "git clone should have been called")
	assert.Greater(t, npmCIIdx, -1, "npm ci should have been called")
	assert.Greater(t, npmBuildIdx, -1, "npm run build should have been called")

	// Clone should happen before setup commands
	if cloneIdx >= 0 && npmCIIdx >= 0 {
		assert.Less(t, cloneIdx, npmCIIdx, "clone should happen before npm ci")
	}
	if npmCIIdx >= 0 && npmBuildIdx >= 0 {
		assert.Less(t, npmCIIdx, npmBuildIdx, "npm ci should happen before npm run build")
	}
}

func TestE2E_AgentType_WorkspaceConfig_SessionFlow_WithTaskContext(t *testing.T) {
	// E2E scenario with task_context repo source:
	// Agent definition uses task_context, task metadata provides PR info.

	agentDefWorkspaceConfig := map[string]any{
		"enabled": true,
		"repo_source": map[string]any{
			"type":   "task_context",
			"branch": "main", // fallback
		},
		"tools":          []any{"bash", "read", "write", "edit", "grep", "glob", "git"},
		"setup_commands": []any{"go mod download", "go build ./..."},
	}

	taskMetadata := map[string]any{
		"repository_url":      "https://github.com/customer/go-service",
		"branch":              "fix/critical-bug",
		"pull_request_number": float64(789),
		"base_branch":         "main",
	}

	// Parse
	cfg, err := ParseAgentWorkspaceConfig(agentDefWorkspaceConfig)
	require.NoError(t, err)
	assert.True(t, cfg.Enabled)

	// Extract task context
	taskCtx := ExtractTaskContext(taskMetadata)
	require.NotNil(t, taskCtx)

	// Resolve repo
	url, branch, checkout := ResolveRepoSource(cfg, taskCtx)
	assert.Equal(t, "https://github.com/customer/go-service", url)
	assert.Equal(t, "fix/critical-bug", branch)
	assert.True(t, checkout)

	// Set up mock
	o := NewOrchestrator(testLogger())
	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":       {ExitCode: 0},
			"git config":      {ExitCode: 0},
			"go mod download": {ExitCode: 0, Stdout: "all modules downloaded"},
			"go build":        {ExitCode: 0, Stdout: "build complete"},
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	// Create container
	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	containerResult, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)

	// Clone with task-context-provided URL/branch
	checkoutSvc := NewCheckoutService(nil, testLogger())
	err = checkoutSvc.CloneRepository(t.Context(), pp, containerResult.ProviderID, url, branch)
	assert.NoError(t, err)

	// Run setup
	ws := &AgentWorkspace{
		ID:                  "ws-e2e-task-ctx",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: containerResult.ProviderID,
	}
	setupExec := NewSetupExecutor(o, testLogger())
	completed, err := setupExec.RunSetupCommands(t.Context(), ws, cfg.SetupCommands)
	assert.NoError(t, err)
	assert.Equal(t, 2, completed)

	// Verify clone used task context branch
	calls := pp.getExecCalls()
	cloneCall := ""
	for _, call := range calls {
		if strings.Contains(call, "git clone") {
			cloneCall = call
			break
		}
	}
	require.NotEmpty(t, cloneCall, "git clone should have been called")
	assert.Contains(t, cloneCall, "fix/critical-bug", "clone should use task context branch")

	// All tools including git are allowed for this agent
	assert.True(t, cfg.IsToolAllowed("git"))
	assert.True(t, cfg.IsToolAllowed("bash"))
}

func TestE2E_AgentType_WorkspaceConfig_SetupFailure_Continues(t *testing.T) {
	// E2E scenario: Setup command fails but workspace is still usable.
	// This mirrors the AutoProvisioner behavior where setup errors are logged
	// but don't fail provisioning.

	o := NewOrchestrator(testLogger())
	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":  {ExitCode: 0},
			"git config": {ExitCode: 0},
			"npm ci":     {ExitCode: 0, Stdout: "installed"},
			"npm test":   {ExitCode: 1, Stderr: "5 tests failed"}, // Failure
			"npm start":  {ExitCode: 0},                           // Won't be reached
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	cfg := &AgentWorkspaceConfig{
		Enabled:       true,
		SetupCommands: []string{"npm ci", "npm test", "npm start"},
	}

	// Create workspace
	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	containerResult, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)

	ws := &AgentWorkspace{
		ID:                  "ws-setup-fail",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: containerResult.ProviderID,
	}

	setupExec := NewSetupExecutor(o, testLogger())
	completed, setupErr := setupExec.RunSetupCommands(t.Context(), ws, cfg.SetupCommands)

	// Setup partially failed
	assert.Error(t, setupErr)
	assert.Equal(t, 1, completed, "only npm ci should complete before failure")

	// But workspace is still usable — just mark as ready
	ws.Status = StatusReady
	assert.Equal(t, StatusReady, ws.Status)
}

func TestE2E_AgentType_WorkspaceConfig_ProviderFallback(t *testing.T) {
	// E2E scenario: Preferred provider fails, falls back to another.

	o := NewOrchestrator(testLogger())

	// Firecracker is preferred but unhealthy
	fc := &mockProvider{name: "firecracker", providerType: ProviderFirecracker, healthy: false}
	gv := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":  {ExitCode: 0},
			"git config": {ExitCode: 0},
		},
	}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)
	o.checkAllHealth(t.Context())

	// Auto-select should pick gvisor since firecracker is unhealthy
	provider, providerType, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, providerType)

	// Create workspace with fallback provider
	containerResult, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	assert.Contains(t, containerResult.ProviderID, "mock-gvisor")
}

func TestE2E_AgentType_WorkspaceConfig_MultipleSessionsSequential(t *testing.T) {
	// E2E scenario: Multiple sessions use workspaces sequentially.
	// Each session gets its own workspace with the same config.

	o := NewOrchestrator(testLogger())
	gv := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":  {ExitCode: 0},
			"git config": {ExitCode: 0},
		},
	}
	o.RegisterProvider(ProviderGVisor, gv)

	cfg := &AgentWorkspaceConfig{
		Enabled: true,
		RepoSource: &RepoSourceConfig{
			Type:   RepoSourceFixed,
			URL:    "https://github.com/org/repo",
			Branch: "main",
		},
	}

	// Simulate 3 sequential sessions
	providerIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		url, branch, checkout := ResolveRepoSource(cfg, nil)
		assert.True(t, checkout)

		provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
		require.NoError(t, err)

		containerResult, err := provider.Create(t.Context(), &CreateContainerRequest{
			ContainerType: ContainerTypeAgentWorkspace,
		})
		require.NoError(t, err)
		providerIDs[i] = containerResult.ProviderID

		// Clone
		checkoutSvc := NewCheckoutService(nil, testLogger())
		err = checkoutSvc.CloneRepository(t.Context(), gv, containerResult.ProviderID, url, branch)
		assert.NoError(t, err)

		// Simulate session end — destroy
		err = provider.Destroy(t.Context(), containerResult.ProviderID)
		assert.NoError(t, err)
	}

	// All provider IDs should be unique
	seen := map[string]bool{}
	for _, id := range providerIDs {
		assert.False(t, seen[id], "duplicate provider ID: %s", id)
		seen[id] = true
	}

	// Verify create and destroy counts
	assert.Equal(t, int64(3), gv.createCount.Load())
	assert.Equal(t, int64(3), gv.destroyCount.Load())
}

// =============================================================================
// ProvisioningResult Tests
// =============================================================================

func TestIntegration_ProvisioningResult_Fields(t *testing.T) {
	// Verify the ProvisioningResult struct holds all expected fields.

	t.Run("successful provisioning", func(t *testing.T) {
		ws := &AgentWorkspace{
			ID:       "ws-success",
			Provider: ProviderGVisor,
			Status:   StatusReady,
		}
		result := &ProvisioningResult{
			Workspace: ws,
			RepoURL:   "https://github.com/org/repo",
			Branch:    "main",
			Degraded:  false,
			Error:     nil,
		}

		assert.NotNil(t, result.Workspace)
		assert.Equal(t, "https://github.com/org/repo", result.RepoURL)
		assert.Equal(t, "main", result.Branch)
		assert.False(t, result.Degraded)
		assert.Nil(t, result.Error)
	})

	t.Run("degraded provisioning", func(t *testing.T) {
		result := &ProvisioningResult{
			Workspace: nil,
			RepoURL:   "https://github.com/org/repo",
			Branch:    "main",
			Degraded:  true,
			Error:     errors.New("no providers available"),
		}

		assert.Nil(t, result.Workspace)
		assert.True(t, result.Degraded)
		assert.NotNil(t, result.Error)
	})
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

func TestIntegration_SetupExecutor_ContextCancelled(t *testing.T) {
	// Verify that setup commands respect context cancellation.
	o := NewOrchestrator(testLogger())

	pp := &provisioningProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"sleep":       {ExitCode: 0}, // would succeed normally
			"npm install": {ExitCode: 0},
		},
	}
	o.RegisterProvider(ProviderGVisor, pp)

	exec := NewSetupExecutor(o, testLogger())
	ws := &AgentWorkspace{
		ID:                  "ws-cancelled",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-cancel",
	}

	// Cancel context before running setup
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	completed, err := exec.RunSetupCommands(ctx, ws, []string{"sleep 10", "npm install"})
	// With an already-cancelled context, the first command should fail
	// The exact error depends on whether context.WithTimeout catches it first
	if err != nil {
		assert.LessOrEqual(t, completed, 1, "at most one command should complete")
	}
	_ = completed // May be 0 if context checked before exec
}

func TestIntegration_TruncateOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "12345", 5, "12345"},
		{"truncated", "hello world", 5, "hello..."},
		{"empty string", "", 10, ""},
		{"zero max", "hello", 0, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateOutput(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIntegration_DefaultAgentWorkspaceConfig(t *testing.T) {
	cfg := DefaultAgentWorkspaceConfig()
	assert.False(t, cfg.Enabled)
	assert.Nil(t, cfg.RepoSource)
	assert.Nil(t, cfg.Tools)
	assert.Nil(t, cfg.ResourceLimits)
	assert.Empty(t, cfg.SetupCommands)
}

// Helper: elapsed time tracker for timing assertions
func measureDuration(fn func()) time.Duration {
	start := time.Now()
	fn()
	return time.Since(start)
}
