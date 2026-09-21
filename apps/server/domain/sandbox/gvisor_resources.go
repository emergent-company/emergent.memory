package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// later process can tell which resources it created.
	sandboxOwnerLabel = "memory.owner"
	// workspaceVolumeLabel names the workspace volume attached to a container.
	workspaceVolumeLabel = "workspace.volume"
	// workspaceTypeLabel records the container type (agent_sandbox / mcp_server).
	workspaceTypeLabel = "workspace.type"
	// warmPoolLabel marks pre-booted warm-pool containers.
	warmPoolLabel = "memory.warm-pool"
	// heartbeatLabel records a per-container liveness lease. Its value is the
	// provider container ID; the lease is a dedicated volume whose creation time
	// is the heartbeat timestamp. Docker labels are immutable, so the lease is
	// refreshed by creating a new volume rather than mutating the container.
	heartbeatLabel = "memory.owner.heartbeat"
)

// hashContainerID returns a short, filesystem-safe token for a container ID,
// used to name heartbeat lease volumes.
func hashContainerID(containerID string) string {
	sum := sha256.Sum256([]byte(containerID))
	return hex.EncodeToString(sum[:8])
}

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
	// ListHeartbeatLeases returns all liveness-lease volumes, one entry per lease
	// volume (a container may briefly have more than one).
	ListHeartbeatLeases(ctx context.Context) ([]HeartbeatLease, error)
	// DestroySandboxContainer removes a container, its workspace volume and its
	// heartbeat leases. Missing resources are tolerated.
	DestroySandboxContainer(ctx context.Context, containerID, volumeName string) error
	// DestroySandboxVolume removes a labelled workspace volume. Missing volumes
	// are tolerated.
	DestroySandboxVolume(ctx context.Context, volumeName string) error
	// DestroyHeartbeatLease removes a single liveness-lease volume. Missing leases
	// are tolerated (idempotent).
	DestroyHeartbeatLease(ctx context.Context, volumeName string) error
}

// HeartbeatLease describes a single liveness-lease volume. Lease volumes are
// deliberately NOT labelled memory.workspace=true, so the orphan volume sweep and
// the workspace enumeration never touch them; they are cleaned up explicitly.
type HeartbeatLease struct {
	Volume      string    `json:"volume"`
	ContainerID string    `json:"container_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// ContainerHeartbeater refreshes the liveness lease for warm-pool containers it
// owns. Implemented by GVisorProvider; providers that do not implement it are
// simply not heartbeated (their containers fall back to owner/DB/grace logic).
type ContainerHeartbeater interface {
	// BeatContainerHeartbeat records a liveness beat for a container.
	BeatContainerHeartbeat(ctx context.Context, containerID string) error
}

// ListHeartbeatLeases enumerates memory.owner.heartbeat lease volumes. Volumes
// without the label are ignored defensively (the Docker filter is the primary
// selector), and only volumes carrying the heartbeat label are ever returned.
func (p *GVisorProvider) ListHeartbeatLeases(ctx context.Context) ([]HeartbeatLease, error) {
	resp, err := p.client.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", heartbeatLabel),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list owner heartbeat leases: %w", err)
	}

	out := make([]HeartbeatLease, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		if v == nil {
			continue
		}
		containerID := v.Labels[heartbeatLabel]
		if containerID == "" {
			continue
		}
		out = append(out, HeartbeatLease{
			Volume:      v.Name,
			ContainerID: containerID,
			CreatedAt:   parseVolumeCreatedAt(v.CreatedAt),
		})
	}
	return out, nil
}

// ListContainerHeartbeats returns the newest heartbeat time per container ID.
// A lease whose timestamp is unparseable is reported as the zero time; callers
// must treat a zero time as "unknown" and spare the container (fail safe).
func (p *GVisorProvider) ListContainerHeartbeats(ctx context.Context) (map[string]time.Time, error) {
	leases, err := p.ListHeartbeatLeases(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]time.Time)
	for _, lease := range leases {
		if existing, ok := out[lease.ContainerID]; !ok || lease.CreatedAt.After(existing) {
			out[lease.ContainerID] = lease.CreatedAt
		}
	}
	return out, nil
}

// DestroyHeartbeatLease removes a single liveness-lease volume. Missing leases are
// tolerated (idempotent).
func (p *GVisorProvider) DestroyHeartbeatLease(ctx context.Context, volumeName string) error {
	if volumeName == "" {
		return nil
	}
	if err := p.client.VolumeRemove(ctx, volumeName, true); err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("failed to remove heartbeat lease %s: %w", volumeName, err)
	}
	return nil
}

// BeatContainerHeartbeat refreshes the liveness lease for a container by creating
// a fresh heartbeat volume, then removing older leases for the same container.
// Creation-before-removal means a lease for the container is always present, so a
// concurrent reconciler never observes a live owner as missing.
func (p *GVisorProvider) BeatContainerHeartbeat(ctx context.Context, containerID string) error {
	if containerID == "" {
		return nil
	}

	name := fmt.Sprintf("memory-hb-%s-%d", hashContainerID(containerID), time.Now().UnixNano())
	_, err := p.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: name,
		Labels: map[string]string{
			heartbeatLabel:    containerID,
			sandboxOwnerLabel: sandboxOwnerIdentity(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to record heartbeat for container %s: %w", containerID, err)
	}

	p.removeOlderHeartbeats(ctx, containerID, name)
	return nil
}

// removeOlderHeartbeats best-effort removes heartbeat leases for containerID other
// than keep. Failures are non-fatal: the reconciler always uses the newest lease,
// so a stale lease left behind cannot make a dead owner look alive.
func (p *GVisorProvider) removeOlderHeartbeats(ctx context.Context, containerID, keep string) {
	resp, err := p.client.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", heartbeatLabel+"="+containerID),
		),
	})
	if err != nil {
		return
	}
	for _, v := range resp.Volumes {
		if v == nil || v.Name == keep {
			continue
		}
		if err := p.client.VolumeRemove(ctx, v.Name, true); err != nil && !client.IsErrNotFound(err) {
			p.log.Warn("failed to remove stale heartbeat lease", "volume", v.Name, "error", err)
		}
	}
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

	// Normal teardown leaves no liveness leases behind.
	p.removeHeartbeatLeasesForContainer(ctx, containerID)

	return firstErr
}

// removeHeartbeatLeasesForContainer best-effort removes every lease volume for a
// container. Used by normal teardown so a destroyed container does not leak leases.
func (p *GVisorProvider) removeHeartbeatLeasesForContainer(ctx context.Context, containerID string) {
	if containerID == "" {
		return
	}
	resp, err := p.client.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", heartbeatLabel+"="+containerID),
		),
	})
	if err != nil {
		return
	}
	for _, v := range resp.Volumes {
		if v == nil {
			continue
		}
		if err := p.client.VolumeRemove(ctx, v.Name, true); err != nil && !client.IsErrNotFound(err) {
			p.log.Warn("failed to remove heartbeat lease", "volume", v.Name, "error", err)
		}
	}
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

// parseVolumeCreatedAt parses the Docker volume CreatedAt string. It returns the
// zero time.Time when the string is empty or unparseable; callers must treat a
// zero time as "unknown" and fail safe (spare the resource). Note this is distinct
// from time.Unix(0, 0), which is the 1970 epoch, not the zero time.
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
