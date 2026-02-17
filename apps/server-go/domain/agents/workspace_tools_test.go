package agents

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent/domain/workspace"
)

// --- Mock Provider ---

// testProvider implements workspace.Provider for testing workspace tools.
type testProvider struct {
	// Configurable responses
	execResult *workspace.ExecResult
	execErr    error
	readResult *workspace.FileReadResult
	readErr    error
	writeErr   error
	listResult *workspace.FileListResult
	listErr    error

	// Captured calls for verification
	lastExecReq   *workspace.ExecRequest
	lastReadReq   *workspace.FileReadRequest
	lastWriteReq  *workspace.FileWriteRequest
	lastListReq   *workspace.FileListRequest
	execCallCount int
}

func newTestProvider() *testProvider {
	return &testProvider{
		execResult: &workspace.ExecResult{ExitCode: 0, Stdout: "", Stderr: ""},
		readResult: &workspace.FileReadResult{Content: "hello\n", TotalLines: 1},
		listResult: &workspace.FileListResult{Files: nil},
	}
}

func (p *testProvider) Create(_ context.Context, _ *workspace.CreateContainerRequest) (*workspace.CreateContainerResult, error) {
	return &workspace.CreateContainerResult{ProviderID: "test-123"}, nil
}
func (p *testProvider) Destroy(_ context.Context, _ string) error { return nil }
func (p *testProvider) Stop(_ context.Context, _ string) error    { return nil }
func (p *testProvider) Resume(_ context.Context, _ string) error  { return nil }
func (p *testProvider) Exec(_ context.Context, _ string, req *workspace.ExecRequest) (*workspace.ExecResult, error) {
	p.lastExecReq = req
	p.execCallCount++
	return p.execResult, p.execErr
}
func (p *testProvider) ReadFile(_ context.Context, _ string, req *workspace.FileReadRequest) (*workspace.FileReadResult, error) {
	p.lastReadReq = req
	return p.readResult, p.readErr
}
func (p *testProvider) WriteFile(_ context.Context, _ string, req *workspace.FileWriteRequest) error {
	p.lastWriteReq = req
	return p.writeErr
}
func (p *testProvider) ListFiles(_ context.Context, _ string, req *workspace.FileListRequest) (*workspace.FileListResult, error) {
	p.lastListReq = req
	return p.listResult, p.listErr
}
func (p *testProvider) Health(_ context.Context) (*workspace.HealthStatus, error) {
	return &workspace.HealthStatus{Healthy: true}, nil
}
func (p *testProvider) Snapshot(_ context.Context, _ string) (string, error) {
	return "", workspace.ErrSnapshotNotSupported
}
func (p *testProvider) CreateFromSnapshot(_ context.Context, _ string, _ *workspace.CreateContainerRequest) (*workspace.CreateContainerResult, error) {
	return nil, workspace.ErrSnapshotNotSupported
}
func (p *testProvider) Capabilities() *workspace.ProviderCapabilities {
	return &workspace.ProviderCapabilities{Name: "test", ProviderType: workspace.ProviderGVisor}
}

// Verify interface compliance
var _ workspace.Provider = (*testProvider)(nil)

func testAgentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- parseWorkspaceGrepOutput tests ---

func TestParseWorkspaceGrepOutput_Empty(t *testing.T) {
	result := parseWorkspaceGrepOutput("")
	assert.Nil(t, result)
}

func TestParseWorkspaceGrepOutput_WhitespaceOnly(t *testing.T) {
	result := parseWorkspaceGrepOutput("   \n  \n")
	assert.Nil(t, result)
}

func TestParseWorkspaceGrepOutput_SingleMatch(t *testing.T) {
	result := parseWorkspaceGrepOutput("/workspace/src/main.go:42:func main() {")
	require.Len(t, result, 1)
	assert.Equal(t, "/workspace/src/main.go", result[0]["file_path"])
	assert.Equal(t, 42, result[0]["line_number"])
	assert.Equal(t, "func main() {", result[0]["line"])
}

func TestParseWorkspaceGrepOutput_MultipleMatches(t *testing.T) {
	input := "/workspace/a.go:1:package main\n/workspace/a.go:3:import \"fmt\"\n/workspace/b.go:10:func helper() {}"
	result := parseWorkspaceGrepOutput(input)
	require.Len(t, result, 3)

	assert.Equal(t, "/workspace/a.go", result[0]["file_path"])
	assert.Equal(t, 1, result[0]["line_number"])
	assert.Equal(t, "package main", result[0]["line"])

	assert.Equal(t, "/workspace/a.go", result[1]["file_path"])
	assert.Equal(t, 3, result[1]["line_number"])
	assert.Equal(t, "import \"fmt\"", result[1]["line"])

	assert.Equal(t, "/workspace/b.go", result[2]["file_path"])
	assert.Equal(t, 10, result[2]["line_number"])
	assert.Equal(t, "func helper() {}", result[2]["line"])
}

func TestParseWorkspaceGrepOutput_ColonsInContent(t *testing.T) {
	// Content contains colons — should only split on first two
	result := parseWorkspaceGrepOutput("/workspace/config.yaml:5:database: host:port:5432")
	require.Len(t, result, 1)
	assert.Equal(t, "/workspace/config.yaml", result[0]["file_path"])
	assert.Equal(t, 5, result[0]["line_number"])
	assert.Equal(t, "database: host:port:5432", result[0]["line"])
}

func TestParseWorkspaceGrepOutput_MalformedLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"no colon", "no-colon-here"},
		{"one colon only", "file:content"},
		{"zero line number", "/workspace/file.go:0:zero line"},
		{"non-numeric line number", "/workspace/file.go:abc:content"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseWorkspaceGrepOutput(tt.input)
			assert.Nil(t, result)
		})
	}
}

func TestParseWorkspaceGrepOutput_TrailingNewline(t *testing.T) {
	result := parseWorkspaceGrepOutput("/workspace/a.go:1:test\n")
	require.Len(t, result, 1)
	assert.Equal(t, "test", result[0]["line"])
}

func TestParseWorkspaceGrepOutput_MixedValidAndInvalid(t *testing.T) {
	input := "/workspace/a.go:1:valid\nmalformed line\n/workspace/b.go:2:also valid"
	result := parseWorkspaceGrepOutput(input)
	require.Len(t, result, 2)
	assert.Equal(t, "valid", result[0]["line"])
	assert.Equal(t, "also valid", result[1]["line"])
}

// --- BuildWorkspaceTools tests ---

func TestBuildWorkspaceTools_AllToolsBuilt(t *testing.T) {
	provider := newTestProvider()
	deps := WorkspaceToolDeps{
		Provider:    provider,
		ProviderID:  "test-provider-123",
		WorkspaceID: "ws-uuid-1",
		Config:      nil, // nil config = all tools allowed
		Logger:      testAgentLogger(),
	}

	tools, err := BuildWorkspaceTools(deps)
	require.NoError(t, err)
	require.Len(t, tools, 7)

	// Verify tool names in deterministic order (matches workspace.ValidToolNames)
	expectedNames := []string{
		"workspace_bash",
		"workspace_read",
		"workspace_write",
		"workspace_edit",
		"workspace_glob",
		"workspace_grep",
		"workspace_git",
	}
	for i, tool := range tools {
		assert.Equal(t, expectedNames[i], tool.Name(), "tool %d name mismatch", i)
	}
}

func TestBuildWorkspaceTools_FilteredByConfig(t *testing.T) {
	provider := newTestProvider()
	cfg := &workspace.AgentWorkspaceConfig{
		Enabled: true,
		Tools:   []string{"bash", "read", "write"}, // Only allow 3 tools
	}
	deps := WorkspaceToolDeps{
		Provider:    provider,
		ProviderID:  "test-provider-123",
		WorkspaceID: "ws-uuid-2",
		Config:      cfg,
		Logger:      testAgentLogger(),
	}

	tools, err := BuildWorkspaceTools(deps)
	require.NoError(t, err)
	require.Len(t, tools, 3)

	assert.Equal(t, "workspace_bash", tools[0].Name())
	assert.Equal(t, "workspace_read", tools[1].Name())
	assert.Equal(t, "workspace_write", tools[2].Name())
}

func TestBuildWorkspaceTools_SingleTool(t *testing.T) {
	provider := newTestProvider()
	cfg := &workspace.AgentWorkspaceConfig{
		Enabled: true,
		Tools:   []string{"git"},
	}
	deps := WorkspaceToolDeps{
		Provider:    provider,
		ProviderID:  "test-provider-123",
		WorkspaceID: "ws-uuid-3",
		Config:      cfg,
		Logger:      testAgentLogger(),
	}

	tools, err := BuildWorkspaceTools(deps)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "workspace_git", tools[0].Name())
}

func TestBuildWorkspaceTools_EmptyToolsListAllowsAll(t *testing.T) {
	provider := newTestProvider()
	cfg := &workspace.AgentWorkspaceConfig{
		Enabled: true,
		Tools:   []string{}, // Empty = all allowed
	}
	deps := WorkspaceToolDeps{
		Provider:    provider,
		ProviderID:  "test-provider-123",
		WorkspaceID: "ws-uuid-4",
		Config:      cfg,
		Logger:      testAgentLogger(),
	}

	tools, err := BuildWorkspaceTools(deps)
	require.NoError(t, err)
	assert.Len(t, tools, 7) // All 7 tools
}

func TestBuildWorkspaceTools_NilProviderReturnsNil(t *testing.T) {
	deps := WorkspaceToolDeps{
		Provider:    nil,
		ProviderID:  "test-provider-123",
		WorkspaceID: "ws-uuid-5",
		Logger:      testAgentLogger(),
	}

	tools, err := BuildWorkspaceTools(deps)
	require.NoError(t, err)
	assert.Nil(t, tools)
}

func TestBuildWorkspaceTools_EmptyProviderIDReturnsNil(t *testing.T) {
	provider := newTestProvider()
	deps := WorkspaceToolDeps{
		Provider:    provider,
		ProviderID:  "",
		WorkspaceID: "ws-uuid-6",
		Logger:      testAgentLogger(),
	}

	tools, err := BuildWorkspaceTools(deps)
	require.NoError(t, err)
	assert.Nil(t, tools)
}

func TestBuildWorkspaceTools_ToolDescriptionsNotEmpty(t *testing.T) {
	provider := newTestProvider()
	deps := WorkspaceToolDeps{
		Provider:    provider,
		ProviderID:  "test-provider-123",
		WorkspaceID: "ws-uuid-7",
		Logger:      testAgentLogger(),
	}

	tools, err := BuildWorkspaceTools(deps)
	require.NoError(t, err)

	for _, tool := range tools {
		assert.NotEmpty(t, tool.Description(), "tool %s should have a description", tool.Name())
	}
}

func TestBuildWorkspaceTools_NoneIsLongRunning(t *testing.T) {
	provider := newTestProvider()
	deps := WorkspaceToolDeps{
		Provider:    provider,
		ProviderID:  "test-provider-123",
		WorkspaceID: "ws-uuid-8",
		Logger:      testAgentLogger(),
	}

	tools, err := BuildWorkspaceTools(deps)
	require.NoError(t, err)

	for _, tool := range tools {
		assert.False(t, tool.IsLongRunning(), "tool %s should not be long-running", tool.Name())
	}
}

// --- augmentInstructionWithWorkspace tests ---

func TestAugmentInstructionWithWorkspace_NilResult(t *testing.T) {
	ae := &AgentExecutor{log: testAgentLogger()}
	result := ae.augmentInstructionWithWorkspace("base instruction", nil)
	assert.Equal(t, "base instruction", result)
}

func TestAugmentInstructionWithWorkspace_NilWorkspace(t *testing.T) {
	ae := &AgentExecutor{log: testAgentLogger()}
	result := ae.augmentInstructionWithWorkspace("base instruction", &workspace.ProvisioningResult{
		Workspace: nil,
	})
	assert.Equal(t, "base instruction", result)
}

func TestAugmentInstructionWithWorkspace_BasicWorkspace(t *testing.T) {
	ae := &AgentExecutor{log: testAgentLogger()}
	wsResult := &workspace.ProvisioningResult{
		Workspace: &workspace.AgentWorkspace{
			ID: "ws-test-123",
		},
	}

	result := ae.augmentInstructionWithWorkspace("You are a helpful assistant.", wsResult)

	assert.Contains(t, result, "You are a helpful assistant.")
	assert.Contains(t, result, "Workspace Environment")
	assert.Contains(t, result, "ws-test-123")
	assert.Contains(t, result, "workspace_")
}

func TestAugmentInstructionWithWorkspace_WithRepo(t *testing.T) {
	ae := &AgentExecutor{log: testAgentLogger()}
	wsResult := &workspace.ProvisioningResult{
		Workspace: &workspace.AgentWorkspace{
			ID: "ws-test-456",
		},
		RepoURL: "https://github.com/org/repo",
		Branch:  "feature-branch",
	}

	result := ae.augmentInstructionWithWorkspace("base", wsResult)

	assert.Contains(t, result, "https://github.com/org/repo")
	assert.Contains(t, result, "feature-branch")
	assert.Contains(t, result, "/workspace")
}

func TestAugmentInstructionWithWorkspace_WithRepoNoBranch(t *testing.T) {
	ae := &AgentExecutor{log: testAgentLogger()}
	wsResult := &workspace.ProvisioningResult{
		Workspace: &workspace.AgentWorkspace{
			ID: "ws-test-789",
		},
		RepoURL: "https://github.com/org/repo",
	}

	result := ae.augmentInstructionWithWorkspace("base", wsResult)

	assert.Contains(t, result, "https://github.com/org/repo")
	assert.NotContains(t, result, "Branch:")
}

// --- provisionWorkspace tests ---

func TestProvisionWorkspace_DisabledReturnsNil(t *testing.T) {
	ae := &AgentExecutor{
		wsEnabled:   false,
		provisioner: nil,
		log:         testAgentLogger(),
	}

	result := ae.provisionWorkspace(context.Background(), "run-1", ExecuteRequest{})
	assert.Nil(t, result)
}

func TestProvisionWorkspace_NilProvisionerReturnsNil(t *testing.T) {
	ae := &AgentExecutor{
		wsEnabled:   true,
		provisioner: nil,
		log:         testAgentLogger(),
	}

	result := ae.provisionWorkspace(context.Background(), "run-1", ExecuteRequest{})
	assert.Nil(t, result)
}

func TestProvisionWorkspace_NilDefinitionReturnsNil(t *testing.T) {
	ae := &AgentExecutor{
		wsEnabled:   true,
		provisioner: &workspace.AutoProvisioner{}, // non-nil but won't be called
		log:         testAgentLogger(),
	}

	result := ae.provisionWorkspace(context.Background(), "run-1", ExecuteRequest{
		AgentDefinition: nil,
	})
	assert.Nil(t, result)
}

func TestProvisionWorkspace_EmptyWorkspaceConfigReturnsNil(t *testing.T) {
	ae := &AgentExecutor{
		wsEnabled:   true,
		provisioner: &workspace.AutoProvisioner{}, // non-nil but won't be called
		log:         testAgentLogger(),
	}

	result := ae.provisionWorkspace(context.Background(), "run-1", ExecuteRequest{
		AgentDefinition: &AgentDefinition{
			WorkspaceConfig: nil, // no workspace config
		},
	})
	assert.Nil(t, result)
}

func TestProvisionWorkspace_EmptyMapWorkspaceConfigReturnsNil(t *testing.T) {
	ae := &AgentExecutor{
		wsEnabled:   true,
		provisioner: &workspace.AutoProvisioner{}, // non-nil but won't be called
		log:         testAgentLogger(),
	}

	result := ae.provisionWorkspace(context.Background(), "run-1", ExecuteRequest{
		AgentDefinition: &AgentDefinition{
			WorkspaceConfig: map[string]any{}, // empty map
		},
	})
	assert.Nil(t, result)
}

// --- teardownWorkspace tests ---

func TestTeardownWorkspace_NilResult(t *testing.T) {
	ae := &AgentExecutor{
		provisioner: nil,
		log:         testAgentLogger(),
	}
	// Should not panic
	ae.teardownWorkspace(context.Background(), nil)
}

func TestTeardownWorkspace_NilWorkspace(t *testing.T) {
	ae := &AgentExecutor{
		provisioner: nil,
		log:         testAgentLogger(),
	}
	// Should not panic
	ae.teardownWorkspace(context.Background(), &workspace.ProvisioningResult{Workspace: nil})
}

func TestTeardownWorkspace_NilProvisioner(t *testing.T) {
	ae := &AgentExecutor{
		provisioner: nil,
		log:         testAgentLogger(),
	}
	// Should not panic
	ae.teardownWorkspace(context.Background(), &workspace.ProvisioningResult{
		Workspace: &workspace.AgentWorkspace{ID: "ws-1"},
	})
}

// --- resolveWorkspaceTools tests ---

func TestResolveWorkspaceTools_NilProvisioner(t *testing.T) {
	ae := &AgentExecutor{
		provisioner: nil,
		log:         testAgentLogger(),
	}

	tools, err := ae.resolveWorkspaceTools(nil, ExecuteRequest{})
	assert.NoError(t, err)
	assert.Nil(t, tools)
}

func TestResolveWorkspaceTools_NilResult(t *testing.T) {
	ae := &AgentExecutor{
		provisioner: &workspace.AutoProvisioner{},
		log:         testAgentLogger(),
	}

	tools, err := ae.resolveWorkspaceTools(nil, ExecuteRequest{})
	assert.NoError(t, err)
	assert.Nil(t, tools)
}

func TestResolveWorkspaceTools_NilWorkspaceInResult(t *testing.T) {
	ae := &AgentExecutor{
		provisioner: &workspace.AutoProvisioner{},
		log:         testAgentLogger(),
	}

	tools, err := ae.resolveWorkspaceTools(&workspace.ProvisioningResult{Workspace: nil}, ExecuteRequest{})
	assert.NoError(t, err)
	assert.Nil(t, tools)
}

// --- sanitizeAgentName tests ---

func TestSanitizeAgentName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "my_agent", "my_agent"},
		{"with spaces", "my agent name", "my_agent_name"},
		{"with special chars", "agent@v2.0!", "agent_v2_0_"},
		{"alphanumeric only", "agent123", "agent123"},
		{"empty", "", "agent"},
		{"all special", "!@#$%", "_____"},
		{"hyphens preserved", "my-agent-v2", "my-agent-v2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeAgentName(tt.input))
		})
	}
}

// --- stepTracker tests ---

func TestStepTracker_Basic(t *testing.T) {
	tracker := newStepTracker(10, 0)
	assert.Equal(t, 0, tracker.current())
	assert.False(t, tracker.exceeded())

	step := tracker.increment()
	assert.Equal(t, 1, step)
	assert.Equal(t, 1, tracker.current())
	assert.False(t, tracker.exceeded())
}

func TestStepTracker_ExceedsMax(t *testing.T) {
	tracker := newStepTracker(3, 0)
	tracker.increment() // 1
	tracker.increment() // 2
	tracker.increment() // 3

	assert.True(t, tracker.exceeded())
}

func TestStepTracker_InitialSteps(t *testing.T) {
	tracker := newStepTracker(10, 5)
	assert.Equal(t, 5, tracker.current())

	tracker.increment() // 6
	assert.Equal(t, 6, tracker.current())
}

func TestStepTracker_InitialStepsAtMax(t *testing.T) {
	tracker := newStepTracker(5, 5)
	assert.True(t, tracker.exceeded())
}

// --- doomLoopDetector tests ---

func TestDoomLoopDetector_NoLoop(t *testing.T) {
	d := newDoomLoopDetector(testAgentLogger())

	action := d.recordCall("tool_a", map[string]any{"key": "val1"})
	assert.Equal(t, doomActionNone, action)

	action = d.recordCall("tool_b", map[string]any{"key": "val2"})
	assert.Equal(t, doomActionNone, action)

	action = d.recordCall("tool_a", map[string]any{"key": "val3"})
	assert.Equal(t, doomActionNone, action)
}

func TestDoomLoopDetector_SameToolDifferentArgs(t *testing.T) {
	d := newDoomLoopDetector(testAgentLogger())

	for i := 0; i < 10; i++ {
		action := d.recordCall("tool_a", map[string]any{"key": fmt.Sprintf("val%d", i)})
		assert.Equal(t, doomActionNone, action, "iteration %d", i)
	}
}

func TestDoomLoopDetector_WarnThreshold(t *testing.T) {
	d := newDoomLoopDetector(testAgentLogger())
	args := map[string]any{"key": "same"}

	for i := 0; i < doomWarnThreshold-1; i++ {
		d.recordCall("tool_a", args)
	}

	action := d.recordCall("tool_a", args)
	assert.Equal(t, doomActionWarn, action)
}

func TestDoomLoopDetector_StopThreshold(t *testing.T) {
	d := newDoomLoopDetector(testAgentLogger())
	args := map[string]any{"key": "same"}

	for i := 0; i < doomStopThreshold-1; i++ {
		d.recordCall("tool_a", args)
	}

	action := d.recordCall("tool_a", args)
	assert.Equal(t, doomActionStop, action)
}

func TestDoomLoopDetector_ResetOnDifferentCall(t *testing.T) {
	d := newDoomLoopDetector(testAgentLogger())
	args := map[string]any{"key": "same"}

	// Build up to warning
	for i := 0; i < doomWarnThreshold-1; i++ {
		d.recordCall("tool_a", args)
	}

	// Different tool resets counter
	action := d.recordCall("tool_b", args)
	assert.Equal(t, doomActionNone, action)

	// Start counting again for tool_b
	action = d.recordCall("tool_b", args)
	assert.Equal(t, doomActionNone, action)
	assert.Equal(t, 2, d.consecutiveCount)
}
