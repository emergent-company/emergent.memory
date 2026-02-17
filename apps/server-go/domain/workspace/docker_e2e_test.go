package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Docker E2E test helpers
// =============================================================================

// skipWithoutDocker skips the test if Docker is not available.
func skipWithoutDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not in PATH, skipping Docker E2E test")
	}
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("docker daemon not running, skipping Docker E2E test")
	}
}

// newRealGVisorProvider creates a GVisorProvider configured for testing
// (standard runtime, no custom network).
func newRealGVisorProvider(t *testing.T) *GVisorProvider {
	t.Helper()
	p, err := NewGVisorProvider(
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		&GVisorProviderConfig{
			ForceStandardRuntime: true, // Use runc — runsc may not be installed
		},
	)
	require.NoError(t, err, "failed to create GVisorProvider")
	return p
}

// destroyOnCleanup registers a cleanup function to destroy a container after the test.
// Uses context.Background() because t.Context() is canceled when the test finishes,
// which would cause the cleanup to fail.
func destroyOnCleanup(t *testing.T, p *GVisorProvider, providerID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := p.Destroy(ctx, providerID); err != nil {
			t.Logf("cleanup: failed to destroy container %s: %v", providerID[:min(12, len(providerID))], err)
		}
	})
}

// =============================================================================
// TG16.1 — Full workspace lifecycle: create → clone → read → edit → run → commit → destroy
// =============================================================================

func TestDockerE2E_FullWorkspaceLifecycle(t *testing.T) {
	skipWithoutDocker(t)
	if testing.Short() {
		t.Skip("skipping long Docker E2E test in short mode")
	}

	p := newRealGVisorProvider(t)

	// Step 1: Create workspace
	result, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err, "failed to create workspace")
	require.NotEmpty(t, result.ProviderID)
	// NOTE: No destroyOnCleanup here — this test explicitly destroys in step 11
	// and verifies the container is gone. A conditional cleanup handles the case
	// where the test fails before reaching the explicit destroy.
	destroyed := false
	t.Cleanup(func() {
		if !destroyed {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := p.Destroy(ctx, result.ProviderID); err != nil {
				t.Logf("cleanup: failed to destroy container %s: %v", result.ProviderID[:min(12, len(result.ProviderID))], err)
			}
		}
	})
	t.Logf("created workspace container: %s", result.ProviderID[:12])

	// Step 2: Install git (ubuntu:22.04 base image doesn't include it)
	installResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command:   "apt-get update -qq && apt-get install -y -qq git >/dev/null 2>&1 && git --version",
		TimeoutMs: 60000,
	})
	require.NoError(t, err, "failed to install git")
	assert.Equal(t, 0, installResult.ExitCode, "git install failed: %s %s", installResult.Stdout, installResult.Stderr)
	assert.Contains(t, installResult.Stdout, "git version")

	// Step 3: Initialize a git repo inside (simulates clone without needing a real remote)
	initResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: `git init /workspace && cd /workspace && git config user.email "test@test.com" && git config user.name "Test"`,
	})
	require.NoError(t, err, "failed to init git repo")
	assert.Equal(t, 0, initResult.ExitCode, "git init failed: %s %s", initResult.Stdout, initResult.Stderr)

	// Step 4: Write a file
	err = p.WriteFile(t.Context(), result.ProviderID, &FileWriteRequest{
		FilePath: "/workspace/hello.go",
		Content:  "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}\n",
	})
	require.NoError(t, err, "failed to write file")

	// Step 5: Read the file back
	readResult, err := p.ReadFile(t.Context(), result.ProviderID, &FileReadRequest{
		FilePath: "/workspace/hello.go",
	})
	require.NoError(t, err, "failed to read file")
	assert.Contains(t, readResult.Content, "Hello, World!", "file content mismatch")
	assert.False(t, readResult.IsBinary)
	assert.False(t, readResult.IsDir)

	// Step 6: Edit the file (write modified content)
	err = p.WriteFile(t.Context(), result.ProviderID, &FileWriteRequest{
		FilePath: "/workspace/hello.go",
		Content:  "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, Agent!\")\n}\n",
	})
	require.NoError(t, err, "failed to edit file")

	// Step 7: Verify edit took effect
	readResult2, err := p.ReadFile(t.Context(), result.ProviderID, &FileReadRequest{
		FilePath: "/workspace/hello.go",
	})
	require.NoError(t, err, "failed to read edited file")
	assert.Contains(t, readResult2.Content, "Hello, Agent!", "edit not applied")
	assert.NotContains(t, readResult2.Content, "Hello, World!", "old content still present")

	// Step 8: Run a command (simulates test execution)
	runResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: "echo 'tests passed' && exit 0",
		Workdir: "/workspace",
	})
	require.NoError(t, err, "failed to run command")
	assert.Equal(t, 0, runResult.ExitCode)
	assert.Contains(t, runResult.Stdout, "tests passed")

	// Step 9: Commit changes
	commitResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: `cd /workspace && git add -A && git commit -m "Agent edit: update greeting"`,
	})
	require.NoError(t, err, "failed to commit")
	assert.Equal(t, 0, commitResult.ExitCode, "git commit failed: %s %s", commitResult.Stdout, commitResult.Stderr)
	assert.Contains(t, commitResult.Stdout, "Agent edit: update greeting")

	// Step 10: Verify commit log
	logResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: "cd /workspace && git log --oneline -1",
	})
	require.NoError(t, err, "failed to get git log")
	assert.Equal(t, 0, logResult.ExitCode)
	assert.Contains(t, logResult.Stdout, "Agent edit")

	// Step 11: List files via glob
	listResult, err := p.ListFiles(t.Context(), result.ProviderID, &FileListRequest{
		Path:    "/workspace",
		Pattern: "*.go",
	})
	require.NoError(t, err, "failed to list files")
	assert.NotEmpty(t, listResult.Files, "should find .go files")
	found := false
	for _, f := range listResult.Files {
		if strings.HasSuffix(f.Path, "hello.go") {
			found = true
			assert.False(t, f.IsDir)
			assert.Greater(t, f.Size, int64(0))
		}
	}
	assert.True(t, found, "hello.go not found in file list")

	// Step 12: Destroy and verify container is gone
	err = p.Destroy(t.Context(), result.ProviderID)
	assert.NoError(t, err, "destroy should succeed")
	destroyed = true

	// Verify container is gone
	_, err = p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: "echo alive",
	})
	assert.Error(t, err, "exec should fail after destroy")

	t.Log("TG16.1: Full workspace lifecycle E2E test passed")
}

// =============================================================================
// TG16.2 — MCP server lifecycle: register → call → crash → restart → call
// =============================================================================

func TestDockerE2E_MCPServerLifecycle(t *testing.T) {
	skipWithoutDocker(t)
	if testing.Short() {
		t.Skip("skipping long Docker E2E test in short mode")
	}

	p := newRealGVisorProvider(t)

	// Step 1: Create a container that acts as an MCP server (simple echo-based mock)
	result, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeMCPServer,
		Cmd:           []string{"sleep", "infinity"},
	})
	require.NoError(t, err, "failed to create MCP container")
	require.NotEmpty(t, result.ProviderID)
	destroyOnCleanup(t, p, result.ProviderID)
	t.Logf("created MCP container: %s", result.ProviderID[:12])

	// Step 2: Write a simple script that simulates an MCP server (responds to exec)
	err = p.WriteFile(t.Context(), result.ProviderID, &FileWriteRequest{
		FilePath: "/workspace/mcp_server.sh",
		Content:  "#!/bin/sh\necho '{\"jsonrpc\":\"2.0\",\"result\":{\"tools\":[\"test\"]},\"id\":1}'\n",
	})
	require.NoError(t, err, "failed to write MCP server script")

	// Step 3: Execute the MCP-like command ("call method")
	callResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: "chmod +x /workspace/mcp_server.sh && /workspace/mcp_server.sh",
	})
	require.NoError(t, err, "MCP call failed")
	assert.Equal(t, 0, callResult.ExitCode)
	assert.Contains(t, callResult.Stdout, "jsonrpc")
	assert.Contains(t, callResult.Stdout, "tools")

	// Step 4: Simulate crash — stop the container
	err = p.Stop(t.Context(), result.ProviderID)
	require.NoError(t, err, "failed to stop container")

	// Step 5: Verify container is not accessible after stop
	_, err = p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: "echo alive",
	})
	assert.Error(t, err, "exec should fail after stop")

	// Step 6: Restart (Resume) — simulates auto-restart on crash
	err = p.Resume(t.Context(), result.ProviderID)
	require.NoError(t, err, "failed to resume container")

	// Step 7: Call again after restart — verify response is the same
	callResult2, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: "/workspace/mcp_server.sh",
	})
	require.NoError(t, err, "MCP call after restart failed")
	assert.Equal(t, 0, callResult2.ExitCode)
	assert.Contains(t, callResult2.Stdout, "jsonrpc")
	assert.Contains(t, callResult2.Stdout, "tools")

	// Step 8: Verify data persistence across restart
	readResult, err := p.ReadFile(t.Context(), result.ProviderID, &FileReadRequest{
		FilePath: "/workspace/mcp_server.sh",
	})
	require.NoError(t, err, "failed to read file after restart")
	assert.Contains(t, readResult.Content, "jsonrpc", "file should persist across restart")

	t.Log("TG16.2: MCP server lifecycle E2E test passed")
}

// =============================================================================
// TG16.4 — Warm pool hit: sub-150ms assignment
// =============================================================================

func TestDockerE2E_WarmPoolHitSubMs(t *testing.T) {
	skipWithoutDocker(t)
	if testing.Short() {
		t.Skip("skipping long Docker E2E test in short mode")
	}

	// Set up orchestrator with real gVisor provider
	o := NewOrchestrator(testLogger())
	p := newRealGVisorProvider(t)
	o.RegisterProvider(ProviderGVisor, p)

	// Create warm pool with 2 containers
	pool := NewWarmPool(o, testLogger(), WarmPoolConfig{Size: 2})

	// Start the pool (pre-creates containers)
	err := pool.Start(t.Context())
	require.NoError(t, err, "failed to start warm pool")

	// Cleanup: stop the pool when test ends
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if stopErr := pool.Stop(ctx); stopErr != nil {
			t.Logf("cleanup: pool stop error: %v", stopErr)
		}
	})

	// Verify pool has containers ready
	metrics := pool.Metrics()
	require.Equal(t, 2, metrics.PoolSize, "pool should have 2 containers")
	require.Equal(t, 2, metrics.TargetSize)

	// Measure acquisition time
	start := time.Now()
	wc := pool.Acquire(ProviderGVisor)
	acquireTime := time.Since(start)

	require.NotNil(t, wc, "should get a warm container")
	t.Logf("warm pool acquisition time: %v", acquireTime)

	// The acquisition itself (just picking from the pool) should be extremely fast.
	// The 150ms threshold in the spec accounts for the entire assignment flow.
	// Pool lookup is essentially mutex + slice access — should be <1ms.
	assert.Less(t, acquireTime.Milliseconds(), int64(150), "acquisition should be sub-150ms")

	// Verify the container is usable
	containerID := wc.ProviderID()
	destroyOnCleanup(t, p, containerID)

	execResult, err := p.Exec(t.Context(), containerID, &ExecRequest{
		Command: "echo warm-pool-hit",
	})
	require.NoError(t, err, "exec on warm container failed")
	assert.Equal(t, 0, execResult.ExitCode)
	assert.Contains(t, execResult.Stdout, "warm-pool-hit")

	// Check metrics
	finalMetrics := pool.Metrics()
	assert.Equal(t, int64(1), finalMetrics.Hits, "should record 1 hit")
	assert.Equal(t, int64(0), finalMetrics.Misses, "should have 0 misses")
	// Pool size should be 1 (one was acquired)
	assert.Equal(t, 1, finalMetrics.PoolSize, "pool should have 1 remaining container")

	// Second acquisition
	start2 := time.Now()
	wc2 := pool.Acquire(ProviderGVisor)
	acquireTime2 := time.Since(start2)
	require.NotNil(t, wc2, "should get second warm container")
	destroyOnCleanup(t, p, wc2.ProviderID())

	t.Logf("second acquisition time: %v", acquireTime2)
	assert.Less(t, acquireTime2.Milliseconds(), int64(150), "second acquisition should be sub-150ms")

	// Third acquisition should be a miss (pool is empty, replenishment is async)
	wc3 := pool.Acquire(ProviderGVisor)
	// May or may not be nil — if replenishment finished fast enough, it could succeed.
	// Either way, verify metrics consistency.
	finalMetrics2 := pool.Metrics()
	if wc3 != nil {
		destroyOnCleanup(t, p, wc3.ProviderID())
		assert.Equal(t, int64(3), finalMetrics2.Hits)
	} else {
		assert.GreaterOrEqual(t, finalMetrics2.Misses, int64(1))
	}

	t.Log("TG16.4: Warm pool hit sub-150ms E2E test passed")
}

// =============================================================================
// TG16.7 — Load test: 50 concurrent workspace creates
// =============================================================================

func TestDockerE2E_ConcurrentWorkspaceCreates(t *testing.T) {
	skipWithoutDocker(t)
	if testing.Short() {
		t.Skip("skipping long Docker E2E test in short mode")
	}

	const concurrency = 50

	p := newRealGVisorProvider(t)

	// Track results
	var (
		mu         sync.Mutex
		successes  int32
		failures   int32
		createdIDs []string
		durations  []time.Duration
		errorMsgs  []string
	)

	// Launch 50 concurrent creates
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			createStart := time.Now()
			result, err := p.Create(t.Context(), &CreateContainerRequest{
				ContainerType: ContainerTypeAgentWorkspace,
				Labels: map[string]string{
					"test.index": fmt.Sprintf("%d", idx),
				},
			})
			dur := time.Since(createStart)

			mu.Lock()
			defer mu.Unlock()

			durations = append(durations, dur)
			if err != nil {
				atomic.AddInt32(&failures, 1)
				errorMsgs = append(errorMsgs, fmt.Sprintf("worker %d: %v", idx, err))
			} else {
				atomic.AddInt32(&successes, 1)
				createdIDs = append(createdIDs, result.ProviderID)
			}
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	// Cleanup: destroy all created containers
	t.Cleanup(func() {
		t.Logf("cleanup: destroying %d containers...", len(createdIDs))
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		var cleanupWg sync.WaitGroup
		for _, id := range createdIDs {
			cleanupWg.Add(1)
			go func(cid string) {
				defer cleanupWg.Done()
				if err := p.Destroy(cleanupCtx, cid); err != nil {
					t.Logf("cleanup: failed to destroy %s: %v", cid[:min(12, len(cid))], err)
				}
			}(id)
		}
		cleanupWg.Wait()
		t.Log("cleanup: all containers destroyed")
	})

	// Report results
	t.Logf("Load test results:")
	t.Logf("  Concurrency:    %d", concurrency)
	t.Logf("  Successes:      %d", successes)
	t.Logf("  Failures:       %d", failures)
	t.Logf("  Total duration: %v", totalDuration)

	if len(durations) > 0 {
		var minDur, maxDur time.Duration
		var totalDur time.Duration
		minDur = durations[0]
		for _, d := range durations {
			totalDur += d
			if d < minDur {
				minDur = d
			}
			if d > maxDur {
				maxDur = d
			}
		}
		avgDur := totalDur / time.Duration(len(durations))
		t.Logf("  Min create time:  %v", minDur)
		t.Logf("  Max create time:  %v", maxDur)
		t.Logf("  Avg create time:  %v", avgDur)
	}

	for _, msg := range errorMsgs {
		t.Logf("  Error: %s", msg)
	}

	// Assertions
	// At least 80% should succeed (Docker may have resource limits)
	successRate := float64(successes) / float64(concurrency)
	assert.GreaterOrEqual(t, successRate, 0.8,
		"at least 80%% of concurrent creates should succeed (got %.0f%%)", successRate*100)

	// Verify created containers are distinct
	idSet := make(map[string]bool)
	for _, id := range createdIDs {
		assert.False(t, idSet[id], "duplicate container ID: %s", id[:min(12, len(id))])
		idSet[id] = true
	}

	// Verify at least some containers are functional
	if len(createdIDs) > 0 {
		// Test first and last created containers
		testContainer := func(cid string) {
			execResult, err := p.Exec(t.Context(), cid, &ExecRequest{
				Command:   "echo ok",
				TimeoutMs: 10000,
			})
			assert.NoError(t, err, "exec on container %s failed", cid[:min(12, len(cid))])
			if err == nil {
				assert.Equal(t, 0, execResult.ExitCode)
				assert.Contains(t, execResult.Stdout, "ok")
			}
		}

		testContainer(createdIDs[0])
		if len(createdIDs) > 1 {
			testContainer(createdIDs[len(createdIDs)-1])
		}
	}

	// Verify health check still works after load
	health, err := p.Health(t.Context())
	require.NoError(t, err)
	assert.True(t, health.Healthy, "provider should still be healthy after load test")
	assert.GreaterOrEqual(t, health.ActiveCount, int(successes),
		"active count should include created containers")

	t.Log("TG16.7: Concurrent workspace creates load test passed")
}

// =============================================================================
// TG17.17 — GitHub App: clone private repo → push → verify bot authorship
// =============================================================================

func TestDockerE2E_GitHubAppClonePushVerify(t *testing.T) {
	skipWithoutDocker(t)
	if testing.Short() {
		t.Skip("skipping long Docker E2E test in short mode")
	}

	// This test requires a configured GitHub App.
	// Skip if env vars are not set.
	if os.Getenv("GITHUB_APP_ID") == "" || os.Getenv("GITHUB_APP_PRIVATE_KEY") == "" {
		t.Skip("GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY not set, skipping GitHub App E2E test")
	}
	if os.Getenv("GITHUB_APP_INSTALLATION_ID") == "" {
		t.Skip("GITHUB_APP_INSTALLATION_ID not set, skipping GitHub App E2E test")
	}
	testRepoURL := os.Getenv("GITHUB_APP_TEST_REPO")
	if testRepoURL == "" {
		t.Skip("GITHUB_APP_TEST_REPO not set (e.g. https://github.com/org/test-repo), skipping")
	}

	p := newRealGVisorProvider(t)

	// Step 1: Create workspace
	result, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err, "failed to create workspace")
	destroyOnCleanup(t, p, result.ProviderID)

	// Step 2: Clone using a simulated GitHub App token (from environment)
	// In a real flow, the CheckoutService would obtain the token via GitCredentialProvider.
	// Here we test the container-level git operations.
	token := os.Getenv("GITHUB_APP_TOKEN") // Pre-generated installation token for testing
	if token == "" {
		t.Skip("GITHUB_APP_TOKEN not set (pre-generated installation token), skipping clone test")
	}

	// Build authenticated clone URL
	// https://github.com/org/repo -> https://x-access-token:TOKEN@github.com/org/repo
	cloneURL := strings.Replace(testRepoURL, "https://", "https://x-access-token:"+token+"@", 1)

	cloneResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command:   fmt.Sprintf(`git clone --depth 1 %q /workspace/repo 2>&1`, cloneURL),
		TimeoutMs: 60000,
	})
	require.NoError(t, err, "git clone exec failed")
	assert.Equal(t, 0, cloneResult.ExitCode,
		"git clone failed: %s", sanitizeGitOutput(cloneResult.Stdout+cloneResult.Stderr))

	// Step 3: Configure bot identity
	configResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: `cd /workspace/repo && git config user.email "bot@emergent.local" && git config user.name "Emergent Bot"`,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, configResult.ExitCode)

	// Step 4: Create a test commit
	testBranch := fmt.Sprintf("e2e-test-%d", time.Now().UnixNano())
	commitResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: fmt.Sprintf(`cd /workspace/repo && git checkout -b %q && echo "test %d" > e2e-test.txt && git add -A && git commit -m "E2E test commit"`, testBranch, time.Now().Unix()),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, commitResult.ExitCode,
		"commit failed: %s %s", commitResult.Stdout, commitResult.Stderr)

	// Step 5: Push
	pushResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command:   fmt.Sprintf(`cd /workspace/repo && git push origin %q 2>&1`, testBranch),
		TimeoutMs: 60000,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, pushResult.ExitCode,
		"push failed: %s", sanitizeGitOutput(pushResult.Stdout+pushResult.Stderr))

	// Step 6: Verify bot authorship
	logResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command: `cd /workspace/repo && git log -1 --format="%an <%ae>"`,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, logResult.ExitCode)
	assert.Contains(t, logResult.Stdout, "Emergent Bot", "commit should be authored by bot")
	assert.Contains(t, logResult.Stdout, "bot@emergent.local", "commit should have bot email")

	// Cleanup: delete the remote branch
	_, _ = p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command:   fmt.Sprintf(`cd /workspace/repo && git push origin --delete %q 2>&1 || true`, testBranch),
		TimeoutMs: 30000,
	})

	t.Log("TG17.17: GitHub App clone/push/verify E2E test passed")
}

// =============================================================================
// Additional Docker E2E: Snapshot lifecycle (supplements TG12)
// =============================================================================

func TestDockerE2E_SnapshotLifecycle(t *testing.T) {
	skipWithoutDocker(t)
	if testing.Short() {
		t.Skip("skipping long Docker E2E test in short mode")
	}

	p := newRealGVisorProvider(t)

	// Step 1: Create workspace and write data
	result, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	destroyOnCleanup(t, p, result.ProviderID)

	err = p.WriteFile(t.Context(), result.ProviderID, &FileWriteRequest{
		FilePath: "/workspace/data.txt",
		Content:  "snapshot-test-data-12345",
	})
	require.NoError(t, err)

	// Step 2: Take snapshot
	snapshotID, err := p.Snapshot(t.Context(), result.ProviderID)
	require.NoError(t, err, "failed to create snapshot")
	require.NotEmpty(t, snapshotID)
	t.Logf("snapshot created: %s", snapshotID)

	// Step 3: Create new workspace from snapshot
	restored, err := p.CreateFromSnapshot(t.Context(), snapshotID, &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err, "failed to create from snapshot")
	destroyOnCleanup(t, p, restored.ProviderID)

	// Step 4: Verify data persisted through snapshot
	readResult, err := p.ReadFile(t.Context(), restored.ProviderID, &FileReadRequest{
		FilePath: "/workspace/data.txt",
	})
	require.NoError(t, err, "failed to read file from restored workspace")
	assert.Contains(t, readResult.Content, "snapshot-test-data-12345",
		"data should persist through snapshot/restore")

	// Step 5: Verify the restored workspace is independently modifiable
	err = p.WriteFile(t.Context(), restored.ProviderID, &FileWriteRequest{
		FilePath: "/workspace/new-file.txt",
		Content:  "only-in-restored",
	})
	require.NoError(t, err)

	// The original workspace should NOT have the new file
	_, origErr := p.ReadFile(t.Context(), result.ProviderID, &FileReadRequest{
		FilePath: "/workspace/new-file.txt",
	})
	assert.Error(t, origErr, "new file should NOT exist in original workspace")

	// Cleanup snapshot volume
	t.Cleanup(func() {
		if rmErr := exec.Command("docker", "volume", "rm", "-f", snapshotID).Run(); rmErr != nil {
			t.Logf("cleanup: failed to remove snapshot volume %s: %v", snapshotID, rmErr)
		}
	})

	t.Log("Docker E2E: Snapshot lifecycle test passed")
}

// =============================================================================
// Additional Docker E2E: Stop and Resume
// =============================================================================

func TestDockerE2E_StopAndResume(t *testing.T) {
	skipWithoutDocker(t)
	if testing.Short() {
		t.Skip("skipping long Docker E2E test in short mode")
	}

	p := newRealGVisorProvider(t)

	// Create workspace with data
	result, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	destroyOnCleanup(t, p, result.ProviderID)

	err = p.WriteFile(t.Context(), result.ProviderID, &FileWriteRequest{
		FilePath: "/workspace/persist.txt",
		Content:  "should-survive-stop",
	})
	require.NoError(t, err)

	// Stop the container
	err = p.Stop(t.Context(), result.ProviderID)
	require.NoError(t, err)

	// Exec should fail while stopped
	_, err = p.Exec(t.Context(), result.ProviderID, &ExecRequest{Command: "echo no"})
	assert.Error(t, err, "should not be able to exec in stopped container")

	// Resume
	err = p.Resume(t.Context(), result.ProviderID)
	require.NoError(t, err)

	// Data should persist
	readResult, err := p.ReadFile(t.Context(), result.ProviderID, &FileReadRequest{
		FilePath: "/workspace/persist.txt",
	})
	require.NoError(t, err)
	assert.Contains(t, readResult.Content, "should-survive-stop")

	t.Log("Docker E2E: Stop and resume test passed")
}
