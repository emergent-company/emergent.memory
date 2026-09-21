package sandbox

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// Label keys used for durable, label-driven reconciliation of sandbox resources.
const (
	// sandboxOwnerLabel identifies the process instance that created a container
	// or volume. It survives process death (it lives in Docker, not memory), so a
	// later process can tell which resources it owns versus which are ownerless.
	sandboxOwnerLabel = "memory.owner"
	// workspaceVolumeLabel names the workspace volume attached to a container.
	workspaceVolumeLabel = "workspace.volume"
	// workspaceTypeLabel records the container type (agent_sandbox / mcp_server).
	workspaceTypeLabel = "workspace.type"
	// workspaceLifecycleLabel optionally records ephemeral/persistent.
	workspaceLifecycleLabel = "workspace.lifecycle"
)

// sandboxOwnerIdentity returns a stable identity for the current process instance.
// It is derived from host, PID and a per-process start token so that two different
// processes (even on the same host) never share an identity. The value is computed
// once and cached for the lifetime of the process.
var (
	sandboxOwnerOnce sync.Once
	sandboxOwnerVal  string

	// processStartToken is generated once at process start; combined with the PID
	// it distinguishes a restarted process that reuses the same PID.
	processStartToken = strconv.FormatInt(time.Now().UnixNano(), 36)
)

func sandboxOwnerIdentity() string {
	sandboxOwnerOnce.Do(func() {
		host, err := os.Hostname()
		if err != nil {
			host = "unknown"
		}
		sandboxOwnerVal = fmt.Sprintf("%s:%d:%s", host, os.Getpid(), processStartToken)
	})
	return sandboxOwnerVal
}

// SandboxContainerInfo describes a Docker container labelled as a sandbox resource.
type SandboxContainerInfo struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	Labels    map[string]string `json:"labels"`
	CreatedAt time.Time         `json:"created_at"`
	State     string            `json:"state"`
	Running   bool              `json:"running"`
}

// SandboxVolumeInfo describes a Docker volume labelled as a sandbox resource.
type SandboxVolumeInfo struct {
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels"`
	CreatedAt time.Time         `json:"created_at"`
}

// SandboxResourceManager enumerates and destroys labelled sandbox resources.
// GVisorProvider implements it; reconciliation depends only on this interface, so
// it can be unit-tested without a Docker daemon.
type SandboxResourceManager interface {
	// ListSandboxContainers returns all containers labelled memory.workspace=true.
	ListSandboxContainers(ctx context.Context) ([]SandboxContainerInfo, error)
	// ListSandboxVolumes returns all volumes labelled memory.workspace=true.
	ListSandboxVolumes(ctx context.Context) ([]SandboxVolumeInfo, error)
	// DestroySandboxContainer removes a container and (when non-empty) the volume
	// named by its workspace.volume label. Missing resources are tolerated.
	DestroySandboxContainer(ctx context.Context, containerID, volumeName string) error
	// DestroySandboxVolume removes a labelled workspace volume. Missing volumes
	// are tolerated.
	DestroySandboxVolume(ctx context.Context, volumeName string) error
}

// ListSandboxContainers enumerates containers carrying the sandbox label
// (memory.workspace=true), including stopped ones, along with their labels and
// creation time. The enumeration is the durable ground truth used by reconciliation.
func (p *GVisorProvider) ListSandboxContainers(ctx context.Context) ([]SandboxContainerInfo, error) {
	list, err := p.client.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", defaultRuntimeLabel+"=true"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list sandbox containers: %w", err)
	}

	out := make([]SandboxContainerInfo, 0, len(list))
	for _, c := range list {
		info := SandboxContainerInfo{
			ID:        c.ID,
			Image:     c.Image,
			Labels:    c.Labels,
			State:     string(c.State),
			Running:   c.State == container.StateRunning,
			CreatedAt: time.Unix(c.Created, 0),
		}
		if len(c.Names) > 0 {
			info.Name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, info)
	}
	return out, nil
}

// ListSandboxVolumes enumerates volumes carrying the sandbox label
// (memory.workspace=true) along with their labels and creation time.
func (p *GVisorProvider) ListSandboxVolumes(ctx context.Context) ([]SandboxVolumeInfo, error) {
	resp, err := p.client.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", defaultRuntimeLabel+"=true"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list sandbox volumes: %w", err)
	}

	out := make([]SandboxVolumeInfo, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		if v == nil {
			continue
		}
		out = append(out, SandboxVolumeInfo{
			Name:      v.Name,
			Labels:    v.Labels,
			CreatedAt: parseVolumeCreatedAt(v.CreatedAt),
		})
	}
	return out, nil
}

// DestroySandboxContainer removes a container and, when known, the workspace
// volume named by its workspace.volume label. It reuses removeWorkspaceVolume so
// the removal path is identical to Destroy. Already-absent resources are not an
// error (idempotent).
func (p *GVisorProvider) DestroySandboxContainer(ctx context.Context, containerID, volumeName string) error {
	var firstErr error

	if containerID != "" {
		err := p.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: false, // volume is removed explicitly below
		})
		if err != nil && !client.IsErrNotFound(err) {
			firstErr = fmt.Errorf("failed to remove container %s: %w", containerID, err)
		}
	}

	if err := p.removeWorkspaceVolume(ctx, volumeName); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

// DestroySandboxVolume removes a labelled workspace volume. Missing volumes are
// tolerated (idempotent).
func (p *GVisorProvider) DestroySandboxVolume(ctx context.Context, volumeName string) error {
	return p.removeWorkspaceVolume(ctx, volumeName)
}

// removeWorkspaceVolume removes a workspace volume by name, tolerating a volume
// that is already gone. This is the shared removal path used by Destroy,
// DestroySandboxContainer and DestroySandboxVolume.
func (p *GVisorProvider) removeWorkspaceVolume(ctx context.Context, volumeName string) error {
	if volumeName == "" {
		return nil
	}
	if err := p.client.VolumeRemove(ctx, volumeName, true); err != nil {
		if client.IsErrNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to remove workspace volume %s: %w", volumeName, err)
	}
	return nil
}

// parseVolumeCreatedAt parses the Docker volume CreatedAt string, returning the
// zero time when it cannot be parsed.
func parseVolumeCreatedAt(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// isPrimaryWorkspaceVolume reports whether a labelled volume is a primary
// workspace volume (the one named by a container's workspace.volume label).
// Snapshot volumes, extra mount volumes and restored-from-snapshot volumes are
// excluded so reconciliation never deletes them as "orphans".
func isPrimaryWorkspaceVolume(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	if labels[workspaceTypeLabel] == "snapshot" {
		return false
	}
	if _, ok := labels["workspace.parent"]; ok {
		return false
	}
	if _, ok := labels["workspace.source"]; ok {
		return false
	}
	if _, ok := labels["workspace.from_snapshot"]; ok {
		return false
	}
	return true
}
