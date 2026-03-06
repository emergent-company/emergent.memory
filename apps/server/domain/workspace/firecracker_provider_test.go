package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirecrackerProvider_Capabilities(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	caps := p.Capabilities()
	assert.Equal(t, "Firecracker (microVM)", caps.Name)
	assert.Equal(t, ProviderFirecracker, caps.ProviderType)
	assert.True(t, caps.SupportsPersistence)
	assert.True(t, caps.SupportsSnapshots)
	assert.True(t, caps.SupportsWarmPool)
	assert.True(t, caps.RequiresKVM)
	assert.Equal(t, 125, caps.EstimatedStartupMs)
}

func TestFirecrackerProvider_GenerateMAC(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	tests := []struct {
		counter uint64
		want    string
	}{
		{1, "AA:FC:00:00:00:01"},
		{255, "AA:FC:00:00:00:FF"},
		{256, "AA:FC:00:00:01:00"},
		{65536, "AA:FC:00:01:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := p.generateMAC(tt.counter)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFirecrackerProvider_IPAssignment(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	// First assignment
	counter1 := p.ipCounter.Add(1)
	x1 := (counter1/254)%254 + 1
	y1 := counter1%254 + 2
	assert.Equal(t, uint64(1), x1)
	assert.Equal(t, uint64(3), y1) // counter=1, y = 1%254+2 = 3

	// 254th assignment wraps to next subnet
	for i := uint64(0); i < 253; i++ {
		p.ipCounter.Add(1)
	}
	counter254 := p.ipCounter.Add(1)
	x254 := (counter254/254)%254 + 1
	y254 := counter254%254 + 2
	assert.Equal(t, uint64(2), x254) // second subnet
	assert.Equal(t, uint64(3), y254) // wraps back to 3
}

func TestFirecrackerProvider_Health_NoKVM(t *testing.T) {
	p := &FirecrackerProvider{
		log:          testLogger(),
		config:       &FirecrackerProviderConfig{},
		vms:          make(map[string]*firecrackerVM),
		kvmAvailable: false,
	}

	status, err := p.Health(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Healthy)
	assert.Contains(t, status.Message, "KVM not available")
}

func TestFirecrackerProvider_Health_WithKVM(t *testing.T) {
	p := &FirecrackerProvider{
		log:          testLogger(),
		config:       &FirecrackerProviderConfig{},
		vms:          make(map[string]*firecrackerVM),
		kvmAvailable: true,
	}

	// Add some fake VMs
	p.vms["vm1"] = &firecrackerVM{id: "vm1"}
	p.vms["vm2"] = &firecrackerVM{id: "vm2"}

	status, err := p.Health(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Healthy)
	assert.Equal(t, 2, status.ActiveCount)
	assert.Contains(t, status.Message, "2 active VMs")
}

func TestFirecrackerProvider_ActiveVMs(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	assert.Equal(t, 0, p.ActiveVMs())

	p.vms["vm1"] = &firecrackerVM{id: "vm1"}
	assert.Equal(t, 1, p.ActiveVMs())

	p.vms["vm2"] = &firecrackerVM{id: "vm2"}
	assert.Equal(t, 2, p.ActiveVMs())
}

func TestFirecrackerProvider_GetVM_NotFound(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	_, err := p.getVM("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VM not found")
}

func TestFirecrackerProvider_GetVM_Stopped(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	p.vms["vm1"] = &firecrackerVM{id: "vm1", stopped: true}

	_, err := p.getVM("vm1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VM is stopped")
}

func TestFirecrackerProvider_GetVM_Found(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	expected := &firecrackerVM{id: "vm1", vmIP: "172.16.1.2"}
	p.vms["vm1"] = expected

	got, err := p.getVM("vm1")
	assert.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestFirecrackerProvider_Create_NoKVM(t *testing.T) {
	p := &FirecrackerProvider{
		log:          testLogger(),
		config:       &FirecrackerProviderConfig{},
		vms:          make(map[string]*firecrackerVM),
		kvmAvailable: false,
	}

	_, err := p.Create(context.Background(), &CreateContainerRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "KVM is not available")
}

func TestFirecrackerProvider_Destroy_NotFound(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	err := p.Destroy(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VM not found")
}

func TestFirecrackerProvider_ConfigDefaults(t *testing.T) {
	enableNAT := true
	p, err := NewFirecrackerProvider(testLogger(), nil)
	// May fail due to data dir permissions in test env; that's OK
	if err != nil {
		t.Skipf("skipping config defaults test: %v", err)
	}

	assert.Equal(t, fcDataDir, p.config.DataDir)
	assert.Equal(t, fcKernelPath, p.config.KernelPath)
	assert.Equal(t, fcDefaultVCPUs, p.config.DefaultVCPUs)
	assert.Equal(t, fcDefaultMemMiB, p.config.DefaultMemMiB)
	assert.Equal(t, fcDefaultDiskSizeMB, p.config.DefaultDiskSizeMB)
	assert.Equal(t, &enableNAT, p.config.EnableIPTablesNAT)
}

func TestFirecrackerProvider_ConfigCustom(t *testing.T) {
	enableNAT := false
	cfg := &FirecrackerProviderConfig{
		DataDir:           t.TempDir(),
		KernelPath:        "/custom/vmlinux",
		RootfsPath:        "/custom/rootfs.ext4",
		DefaultVCPUs:      4,
		DefaultMemMiB:     1024,
		DefaultDiskSizeMB: 20480,
		EnableIPTablesNAT: &enableNAT,
	}

	p, err := NewFirecrackerProvider(testLogger(), cfg)
	require.NoError(t, err)

	assert.Equal(t, cfg.DataDir, p.config.DataDir)
	assert.Equal(t, "/custom/vmlinux", p.config.KernelPath)
	assert.Equal(t, "/custom/rootfs.ext4", p.config.RootfsPath)
	assert.Equal(t, 4, p.config.DefaultVCPUs)
	assert.Equal(t, 1024, p.config.DefaultMemMiB)
	assert.Equal(t, 20480, p.config.DefaultDiskSizeMB)
	assert.Equal(t, &enableNAT, p.config.EnableIPTablesNAT)
}

func TestFirecrackerProvider_DataDirStructure(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &FirecrackerProviderConfig{
		DataDir: tmpDir,
	}

	_, err := NewFirecrackerProvider(testLogger(), cfg)
	require.NoError(t, err)

	// Verify subdirectories were created
	for _, sub := range []string{"sockets", "disks", "snapshots"} {
		path := filepath.Join(tmpDir, sub)
		info, err := os.Stat(path)
		assert.NoError(t, err, "subdirectory %s should exist", sub)
		assert.True(t, info.IsDir(), "%s should be a directory", sub)
	}
}

func TestFirecrackerProvider_AgentCall_Success(t *testing.T) {
	// Create a mock HTTP server that acts as the in-VM agent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		switch r.URL.Path {
		case "/exec":
			var req ExecRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			assert.NoError(t, err)
			assert.Equal(t, "echo hello", req.Command)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ExecResult{
				Stdout:     "hello\n",
				ExitCode:   0,
				DurationMs: 10,
			})
		case "/read":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(FileReadResult{
				Content:    "1: line one\n",
				TotalLines: 1,
			})
		case "/write":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/list":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(FileListResult{
				Files: []FileInfo{{Path: "/workspace/test.txt", Size: 100}},
			})
		}
	}))
	defer server.Close()

	p := &FirecrackerProvider{
		log:        testLogger(),
		config:     &FirecrackerProviderConfig{},
		vms:        make(map[string]*firecrackerVM),
		httpClient: server.Client(),
	}

	vm := &firecrackerVM{
		id:       "test-vm",
		agentURL: server.URL,
	}
	p.vms["test-vm"] = vm

	// Test Exec
	execResult, err := p.Exec(context.Background(), "test-vm", &ExecRequest{Command: "echo hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello\n", execResult.Stdout)
	assert.Equal(t, 0, execResult.ExitCode)

	// Test ReadFile
	readResult, err := p.ReadFile(context.Background(), "test-vm", &FileReadRequest{FilePath: "/workspace/test.txt"})
	require.NoError(t, err)
	assert.Equal(t, "1: line one\n", readResult.Content)

	// Test WriteFile
	err = p.WriteFile(context.Background(), "test-vm", &FileWriteRequest{FilePath: "/workspace/new.txt", Content: "data"})
	require.NoError(t, err)

	// Test ListFiles
	listResult, err := p.ListFiles(context.Background(), "test-vm", &FileListRequest{Pattern: "*.txt"})
	require.NoError(t, err)
	assert.Len(t, listResult.Files, 1)
	assert.Equal(t, "/workspace/test.txt", listResult.Files[0].Path)
}

func TestFirecrackerProvider_AgentCall_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	p := &FirecrackerProvider{
		log:        testLogger(),
		config:     &FirecrackerProviderConfig{},
		vms:        make(map[string]*firecrackerVM),
		httpClient: server.Client(),
	}

	vm := &firecrackerVM{
		id:       "test-vm",
		agentURL: server.URL,
	}
	p.vms["test-vm"] = vm

	_, err := p.Exec(context.Background(), "test-vm", &ExecRequest{Command: "fail"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestFirecrackerProvider_AgentCall_VMNotFound(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	_, err := p.Exec(context.Background(), "nonexistent", &ExecRequest{Command: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VM not found")
}

func TestFirecrackerProvider_Stop_AlreadyStopped(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	p.vms["vm1"] = &firecrackerVM{id: "vm1", stopped: true}

	err := p.Stop(context.Background(), "vm1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already stopped")
}

func TestFirecrackerProvider_Resume_NotStopped(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
		vms:    make(map[string]*firecrackerVM),
	}

	p.vms["vm1"] = &firecrackerVM{id: "vm1", stopped: false}

	err := p.Resume(context.Background(), "vm1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not stopped")
}

func TestFirecrackerProvider_CreateFromSnapshot_NoKVM(t *testing.T) {
	p := &FirecrackerProvider{
		log:          testLogger(),
		config:       &FirecrackerProviderConfig{},
		vms:          make(map[string]*firecrackerVM),
		kvmAvailable: false,
	}

	_, err := p.CreateFromSnapshot(context.Background(), "snap-123", &CreateContainerRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "KVM is not available")
}

func TestFirecrackerProvider_CreateFromSnapshot_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	p := &FirecrackerProvider{
		log: testLogger(),
		config: &FirecrackerProviderConfig{
			DataDir: tmpDir,
		},
		vms:          make(map[string]*firecrackerVM),
		kvmAvailable: true,
	}

	// Create snapshots directory
	_ = os.MkdirAll(filepath.Join(tmpDir, "snapshots", "test-snap"), 0755)

	_, err := p.CreateFromSnapshot(context.Background(), "test-snap", &CreateContainerRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot file missing")
}

func TestFirecrackerProvider_CopyFile(t *testing.T) {
	p := &FirecrackerProvider{
		log:    testLogger(),
		config: &FirecrackerProviderConfig{},
	}

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")

	// Create source file
	err := os.WriteFile(srcPath, []byte("test data"), 0644)
	require.NoError(t, err)

	// Copy
	err = p.copyFile(srcPath, dstPath)
	require.NoError(t, err)

	// Verify
	data, err := os.ReadFile(dstPath)
	require.NoError(t, err)
	assert.Equal(t, "test data", string(data))
}

func TestFirecrackerProvider_WaitForAgent_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	p := &FirecrackerProvider{
		log:        testLogger(),
		config:     &FirecrackerProviderConfig{},
		httpClient: server.Client(),
	}

	vm := &firecrackerVM{
		id:       "test-vm",
		agentURL: server.URL,
	}

	err := p.waitForAgent(context.Background(), vm)
	assert.NoError(t, err)
}

func TestFirecrackerProvider_WaitForAgent_ContextCancel(t *testing.T) {
	// Server that never responds (we cancel before it would)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	p := &FirecrackerProvider{
		log:        testLogger(),
		config:     &FirecrackerProviderConfig{},
		httpClient: server.Client(),
	}

	vm := &firecrackerVM{
		id:       "test-vm",
		agentURL: server.URL,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := p.waitForAgent(ctx, vm)
	assert.Error(t, err)
}

func TestFirecrackerProvider_IsKVMAvailable(t *testing.T) {
	p := &FirecrackerProvider{
		log:          testLogger(),
		config:       &FirecrackerProviderConfig{},
		kvmAvailable: false,
	}
	assert.False(t, p.IsKVMAvailable())

	p.kvmAvailable = true
	assert.True(t, p.IsKVMAvailable())
}
