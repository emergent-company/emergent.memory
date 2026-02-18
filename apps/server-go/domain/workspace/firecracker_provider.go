package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

const (
	// fcDataDir is the base directory for Firecracker VM data (block devices, sockets, snapshots).
	fcDataDir = "/var/lib/emergent/firecracker"

	// fcKernelPath is the default path to the uncompressed Linux kernel for microVMs.
	fcKernelPath = "/var/lib/emergent/firecracker/vmlinux"

	// fcAgentPort is the port the in-VM agent listens on inside the microVM.
	fcAgentPort = 8080

	// fcDefaultVCPUs is the default number of vCPUs for a microVM.
	fcDefaultVCPUs = 2

	// fcDefaultMemMiB is the default memory in MiB for a microVM.
	fcDefaultMemMiB = 512

	// fcDefaultDiskSizeMB is the default sparse disk size in MB.
	fcDefaultDiskSizeMB = 10240 // 10GB

	// fcSubnetPrefix is the /16 subnet used for VM networking.
	// Each VM gets a unique IP: 172.16.X.Y where X.Y is derived from a counter.
	fcSubnetPrefix = "172.16"

	// fcAgentTimeout is the default HTTP timeout for agent calls.
	fcAgentTimeout = 30 * time.Second

	// fcStartupTimeout is the maximum time to wait for VM startup + agent ready.
	fcStartupTimeout = 30 * time.Second

	// fcKernelArgs is the kernel command line for microVMs.
	fcKernelArgs = "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init random.trust_cpu=on"
)

// FirecrackerProvider implements the Provider interface using Firecracker microVMs.
// Each workspace is an isolated microVM with its own kernel, rootfs, and network.
// Commands are executed via an HTTP agent running inside the VM.
type FirecrackerProvider struct {
	log          *slog.Logger
	config       *FirecrackerProviderConfig
	kvmAvailable bool

	mu sync.RWMutex
	// vms tracks active VMs by provider ID (container ID in Firecracker's socket path).
	vms map[string]*firecrackerVM

	// ipCounter is used to assign unique IPs to VMs within the 172.16.0.0/16 subnet.
	ipCounter atomic.Uint64

	// httpClient is reused for all agent HTTP calls.
	httpClient *http.Client
}

// firecrackerVM tracks the state of a running microVM.
type firecrackerVM struct {
	id          string               // Unique VM identifier
	machine     *firecracker.Machine // The Firecracker machine handle
	socketPath  string               // Unix socket for Firecracker API
	blockDevice string               // Path to rootfs block device file
	dataDevice  string               // Path to data block device file (mounted at /workspace)
	tapDevice   string               // TAP device name on host
	hostIP      string               // Host-side IP (gateway for the VM)
	vmIP        string               // VM-side IP
	agentURL    string               // Base URL for the in-VM agent
	snapshotDir string               // Directory for VM snapshots (if any)
	stopped     bool                 // Whether the VM is paused/stopped
}

// FirecrackerProviderConfig holds configuration for the Firecracker provider.
type FirecrackerProviderConfig struct {
	// DataDir is the base directory for VM data. Defaults to /var/lib/emergent/firecracker.
	DataDir string

	// KernelPath is the path to the uncompressed Linux kernel. Defaults to fcKernelPath.
	KernelPath string

	// RootfsPath is the path to the base rootfs image (read-only, used as template).
	// Each VM gets a copy-on-write overlay of this image.
	RootfsPath string

	// DefaultVCPUs is the default number of vCPUs. Defaults to 2.
	DefaultVCPUs int

	// DefaultMemMiB is the default memory in MiB. Defaults to 512.
	DefaultMemMiB int

	// DefaultDiskSizeMB is the default disk size in MB. Defaults to 10240 (10GB).
	DefaultDiskSizeMB int

	// EnableIPTablesNAT controls whether iptables NAT rules are created for outbound access.
	// Defaults to true.
	EnableIPTablesNAT *bool
}

// NewFirecrackerProvider creates a new Firecracker-based workspace provider.
func NewFirecrackerProvider(log *slog.Logger, cfg *FirecrackerProviderConfig) (*FirecrackerProvider, error) {
	if cfg == nil {
		cfg = &FirecrackerProviderConfig{}
	}

	// Apply defaults
	if cfg.DataDir == "" {
		cfg.DataDir = fcDataDir
	}
	if cfg.KernelPath == "" {
		cfg.KernelPath = fcKernelPath
	}
	if cfg.DefaultVCPUs <= 0 {
		cfg.DefaultVCPUs = fcDefaultVCPUs
	}
	if cfg.DefaultMemMiB <= 0 {
		cfg.DefaultMemMiB = fcDefaultMemMiB
	}
	if cfg.DefaultDiskSizeMB <= 0 {
		cfg.DefaultDiskSizeMB = fcDefaultDiskSizeMB
	}
	if cfg.EnableIPTablesNAT == nil {
		enableNAT := true
		cfg.EnableIPTablesNAT = &enableNAT
	}

	p := &FirecrackerProvider{
		log:    log.With("component", "firecracker-provider"),
		config: cfg,
		vms:    make(map[string]*firecrackerVM),
		httpClient: &http.Client{
			Timeout: fcAgentTimeout,
		},
	}

	// Detect KVM availability
	p.kvmAvailable = p.detectKVM()

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", cfg.DataDir, err)
	}

	// Create subdirectories
	for _, sub := range []string{"sockets", "disks", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, sub), 0755); err != nil {
			return nil, fmt.Errorf("failed to create %s directory: %w", sub, err)
		}
	}

	return p, nil
}

// detectKVM checks if /dev/kvm exists and is accessible.
func (p *FirecrackerProvider) detectKVM() bool {
	info, err := os.Stat("/dev/kvm")
	if err != nil {
		p.log.Warn("KVM not available", "error", err)
		return false
	}
	// Check that it's a character device
	if info.Mode()&os.ModeCharDevice == 0 {
		p.log.Warn("/dev/kvm exists but is not a character device")
		return false
	}
	// Try to open it to check permissions
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		p.log.Warn("KVM exists but not accessible", "error", err)
		return false
	}
	f.Close()
	p.log.Info("KVM available")
	return true
}

// Capabilities returns what this provider supports.
func (p *FirecrackerProvider) Capabilities() *ProviderCapabilities {
	return &ProviderCapabilities{
		Name:                "Firecracker (microVM)",
		SupportsPersistence: true,
		SupportsSnapshots:   true,
		SupportsWarmPool:    true,
		RequiresKVM:         true,
		EstimatedStartupMs:  125,
		ProviderType:        ProviderFirecracker,
	}
}

// Create provisions a new Firecracker microVM with an isolated network and filesystem.
func (p *FirecrackerProvider) Create(ctx context.Context, req *CreateContainerRequest) (*CreateContainerResult, error) {
	if !p.kvmAvailable {
		return nil, fmt.Errorf("KVM is not available — Firecracker requires /dev/kvm")
	}

	// Generate unique VM ID
	vmID := fmt.Sprintf("fc-%d", time.Now().UnixNano())

	// Assign unique IP addresses
	counter := p.ipCounter.Add(1)
	// Split counter into two octets for /16 subnet: 172.16.X.Y
	// X = (counter / 254) + 1, Y = (counter % 254) + 2 (avoiding .0 and .255)
	x := (counter/254)%254 + 1
	y := counter%254 + 2
	vmIP := fmt.Sprintf("%s.%d.%d", fcSubnetPrefix, x, y)
	hostIP := fmt.Sprintf("%s.%d.1", fcSubnetPrefix, x)
	tapName := fmt.Sprintf("fctap-%d", counter)

	// Parse resource limits
	vcpus := int64(p.config.DefaultVCPUs)
	memMiB := int64(p.config.DefaultMemMiB)
	diskSizeMB := p.config.DefaultDiskSizeMB

	if req.ResourceLimits != nil {
		if req.ResourceLimits.CPU != "" {
			if v, err := strconv.ParseFloat(req.ResourceLimits.CPU, 64); err == nil && v > 0 {
				vcpus = int64(v)
				if vcpus < 1 {
					vcpus = 1
				}
			}
		}
		if req.ResourceLimits.Memory != "" {
			mem := parseMemoryBytes(req.ResourceLimits.Memory)
			if mem > 0 {
				memMiB = mem / (1024 * 1024)
				if memMiB < 128 {
					memMiB = 128
				}
			}
		}
		if req.ResourceLimits.Disk != "" {
			disk := parseMemoryBytes(req.ResourceLimits.Disk)
			if disk > 0 {
				diskSizeMB = int(disk / (1024 * 1024))
			}
		}
	}

	// Create sparse block device for data (/workspace)
	dataDevicePath := filepath.Join(p.config.DataDir, "disks", vmID+"-data.ext4")
	if err := p.createSparseExt4(dataDevicePath, diskSizeMB); err != nil {
		return nil, fmt.Errorf("failed to create data block device: %w", err)
	}

	// Set up TAP device and networking
	if err := p.setupTAPDevice(tapName, hostIP); err != nil {
		_ = os.Remove(dataDevicePath)
		return nil, fmt.Errorf("failed to set up TAP device: %w", err)
	}

	// Set up iptables NAT if enabled
	if *p.config.EnableIPTablesNAT {
		if err := p.setupIPTablesNAT(tapName, vmIP); err != nil {
			p.log.Warn("failed to set up iptables NAT", "error", err)
			// Non-fatal — VM will work but without outbound access
		}
	}

	// Socket path for Firecracker API
	socketPath := filepath.Join(p.config.DataDir, "sockets", vmID+".sock")

	// Build Firecracker config
	rootfsPath := p.config.RootfsPath
	if rootfsPath == "" {
		rootfsPath = filepath.Join(p.config.DataDir, "rootfs.ext4")
	}

	drives := []models.Drive{
		{
			DriveID:      firecracker.String("rootfs"),
			PathOnHost:   firecracker.String(rootfsPath),
			IsRootDevice: firecracker.Bool(true),
			IsReadOnly:   firecracker.Bool(true),
		},
		{
			DriveID:      firecracker.String("data"),
			PathOnHost:   firecracker.String(dataDevicePath),
			IsRootDevice: firecracker.Bool(false),
			IsReadOnly:   firecracker.Bool(false),
		},
	}

	networkIfaces := firecracker.NetworkInterfaces{
		{
			StaticConfiguration: &firecracker.StaticNetworkConfiguration{
				MacAddress:  p.generateMAC(counter),
				HostDevName: tapName,
				IPConfiguration: &firecracker.IPConfiguration{
					IPAddr: net.IPNet{
						IP:   net.ParseIP(vmIP),
						Mask: net.CIDRMask(24, 32),
					},
					Gateway:     net.ParseIP(hostIP),
					Nameservers: []string{"8.8.8.8", "8.8.4.4"},
					IfName:      "eth0",
				},
			},
		},
	}

	fcConfig := firecracker.Config{
		SocketPath:        socketPath,
		KernelImagePath:   p.config.KernelPath,
		KernelArgs:        fcKernelArgs,
		Drives:            drives,
		NetworkInterfaces: networkIfaces,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:       firecracker.Int64(vcpus),
			MemSizeMib:      firecracker.Int64(memMiB),
			TrackDirtyPages: true, // Enable for snapshot support
		},
		VMID: vmID,
	}

	// Create and start the machine
	machine, err := firecracker.NewMachine(ctx, fcConfig)
	if err != nil {
		p.cleanupVMResources(vmID, tapName, vmIP, dataDevicePath, socketPath)
		return nil, fmt.Errorf("failed to create Firecracker machine: %w", err)
	}

	if err := machine.Start(ctx); err != nil {
		p.cleanupVMResources(vmID, tapName, vmIP, dataDevicePath, socketPath)
		return nil, fmt.Errorf("failed to start Firecracker VM: %w", err)
	}

	// Build agent URL
	agentURL := fmt.Sprintf("http://%s:%d", vmIP, fcAgentPort)

	vm := &firecrackerVM{
		id:          vmID,
		machine:     machine,
		socketPath:  socketPath,
		blockDevice: rootfsPath,
		dataDevice:  dataDevicePath,
		tapDevice:   tapName,
		hostIP:      hostIP,
		vmIP:        vmIP,
		agentURL:    agentURL,
	}

	// Wait for agent to become ready
	if err := p.waitForAgent(ctx, vm); err != nil {
		_ = machine.StopVMM()
		p.cleanupVMResources(vmID, tapName, vmIP, dataDevicePath, socketPath)
		return nil, fmt.Errorf("VM started but agent not ready: %w", err)
	}

	// Register VM
	p.mu.Lock()
	p.vms[vmID] = vm
	p.mu.Unlock()

	p.log.Info("Firecracker VM created",
		"vm_id", vmID,
		"vcpus", vcpus,
		"mem_mib", memMiB,
		"vm_ip", vmIP,
		"tap", tapName,
	)

	return &CreateContainerResult{ProviderID: vmID}, nil
}

// Destroy permanently removes a Firecracker VM and all its resources.
func (p *FirecrackerProvider) Destroy(ctx context.Context, providerID string) error {
	p.mu.Lock()
	vm, ok := p.vms[providerID]
	if ok {
		delete(p.vms, providerID)
	}
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("VM not found: %s", providerID)
	}

	// Stop the VM
	if vm.machine != nil {
		_ = vm.machine.Shutdown(ctx)
		// Give it a moment to shut down gracefully
		time.Sleep(500 * time.Millisecond)
		_ = vm.machine.StopVMM()
	}

	// Clean up all resources
	p.cleanupVMResources(vm.id, vm.tapDevice, vm.vmIP, vm.dataDevice, vm.socketPath)

	// Clean up snapshot directory if it exists
	if vm.snapshotDir != "" {
		_ = os.RemoveAll(vm.snapshotDir)
	}

	p.log.Info("Firecracker VM destroyed", "vm_id", providerID)
	return nil
}

// Stop pauses a running Firecracker VM, preserving its state.
func (p *FirecrackerProvider) Stop(ctx context.Context, providerID string) error {
	p.mu.RLock()
	vm, ok := p.vms[providerID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("VM not found: %s", providerID)
	}

	if vm.stopped {
		return fmt.Errorf("VM is already stopped: %s", providerID)
	}

	if err := vm.machine.PauseVM(ctx); err != nil {
		return fmt.Errorf("failed to pause VM: %w", err)
	}

	p.mu.Lock()
	vm.stopped = true
	p.mu.Unlock()

	p.log.Info("Firecracker VM stopped", "vm_id", providerID)
	return nil
}

// Resume resumes a previously stopped Firecracker VM.
func (p *FirecrackerProvider) Resume(ctx context.Context, providerID string) error {
	p.mu.RLock()
	vm, ok := p.vms[providerID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("VM not found: %s", providerID)
	}

	if !vm.stopped {
		return fmt.Errorf("VM is not stopped: %s", providerID)
	}

	if err := vm.machine.ResumeVM(ctx); err != nil {
		return fmt.Errorf("failed to resume VM: %w", err)
	}

	p.mu.Lock()
	vm.stopped = false
	p.mu.Unlock()

	// Wait for agent to become ready again
	if err := p.waitForAgent(ctx, vm); err != nil {
		p.log.Warn("agent not responsive after resume", "vm_id", providerID, "error", err)
	}

	p.log.Info("Firecracker VM resumed", "vm_id", providerID)
	return nil
}

// Exec executes a command inside a Firecracker VM via the in-VM HTTP agent.
func (p *FirecrackerProvider) Exec(ctx context.Context, providerID string, req *ExecRequest) (*ExecResult, error) {
	vm, err := p.getVM(providerID)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal exec request: %w", err)
	}

	// Use a longer timeout for exec operations
	timeout := fcAgentTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs)*time.Millisecond + 5*time.Second
	}

	httpCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, vm.agentURL+"/exec", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call VM agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("VM agent exec failed (status %d): %s", resp.StatusCode, string(errBody))
	}

	var result ExecResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode exec result: %w", err)
	}

	return &result, nil
}

// ReadFile reads file content from a Firecracker VM via the in-VM HTTP agent.
func (p *FirecrackerProvider) ReadFile(ctx context.Context, providerID string, req *FileReadRequest) (*FileReadResult, error) {
	vm, err := p.getVM(providerID)
	if err != nil {
		return nil, err
	}

	var result FileReadResult
	if err := p.agentCall(ctx, vm, "/read", req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// WriteFile writes file content to a Firecracker VM via the in-VM HTTP agent.
func (p *FirecrackerProvider) WriteFile(ctx context.Context, providerID string, req *FileWriteRequest) error {
	vm, err := p.getVM(providerID)
	if err != nil {
		return err
	}

	var result map[string]any
	return p.agentCall(ctx, vm, "/write", req, &result)
}

// ListFiles returns files matching a glob pattern inside a Firecracker VM.
func (p *FirecrackerProvider) ListFiles(ctx context.Context, providerID string, req *FileListRequest) (*FileListResult, error) {
	vm, err := p.getVM(providerID)
	if err != nil {
		return nil, err
	}

	var result FileListResult
	if err := p.agentCall(ctx, vm, "/list", req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Snapshot creates a point-in-time snapshot of a Firecracker VM's state.
// Uses Firecracker's native snapshot mechanism: PauseVM → CreateSnapshot → files on disk.
func (p *FirecrackerProvider) Snapshot(ctx context.Context, providerID string) (string, error) {
	p.mu.RLock()
	vm, ok := p.vms[providerID]
	p.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("VM not found: %s", providerID)
	}

	snapshotID := fmt.Sprintf("fc-snap-%d", time.Now().UnixNano())
	snapshotDir := filepath.Join(p.config.DataDir, "snapshots", snapshotID)
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	memPath := filepath.Join(snapshotDir, "mem")
	snapPath := filepath.Join(snapshotDir, "vmstate")

	// Pause VM before snapshot
	wasPaused := vm.stopped
	if !wasPaused {
		if err := vm.machine.PauseVM(ctx); err != nil {
			_ = os.RemoveAll(snapshotDir)
			return "", fmt.Errorf("failed to pause VM for snapshot: %w", err)
		}
	}

	// Create snapshot
	if err := vm.machine.CreateSnapshot(ctx, memPath, snapPath); err != nil {
		if !wasPaused {
			_ = vm.machine.ResumeVM(ctx)
		}
		_ = os.RemoveAll(snapshotDir)
		return "", fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Also copy the data block device
	dataSnapPath := filepath.Join(snapshotDir, "data.ext4")
	if err := p.copyFile(vm.dataDevice, dataSnapPath); err != nil {
		if !wasPaused {
			_ = vm.machine.ResumeVM(ctx)
		}
		_ = os.RemoveAll(snapshotDir)
		return "", fmt.Errorf("failed to snapshot data device: %w", err)
	}

	// Resume if wasn't already paused
	if !wasPaused {
		if err := vm.machine.ResumeVM(ctx); err != nil {
			p.log.Warn("failed to resume VM after snapshot", "vm_id", providerID, "error", err)
		}
		p.mu.Lock()
		vm.stopped = false
		p.mu.Unlock()
	}

	p.mu.Lock()
	vm.snapshotDir = snapshotDir
	p.mu.Unlock()

	p.log.Info("Firecracker VM snapshot created",
		"vm_id", providerID,
		"snapshot_id", snapshotID,
	)

	return snapshotID, nil
}

// CreateFromSnapshot creates a new Firecracker VM from a previously-taken snapshot.
func (p *FirecrackerProvider) CreateFromSnapshot(ctx context.Context, snapshotID string, req *CreateContainerRequest) (*CreateContainerResult, error) {
	if !p.kvmAvailable {
		return nil, fmt.Errorf("KVM is not available — Firecracker requires /dev/kvm")
	}

	snapshotDir := filepath.Join(p.config.DataDir, "snapshots", snapshotID)

	// Verify snapshot exists
	memPath := filepath.Join(snapshotDir, "mem")
	snapPath := filepath.Join(snapshotDir, "vmstate")
	dataSnapPath := filepath.Join(snapshotDir, "data.ext4")

	for _, path := range []string{memPath, snapPath, dataSnapPath} {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("snapshot file missing: %s: %w", path, err)
		}
	}

	// Generate unique VM ID
	vmID := fmt.Sprintf("fc-%d", time.Now().UnixNano())

	// Assign unique IP
	counter := p.ipCounter.Add(1)
	x := (counter/254)%254 + 1
	y := counter%254 + 2
	vmIP := fmt.Sprintf("%s.%d.%d", fcSubnetPrefix, x, y)
	hostIP := fmt.Sprintf("%s.%d.1", fcSubnetPrefix, x)
	tapName := fmt.Sprintf("fctap-%d", counter)

	// Copy data device from snapshot
	dataDevicePath := filepath.Join(p.config.DataDir, "disks", vmID+"-data.ext4")
	if err := p.copyFile(dataSnapPath, dataDevicePath); err != nil {
		return nil, fmt.Errorf("failed to restore data device from snapshot: %w", err)
	}

	// Set up TAP device
	if err := p.setupTAPDevice(tapName, hostIP); err != nil {
		_ = os.Remove(dataDevicePath)
		return nil, fmt.Errorf("failed to set up TAP device: %w", err)
	}

	if *p.config.EnableIPTablesNAT {
		if err := p.setupIPTablesNAT(tapName, vmIP); err != nil {
			p.log.Warn("failed to set up iptables NAT for restored VM", "error", err)
		}
	}

	socketPath := filepath.Join(p.config.DataDir, "sockets", vmID+".sock")

	rootfsPath := p.config.RootfsPath
	if rootfsPath == "" {
		rootfsPath = filepath.Join(p.config.DataDir, "rootfs.ext4")
	}

	// Build config for snapshot restoration
	drives := []models.Drive{
		{
			DriveID:      firecracker.String("rootfs"),
			PathOnHost:   firecracker.String(rootfsPath),
			IsRootDevice: firecracker.Bool(true),
			IsReadOnly:   firecracker.Bool(true),
		},
		{
			DriveID:      firecracker.String("data"),
			PathOnHost:   firecracker.String(dataDevicePath),
			IsRootDevice: firecracker.Bool(false),
			IsReadOnly:   firecracker.Bool(false),
		},
	}

	networkIfaces := firecracker.NetworkInterfaces{
		{
			StaticConfiguration: &firecracker.StaticNetworkConfiguration{
				MacAddress:  p.generateMAC(counter),
				HostDevName: tapName,
				IPConfiguration: &firecracker.IPConfiguration{
					IPAddr: net.IPNet{
						IP:   net.ParseIP(vmIP),
						Mask: net.CIDRMask(24, 32),
					},
					Gateway:     net.ParseIP(hostIP),
					Nameservers: []string{"8.8.8.8", "8.8.4.4"},
					IfName:      "eth0",
				},
			},
		},
	}

	fcConfig := firecracker.Config{
		SocketPath:        socketPath,
		KernelImagePath:   p.config.KernelPath,
		Drives:            drives,
		NetworkInterfaces: networkIfaces,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:       firecracker.Int64(int64(p.config.DefaultVCPUs)),
			MemSizeMib:      firecracker.Int64(int64(p.config.DefaultMemMiB)),
			TrackDirtyPages: true,
		},
		VMID: vmID,
		// Load from snapshot
		Snapshot: firecracker.SnapshotConfig{
			MemFilePath:  memPath,
			SnapshotPath: snapPath,
			ResumeVM:     true,
		},
	}

	machine, err := firecracker.NewMachine(ctx, fcConfig)
	if err != nil {
		p.cleanupVMResources(vmID, tapName, vmIP, dataDevicePath, socketPath)
		return nil, fmt.Errorf("failed to create Firecracker machine from snapshot: %w", err)
	}

	if err := machine.Start(ctx); err != nil {
		p.cleanupVMResources(vmID, tapName, vmIP, dataDevicePath, socketPath)
		return nil, fmt.Errorf("failed to start Firecracker VM from snapshot: %w", err)
	}

	agentURL := fmt.Sprintf("http://%s:%d", vmIP, fcAgentPort)

	vm := &firecrackerVM{
		id:          vmID,
		machine:     machine,
		socketPath:  socketPath,
		blockDevice: rootfsPath,
		dataDevice:  dataDevicePath,
		tapDevice:   tapName,
		hostIP:      hostIP,
		vmIP:        vmIP,
		agentURL:    agentURL,
	}

	// Wait for agent
	if err := p.waitForAgent(ctx, vm); err != nil {
		_ = machine.StopVMM()
		p.cleanupVMResources(vmID, tapName, vmIP, dataDevicePath, socketPath)
		return nil, fmt.Errorf("VM restored from snapshot but agent not ready: %w", err)
	}

	p.mu.Lock()
	p.vms[vmID] = vm
	p.mu.Unlock()

	p.log.Info("Firecracker VM created from snapshot",
		"vm_id", vmID,
		"snapshot_id", snapshotID,
		"vm_ip", vmIP,
	)

	return &CreateContainerResult{ProviderID: vmID}, nil
}

// Health checks KVM availability and reports active VM count.
func (p *FirecrackerProvider) Health(_ context.Context) (*HealthStatus, error) {
	p.mu.RLock()
	activeCount := len(p.vms)
	p.mu.RUnlock()

	if !p.kvmAvailable {
		return &HealthStatus{
			Healthy: false,
			Message: "KVM not available — /dev/kvm not accessible",
		}, nil
	}

	return &HealthStatus{
		Healthy:     true,
		Message:     fmt.Sprintf("KVM available, %d active VMs", activeCount),
		ActiveCount: activeCount,
	}, nil
}

// --- Internal Helpers ---

// getVM retrieves a VM by provider ID, returning an error if not found or stopped.
func (p *FirecrackerProvider) getVM(providerID string) (*firecrackerVM, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	vm, ok := p.vms[providerID]
	if !ok {
		return nil, fmt.Errorf("VM not found: %s", providerID)
	}
	if vm.stopped {
		return nil, fmt.Errorf("VM is stopped: %s", providerID)
	}
	return vm, nil
}

// agentCall makes an HTTP POST call to the in-VM agent and decodes the response.
func (p *FirecrackerProvider) agentCall(ctx context.Context, vm *firecrackerVM, path string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpCtx, cancel := context.WithTimeout(ctx, fcAgentTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, vm.agentURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to call VM agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("VM agent call to %s failed (status %d): %s", path, resp.StatusCode, string(errBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response from %s: %w", path, err)
		}
	}

	return nil
}

// waitForAgent polls the VM agent health endpoint until it responds or times out.
func (p *FirecrackerProvider) waitForAgent(ctx context.Context, vm *firecrackerVM) error {
	deadline := time.Now().Add(fcStartupTimeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, vm.agentURL+"/health", nil)
		if err != nil {
			cancel()
			continue
		}

		resp, err := p.httpClient.Do(req)
		cancel()

		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				p.log.Debug("VM agent ready", "vm_id", vm.id)
				return nil
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("VM agent at %s did not become ready within %v", vm.agentURL, fcStartupTimeout)
}

// createSparseExt4 creates a sparse ext4 filesystem file of the given size.
func (p *FirecrackerProvider) createSparseExt4(path string, sizeMB int) error {
	// Create sparse file
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create sparse file: %w", err)
	}

	if err := f.Truncate(int64(sizeMB) * 1024 * 1024); err != nil {
		f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("failed to set sparse file size: %w", err)
	}
	f.Close()

	// Format as ext4
	cmd := exec.Command("mkfs.ext4", "-F", "-q", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("failed to format ext4: %s: %w", string(output), err)
	}

	return nil
}

// setupTAPDevice creates a TAP network device and assigns an IP address.
func (p *FirecrackerProvider) setupTAPDevice(tapName, hostIP string) error {
	// Create TAP device
	cmd := exec.Command("ip", "tuntap", "add", tapName, "mode", "tap")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create TAP device %s: %s: %w", tapName, string(output), err)
	}

	// Assign IP address
	cmd = exec.Command("ip", "addr", "add", hostIP+"/24", "dev", tapName)
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = exec.Command("ip", "link", "delete", tapName).Run()
		return fmt.Errorf("failed to assign IP to %s: %s: %w", tapName, string(output), err)
	}

	// Bring up the interface
	cmd = exec.Command("ip", "link", "set", tapName, "up")
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = exec.Command("ip", "link", "delete", tapName).Run()
		return fmt.Errorf("failed to bring up %s: %s: %w", tapName, string(output), err)
	}

	return nil
}

// setupIPTablesNAT configures iptables rules for outbound NAT from the VM.
func (p *FirecrackerProvider) setupIPTablesNAT(tapName, vmIP string) error {
	// Enable IP forwarding
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644); err != nil {
		return fmt.Errorf("failed to enable IP forwarding: %w", err)
	}

	// Add NAT masquerade rule
	cmd := exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
		"-s", vmIP+"/32", "-o", "eth0", "-j", "MASQUERADE")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add NAT rule: %s: %w", string(output), err)
	}

	// Allow forwarding from TAP to outbound
	cmd = exec.Command("iptables", "-A", "FORWARD",
		"-i", tapName, "-o", "eth0", "-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add forward rule: %s: %w", string(output), err)
	}

	// Allow return traffic
	cmd = exec.Command("iptables", "-A", "FORWARD",
		"-i", "eth0", "-o", tapName,
		"-m", "state", "--state", "ESTABLISHED,RELATED",
		"-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add return traffic rule: %s: %w", string(output), err)
	}

	return nil
}

// cleanupVMResources removes TAP device, block device, and socket file.
func (p *FirecrackerProvider) cleanupVMResources(vmID, tapName, vmIP, dataDevice, socketPath string) {
	// Remove TAP device
	if tapName != "" {
		// Clean up iptables rules for this specific VM (best effort)
		if vmIP != "" {
			_ = exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING",
				"-s", vmIP+"/32", "-o", "eth0", "-j", "MASQUERADE").Run()
			_ = exec.Command("iptables", "-D", "FORWARD",
				"-i", tapName, "-o", "eth0", "-j", "ACCEPT").Run()
			_ = exec.Command("iptables", "-D", "FORWARD",
				"-i", "eth0", "-o", tapName,
				"-m", "state", "--state", "ESTABLISHED,RELATED",
				"-j", "ACCEPT").Run()
		}

		cmd := exec.Command("ip", "link", "delete", tapName)
		if err := cmd.Run(); err != nil {
			p.log.Warn("failed to remove TAP device", "tap", tapName, "error", err)
		}
	}

	// Remove data block device
	if dataDevice != "" {
		if err := os.Remove(dataDevice); err != nil && !os.IsNotExist(err) {
			p.log.Warn("failed to remove data block device", "path", dataDevice, "error", err)
		}
	}

	// Remove socket
	if socketPath != "" {
		_ = os.Remove(socketPath)
	}
}

// generateMAC generates a unique MAC address for a VM based on a counter.
func (p *FirecrackerProvider) generateMAC(counter uint64) string {
	return fmt.Sprintf("AA:FC:00:%02X:%02X:%02X",
		(counter>>16)&0xFF,
		(counter>>8)&0xFF,
		counter&0xFF,
	)
}

// copyFile copies a file from src to dst using cp --reflink=auto for CoW when available.
func (p *FirecrackerProvider) copyFile(src, dst string) error {
	// Try copy-on-write first (efficient on btrfs/xfs)
	cmd := exec.Command("cp", "--reflink=auto", src, dst)
	if err := cmd.Run(); err != nil {
		// Fall back to regular copy
		srcFile, err := os.Open(src)
		if err != nil {
			return fmt.Errorf("failed to open source file: %w", err)
		}
		defer srcFile.Close()

		dstFile, err := os.Create(dst)
		if err != nil {
			return fmt.Errorf("failed to create destination file: %w", err)
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}
	}
	return nil
}

// ActiveVMs returns the number of currently running VMs.
func (p *FirecrackerProvider) ActiveVMs() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.vms)
}

// IsKVMAvailable returns whether KVM is available on this host.
func (p *FirecrackerProvider) IsKVMAvailable() bool {
	return p.kvmAvailable
}
