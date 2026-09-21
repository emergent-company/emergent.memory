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
	return volume.ListResponse{Volumes: f.volumes}, nil
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
	return f.volumeRemoveErr
}

func (f *fakeDockerClient) VolumeCreate(_ context.Context, opts volume.CreateOptions) (volume.Volume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdVolumeLabels = cloneLabels(opts.Labels)
	return volume.Volume{
		Name:      opts.Name,
		Labels:    opts.Labels,
		CreatedAt: time.Now().Format(time.RFC3339),
	}, nil
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

// Compile-time assertions that the provider satisfies the manager interface.
var _ SandboxResourceManager = (*GVisorProvider)(nil)
