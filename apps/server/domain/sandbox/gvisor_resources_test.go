package sandbox

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDockerClient implements only the client.APIClient methods exercised by the
// GVisorProvider resource paths. Unimplemented methods are promoted from the
// embedded nil interface and would panic if called.
type fakeDockerClient struct {
	client.APIClient

	mu sync.Mutex

	containers []container.Summary
	volumes    []*volume.Volume

	createdContainerLabels map[string]string
	createdContainerName   string
	createdVolumeLabels    map[string]string

	lastContainerFilters filters.Args
	lastVolumeFilters    filters.Args

	containerRemoveErr error
	volumeRemoveErr    error

	removedContainers []string
	removedVolumes    []string
}

func (f *fakeDockerClient) ContainerList(_ context.Context, opts container.ListOptions) ([]container.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastContainerFilters = opts.Filters
	return f.containers, nil
}

func (f *fakeDockerClient) VolumeList(_ context.Context, opts volume.ListOptions) (volume.ListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastVolumeFilters = opts.Filters
	var out []*volume.Volume
	for _, v := range f.volumes {
		if volumeMatchesLabelFilters(v, opts.Filters) {
			out = append(out, v)
		}
	}
	return volume.ListResponse{Volumes: out}, nil
}

// volumeMatchesLabelFilters applies Docker "label" filters (key or key=value).
func volumeMatchesLabelFilters(v *volume.Volume, args filters.Args) bool {
	for _, expr := range args.Get("label") {
		key, val, hasVal := strings.Cut(expr, "=")
		got, ok := v.Labels[key]
		if !ok {
			return false
		}
		if hasVal && got != val {
			return false
		}
	}
	return true
}

func (f *fakeDockerClient) ContainerRemove(_ context.Context, id string, _ container.RemoveOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedContainers = append(f.removedContainers, id)
	return f.containerRemoveErr
}

func (f *fakeDockerClient) VolumeRemove(_ context.Context, name string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedVolumes = append(f.removedVolumes, name)
	if f.volumeRemoveErr != nil {
		return f.volumeRemoveErr
	}
	kept := f.volumes[:0]
	for _, v := range f.volumes {
		if v.Name != name {
			kept = append(kept, v)
		}
	}
	f.volumes = kept
	return nil
}

func (f *fakeDockerClient) VolumeCreate(_ context.Context, opts volume.CreateOptions) (volume.Volume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdVolumeLabels = cloneLabels(opts.Labels)
	v := volume.Volume{
		Name:      opts.Name,
		Labels:    opts.Labels,
		CreatedAt: time.Now().Format(time.RFC3339Nano),
	}
	f.volumes = append(f.volumes, &v)
	return v, nil
}

func (f *fakeDockerClient) VolumeInspect(_ context.Context, name string) (volume.Volume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.volumes {
		if v.Name == name {
			return *v, nil
		}
	}
	return volume.Volume{}, errdefs.NotFound(errors.New("no such volume: " + name))
}

func (f *fakeDockerClient) ImagePull(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeDockerClient) ImageInspectWithRaw(_ context.Context, _ string) (image.InspectResponse, []byte, error) {
	return image.InspectResponse{RepoDigests: []string{"sha256:testdigest"}}, nil, nil
}

func (f *fakeDockerClient) ContainerCreate(_ context.Context, cfg *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdContainerLabels = cloneLabels(cfg.Labels)
	f.createdContainerName = name
	return container.CreateResponse{ID: "container-1234567890ab"}, nil
}

func (f *fakeDockerClient) ContainerStart(_ context.Context, _ string, _ container.StartOptions) error {
	return nil
}

func (f *fakeDockerClient) ContainerWait(_ context.Context, _ string, _ container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)
	statusCh <- container.WaitResponse{StatusCode: 0}
	return statusCh, errCh
}

func cloneLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}

func newTestProvider(fake *fakeDockerClient) *GVisorProvider {
	return &GVisorProvider{client: fake, log: testLogger()}
}

// --- R1: ownership labels on create ---

func TestGVisorProvider_Create_AppliesOwnershipLabels(t *testing.T) {
	fake := &fakeDockerClient{}
	p := newTestProvider(fake)

	_, err := p.Create(context.Background(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentSandbox,
	})
	require.NoError(t, err)

	require.NotNil(t, fake.createdContainerLabels)
	assert.Equal(t, "true", fake.createdContainerLabels[defaultRuntimeLabel])
	assert.Equal(t, string(ContainerTypeAgentSandbox), fake.createdContainerLabels[workspaceTypeLabel])
	assert.Equal(t, sandboxOwnerIdentity(), fake.createdContainerLabels[sandboxOwnerLabel])
	assert.NotEmpty(t, fake.createdContainerLabels[workspaceVolumeLabel],
		"container must record the name of its workspace volume")

	require.NotNil(t, fake.createdVolumeLabels)
	assert.Equal(t, "true", fake.createdVolumeLabels[defaultRuntimeLabel])
	assert.Equal(t, string(ContainerTypeAgentSandbox), fake.createdVolumeLabels[workspaceTypeLabel])
	assert.Equal(t, sandboxOwnerIdentity(), fake.createdVolumeLabels[sandboxOwnerLabel],
		"volume must carry the owning-process identity label")
}

func TestGVisorProvider_CreateFromSnapshot_AppliesOwnershipLabels(t *testing.T) {
	fake := &fakeDockerClient{}
	// The snapshot volume must exist for CreateFromSnapshot to proceed.
	fake.volumes = []*volume.Volume{{Name: "snap-vol", Labels: map[string]string{defaultRuntimeLabel: "true"}}}
	p := newTestProvider(fake)

	_, err := p.CreateFromSnapshot(context.Background(), "snap-vol", &CreateContainerRequest{
		ContainerType: ContainerTypeAgentSandbox,
	})
	require.NoError(t, err)

	assert.Equal(t, sandboxOwnerIdentity(), fake.createdContainerLabels[sandboxOwnerLabel])
	assert.Equal(t, sandboxOwnerIdentity(), fake.createdVolumeLabels[sandboxOwnerLabel])
}

// --- R1: label-scoped enumeration ---

func TestGVisorProvider_ListSandboxContainers_FiltersByLabel(t *testing.T) {
	fake := &fakeDockerClient{
		containers: []container.Summary{
			{
				ID:      "abc123",
				Names:   []string{"/memory-ws-1"},
				Image:   "img:latest",
				Created: time.Now().Add(-time.Hour).Unix(),
				State:   container.StateRunning,
				Labels:  map[string]string{defaultRuntimeLabel: "true"},
			},
		},
	}
	p := newTestProvider(fake)

	got, err := p.ListSandboxContainers(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)

	assert.Equal(t, "abc123", got[0].ID)
	assert.Equal(t, "memory-ws-1", got[0].Name, "leading slash should be trimmed")
	assert.True(t, got[0].Running)
	assert.Equal(t, "running", got[0].State)
	assert.NotZero(t, got[0].CreatedAt)

	// Enumeration must scope the Docker query to labelled sandbox containers.
	assert.Contains(t, fake.lastContainerFilters.Get("label"), defaultRuntimeLabel+"=true")
}

func TestGVisorProvider_ListSandboxVolumes_FiltersByLabel(t *testing.T) {
	created := time.Now().Add(-time.Hour).Truncate(time.Second)
	fake := &fakeDockerClient{
		volumes: []*volume.Volume{
			{
				Name:      "memory-workspace-1",
				CreatedAt: created.Format(time.RFC3339),
				Labels:    map[string]string{defaultRuntimeLabel: "true"},
			},
		},
	}
	p := newTestProvider(fake)

	got, err := p.ListSandboxVolumes(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "memory-workspace-1", got[0].Name)
	assert.True(t, got[0].CreatedAt.Equal(created), "created time should be parsed")

	assert.Contains(t, fake.lastVolumeFilters.Get("label"), defaultRuntimeLabel+"=true")
}

// --- R1: destroy helper ---

func TestGVisorProvider_DestroySandboxContainer_RemovesContainerAndVolume(t *testing.T) {
	fake := &fakeDockerClient{}
	p := newTestProvider(fake)

	err := p.DestroySandboxContainer(context.Background(), "container-1", "memory-workspace-1")
	require.NoError(t, err)

	assert.Equal(t, []string{"container-1"}, fake.removedContainers)
	assert.Equal(t, []string{"memory-workspace-1"}, fake.removedVolumes)
}

func TestGVisorProvider_DestroySandboxContainer_IdempotentForMissingResources(t *testing.T) {
	fake := &fakeDockerClient{
		containerRemoveErr: errdefs.NotFound(errors.New("no such container")),
		volumeRemoveErr:    errdefs.NotFound(errors.New("no such volume")),
	}
	p := newTestProvider(fake)

	err := p.DestroySandboxContainer(context.Background(), "gone", "gone-vol")
	require.NoError(t, err, "already-absent resources must not be an error")
}

func TestGVisorProvider_DestroySandboxContainer_PropagatesRealErrors(t *testing.T) {
	fake := &fakeDockerClient{containerRemoveErr: errors.New("daemon exploded")}
	p := newTestProvider(fake)

	err := p.DestroySandboxContainer(context.Background(), "c1", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daemon exploded")
}

func TestGVisorProvider_DestroySandboxVolume_Idempotent(t *testing.T) {
	fake := &fakeDockerClient{volumeRemoveErr: errdefs.NotFound(errors.New("no such volume"))}
	p := newTestProvider(fake)

	require.NoError(t, p.DestroySandboxVolume(context.Background(), "missing"))
	// Empty names are a no-op and must not call Docker.
	require.NoError(t, p.DestroySandboxVolume(context.Background(), ""))
	assert.Equal(t, []string{"missing"}, fake.removedVolumes)
}

func TestParseContainerCreatedAt(t *testing.T) {
	assert.True(t, parseContainerCreatedAt(0).IsZero(), "zero must be treated as unknown, not 1970")
	assert.True(t, parseContainerCreatedAt(-1).IsZero(), "negative must be treated as unknown")

	want := time.Unix(1700000000, 0)
	assert.Equal(t, want, parseContainerCreatedAt(1700000000))
	assert.True(t, parseContainerCreatedAt(1700000000).Equal(want))
}

func TestIsPrimaryWorkspaceVolume(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"primary", map[string]string{defaultRuntimeLabel: "true", workspaceTypeLabel: "agent_sandbox"}, true},
		{"nil", nil, false},
		{"snapshot type", map[string]string{workspaceTypeLabel: "snapshot"}, false},
		{"extra volume", map[string]string{"workspace.parent": "memory-workspace-1"}, false},
		{"snapshot source", map[string]string{"workspace.source": "memory-workspace-1"}, false},
		{"from snapshot", map[string]string{"workspace.from_snapshot": "snap-1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPrimaryWorkspaceVolume(tt.labels))
		})
	}
}

// --- Heartbeat leases ---

func TestGVisorProvider_BeatContainerHeartbeat_CreatesLease(t *testing.T) {
	fake := &fakeDockerClient{}
	p := newTestProvider(fake)

	require.NoError(t, p.BeatContainerHeartbeat(context.Background(), "container-abc"))

	hb, err := p.ListContainerHeartbeats(context.Background())
	require.NoError(t, err)
	require.Contains(t, hb, "container-abc")
	assert.WithinDuration(t, time.Now(), hb["container-abc"], 5*time.Second)
}

func TestGVisorProvider_ListContainerHeartbeats_NewestWinsAndIgnoresWorkspaceVolumes(t *testing.T) {
	fake := &fakeDockerClient{
		volumes: []*volume.Volume{
			{Name: "hb-old", Labels: map[string]string{heartbeatLabel: "c1"}, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)},
			{Name: "hb-new", Labels: map[string]string{heartbeatLabel: "c1"}, CreatedAt: time.Now().Format(time.RFC3339Nano)},
			{Name: "ws-vol", Labels: map[string]string{defaultRuntimeLabel: "true"}, CreatedAt: time.Now().Format(time.RFC3339Nano)},
		},
	}
	p := newTestProvider(fake)

	hb, err := p.ListContainerHeartbeats(context.Background())
	require.NoError(t, err)
	require.Len(t, hb, 1, "workspace volumes must not be treated as heartbeats")
	assert.WithinDuration(t, time.Now(), hb["c1"], 5*time.Second, "newest lease must win")
}

func TestGVisorProvider_BeatContainerHeartbeat_RemovesOlderLeases(t *testing.T) {
	fake := &fakeDockerClient{}
	p := newTestProvider(fake)

	require.NoError(t, p.BeatContainerHeartbeat(context.Background(), "c1"))
	require.NoError(t, p.BeatContainerHeartbeat(context.Background(), "c1"))

	fake.mu.Lock()
	var leaseCount int
	for _, v := range fake.volumes {
		if v.Labels[heartbeatLabel] == "c1" {
			leaseCount++
		}
	}
	fake.mu.Unlock()

	assert.Equal(t, 1, leaseCount, "only the newest lease for a container should remain")
	hb, err := p.ListContainerHeartbeats(context.Background())
	require.NoError(t, err)
	assert.Contains(t, hb, "c1")
}

func TestGVisorProvider_ListHeartbeatLeases(t *testing.T) {
	fake := &fakeDockerClient{
		volumes: []*volume.Volume{
			{Name: "hb-1", Labels: map[string]string{heartbeatLabel: "c1"}, CreatedAt: time.Now().Format(time.RFC3339Nano)},
			{Name: "ws", Labels: map[string]string{defaultRuntimeLabel: "true"}, CreatedAt: time.Now().Format(time.RFC3339Nano)},
		},
	}
	p := newTestProvider(fake)

	leases, err := p.ListHeartbeatLeases(context.Background())
	require.NoError(t, err)
	require.Len(t, leases, 1)
	assert.Equal(t, "hb-1", leases[0].Volume)
	assert.Equal(t, "c1", leases[0].ContainerID)
}

// TestGVisorProvider_BeatContainerHeartbeat_ConcurrentNeverLosesLease is the
// regression test for the create+prune race: without beatMu, two concurrent beats
// for the same container can each prune the other's freshly created lease, leaving
// no lease at all and breaking the "a lease is always present" invariant.
func TestGVisorProvider_BeatContainerHeartbeat_ConcurrentNeverLosesLease(t *testing.T) {
	skipWithoutDocker(t)

	p := newRealGVisorProvider(t)
	const containerID = "concurrent-heartbeat-regression"

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		p.removeHeartbeatLeasesForContainer(ctx, containerID)
	})

	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.BeatContainerHeartbeat(context.Background(), containerID); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("BeatContainerHeartbeat returned an error: %v", err)
	}

	leases, err := p.ListHeartbeatLeases(context.Background())
	require.NoError(t, err)
	count := 0
	for _, l := range leases {
		if l.ContainerID == containerID {
			count++
		}
	}
	assert.GreaterOrEqual(t, count, 1, "at least one lease for the container must survive concurrent beats")
}

// TestGVisorProvider_Destroy_RemovesHeartbeatLeases is the regression test for the
// normal destroy path: Destroy must remove the container's liveness leases, not
// just its workspace volume. WarmPool.Stop, the Acquire staleness discard, drainExcess
// and CleanupJob all route through Destroy, so a graceful destroy that left leases
// behind would accumulate heartbeat volumes until a later reconciliation pass.
func TestGVisorProvider_Destroy_RemovesHeartbeatLeases(t *testing.T) {
	skipWithoutDocker(t)

	p := newRealGVisorProvider(t)
	const containerID = "destroy-heartbeat-regression"

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		p.removeHeartbeatLeasesForContainer(ctx, containerID)
	})

	require.NoError(t, p.BeatContainerHeartbeat(context.Background(), containerID))

	// A lease must exist before destroy.
	leases, err := p.ListHeartbeatLeases(context.Background())
	require.NoError(t, err)
	count := 0
	for _, l := range leases {
		if l.ContainerID == containerID {
			count++
		}
	}
	require.GreaterOrEqual(t, count, 1, "a lease must exist before destroy")

	// Destroy tolerates a missing container (idempotent) but must still remove leases.
	require.NoError(t, p.Destroy(context.Background(), containerID))

	leases, err = p.ListHeartbeatLeases(context.Background())
	require.NoError(t, err)
	for _, l := range leases {
		assert.NotEqual(t, containerID, l.ContainerID, "Destroy must remove every lease for the container")
	}
}

func TestGVisorProvider_ListSandboxVolumes_ExcludesHeartbeatLeases(t *testing.T) {
	fake := &fakeDockerClient{
		volumes: []*volume.Volume{
			{Name: "memory-workspace-1", Labels: map[string]string{defaultRuntimeLabel: "true"}, CreatedAt: time.Now().Format(time.RFC3339Nano)},
			{Name: "memory-hb-1", Labels: map[string]string{heartbeatLabel: "c1"}, CreatedAt: time.Now().Format(time.RFC3339Nano)},
		},
	}
	p := newTestProvider(fake)

	vols, err := p.ListSandboxVolumes(context.Background())
	require.NoError(t, err)
	require.Len(t, vols, 1)
	assert.Equal(t, "memory-workspace-1", vols[0].Name, "lease volumes must be excluded by the memory.workspace label filter")
}

func TestGVisorProvider_DestroySandboxContainer_RemovesHeartbeatLeases(t *testing.T) {
	fake := &fakeDockerClient{
		volumes: []*volume.Volume{
			{Name: "memory-workspace-1", Labels: map[string]string{defaultRuntimeLabel: "true"}, CreatedAt: time.Now().Format(time.RFC3339Nano)},
			{Name: "memory-hb-1", Labels: map[string]string{heartbeatLabel: "container-1"}, CreatedAt: time.Now().Format(time.RFC3339Nano)},
		},
	}
	p := newTestProvider(fake)

	require.NoError(t, p.DestroySandboxContainer(context.Background(), "container-1", "memory-workspace-1"))

	assert.Contains(t, fake.removedVolumes, "memory-workspace-1")
	assert.Contains(t, fake.removedVolumes, "memory-hb-1", "normal teardown must remove the container's leases")
}

func TestGVisorProvider_DestroyHeartbeatLease_Idempotent(t *testing.T) {
	fake := &fakeDockerClient{volumeRemoveErr: errdefs.NotFound(errors.New("no such volume"))}
	p := newTestProvider(fake)

	require.NoError(t, p.DestroyHeartbeatLease(context.Background(), "missing"))
	require.NoError(t, p.DestroyHeartbeatLease(context.Background(), ""))
	assert.Equal(t, []string{"missing"}, fake.removedVolumes)
}

// Compile-time assertions that the provider satisfies the manager interfaces.
var (
	_ SandboxResourceManager = (*GVisorProvider)(nil)
	_ ContainerHeartbeater   = (*GVisorProvider)(nil)
)
