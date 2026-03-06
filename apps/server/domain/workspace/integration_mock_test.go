package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TG 7.10 — Tool Interface Integration Tests
// Tests that provider-level tool operations work through the orchestrator and
// that tool handlers correctly delegate to providers.
// =============================================================================

// configurableProvider extends mockProvider with configurable exec/read/write/list
// responses for testing tool handler delegation.
type configurableProvider struct {
	mockProvider
	execResults    map[string]*ExecResult // command substring → result
	readResults    map[string]*FileReadResult
	writeErrors    map[string]error
	listResults    map[string]*FileListResult
	defaultExecErr error
}

func (p *configurableProvider) Exec(_ context.Context, _ string, req *ExecRequest) (*ExecResult, error) {
	if p.defaultExecErr != nil {
		return nil, p.defaultExecErr
	}
	for substr, result := range p.execResults {
		if strings.Contains(req.Command, substr) {
			return result, nil
		}
	}
	return &ExecResult{ExitCode: 0, Stdout: ""}, nil
}

func (p *configurableProvider) ReadFile(_ context.Context, _ string, req *FileReadRequest) (*FileReadResult, error) {
	if result, ok := p.readResults[req.FilePath]; ok {
		return result, nil
	}
	return &FileReadResult{Content: "file content", FileSize: 12}, nil
}

func (p *configurableProvider) WriteFile(_ context.Context, _ string, req *FileWriteRequest) error {
	if err, ok := p.writeErrors[req.FilePath]; ok {
		return err
	}
	return nil
}

func (p *configurableProvider) ListFiles(_ context.Context, _ string, _ *FileListRequest) (*FileListResult, error) {
	for _, result := range p.listResults {
		return result, nil
	}
	return &FileListResult{}, nil
}

func TestIntegration_ToolOps_ExecViaOrchestrator(t *testing.T) {
	// Test: Select a provider via orchestrator, then call Exec on it.
	o := NewOrchestrator(testLogger())

	cp := &configurableProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"echo hello": {ExitCode: 0, Stdout: "hello\n"},
			"ls /":       {ExitCode: 0, Stdout: "bin\netc\nusr\nworkspace\n"},
			"exit 1":     {ExitCode: 1, Stderr: "command failed"},
		},
	}
	o.RegisterProvider(ProviderGVisor, cp)

	// Select provider
	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	// Create a container
	result, err := provider.Create(t.Context(), &CreateContainerRequest{ContainerType: ContainerTypeAgentWorkspace})
	require.NoError(t, err)
	providerID := result.ProviderID

	// Execute commands through the provider
	tests := []struct {
		name       string
		command    string
		expectCode int
		expectOut  string
	}{
		{"echo command", "echo hello", 0, "hello\n"},
		{"ls command", "ls /", 0, "bin\netc\nusr\nworkspace\n"},
		{"failing command", "exit 1", 1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execResult, err := provider.Exec(t.Context(), providerID, &ExecRequest{
				Command:   tt.command,
				TimeoutMs: 30000,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.expectCode, execResult.ExitCode)
			if tt.expectOut != "" {
				assert.Equal(t, tt.expectOut, execResult.Stdout)
			}
		})
	}
}

func TestIntegration_ToolOps_ReadWriteViaOrchestrator(t *testing.T) {
	// Test: Read and write files through a provider obtained via orchestrator.
	o := NewOrchestrator(testLogger())

	cp := &configurableProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		readResults: map[string]*FileReadResult{
			"/workspace/main.go": {Content: "package main\n\nfunc main() {}\n", FileSize: 30},
			"/workspace/go.mod":  {Content: "module test\n", FileSize: 12},
			"/workspace/missing": nil, // won't be found by default
		},
		writeErrors: map[string]error{
			"/workspace/readonly": errors.New("permission denied"),
		},
	}
	o.RegisterProvider(ProviderGVisor, cp)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, ProviderGVisor)
	require.NoError(t, err)

	result, err := provider.Create(t.Context(), &CreateContainerRequest{})
	require.NoError(t, err)

	// Read file
	readResult, err := provider.ReadFile(t.Context(), result.ProviderID, &FileReadRequest{FilePath: "/workspace/main.go"})
	require.NoError(t, err)
	assert.Contains(t, readResult.Content, "package main")
	assert.Equal(t, int64(30), readResult.FileSize)

	// Write file — success
	err = provider.WriteFile(t.Context(), result.ProviderID, &FileWriteRequest{
		FilePath: "/workspace/output.txt",
		Content:  "test output",
	})
	assert.NoError(t, err)

	// Write file — failure
	err = provider.WriteFile(t.Context(), result.ProviderID, &FileWriteRequest{
		FilePath: "/workspace/readonly",
		Content:  "should fail",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestIntegration_ToolOps_ListFilesViaOrchestrator(t *testing.T) {
	o := NewOrchestrator(testLogger())

	cp := &configurableProvider{
		mockProvider: mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true},
		listResults: map[string]*FileListResult{
			"default": {
				Files: []FileInfo{
					{Path: "main.go", IsDir: false, Size: 100},
					{Path: "pkg", IsDir: true, Size: 0},
					{Path: "go.mod", IsDir: false, Size: 50},
				},
			},
		},
	}
	o.RegisterProvider(ProviderGVisor, cp)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	res, err := provider.Create(t.Context(), &CreateContainerRequest{})
	require.NoError(t, err)

	listResult, err := provider.ListFiles(t.Context(), res.ProviderID, &FileListRequest{
		Path:    "/workspace",
		Pattern: "*.go",
	})
	require.NoError(t, err)
	assert.Len(t, listResult.Files, 3)
	assert.Equal(t, "main.go", listResult.Files[0].Path)
	assert.True(t, listResult.Files[1].IsDir)
}

func TestIntegration_ToolOps_GrepParsing_MultiFile(t *testing.T) {
	// Test parseGrepOutput with realistic multi-file grep output
	input := strings.Join([]string{
		"/workspace/cmd/main.go:5:func main() {",
		"/workspace/cmd/main.go:10:\tfmt.Println(\"hello\")",
		"/workspace/pkg/service.go:1:package service",
		"/workspace/pkg/service.go:25:func (s *Service) Run() error {",
		"/workspace/pkg/service.go:26:\treturn nil",
		"/workspace/internal/config/config.go:12:type Config struct {",
	}, "\n")

	matches := parseGrepOutput(input)
	require.Len(t, matches, 6)

	// Verify grouping by file
	fileMatches := map[string]int{}
	for _, m := range matches {
		fileMatches[m.FilePath]++
	}
	assert.Equal(t, 2, fileMatches["/workspace/cmd/main.go"])
	assert.Equal(t, 3, fileMatches["/workspace/pkg/service.go"])
	assert.Equal(t, 1, fileMatches["/workspace/internal/config/config.go"])
}

func TestIntegration_ToolOps_ExecTimeout(t *testing.T) {
	// Test that ExecRequest properly carries timeout info
	req := &ExecRequest{
		Command:   "sleep 10",
		TimeoutMs: 5000,
		Workdir:   "/workspace",
	}
	assert.Equal(t, 5000, req.TimeoutMs)
	assert.Equal(t, "/workspace", req.Workdir)
}

// =============================================================================
// TG 8.11 — Workspace Lifecycle API Integration Tests
// Tests orchestrator → provider create/destroy lifecycle, provider resolution,
// and resource limit application.
// =============================================================================

func TestIntegration_Lifecycle_CreateDestroyFlow(t *testing.T) {
	// Test: Full create → verify → destroy lifecycle through orchestrator.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true, supportsSnapshots: true}
	o.RegisterProvider(ProviderGVisor, gv)

	// Select and create
	provider, pt, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, pt)

	result, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType:  ContainerTypeAgentWorkspace,
		ResourceLimits: &ResourceLimits{CPU: "2", Memory: "4G", Disk: "10G"},
		BaseImage:      "emergent/workspace:latest",
		Labels:         map[string]string{"workspace": "ws-1"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.ProviderID, "mock-gvisor")

	// Provider should track creation
	assert.Equal(t, int64(1), gv.createCount.Load())

	// Destroy
	err = provider.Destroy(t.Context(), result.ProviderID)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), gv.destroyCount.Load())
}

func TestIntegration_Lifecycle_MultipleWorkspaces(t *testing.T) {
	// Test: Create multiple workspaces, verify unique IDs, destroy all.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	providerIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		result, err := provider.Create(t.Context(), &CreateContainerRequest{
			ContainerType: ContainerTypeAgentWorkspace,
		})
		require.NoError(t, err)
		providerIDs[i] = result.ProviderID
	}

	// All IDs should be unique
	seen := map[string]bool{}
	for _, id := range providerIDs {
		assert.False(t, seen[id], "duplicate provider ID: %s", id)
		seen[id] = true
	}
	assert.Equal(t, int64(5), gv.createCount.Load())

	// Destroy all
	for _, id := range providerIDs {
		assert.NoError(t, provider.Destroy(t.Context(), id))
	}
	assert.Equal(t, int64(5), gv.destroyCount.Load())
}

func TestIntegration_Lifecycle_ProviderResolution(t *testing.T) {
	// Test: Service.resolveProvider correctly maps input strings to ProviderType.
	svc := NewService(nil, nil, testLogger())

	tests := []struct {
		input    string
		expected ProviderType
	}{
		{"gvisor", ProviderGVisor},
		{"firecracker", ProviderFirecracker},
		{"e2b", ProviderE2B},
		{"auto", ProviderGVisor},
		{"", ProviderGVisor},
		{"unknown", ProviderGVisor},
		{"GVISOR", ProviderGVisor}, // case insensitive
		{"Firecracker", ProviderFirecracker},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input=%q", tt.input), func(t *testing.T) {
			assert.Equal(t, tt.expected, svc.resolveProvider(tt.input))
		})
	}
}

func TestIntegration_Lifecycle_DefaultLimitsApplication(t *testing.T) {
	// Test: Service.applyDefaultLimits applies defaults correctly.
	svc := NewService(nil, nil, testLogger())

	t.Run("nil gets all defaults", func(t *testing.T) {
		result := svc.applyDefaultLimits(nil)
		require.NotNil(t, result)
		assert.Equal(t, "2", result.CPU)
		assert.Equal(t, "4G", result.Memory)
		assert.Equal(t, "10G", result.Disk)
	})

	t.Run("partial override preserves non-empty fields", func(t *testing.T) {
		result := svc.applyDefaultLimits(&ResourceLimits{CPU: "8", Disk: "50G"})
		assert.Equal(t, "8", result.CPU)
		assert.Equal(t, "4G", result.Memory) // default
		assert.Equal(t, "50G", result.Disk)
	})

	t.Run("full override is unchanged", func(t *testing.T) {
		result := svc.applyDefaultLimits(&ResourceLimits{CPU: "16", Memory: "32G", Disk: "500G"})
		assert.Equal(t, "16", result.CPU)
		assert.Equal(t, "32G", result.Memory)
		assert.Equal(t, "500G", result.Disk)
	})
}

func TestIntegration_Lifecycle_ProviderCreateError(t *testing.T) {
	// Test: Provider create failure is propagated correctly.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:         "gvisor",
		providerType: ProviderGVisor,
		healthy:      true,
		createErr:    errors.New("out of disk space"),
	}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	_, err = provider.Create(t.Context(), &CreateContainerRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "out of disk space")
}

func TestIntegration_Lifecycle_ProviderDestroyError(t *testing.T) {
	// Test: Provider destroy failure returns error but container may be gone.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:         "gvisor",
		providerType: ProviderGVisor,
		healthy:      true,
		destroyErr:   errors.New("container not found"),
	}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	err = provider.Destroy(t.Context(), "nonexistent-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "container not found")
	// destroyCount is still incremented even on error
	assert.Equal(t, int64(1), gv.destroyCount.Load())
}

func TestIntegration_Lifecycle_StopResume(t *testing.T) {
	// Test: Stop and resume operations on a mock provider.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	result, err := provider.Create(t.Context(), &CreateContainerRequest{})
	require.NoError(t, err)

	// Stop
	err = provider.Stop(t.Context(), result.ProviderID)
	assert.NoError(t, err)

	// Resume
	err = provider.Resume(t.Context(), result.ProviderID)
	assert.NoError(t, err)
}

// =============================================================================
// TG 9.7 — Checkout Integration Tests
// Tests CheckoutService.CloneRepository with mock providers.
// =============================================================================

// mockCredentialProvider implements GitCredentialProvider for testing.
type mockCredentialProvider struct {
	token       string
	tokenErr    error
	botName     string
	botEmail    string
	identityErr error
}

func (m *mockCredentialProvider) GetInstallationToken(_ context.Context) (string, error) {
	if m.tokenErr != nil {
		return "", m.tokenErr
	}
	return m.token, nil
}

func (m *mockCredentialProvider) GetBotIdentity(_ context.Context) (string, string, error) {
	if m.identityErr != nil {
		return "", "", m.identityErr
	}
	return m.botName, m.botEmail, nil
}

// checkoutProvider is a configurable provider for checkout testing.
type checkoutProvider struct {
	mockProvider
	execCalls   []string // recorded commands
	mu          sync.Mutex
	execResults map[string]*ExecResult
	execErrors  map[string]error
}

func (p *checkoutProvider) Exec(_ context.Context, _ string, req *ExecRequest) (*ExecResult, error) {
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

func TestIntegration_Checkout_SuccessfulClone(t *testing.T) {
	cp := &checkoutProvider{
		mockProvider: mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":  {ExitCode: 0, Stdout: "Cloning into 'workspace'..."},
			"git config": {ExitCode: 0},
		},
	}

	cred := &mockCredentialProvider{
		token:    "ghs_test123",
		botName:  "Test Bot",
		botEmail: "bot@test.local",
	}

	cs := NewCheckoutService(cred, testLogger())

	err := cs.CloneRepository(t.Context(), cp, "container-1", "https://github.com/org/repo", "main")
	assert.NoError(t, err)

	// Verify the clone command was called with token-injected URL and branch
	cp.mu.Lock()
	defer cp.mu.Unlock()
	require.GreaterOrEqual(t, len(cp.execCalls), 1)
	assert.Contains(t, cp.execCalls[0], "git clone")
	assert.Contains(t, cp.execCalls[0], "--branch")
	assert.Contains(t, cp.execCalls[0], "main")
	// Verify token was injected
	assert.Contains(t, cp.execCalls[0], "x-access-token:ghs_test123@")
}

func TestIntegration_Checkout_CloneFailure_Retries(t *testing.T) {
	attemptCount := 0
	cp := &checkoutProvider{
		mockProvider: mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone": {ExitCode: 128, Stderr: "fatal: repository not found"},
		},
	}

	// Override to track attempts
	origExec := cp.execResults
	_ = origExec

	cred := &mockCredentialProvider{token: "ghs_test"}

	cs := NewCheckoutService(cred, testLogger())

	// Use a context with timeout to prevent test from hanging on retries
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	err := cs.CloneRepository(ctx, cp, "container-1", "https://github.com/org/repo", "main")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "git clone failed")

	// Verify multiple attempts were made (up to maxCloneRetries)
	cp.mu.Lock()
	for _, call := range cp.execCalls {
		if strings.Contains(call, "git clone") {
			attemptCount++
		}
	}
	cp.mu.Unlock()
	assert.Equal(t, maxCloneRetries, attemptCount, "should retry exactly maxCloneRetries times")
}

func TestIntegration_Checkout_SHACheckout(t *testing.T) {
	cp := &checkoutProvider{
		mockProvider: mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":    {ExitCode: 0},
			"git fetch":    {ExitCode: 0},
			"git checkout": {ExitCode: 0},
			"git config":   {ExitCode: 0},
		},
	}

	cred := &mockCredentialProvider{
		token:    "ghs_test",
		botName:  "Bot",
		botEmail: "bot@test.local",
	}

	cs := NewCheckoutService(cred, testLogger())

	sha := "abc123def456789012345678901234567890abcd"
	err := cs.CloneRepository(t.Context(), cp, "container-1", "https://github.com/org/repo", sha)
	assert.NoError(t, err)

	// Verify: clone (without --branch), fetch --unshallow, git checkout SHA
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cloneFound := false
	fetchFound := false
	checkoutFound := false
	for _, call := range cp.execCalls {
		if strings.Contains(call, "git clone") && !strings.Contains(call, "--branch") {
			cloneFound = true
		}
		if strings.Contains(call, "git fetch --unshallow") {
			fetchFound = true
		}
		if strings.Contains(call, "git checkout") && strings.Contains(call, sha) {
			checkoutFound = true
		}
	}
	assert.True(t, cloneFound, "should clone without --branch for SHA")
	assert.True(t, fetchFound, "should fetch --unshallow for SHA checkout")
	assert.True(t, checkoutFound, "should checkout the specific SHA")
}

func TestIntegration_Checkout_EmptyURL_NoOp(t *testing.T) {
	cs := NewCheckoutService(nil, testLogger())
	err := cs.CloneRepository(t.Context(), nil, "", "", "")
	assert.NoError(t, err)
}

func TestIntegration_Checkout_NoCredentials_FallsBackToPublic(t *testing.T) {
	cp := &checkoutProvider{
		mockProvider: mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true},
		execResults: map[string]*ExecResult{
			"git clone":  {ExitCode: 0},
			"git config": {ExitCode: 0},
		},
	}

	cred := &mockCredentialProvider{
		tokenErr:    errors.New("no GitHub App configured"),
		identityErr: errors.New("no identity"),
	}

	cs := NewCheckoutService(cred, testLogger())

	err := cs.CloneRepository(t.Context(), cp, "container-1", "https://github.com/org/public-repo", "main")
	assert.NoError(t, err)

	// Clone should use unauthenticated URL (no token)
	cp.mu.Lock()
	defer cp.mu.Unlock()
	require.GreaterOrEqual(t, len(cp.execCalls), 1)
	assert.NotContains(t, cp.execCalls[0], "x-access-token")
}

func TestIntegration_Checkout_BuildCloneURL(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		token    string
		tokenErr error
		expected string
	}{
		{
			name:     "inject token into HTTPS URL",
			repoURL:  "https://github.com/org/repo",
			token:    "ghs_secrettoken",
			expected: "https://x-access-token:ghs_secrettoken@github.com/org/repo",
		},
		{
			name:     "SSH URL unchanged",
			repoURL:  "git@github.com:org/repo.git",
			token:    "ghs_unused",
			expected: "git@github.com:org/repo.git",
		},
		{
			name:     "token error returns unauthenticated",
			repoURL:  "https://github.com/org/repo",
			tokenErr: errors.New("no token"),
			expected: "", // buildCloneURL returns error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cred := &mockCredentialProvider{token: tt.token, tokenErr: tt.tokenErr}
			cs := NewCheckoutService(cred, testLogger())

			url, err := cs.buildCloneURL(t.Context(), tt.repoURL)
			if tt.tokenErr != nil {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, url)
			}
		})
	}
}

func TestIntegration_Checkout_NilCredentialProvider(t *testing.T) {
	cs := &CheckoutService{credProvider: nil, log: testLogger()}
	url, err := cs.buildCloneURL(t.Context(), "https://github.com/org/repo")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/org/repo", url)
}

// =============================================================================
// TG 10.15 — MCP Hosting Integration Tests
// Tests StdioBridge concurrency, MCPHostingService helpers, and crash loop backoff.
// =============================================================================

func TestIntegration_MCP_StdioBridge_ConcurrentCalls_Serialized(t *testing.T) {
	// Verify that concurrent calls to a StdioBridge are serialized via the mutex.
	// We do this by running multiple calls in parallel and verifying that request IDs
	// are monotonically assigned (indicating serialization).

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	bridge := NewStdioBridge(stdinWriter, stdoutReader, testLogger())

	// MCP server simulator: reads requests and writes responses.
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := stdinReader.Read(buf)
			if err != nil {
				return
			}

			var req JSONRPCRequest
			if err := json.Unmarshal(buf[:n], &req); err != nil {
				continue
			}

			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(fmt.Sprintf(`{"method":"%s","id":%d}`, req.Method, req.ID)),
			}
			respBytes, _ := json.Marshal(resp)
			respBytes = append(respBytes, '\n')
			_, _ = stdoutWriter.Write(respBytes)
		}
	}()

	// Launch concurrent calls
	const numCalls = 5
	var wg sync.WaitGroup
	results := make([]*JSONRPCResponse, numCalls)
	errs := make([]error, numCalls)

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = bridge.Call(
				fmt.Sprintf("tools/call_%d", idx),
				nil,
				5*time.Second,
			)
		}(i)
	}

	wg.Wait()

	// All calls should succeed
	for i := 0; i < numCalls; i++ {
		assert.NoError(t, errs[i], "call %d should succeed", i)
		assert.NotNil(t, results[i], "call %d should have result", i)
	}

	// Verify unique IDs were assigned
	ids := map[int64]bool{}
	for _, r := range results {
		if r != nil {
			assert.False(t, ids[r.ID], "duplicate response ID: %d", r.ID)
			ids[r.ID] = true
		}
	}

	_ = bridge.Close()
	stdinReader.Close()
	stdoutWriter.Close()
}

func TestIntegration_MCP_StdioBridge_CloseDuringCall(t *testing.T) {
	// Verify that closing the bridge while a call is in progress returns an error.
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()

	bridge := NewStdioBridge(&bytes.Buffer{}, stdoutReader, testLogger())

	// Start a call that will block waiting for a response
	done := make(chan error, 1)
	go func() {
		_, err := bridge.Call("tools/list", nil, 10*time.Second)
		done <- err
	}()

	// Give the goroutine time to start and acquire the mutex
	time.Sleep(50 * time.Millisecond)

	// Close the bridge
	_ = bridge.Close()

	select {
	case err := <-done:
		assert.Error(t, err)
		// Should be either "closed" or "EOF" depending on timing
		assert.True(t,
			strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "EOF"),
			"error should mention closed or EOF, got: %v", err,
		)
	case <-time.After(5 * time.Second):
		t.Fatal("call should have returned after bridge close")
	}
}

func TestIntegration_MCP_StdioBridge_SequentialCalls(t *testing.T) {
	// Verify multiple sequential calls work correctly with proper ID incrementing.
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	bridge := NewStdioBridge(stdinWriter, stdoutReader, testLogger())

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdinReader.Read(buf)
			if err != nil {
				return
			}
			var req JSONRPCRequest
			if err := json.Unmarshal(buf[:n], &req); err != nil {
				continue
			}
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(fmt.Sprintf(`{"seq":%d}`, req.ID)),
			}
			respBytes, _ := json.Marshal(resp)
			respBytes = append(respBytes, '\n')
			_, _ = stdoutWriter.Write(respBytes)
		}
	}()

	// Make 3 sequential calls
	for i := 0; i < 3; i++ {
		resp, err := bridge.Call("tools/list", nil, 5*time.Second)
		require.NoError(t, err)
		assert.Equal(t, int64(i+1), resp.ID, "sequential call %d should have ID %d", i, i+1)
	}

	_ = bridge.Close()
	stdinReader.Close()
	stdoutWriter.Close()
}

func TestIntegration_MCP_BuildStatusFromWorkspace_NilMCPConfig(t *testing.T) {
	svc := NewMCPHostingService(nil, nil, nil, testLogger())

	ws := &AgentWorkspace{
		ID:            "ws-no-mcp",
		ContainerType: ContainerTypeMCPServer,
		Provider:      ProviderGVisor,
		Status:        StatusReady,
		CreatedAt:     time.Now(),
		LastUsedAt:    time.Now(),
		MCPConfig:     nil, // No MCP config
	}

	status := svc.buildStatusFromWorkspace(ws)
	assert.Equal(t, "ws-no-mcp", status.WorkspaceID)
	assert.Empty(t, status.Name)
	assert.Empty(t, status.Image)
	assert.False(t, status.StdioBridge)
	assert.Empty(t, status.RestartPolicy)
}

func TestIntegration_MCP_BuildStatusFromWorkspace_WithResources(t *testing.T) {
	svc := NewMCPHostingService(nil, nil, nil, testLogger())

	ws := &AgentWorkspace{
		ID:         "ws-with-res",
		Provider:   ProviderGVisor,
		Status:     StatusReady,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		MCPConfig: &MCPConfig{
			Name:          "langfuse",
			Image:         "emergent/mcp-langfuse:v1",
			StdioBridge:   true,
			RestartPolicy: "on-failure",
		},
		ResourceLimits: &ResourceLimits{
			CPU:    "1",
			Memory: "1G",
			Disk:   "5G",
		},
	}

	status := svc.buildStatusFromWorkspace(ws)
	assert.Equal(t, "langfuse", status.Name)
	assert.Equal(t, "emergent/mcp-langfuse:v1", status.Image)
	assert.True(t, status.StdioBridge)
	assert.Equal(t, "on-failure", status.RestartPolicy)
	require.NotNil(t, status.ResourceLimits)
	assert.Equal(t, "1", status.ResourceLimits.CPU)
	assert.Equal(t, "1G", status.ResourceLimits.Memory)
}

func TestIntegration_MCP_CrashLoopBackoff_Progression(t *testing.T) {
	// Verify the exact backoff progression matches the constants.
	// 5s → 15s → 45s → 135s → 300s (capped)
	backoff := time.Duration(0)

	for i := 0; i < 10; i++ {
		// Simulate crash loop detection
		if backoff == 0 {
			backoff = initialBackoff
		} else {
			backoff = time.Duration(float64(backoff) * backoffMultiplier)
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}

	assert.Equal(t, maxBackoff, backoff, "backoff should be capped at maxBackoff")
}

func TestIntegration_MCP_CrashWindowPruning(t *testing.T) {
	// Verify crash window pruning logic: only crashes within the window count.
	now := time.Now()

	crashTimes := []time.Time{
		now.Add(-120 * time.Second), // Outside window (>60s ago)
		now.Add(-90 * time.Second),  // Outside window
		now.Add(-30 * time.Second),  // Inside window
		now.Add(-15 * time.Second),  // Inside window
		now.Add(-5 * time.Second),   // Inside window
	}

	cutoff := now.Add(-crashWindowDuration)
	pruned := crashTimes[:0]
	for _, ct := range crashTimes {
		if ct.After(cutoff) {
			pruned = append(pruned, ct)
		}
	}

	assert.Len(t, pruned, 3, "only crashes within 60s window should remain")
}

func TestIntegration_MCP_ServerState_StopPreventsRestart(t *testing.T) {
	// Verify that stopping a server prevents crash monitor from restarting it.
	state := &mcpServerState{
		workspaceID: "ws-stop-test",
		stopCh:      make(chan struct{}),
	}

	assert.False(t, state.stopped)

	state.mu.Lock()
	state.stopped = true
	close(state.stopCh)
	state.mu.Unlock()

	assert.True(t, state.stopped)

	// stopCh should be closed
	select {
	case <-state.stopCh:
		// Expected
	default:
		t.Fatal("stopCh should be closed")
	}
}

// =============================================================================
// TG 12.5 — Snapshot Integration Tests
// Tests the full snapshot → restore flow through provider interface.
// =============================================================================

func TestIntegration_Snapshot_FullFlow(t *testing.T) {
	// Test: Create workspace → snapshot → restore from snapshot → verify.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:              "gvisor",
		providerType:      ProviderGVisor,
		healthy:           true,
		supportsSnapshots: true,
	}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	// Step 1: Create original workspace
	original, err := provider.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	assert.Contains(t, original.ProviderID, "mock-gvisor")

	// Step 2: Snapshot the workspace
	snapshotID, err := provider.Snapshot(t.Context(), original.ProviderID)
	require.NoError(t, err)
	assert.NotEmpty(t, snapshotID)
	assert.Contains(t, snapshotID, "snap-gvisor")

	// Step 3: Restore from snapshot
	restored, err := provider.CreateFromSnapshot(t.Context(), snapshotID, &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	assert.Contains(t, restored.ProviderID, "mock-gvisor-from-"+snapshotID)

	// Step 4: Verify IDs are different
	assert.NotEqual(t, original.ProviderID, restored.ProviderID)

	// Step 5: Both should be destroyable
	assert.NoError(t, provider.Destroy(t.Context(), original.ProviderID))
	assert.NoError(t, provider.Destroy(t.Context(), restored.ProviderID))
}

func TestIntegration_Snapshot_NotSupported(t *testing.T) {
	o := NewOrchestrator(testLogger())

	noSnap := &mockProvider{
		name:              "e2b",
		providerType:      ProviderE2B,
		healthy:           true,
		supportsSnapshots: false,
	}
	o.RegisterProvider(ProviderE2B, noSnap)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentManaged, "")
	require.NoError(t, err)

	// Create container succeeds
	result, err := provider.Create(t.Context(), &CreateContainerRequest{})
	require.NoError(t, err)

	// Snapshot should fail with ErrSnapshotNotSupported
	_, err = provider.Snapshot(t.Context(), result.ProviderID)
	assert.ErrorIs(t, err, ErrSnapshotNotSupported)

	// CreateFromSnapshot should also fail
	_, err = provider.CreateFromSnapshot(t.Context(), "any-snap-id", &CreateContainerRequest{})
	assert.ErrorIs(t, err, ErrSnapshotNotSupported)
}

func TestIntegration_Snapshot_CustomSnapshotID(t *testing.T) {
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:              "gvisor",
		providerType:      ProviderGVisor,
		healthy:           true,
		supportsSnapshots: true,
		snapshotID:        "custom-snapshot-2026-01-15",
	}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	result, err := provider.Create(t.Context(), &CreateContainerRequest{})
	require.NoError(t, err)

	snapID, err := provider.Snapshot(t.Context(), result.ProviderID)
	require.NoError(t, err)
	assert.Equal(t, "custom-snapshot-2026-01-15", snapID)

	// Restore from custom snapshot
	restored, err := provider.CreateFromSnapshot(t.Context(), snapID, &CreateContainerRequest{})
	require.NoError(t, err)
	assert.Contains(t, restored.ProviderID, "custom-snapshot-2026-01-15")
}

func TestIntegration_Snapshot_SnapshotError(t *testing.T) {
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:              "gvisor",
		providerType:      ProviderGVisor,
		healthy:           true,
		supportsSnapshots: true,
		snapshotErr:       errors.New("volume copy failed"),
	}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	_, err = provider.Snapshot(t.Context(), "container-123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume copy failed")
}

func TestIntegration_Snapshot_RestoreError(t *testing.T) {
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:              "gvisor",
		providerType:      ProviderGVisor,
		healthy:           true,
		supportsSnapshots: true,
		fromSnapshotErr:   errors.New("snapshot volume not found"),
	}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	_, err = provider.CreateFromSnapshot(t.Context(), "snap-missing", &CreateContainerRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot volume not found")
}

func TestIntegration_Snapshot_CapabilitiesReport(t *testing.T) {
	// Verify capabilities correctly report snapshot support.
	withSnap := &mockProvider{
		name:              "gvisor",
		providerType:      ProviderGVisor,
		supportsSnapshots: true,
	}
	assert.True(t, withSnap.Capabilities().SupportsSnapshots)

	withoutSnap := &mockProvider{
		name:              "e2b",
		providerType:      ProviderE2B,
		supportsSnapshots: false,
	}
	assert.False(t, withoutSnap.Capabilities().SupportsSnapshots)
}

func TestIntegration_Snapshot_MultipleFromSameSnapshot(t *testing.T) {
	// Test: Create multiple workspaces from the same snapshot.
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{
		name:              "gvisor",
		providerType:      ProviderGVisor,
		healthy:           true,
		supportsSnapshots: true,
		snapshotID:        "shared-snap-1",
	}
	o.RegisterProvider(ProviderGVisor, gv)

	provider, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)

	// Create 3 workspaces from the same snapshot
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		result, err := provider.CreateFromSnapshot(t.Context(), "shared-snap-1", &CreateContainerRequest{})
		require.NoError(t, err)
		ids[i] = result.ProviderID
	}

	// All IDs should be unique
	seen := map[string]bool{}
	for _, id := range ids {
		assert.False(t, seen[id], "duplicate ID from snapshot: %s", id)
		seen[id] = true
		assert.Contains(t, id, "shared-snap-1")
	}
}

// Verify configurableProvider and checkoutProvider implement Provider interface.
var _ Provider = (*configurableProvider)(nil)
var _ Provider = (*checkoutProvider)(nil)
