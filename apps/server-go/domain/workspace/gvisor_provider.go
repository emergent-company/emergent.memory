package workspace

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const (
	defaultWorkspaceImage = "emergent-workspace:latest"
	gvisorRuntime         = "runsc"
	defaultRuntimeLabel   = "emergent.workspace"
	workspaceDir          = "/workspace"
	maxOutputBytes        = 50 * 1024 // 50KB output limit
)

// GVisorProvider implements the Provider interface using Docker with gVisor (runsc) runtime.
// Falls back to standard Docker runtime when gVisor is not available.
type GVisorProvider struct {
	client       client.APIClient
	log          *slog.Logger
	useGVisor    bool   // Whether gVisor runtime is available
	runtimeName  string // "runsc" or "" (default)
	networkName  string // Docker network for container isolation
	defaultImage string // Override for default workspace image
}

// GVisorProviderConfig holds configuration for the gVisor provider.
type GVisorProviderConfig struct {
	// ForceStandardRuntime disables gVisor even if available (for testing).
	ForceStandardRuntime bool
	// NetworkName is the Docker network to attach workspace containers to (e.g. "workspace_net").
	// When set, containers are isolated from infrastructure services.
	// Leave empty to use the default Docker bridge network.
	NetworkName string
	// DefaultImage overrides the default workspace base image.
	// When empty, falls back to the package constant (emergent-workspace:latest).
	DefaultImage string
}

// NewGVisorProvider creates a new gVisor-based workspace provider.
func NewGVisorProvider(log *slog.Logger, cfg *GVisorProviderConfig) (*GVisorProvider, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	p := &GVisorProvider{
		client: cli,
		log:    log.With("component", "gvisor-provider"),
	}

	if cfg != nil {
		p.networkName = cfg.NetworkName
		p.defaultImage = cfg.DefaultImage
	}

	if cfg != nil && cfg.ForceStandardRuntime {
		p.useGVisor = false
		p.runtimeName = ""
		p.log.Warn("gVisor runtime disabled by configuration, using standard Docker runtime")
	} else {
		// Detect gVisor runtime availability
		p.detectRuntime(context.Background())
	}

	return p, nil
}

// detectRuntime checks if the gVisor (runsc) runtime is available on the Docker daemon.
func (p *GVisorProvider) detectRuntime(ctx context.Context) {
	info, err := p.client.Info(ctx)
	if err != nil {
		p.log.Warn("failed to get Docker info, falling back to standard runtime", "error", err)
		p.useGVisor = false
		p.runtimeName = ""
		return
	}

	for name := range info.Runtimes {
		if name == gvisorRuntime {
			p.useGVisor = true
			p.runtimeName = gvisorRuntime
			p.log.Info("gVisor runtime detected, using runsc for workspace isolation")
			return
		}
	}

	p.useGVisor = false
	p.runtimeName = ""
	p.log.Warn("gVisor runtime (runsc) not found, falling back to standard Docker runtime — reduced isolation")
}

// Capabilities returns what this provider supports.
func (p *GVisorProvider) Capabilities() *ProviderCapabilities {
	return &ProviderCapabilities{
		Name:                "gVisor (Docker)",
		SupportsPersistence: true,
		SupportsSnapshots:   true,
		SupportsWarmPool:    true,
		RequiresKVM:         false,
		EstimatedStartupMs:  50,
		ProviderType:        ProviderGVisor,
	}
}

// Create provisions a new workspace container with gVisor runtime and named volume.
func (p *GVisorProvider) Create(ctx context.Context, req *CreateContainerRequest) (*CreateContainerResult, error) {
	p.log.Info("starting workspace container creation",
		"container_type", req.ContainerType,
		"base_image_override", req.BaseImage,
	)

	image := defaultWorkspaceImage
	if p.defaultImage != "" {
		image = p.defaultImage
	}
	if req.BaseImage != "" {
		image = req.BaseImage
	}

	p.log.Info("using workspace image", "image", image)

	// Ensure image is available
	p.log.Info("ensuring image is available", "image", image)
	if err := p.ensureImage(ctx, image); err != nil {
		p.log.Error("failed to ensure image", "image", image, "error", err)
		return nil, fmt.Errorf("failed to ensure image %s: %w", image, err)
	}
	p.log.Info("image is ready", "image", image)

	// Generate volume name
	volumeName := fmt.Sprintf("emergent-workspace-%d", time.Now().UnixNano())
	p.log.Info("creating volume", "volume_name", volumeName)

	// Create named volume for persistence
	_, err := p.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: volumeName,
		Labels: map[string]string{
			defaultRuntimeLabel: "true",
			"workspace.type":    string(req.ContainerType),
		},
	})
	if err != nil {
		p.log.Error("failed to create volume", "volume_name", volumeName, "error", err)
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}
	p.log.Info("volume created successfully", "volume_name", volumeName)

	// Determine command — MCP containers use custom commands, workspaces use sleep
	cmd := []string{"sleep", "infinity"}
	if len(req.Cmd) > 0 {
		cmd = req.Cmd
	}

	// Build container config
	containerConfig := &container.Config{
		Image: image,
		Cmd:   cmd,
		Labels: map[string]string{
			defaultRuntimeLabel: "true",
			"workspace.type":    string(req.ContainerType),
			"workspace.volume":  volumeName,
		},
		WorkingDir: workspaceDir,
	}

	// Set stdin attachment for stdio bridge (MCP servers)
	if req.AttachStdin {
		containerConfig.OpenStdin = true
		containerConfig.StdinOnce = false
		containerConfig.AttachStdin = true
		containerConfig.AttachStdout = true
		containerConfig.AttachStderr = true
	}

	// Inject default environment variables for API discoverability
	containerConfig.Env = append(containerConfig.Env, "EMERGENT_API_URL=http://host.docker.internal:3002")

	// Set caller-provided environment variables (may override defaults)
	if len(req.Env) > 0 {
		for k, v := range req.Env {
			containerConfig.Env = append(containerConfig.Env, k+"="+v)
		}
	}

	// Merge caller-provided labels
	for k, v := range req.Labels {
		containerConfig.Labels[k] = v
	}

	// Build host config with resource limits and volume mount
	mounts := []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: volumeName,
			Target: workspaceDir,
		},
	}

	// Add extra volume mounts (for MCP persistent data)
	for i, mountPath := range req.ExtraVolumes {
		extraVolumeName := fmt.Sprintf("%s-extra-%d", volumeName, i)
		_, err := p.client.VolumeCreate(ctx, volume.CreateOptions{
			Name: extraVolumeName,
			Labels: map[string]string{
				defaultRuntimeLabel: "true",
				"workspace.type":    string(req.ContainerType),
				"workspace.parent":  volumeName,
			},
		})
		if err != nil {
			// Clean up primary volume on failure
			_ = p.client.VolumeRemove(ctx, volumeName, true)
			return nil, fmt.Errorf("failed to create extra volume for %s: %w", mountPath, err)
		}
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: extraVolumeName,
			Target: mountPath,
		})
	}

	hostConfig := &container.HostConfig{
		Mounts:        mounts,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
		// Ensure host.docker.internal resolves on Linux Docker (automatic on Docker Desktop).
		// This allows workspace containers to reach the Emergent API on the host.
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}

	// Set gVisor runtime if available
	if p.useGVisor {
		hostConfig.Runtime = p.runtimeName
	}

	// Apply resource limits
	if req.ResourceLimits != nil {
		p.applyResourceLimits(hostConfig, req.ResourceLimits)
	}

	// Create container
	var networkConfig *network.NetworkingConfig
	if p.networkName != "" {
		p.log.Info("attaching container to network", "network", p.networkName)
		networkConfig = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				p.networkName: {},
			},
		}
	}

	p.log.Info("creating Docker container",
		"image", image,
		"runtime", p.runtimeName,
		"volume", volumeName,
		"workdir", workspaceDir,
	)

	containerName := fmt.Sprintf("emergent-ws-%d", time.Now().UnixNano())
	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		p.log.Error("failed to create Docker container", "image", image, "error", err)
		// Clean up volume on failure
		_ = p.client.VolumeRemove(ctx, volumeName, true)
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	p.log.Info("Docker container created successfully", "container_id", resp.ID[:12], "name", containerName)

	// Start container
	p.log.Info("starting Docker container", "container_id", resp.ID[:12])
	if err := p.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		p.log.Error("failed to start Docker container", "container_id", resp.ID[:12], "error", err)
		// Clean up on failure
		_ = p.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		_ = p.client.VolumeRemove(ctx, volumeName, true)
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	p.log.Info("workspace container created",
		"container_id", resp.ID[:12],
		"image", image,
		"runtime", p.runtimeName,
		"volume", volumeName,
	)

	return &CreateContainerResult{
		ProviderID: resp.ID,
	}, nil
}

// Destroy permanently removes a workspace container and its volume.
func (p *GVisorProvider) Destroy(ctx context.Context, providerID string) error {
	// Get volume name from container labels before removing
	volumeName := ""
	info, err := p.client.ContainerInspect(ctx, providerID)
	if err == nil {
		volumeName = info.Config.Labels["workspace.volume"]
	}

	// Remove container (force kill if running)
	if err := p.client.ContainerRemove(ctx, providerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: false, // We handle volume separately
	}); err != nil {
		if !client.IsErrNotFound(err) {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	}

	// Remove associated volume
	if volumeName != "" {
		if err := p.client.VolumeRemove(ctx, volumeName, true); err != nil {
			p.log.Warn("failed to remove workspace volume", "volume", volumeName, "error", err)
		}
	}

	p.log.Info("workspace container destroyed", "container_id", providerID[:min(12, len(providerID))])
	return nil
}

// Stop pauses a workspace container.
func (p *GVisorProvider) Stop(ctx context.Context, providerID string) error {
	timeout := 10
	if err := p.client.ContainerStop(ctx, providerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	p.log.Info("workspace container stopped", "container_id", providerID[:min(12, len(providerID))])
	return nil
}

// Resume restarts a stopped workspace container.
func (p *GVisorProvider) Resume(ctx context.Context, providerID string) error {
	if err := p.client.ContainerStart(ctx, providerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to resume container: %w", err)
	}
	p.log.Info("workspace container resumed", "container_id", providerID[:min(12, len(providerID))])
	return nil
}

// Snapshot creates a point-in-time snapshot of a workspace container's filesystem.
// For gVisor, this creates a new Docker volume by copying data from the workspace's volume.
func (p *GVisorProvider) Snapshot(ctx context.Context, providerID string) (string, error) {
	// Get the volume name from the container's labels
	info, err := p.client.ContainerInspect(ctx, providerID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	volumeName := info.Config.Labels["workspace.volume"]
	if volumeName == "" {
		return "", fmt.Errorf("container has no workspace volume label")
	}

	// Create snapshot volume
	snapshotID := fmt.Sprintf("emergent-snapshot-%d", time.Now().UnixNano())
	_, err = p.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: snapshotID,
		Labels: map[string]string{
			defaultRuntimeLabel:   "true",
			"workspace.type":      "snapshot",
			"workspace.source":    volumeName,
			"workspace.container": providerID,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create snapshot volume: %w", err)
	}

	// Copy data from source volume to snapshot volume using a temporary container.
	// Mount both volumes and use cp to copy all data.
	copyCmd := []string{"sh", "-c", "cp -a /source/. /snapshot/"}
	copyConfig := &container.Config{
		Image: info.Config.Image,
		Cmd:   copyCmd,
	}
	copyHostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: volumeName, Target: "/source", ReadOnly: true},
			{Type: mount.TypeVolume, Source: snapshotID, Target: "/snapshot"},
		},
	}

	snapCopyName := fmt.Sprintf("emergent-ws-snap-%d", time.Now().UnixNano())
	copyResp, err := p.client.ContainerCreate(ctx, copyConfig, copyHostConfig, nil, nil, snapCopyName)
	if err != nil {
		_ = p.client.VolumeRemove(ctx, snapshotID, true)
		return "", fmt.Errorf("failed to create snapshot copy container: %w", err)
	}
	defer func() {
		_ = p.client.ContainerRemove(ctx, copyResp.ID, container.RemoveOptions{Force: true})
	}()

	if err := p.client.ContainerStart(ctx, copyResp.ID, container.StartOptions{}); err != nil {
		_ = p.client.VolumeRemove(ctx, snapshotID, true)
		return "", fmt.Errorf("failed to start snapshot copy: %w", err)
	}

	// Wait for copy to complete
	statusCh, errCh := p.client.ContainerWait(ctx, copyResp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			_ = p.client.VolumeRemove(ctx, snapshotID, true)
			return "", fmt.Errorf("failed waiting for snapshot copy: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			_ = p.client.VolumeRemove(ctx, snapshotID, true)
			return "", fmt.Errorf("snapshot copy failed with exit code %d", status.StatusCode)
		}
	}

	p.log.Info("workspace snapshot created",
		"container_id", providerID[:min(12, len(providerID))],
		"source_volume", volumeName,
		"snapshot_id", snapshotID,
	)

	return snapshotID, nil
}

// CreateFromSnapshot creates a new workspace container from a previously-taken snapshot.
func (p *GVisorProvider) CreateFromSnapshot(ctx context.Context, snapshotID string, req *CreateContainerRequest) (*CreateContainerResult, error) {
	// Verify the snapshot volume exists
	_, err := p.client.VolumeInspect(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("snapshot volume not found: %s: %w", snapshotID, err)
	}

	imgRef := defaultWorkspaceImage
	if p.defaultImage != "" {
		imgRef = p.defaultImage
	}
	if req.BaseImage != "" {
		imgRef = req.BaseImage
	}

	if err := p.ensureImage(ctx, imgRef); err != nil {
		return nil, fmt.Errorf("failed to ensure image %s: %w", imgRef, err)
	}

	// Create a new workspace volume and copy snapshot data into it
	volumeName := fmt.Sprintf("emergent-workspace-%d", time.Now().UnixNano())
	_, err = p.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: volumeName,
		Labels: map[string]string{
			defaultRuntimeLabel:       "true",
			"workspace.type":          string(req.ContainerType),
			"workspace.from_snapshot": snapshotID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace volume: %w", err)
	}

	// Copy snapshot data to new volume
	copyCmd := []string{"sh", "-c", "cp -a /snapshot/. /workspace/"}
	copyConfig := &container.Config{
		Image: imgRef,
		Cmd:   copyCmd,
	}
	copyHostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: snapshotID, Target: "/snapshot", ReadOnly: true},
			{Type: mount.TypeVolume, Source: volumeName, Target: "/workspace"},
		},
	}

	restoreCopyName := fmt.Sprintf("emergent-ws-restore-%d", time.Now().UnixNano())
	copyResp, err := p.client.ContainerCreate(ctx, copyConfig, copyHostConfig, nil, nil, restoreCopyName)
	if err != nil {
		_ = p.client.VolumeRemove(ctx, volumeName, true)
		return nil, fmt.Errorf("failed to create restore copy container: %w", err)
	}

	// Ensure copy container is removed if we return early
	defer func() {
		if copyResp.ID != "" {
			_ = p.client.ContainerRemove(ctx, copyResp.ID, container.RemoveOptions{Force: true})
		}
	}()

	if err := p.client.ContainerStart(ctx, copyResp.ID, container.StartOptions{}); err != nil {
		// defer will handle container removal
		_ = p.client.VolumeRemove(ctx, volumeName, true)
		return nil, fmt.Errorf("failed to start restore copy: %w", err)
	}

	// Wait for copy
	statusCh, errCh := p.client.ContainerWait(ctx, copyResp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			// defer will handle container removal
			_ = p.client.VolumeRemove(ctx, volumeName, true)
			return nil, fmt.Errorf("failed waiting for restore copy: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			// defer will handle container removal
			_ = p.client.VolumeRemove(ctx, volumeName, true)
			return nil, fmt.Errorf("restore copy failed with exit code %d", status.StatusCode)
		}
	}
	// defer will handle container removal

	// Now create the actual workspace container with the restored volume
	cmd := []string{"sleep", "infinity"}
	if len(req.Cmd) > 0 {
		cmd = req.Cmd
	}

	containerConfig := &container.Config{
		Image: imgRef,
		Cmd:   cmd,
		Labels: map[string]string{
			defaultRuntimeLabel:       "true",
			"workspace.type":          string(req.ContainerType),
			"workspace.volume":        volumeName,
			"workspace.from_snapshot": snapshotID,
		},
		WorkingDir: workspaceDir,
	}

	for k, v := range req.Labels {
		containerConfig.Labels[k] = v
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: volumeName, Target: workspaceDir},
		},
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
		ExtraHosts:    []string{"host.docker.internal:host-gateway"},
	}

	if p.useGVisor {
		hostConfig.Runtime = p.runtimeName
	}

	if req.ResourceLimits != nil {
		p.applyResourceLimits(hostConfig, req.ResourceLimits)
	}

	containerName := fmt.Sprintf("emergent-ws-%d", time.Now().UnixNano())
	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		_ = p.client.VolumeRemove(ctx, volumeName, true)
		return nil, fmt.Errorf("failed to create container from snapshot: %w", err)
	}

	if err := p.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = p.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		_ = p.client.VolumeRemove(ctx, volumeName, true)
		return nil, fmt.Errorf("failed to start container from snapshot: %w", err)
	}

	p.log.Info("workspace created from snapshot",
		"container_id", resp.ID[:12],
		"snapshot_id", snapshotID,
		"volume", volumeName,
	)

	return &CreateContainerResult{ProviderID: resp.ID}, nil
}

// Exec executes a command inside a workspace container.
func (p *GVisorProvider) Exec(ctx context.Context, providerID string, req *ExecRequest) (*ExecResult, error) {
	start := time.Now()

	workdir := req.Workdir
	if workdir == "" {
		workdir = workspaceDir
	}

	execConfig := container.ExecOptions{
		Cmd:          []string{"/bin/sh", "-c", req.Command},
		WorkingDir:   workdir,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := p.client.ContainerExecCreate(ctx, providerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	// Apply timeout
	execCtx := ctx
	if req.TimeoutMs > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	attachResp, err := p.client.ContainerExecAttach(execCtx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	// Read stdout and stderr
	var stdoutBuf, stderrBuf bytes.Buffer
	_, err = stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attachResp.Reader)
	if err != nil && err != io.EOF {
		// Context timeout will cause a read error
		if execCtx.Err() != nil {
			return &ExecResult{
				Stdout:     stdoutBuf.String(),
				Stderr:     stderrBuf.String(),
				ExitCode:   -1,
				DurationMs: time.Since(start).Milliseconds(),
				Truncated:  false,
			}, fmt.Errorf("command timed out after %dms", req.TimeoutMs)
		}
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Get exit code
	inspectResp, err := p.client.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	stdout := stdoutBuf.String()
	truncated := false
	if len(stdout) > maxOutputBytes {
		stdout = stdout[:maxOutputBytes]
		truncated = true
	}

	return &ExecResult{
		Stdout:     stdout,
		Stderr:     stderrBuf.String(),
		ExitCode:   inspectResp.ExitCode,
		DurationMs: time.Since(start).Milliseconds(),
		Truncated:  truncated,
	}, nil
}

// ReadFile reads file content from a workspace container.
func (p *GVisorProvider) ReadFile(ctx context.Context, providerID string, req *FileReadRequest) (*FileReadResult, error) {
	// Check if path is a directory
	checkResult, err := p.Exec(ctx, providerID, &ExecRequest{
		Command: fmt.Sprintf("test -d %q && echo DIR || echo FILE", req.FilePath),
	})
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(checkResult.Stdout) == "DIR" {
		// List directory
		dirResult, err := p.Exec(ctx, providerID, &ExecRequest{
			Command: fmt.Sprintf("ls -1F %q 2>/dev/null", req.FilePath),
		})
		if err != nil {
			return nil, err
		}
		return &FileReadResult{
			Content: dirResult.Stdout,
			IsDir:   true,
		}, nil
	}

	// Check if file exists
	existsResult, err := p.Exec(ctx, providerID, &ExecRequest{
		Command: fmt.Sprintf("test -f %q && echo EXISTS || echo NOTFOUND", req.FilePath),
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(existsResult.Stdout) == "NOTFOUND" {
		return nil, fmt.Errorf("file not found: %s", req.FilePath)
	}

	// Check if binary
	mimeResult, err := p.Exec(ctx, providerID, &ExecRequest{
		Command: fmt.Sprintf("file --mime-type -b %q 2>/dev/null || echo unknown", req.FilePath),
	})
	if err != nil {
		return nil, err
	}
	mime := strings.TrimSpace(mimeResult.Stdout)
	if !strings.HasPrefix(mime, "text/") && mime != "application/json" && mime != "application/xml" && !strings.Contains(mime, "empty") && mime != "unknown" {
		// Binary file — return metadata only
		sizeResult, err := p.Exec(ctx, providerID, &ExecRequest{
			Command: fmt.Sprintf("stat -c %%s %q 2>/dev/null || echo 0", req.FilePath),
		})
		if err != nil {
			return nil, err
		}
		return &FileReadResult{
			IsBinary: true,
			Content:  fmt.Sprintf("Binary file (%s)", mime),
			FileSize: parseSize(strings.TrimSpace(sizeResult.Stdout)),
		}, nil
	}

	// Build read command with offset/limit
	cmd := fmt.Sprintf("cat -n %q", req.FilePath)
	if req.Offset > 0 || req.Limit > 0 {
		if req.Offset > 0 && req.Limit > 0 {
			end := req.Offset + req.Limit - 1
			cmd = fmt.Sprintf("sed -n '%d,%dp' %q | awk '{printf \"%%d: %%s\\n\", NR+%d-1, $0}'", req.Offset, end, req.FilePath, req.Offset)
		} else if req.Offset > 0 {
			cmd = fmt.Sprintf("tail -n +%d %q | cat -n | awk '{printf \"%%d: %%s\\n\", $1+%d-1, substr($0, index($0,$2))}'", req.Offset, req.FilePath, req.Offset)
		} else if req.Limit > 0 {
			cmd = fmt.Sprintf("head -n %d %q | cat -n", req.Limit, req.FilePath)
		}
	}

	result, err := p.Exec(ctx, providerID, &ExecRequest{Command: cmd})
	if err != nil {
		return nil, err
	}

	// Count total lines
	wcResult, err := p.Exec(ctx, providerID, &ExecRequest{
		Command: fmt.Sprintf("wc -l < %q", req.FilePath),
	})
	if err != nil {
		return nil, err
	}

	return &FileReadResult{
		Content:    result.Stdout,
		TotalLines: parseInt(strings.TrimSpace(wcResult.Stdout)),
	}, nil
}

// WriteFile writes file content to a workspace container, creating parent directories as needed.
func (p *GVisorProvider) WriteFile(ctx context.Context, providerID string, req *FileWriteRequest) error {
	// Create parent directories safely
	if idx := strings.LastIndex(req.FilePath, "/"); idx > 0 {
		dir := req.FilePath[:idx]
		_, err := p.Exec(ctx, providerID, &ExecRequest{
			Command: fmt.Sprintf("mkdir -p %q", dir),
		})
		if err != nil {
			return fmt.Errorf("failed to create parent directories: %w", err)
		}
	}

	// Write file via base64 to handle special characters safely
	encoded := base64Encode(req.Content)
	_, err := p.Exec(ctx, providerID, &ExecRequest{
		Command: fmt.Sprintf("echo %q | base64 -d > %q", encoded, req.FilePath),
	})
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// ListFiles returns files matching a glob pattern inside a workspace container.
func (p *GVisorProvider) ListFiles(ctx context.Context, providerID string, req *FileListRequest) (*FileListResult, error) {
	searchPath := req.Path
	if searchPath == "" {
		searchPath = workspaceDir
	}

	// Use find with glob pattern
	cmd := fmt.Sprintf("find %q -name %q -printf '%%T@ %%y %%s %%p\\n' 2>/dev/null | sort -rn", searchPath, req.Pattern)
	result, err := p.Exec(ctx, providerID, &ExecRequest{Command: cmd})
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}

		fi := FileInfo{
			Path:  parts[3],
			IsDir: parts[1] == "d",
			Size:  parseSize(parts[2]),
		}

		// Parse modification time from epoch
		if ts := parseFloat(parts[0]); ts > 0 {
			fi.ModifiedAt = time.Unix(int64(ts), 0)
		}

		files = append(files, fi)
	}

	return &FileListResult{Files: files}, nil
}

// Health checks the Docker daemon connectivity and gVisor runtime availability.
func (p *GVisorProvider) Health(ctx context.Context) (*HealthStatus, error) {
	_, err := p.client.Ping(ctx)
	if err != nil {
		return &HealthStatus{
			Healthy: false,
			Message: fmt.Sprintf("Docker daemon unreachable: %v", err),
		}, nil
	}

	// Count active workspace containers
	containerList, err := p.client.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", defaultRuntimeLabel+"=true"),
		),
	})
	if err != nil {
		return &HealthStatus{
			Healthy: true,
			Message: "Docker daemon healthy but failed to count containers",
		}, nil
	}

	msg := "Docker daemon healthy"
	if p.useGVisor {
		msg += ", gVisor (runsc) runtime active"
	} else {
		msg += ", standard runtime (no gVisor)"
	}

	return &HealthStatus{
		Healthy:     true,
		Message:     msg,
		ActiveCount: len(containerList),
	}, nil
}

// --- Internal Helpers ---

// applyResourceLimits sets CPU, memory, and disk limits on the host config.
func (p *GVisorProvider) applyResourceLimits(hostConfig *container.HostConfig, limits *ResourceLimits) {
	if limits.CPU != "" {
		cpus := parseFloat(limits.CPU)
		if cpus > 0 {
			hostConfig.NanoCPUs = int64(cpus * 1e9)
		}
	}
	if limits.Memory != "" {
		mem := parseMemoryBytes(limits.Memory)
		if mem > 0 {
			hostConfig.Memory = mem
		}
	}
	// Disk limits are handled via volume quotas (not directly supported in Docker)
}

// ensureImage pulls an image if it's not already available locally.
func (p *GVisorProvider) ensureImage(ctx context.Context, imgRef string) error {
	p.log.Info("checking if image exists locally", "image", imgRef)
	_, _, err := p.client.ImageInspectWithRaw(ctx, imgRef)
	if err == nil {
		p.log.Info("image already exists locally", "image", imgRef)
		return nil // Image already exists
	}

	p.log.Info("image not found locally, pulling from registry", "image", imgRef)
	reader, err := p.client.ImagePull(ctx, imgRef, image.PullOptions{})
	if err != nil {
		p.log.Error("failed to start image pull", "image", imgRef, "error", err)
		return fmt.Errorf("failed to pull image %s: %w", imgRef, err)
	}
	defer reader.Close()

	p.log.Info("image pull started, consuming output stream", "image", imgRef)
	// Consume the pull output (required for the pull to complete)
	bytesRead, err := io.Copy(io.Discard, reader)
	if err != nil {
		p.log.Error("error reading image pull output", "image", imgRef, "error", err)
		return fmt.Errorf("failed to read pull output for %s: %w", imgRef, err)
	}

	p.log.Info("image pull completed successfully", "image", imgRef, "bytes_read", bytesRead)
	return nil
}

// parseMemoryBytes converts a memory string like "4G", "512M" to bytes.
func parseMemoryBytes(s string) int64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}

	multiplier := int64(1)
	if strings.HasSuffix(s, "G") || strings.HasSuffix(s, "GB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimRight(s, "GB")
	} else if strings.HasSuffix(s, "M") || strings.HasSuffix(s, "MB") {
		multiplier = 1024 * 1024
		s = strings.TrimRight(s, "MB")
	} else if strings.HasSuffix(s, "K") || strings.HasSuffix(s, "KB") {
		multiplier = 1024
		s = strings.TrimRight(s, "KB")
	}

	val := parseFloat(s)
	return int64(val) * multiplier
}

// parseSize parses a string into int64.
func parseSize(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

// parseInt parses a string into int.
func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// parseFloat parses a string into float64.
func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// base64Encode encodes a string to base64.
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// ContainerAttachment represents a live stdin/stdout connection to a container.
type ContainerAttachment struct {
	Conn   io.ReadWriteCloser // Bidirectional connection to container
	Reader io.Reader          // Demuxed stdout reader
	conn   interface{ CloseWrite() error }
}

// Close closes the container attachment.
func (a *ContainerAttachment) Close() error {
	return a.Conn.Close()
}

// AttachToContainer attaches to a running container's stdin/stdout for stdio bridge communication.
// The caller is responsible for closing the returned ContainerAttachment.
func (p *GVisorProvider) AttachToContainer(ctx context.Context, providerID string) (*ContainerAttachment, error) {
	resp, err := p.client.ContainerAttach(ctx, providerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to container: %w", err)
	}

	return &ContainerAttachment{
		Conn:   resp.Conn,
		Reader: resp.Reader,
	}, nil
}

// InspectContainer returns the current state of a container.
func (p *GVisorProvider) InspectContainer(ctx context.Context, providerID string) (*ContainerInspection, error) {
	info, err := p.client.ContainerInspect(ctx, providerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, fmt.Errorf("container not found: %s", providerID)
		}
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	result := &ContainerInspection{
		Running:    info.State.Running,
		ExitCode:   info.State.ExitCode,
		StartedAt:  info.State.StartedAt,
		FinishedAt: info.State.FinishedAt,
		Status:     info.State.Status,
	}

	return result, nil
}

// ContainerInspection holds the inspection result of a container.
type ContainerInspection struct {
	Running    bool   `json:"running"`
	ExitCode   int    `json:"exit_code"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Status     string `json:"status"` // "created", "running", "paused", "restarting", "removing", "exited", "dead"
}
