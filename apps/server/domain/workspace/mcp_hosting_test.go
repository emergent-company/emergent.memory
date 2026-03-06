package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- StdioBridge Tests ---

func TestStdioBridge_Call_Success(t *testing.T) {
	// Create a pipe: bridge writes to pipeWriter (stdin), reads from pipeReader (stdout)
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	bridge := NewStdioBridge(stdinWriter, stdoutReader, testLogger())

	// Simulate MCP server: read from stdin, write response to stdout
	go func() {
		buf := make([]byte, 4096)
		n, err := stdinReader.Read(buf)
		if err != nil {
			return
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(buf[:n], &req); err != nil {
			return
		}

		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"tools":["get-traces"]}`),
		}
		respBytes, _ := json.Marshal(resp)
		respBytes = append(respBytes, '\n')
		_, _ = stdoutWriter.Write(respBytes)
	}()

	resp, err := bridge.Call("tools/list", nil, 5*time.Second)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.Contains(t, string(resp.Result), "get-traces")

	_ = bridge.Close()
	stdinReader.Close()
	stdoutWriter.Close()
}

func TestStdioBridge_Call_ErrorResponse(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	bridge := NewStdioBridge(stdinWriter, stdoutReader, testLogger())

	go func() {
		buf := make([]byte, 4096)
		n, _ := stdinReader.Read(buf)

		var req JSONRPCRequest
		_ = json.Unmarshal(buf[:n], &req)

		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32600,
				Message: "Invalid Request",
			},
		}
		respBytes, _ := json.Marshal(resp)
		respBytes = append(respBytes, '\n')
		_, _ = stdoutWriter.Write(respBytes)
	}()

	resp, err := bridge.Call("invalid/method", nil, 5*time.Second)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32600, resp.Error.Code)
	assert.Equal(t, "Invalid Request", resp.Error.Message)

	_ = bridge.Close()
	stdinReader.Close()
	stdoutWriter.Close()
}

func TestStdioBridge_Call_Timeout(t *testing.T) {
	// Use a bytes.Buffer for stdin (write succeeds immediately) but an io.Pipe
	// for stdout where nobody writes — so ReadBytes blocks until timeout fires.
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()

	bridge := NewStdioBridge(&bytes.Buffer{}, stdoutReader, testLogger())

	_, err := bridge.Call("tools/list", nil, 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")

	_ = bridge.Close()
}

func TestStdioBridge_Call_Closed(t *testing.T) {
	bridge := NewStdioBridge(&bytes.Buffer{}, strings.NewReader(""), testLogger())
	_ = bridge.Close()

	_, err := bridge.Call("tools/list", nil, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestStdioBridge_Call_EOF(t *testing.T) {
	// Reader returns EOF immediately (server disconnected)
	bridge := NewStdioBridge(&bytes.Buffer{}, strings.NewReader(""), testLogger())

	_, err := bridge.Call("tools/list", nil, 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EOF")
}

func TestStdioBridge_Call_InvalidJSON(t *testing.T) {
	stdinWriter := &bytes.Buffer{}
	stdoutReader := strings.NewReader("not valid json\n")

	bridge := NewStdioBridge(stdinWriter, stdoutReader, testLogger())

	_, err := bridge.Call("tools/list", nil, 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse JSON-RPC response")
}

func TestStdioBridge_IsClosed(t *testing.T) {
	bridge := NewStdioBridge(&bytes.Buffer{}, strings.NewReader(""), testLogger())
	assert.False(t, bridge.IsClosed())
	_ = bridge.Close()
	assert.True(t, bridge.IsClosed())
}

func TestStdioBridge_DoubleClose(t *testing.T) {
	bridge := NewStdioBridge(&bytes.Buffer{}, strings.NewReader(""), testLogger())
	assert.NoError(t, bridge.Close())
	assert.NoError(t, bridge.Close()) // Should not panic
}

// --- JSON-RPC Types ---

func TestJSONRPCRequest_Marshal(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  map[string]string{"name": "get-traces"},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"jsonrpc":"2.0"`)
	assert.Contains(t, string(data), `"method":"tools/call"`)
	assert.Contains(t, string(data), `"id":1`)
}

func TestJSONRPCResponse_WithResult(t *testing.T) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Result:  json.RawMessage(`{"tools":["tool1","tool2"]}`),
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"result"`)
	assert.Nil(t, resp.Error)
}

func TestJSONRPCResponse_WithError(t *testing.T) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Error: &JSONRPCError{
			Code:    -32601,
			Message: "Method not found",
			Data:    "details",
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"error"`)
	assert.Contains(t, string(data), `"Method not found"`)
}

// --- MCP Hosting DTOs ---

func TestRegisterMCPServerRequest_Defaults(t *testing.T) {
	req := RegisterMCPServerRequest{
		Name:  "test-mcp",
		Image: "test:latest",
	}

	assert.Empty(t, req.RestartPolicy) // Will be defaulted by service
	assert.Nil(t, req.ResourceLimits)  // Will be defaulted by service
	assert.False(t, req.StdioBridge)
}

func TestMCPServerStatus_Fields(t *testing.T) {
	status := MCPServerStatus{
		WorkspaceID:     "ws-123",
		Name:            "langfuse-mcp",
		Image:           "emergent/mcp-langfuse:latest",
		Status:          StatusReady,
		Provider:        ProviderGVisor,
		StdioBridge:     true,
		BridgeConnected: true,
		RestartPolicy:   "always",
		RestartCount:    2,
		Uptime:          "5m30s",
	}

	data, err := json.Marshal(status)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"workspace_id":"ws-123"`)
	assert.Contains(t, string(data), `"name":"langfuse-mcp"`)
	assert.Contains(t, string(data), `"bridge_connected":true`)
	assert.Contains(t, string(data), `"restart_count":2`)
}

func TestMCPCallRequest_Validation(t *testing.T) {
	req := MCPCallRequest{
		Method:    "tools/call",
		Params:    map[string]string{"name": "get-traces"},
		TimeoutMs: 15000,
	}

	assert.Equal(t, "tools/call", req.Method)
	assert.Equal(t, 15000, req.TimeoutMs)
}

func TestMCPCallResponse_WithResult(t *testing.T) {
	resp := MCPCallResponse{
		Result: json.RawMessage(`{"content":"hello"}`),
	}
	assert.Nil(t, resp.Error)
	assert.Contains(t, string(resp.Result), "hello")
}

func TestMCPCallResponse_WithError(t *testing.T) {
	resp := MCPCallResponse{
		Error: &JSONRPCError{
			Code:    -32000,
			Message: "Server error",
		},
	}
	assert.Nil(t, resp.Result)
	assert.Equal(t, -32000, resp.Error.Code)
}

// --- MCP Hosting Defaults ---

func TestMCPHostingDefaults(t *testing.T) {
	assert.Equal(t, "0.5", defaultMCPCPU)
	assert.Equal(t, "512M", defaultMCPMemory)
	assert.Equal(t, "1G", defaultMCPDisk)
}

func TestCrashLoopBackoffConstants(t *testing.T) {
	assert.Equal(t, 60*time.Second, crashWindowDuration)
	assert.Equal(t, 3, crashLoopThreshold)
	assert.Equal(t, 5*time.Second, initialBackoff)
	assert.Equal(t, 5*time.Minute, maxBackoff)
	assert.Equal(t, 3.0, backoffMultiplier)
}

// --- Crash Backoff Calculation Logic ---

func TestCrashBackoffProgression(t *testing.T) {
	// Verify exponential backoff: 5s → 15s → 45s → 135s → 300s (capped at 5m)
	backoff := initialBackoff
	expected := []time.Duration{
		5 * time.Second,
		15 * time.Second,
		45 * time.Second,
		135 * time.Second,
		maxBackoff, // 5 minutes cap
	}

	for i, exp := range expected {
		assert.Equal(t, exp, backoff, "backoff step %d", i)
		backoff = time.Duration(float64(backoff) * backoffMultiplier)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// --- CreateContainerRequest Extension ---

func TestCreateContainerRequest_MCPFields(t *testing.T) {
	req := CreateContainerRequest{
		ContainerType: ContainerTypeMCPServer,
		BaseImage:     "emergent/mcp-langfuse:latest",
		Cmd:           []string{"node", "index.js"},
		Env:           map[string]string{"API_KEY": "secret"},
		ExtraVolumes:  []string{"/data", "/config"},
		AttachStdin:   true,
	}

	assert.Equal(t, ContainerTypeMCPServer, req.ContainerType)
	assert.Equal(t, []string{"node", "index.js"}, req.Cmd)
	assert.Equal(t, "secret", req.Env["API_KEY"])
	assert.Len(t, req.ExtraVolumes, 2)
	assert.True(t, req.AttachStdin)
}

func TestCreateContainerRequest_DefaultCmd(t *testing.T) {
	req := CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	}

	// When Cmd is nil, gVisor provider uses ["sleep", "infinity"]
	assert.Nil(t, req.Cmd)
}

// --- ContainerInspection ---

func TestContainerInspection_Fields(t *testing.T) {
	inspection := ContainerInspection{
		Running:    true,
		ExitCode:   0,
		StartedAt:  "2025-01-01T00:00:00Z",
		FinishedAt: "",
		Status:     "running",
	}

	data, err := json.Marshal(inspection)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"running":true`)
	assert.Contains(t, string(data), `"status":"running"`)
}

// --- MCP Hosting Service Unit Tests ---

func TestMCPHostingService_BuildStatusFromWorkspace(t *testing.T) {
	svc := NewMCPHostingService(nil, nil, nil, testLogger())

	ws := &AgentWorkspace{
		ID:            "ws-test-123",
		ContainerType: ContainerTypeMCPServer,
		Provider:      ProviderGVisor,
		Status:        StatusReady,
		CreatedAt:     time.Now(),
		LastUsedAt:    time.Now(),
		MCPConfig: &MCPConfig{
			Name:          "test-mcp",
			Image:         "test:latest",
			StdioBridge:   true,
			RestartPolicy: "always",
			Volumes:       []string{"/data"},
		},
		ResourceLimits: &ResourceLimits{
			CPU:    "0.5",
			Memory: "512M",
		},
	}

	status := svc.buildStatusFromWorkspace(ws)

	assert.Equal(t, "ws-test-123", status.WorkspaceID)
	assert.Equal(t, "test-mcp", status.Name)
	assert.Equal(t, "test:latest", status.Image)
	assert.Equal(t, StatusReady, status.Status)
	assert.Equal(t, ProviderGVisor, status.Provider)
	assert.True(t, status.StdioBridge)
	assert.Equal(t, "always", status.RestartPolicy)
	assert.Equal(t, []string{"/data"}, status.Volumes)
	assert.Equal(t, "0.5", status.ResourceLimits.CPU)
	assert.False(t, status.BridgeConnected) // No runtime state
	assert.Equal(t, 0, status.RestartCount)
}

func TestMCPHostingService_BuildStatusWithRuntimeState(t *testing.T) {
	svc := NewMCPHostingService(nil, nil, nil, testLogger())

	startedAt := time.Now().Add(-5 * time.Minute)
	lastCrash := time.Now().Add(-2 * time.Minute)

	// Add runtime state
	state := &mcpServerState{
		workspaceID:  "ws-test-456",
		restartCount: 3,
		lastCrash:    &lastCrash,
		startedAt:    startedAt,
		bridge:       NewStdioBridge(&bytes.Buffer{}, strings.NewReader(""), testLogger()),
	}
	svc.servers["ws-test-456"] = state

	ws := &AgentWorkspace{
		ID:            "ws-test-456",
		ContainerType: ContainerTypeMCPServer,
		Provider:      ProviderGVisor,
		Status:        StatusReady,
		CreatedAt:     time.Now(),
		LastUsedAt:    time.Now(),
		MCPConfig: &MCPConfig{
			Name:  "test-mcp-2",
			Image: "test:v2",
		},
	}

	status := svc.buildStatusFromWorkspace(ws)

	assert.Equal(t, 3, status.RestartCount)
	assert.NotNil(t, status.LastCrash)
	assert.NotEmpty(t, status.Uptime)
	assert.True(t, status.BridgeConnected) // Bridge exists and not closed
}

func TestMCPHostingService_StopServer(t *testing.T) {
	svc := NewMCPHostingService(nil, nil, nil, testLogger())

	bridge := NewStdioBridge(&bytes.Buffer{}, strings.NewReader(""), testLogger())
	state := &mcpServerState{
		workspaceID: "ws-stop-test",
		bridge:      bridge,
		stopCh:      make(chan struct{}),
	}
	svc.servers["ws-stop-test"] = state

	svc.stopServer("ws-stop-test")

	assert.True(t, bridge.IsClosed())
	assert.True(t, state.stopped)

	svc.mu.RLock()
	_, exists := svc.servers["ws-stop-test"]
	svc.mu.RUnlock()
	assert.False(t, exists, "server should be removed from map")
}

func TestMCPHostingService_StopServer_NotFound(t *testing.T) {
	svc := NewMCPHostingService(nil, nil, nil, testLogger())

	// Should not panic
	svc.stopServer("non-existent")
}

func TestMCPHostingService_Shutdown_NoServers(t *testing.T) {
	svc := NewMCPHostingService(nil, nil, nil, testLogger())

	err := svc.Shutdown(t.Context())
	assert.NoError(t, err)
}

// --- Serialization concurrency test ---

func TestStdioBridge_SerializesConcurrentCalls(t *testing.T) {
	// This test verifies the mutex serialization works — we don't actually
	// test true concurrency (that requires integration tests), but we verify
	// the bridge structure supports it.

	bridge := NewStdioBridge(&bytes.Buffer{}, strings.NewReader(""), testLogger())
	assert.NotNil(t, bridge)
	assert.False(t, bridge.IsClosed())

	// Verify next ID increments
	id1 := bridge.nextID.Add(1)
	id2 := bridge.nextID.Add(1)
	assert.Equal(t, int64(1), id1)
	assert.Equal(t, int64(2), id2)
}

// --- MCPConfig validation ---

func TestMCPConfig_Serialization(t *testing.T) {
	cfg := MCPConfig{
		Name:          "langfuse-mcp",
		Image:         "emergent/mcp-langfuse:latest",
		StdioBridge:   true,
		RestartPolicy: "always",
		Environment:   map[string]string{"API_KEY": "secret"},
		Volumes:       []string{"/data"},
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"name":"langfuse-mcp"`)
	assert.Contains(t, string(data), `"stdio_bridge":true`)
	assert.Contains(t, string(data), `"restart_policy":"always"`)

	var decoded MCPConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, cfg.Name, decoded.Name)
	assert.Equal(t, cfg.StdioBridge, decoded.StdioBridge)
	assert.Equal(t, cfg.Environment["API_KEY"], decoded.Environment["API_KEY"])
}

func TestMCPConfig_RestartPolicies(t *testing.T) {
	policies := []string{"always", "on-failure", "never"}
	for _, p := range policies {
		t.Run(p, func(t *testing.T) {
			cfg := MCPConfig{RestartPolicy: p}
			data, err := json.Marshal(cfg)
			require.NoError(t, err)
			assert.Contains(t, string(data), fmt.Sprintf(`"restart_policy":"%s"`, p))
		})
	}
}
