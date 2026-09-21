package sandbox

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// fakeResourceManager is an in-memory SandboxResourceManager.
type fakeResourceManager struct {
	containers []SandboxContainerInfo
	volumes    []SandboxVolumeInfo

	heartbeats        map[string]time.Time
	listContainersErr error
	listVolumesErr    error
	listHeartbeatsErr error

	destroyErr map[string]error // keyed by container ID or volume name

	listCalls atomic.Int64
	started   chan struct{} // closed when ListSandboxContainers is first entered
	release   chan struct{} // when non-nil, ListSandboxContainers blocks on it
	startOnce sync.Once

	mu                  sync.Mutex
	destroyedContainers []string
	destroyedVolumes    []string
}

func (f *fakeResourceManager) ListSandboxContainers(_ context.Context) ([]SandboxContainerInfo, error) {
	f.startOnce.Do(func() {
		if f.started != nil {
			close(f.started)
		}
	})
	if f.release != nil {
		<-f.release
	}
	f.listCalls.Add(1)
	return f.containers, f.listContainersErr
}

func (f *fakeResourceManager) ListSandboxVolumes(_ context.Context) ([]SandboxVolumeInfo, error) {
	return f.volumes, f.listVolumesErr
}

func (f *fakeResourceManager) ListContainerHeartbeats(_ context.Context) (map[string]time.Time, error) {
	return f.heartbeats, f.listHeartbeatsErr
}

func (f *fakeResourceManager) DestroySandboxContainer(_ context.Context, id, volumeName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyedContainers = append(f.destroyedContainers, id)
	if err := f.destroyErr[id]; err != nil {
		return err
	}
	if volumeName != "" {
		f.destroyedVolumes = append(f.destroyedVolumes, volumeName)
	}
	return nil
}

func (f *fakeResourceManager) DestroySandboxVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyedVolumes = append(f.destroyedVolumes, name)
	return f.destroyErr[name]
}

type fakeRefStore struct {
	active []*AgentSandbox
	err    error
}

func (f *fakeRefStore) ListActive(_ context.Context) ([]*AgentSandbox, error) {
	return f.active, f.err
}

func newTestReconciler(mgr SandboxResourceManager, store WorkspaceRefStore, grace time.Duration) *Reconciler {
	r := NewReconciler(nil, store, grace, testLogger())
	r.resources = mgr
	r.now = func() time.Time { return testNow }
	return r
}

func sandboxContainer(id string, labels map[string]string, age time.Duration) SandboxContainerInfo {
	if labels == nil {
		labels = map[string]string{}
	}
	return SandboxContainerInfo{
		ID:        id,
		Name:      id,
		Labels:    labels,
		CreatedAt: testNow.Add(-age),
		State:     "running",
		Running:   true,
	}
}

func primaryLabels(volume string) map[string]string {
	return map[string]string{
		defaultRuntimeLabel:  "true",
		workspaceTypeLabel:   string(ContainerTypeAgentSandbox),
		workspaceVolumeLabel: volume,
	}
}

func warmLabels(volume string) map[string]string {
	labels := primaryLabels(volume)
	labels[warmPoolLabel] = "true"
	return labels
}

// --- R2: ownerless orphans are reconciled ---

func TestReconciler_OwnerlessPastGrace_DestroyedWithVolume(t *testing.T) {
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("c1", primaryLabels("vol-1"), time.Hour)},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Equal(t, []string{"c1"}, mgr.destroyedContainers)
	assert.Equal(t, []string{"vol-1"}, mgr.destroyedVolumes)
	assert.Equal(t, 1, result.Reconciled)
	assert.Equal(t, 0, result.Failed)
}

func TestReconciler_CurrentProcessContainer_Kept(t *testing.T) {
	labels := primaryLabels("vol-owned")
	labels[sandboxOwnerLabel] = sandboxOwnerIdentity()
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("c-owned", labels, time.Hour)},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedContainers, "the current process's own container must never be destroyed")
	assert.Empty(t, mgr.destroyedVolumes)
	assert.Equal(t, 1, result.Skipped)
}

func TestReconciler_DBActiveContainer_Kept(t *testing.T) {
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("c-active", primaryLabels("vol-active"), time.Hour)},
	}
	store := &fakeRefStore{active: []*AgentSandbox{
		{ProviderWorkspaceID: "c-active", Status: StatusReady, Lifecycle: LifecycleEphemeral, ContainerType: ContainerTypeAgentSandbox},
	}}
	r := newTestReconciler(mgr, store, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedContainers)
	assert.Equal(t, 1, result.Skipped)
}

func TestReconciler_PersistentMCP_Kept(t *testing.T) {
	t.Run("db row marks persistent", func(t *testing.T) {
		mgr := &fakeResourceManager{
			containers: []SandboxContainerInfo{sandboxContainer("c-mcp", primaryLabels("vol-mcp"), time.Hour)},
		}
		store := &fakeRefStore{active: []*AgentSandbox{
			{ProviderWorkspaceID: "c-mcp", Status: StatusReady, Lifecycle: LifecyclePersistent, ContainerType: ContainerTypeMCPServer},
		}}
		r := newTestReconciler(mgr, store, 15*time.Minute)

		result := r.Reconcile(context.Background())

		assert.Empty(t, mgr.destroyedContainers)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("container type label marks persistent", func(t *testing.T) {
		labels := map[string]string{
			defaultRuntimeLabel:  "true",
			workspaceTypeLabel:   string(ContainerTypeMCPServer),
			workspaceVolumeLabel: "vol-mcp",
		}
		mgr := &fakeResourceManager{
			containers: []SandboxContainerInfo{sandboxContainer("c-mcp", labels, time.Hour)},
		}
		r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

		result := r.Reconcile(context.Background())

		assert.Empty(t, mgr.destroyedContainers, "MCP containers are excluded explicitly even without a DB row")
		assert.Equal(t, 1, result.Skipped)
	})

}

func TestReconciler_ContainerWithinGrace_Kept(t *testing.T) {
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("c-young", primaryLabels("vol-young"), 5*time.Minute)},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedContainers)
	assert.Equal(t, 1, result.Skipped)
}

func TestReconciler_ConfiguredGracePeriodIsHonoured(t *testing.T) {
	// Age 30m: inside a 1h grace window, outside a 10m window.
	container := sandboxContainer("c1", primaryLabels("vol-1"), 30*time.Minute)

	mgr := &fakeResourceManager{containers: []SandboxContainerInfo{container}}
	r := newTestReconciler(mgr, &fakeRefStore{}, time.Hour)
	r.Reconcile(context.Background())
	assert.Empty(t, mgr.destroyedContainers, "1h grace should spare a 30m-old container")

	mgr2 := &fakeResourceManager{containers: []SandboxContainerInfo{container}}
	r2 := newTestReconciler(mgr2, &fakeRefStore{}, 10*time.Minute)
	r2.Reconcile(context.Background())
	assert.Equal(t, []string{"c1"}, mgr2.destroyedContainers, "10m grace should reap a 30m-old container")
}

func TestReconciler_FailingDestroyDoesNotAbortPass(t *testing.T) {
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{
			sandboxContainer("c1", primaryLabels("vol-1"), time.Hour),
			sandboxContainer("c2", primaryLabels("vol-2"), time.Hour),
		},
		destroyErr: map[string]error{"c1": errors.New("daemon error")},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.ElementsMatch(t, []string{"c1", "c2"}, mgr.destroyedContainers, "both orphans should be attempted")
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 1, result.Reconciled)
}

// --- R2: orphan volumes ---

func TestReconciler_OrphanVolumeWithoutContainer_Removed(t *testing.T) {
	mgr := &fakeResourceManager{
		volumes: []SandboxVolumeInfo{
			{
				Name:      "vol-orphan",
				Labels:    map[string]string{defaultRuntimeLabel: "true", workspaceTypeLabel: "agent_sandbox"},
				CreatedAt: testNow.Add(-time.Hour),
			},
		},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Equal(t, []string{"vol-orphan"}, mgr.destroyedVolumes)
	assert.Equal(t, 1, result.Reconciled)
}

func TestReconciler_VolumeOfKeptContainer_Protected(t *testing.T) {
	labels := primaryLabels("vol-active")
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("c-active", labels, time.Hour)},
		volumes: []SandboxVolumeInfo{
			{Name: "vol-active", Labels: map[string]string{defaultRuntimeLabel: "true"}, CreatedAt: testNow.Add(-time.Hour)},
		},
	}
	store := &fakeRefStore{active: []*AgentSandbox{
		{ProviderWorkspaceID: "c-active", Status: StatusReady, Lifecycle: LifecycleEphemeral, ContainerType: ContainerTypeAgentSandbox},
	}}
	r := newTestReconciler(mgr, store, 15*time.Minute)

	r.Reconcile(context.Background())

	assert.NotContains(t, mgr.destroyedVolumes, "vol-active")
}

func TestReconciler_NonPrimaryAndOwnedVolumes_Protected(t *testing.T) {
	ownLabels := map[string]string{defaultRuntimeLabel: "true", sandboxOwnerLabel: sandboxOwnerIdentity()}
	mgr := &fakeResourceManager{
		volumes: []SandboxVolumeInfo{
			{Name: "vol-extra", Labels: map[string]string{"workspace.parent": "memory-workspace-1"}, CreatedAt: testNow.Add(-time.Hour)},
			{Name: "vol-snapshot", Labels: map[string]string{workspaceTypeLabel: "snapshot"}, CreatedAt: testNow.Add(-time.Hour)},
			{Name: "vol-owned", Labels: ownLabels, CreatedAt: testNow.Add(-time.Hour)},
			{Name: "vol-young", Labels: map[string]string{defaultRuntimeLabel: "true"}, CreatedAt: testNow.Add(-time.Minute)},
		},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedVolumes)
}

// --- BLOCKING 1: container-enumeration failure must abort the pass ---

func TestReconciler_ContainerListFailure_NoVolumeDestruction(t *testing.T) {
	mgr := &fakeResourceManager{
		listContainersErr: errors.New("docker daemon unavailable"),
		// A live container's volume exists, but the container itself is invisible
		// because enumeration failed. It must not be reaped as an orphan volume.
		volumes: []SandboxVolumeInfo{
			{
				Name:      "vol-live",
				Labels:    map[string]string{defaultRuntimeLabel: "true", workspaceTypeLabel: "agent_sandbox"},
				CreatedAt: testNow.Add(-time.Hour),
			},
		},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedContainers)
	assert.Empty(t, mgr.destroyedVolumes, "container-list failure must abort before the volume sweep")
	assert.Equal(t, ReconcileResult{}, result)
}

// --- R2/peer liveness: heartbeat lease ---

func TestReconciler_WarmPoolFreshHeartbeat_LivePeerSpared(t *testing.T) {
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("warm-peer", warmLabels("vol-peer"), 5*time.Hour)},
		heartbeats: map[string]time.Time{"warm-peer": testNow.Add(-1 * time.Minute)},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedContainers, "a live peer's warm container must be spared by its fresh heartbeat")
	assert.Empty(t, mgr.destroyedVolumes)
	assert.Equal(t, 1, result.Skipped)
}

func TestReconciler_WarmPoolStaleHeartbeat_Reaped(t *testing.T) {
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("warm-dead", warmLabels("vol-dead"), 5*time.Hour)},
		// Older than 3 × heartbeat interval (default TTL 6m).
		heartbeats: map[string]time.Time{"warm-dead": testNow.Add(-30 * time.Minute)},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Equal(t, []string{"warm-dead"}, mgr.destroyedContainers, "a stale heartbeat means the owner is dead")
	assert.Equal(t, []string{"vol-dead"}, mgr.destroyedVolumes)
	assert.Equal(t, 1, result.Reconciled)
}

func TestReconciler_WarmPoolMissingHeartbeat_Reaped(t *testing.T) {
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("warm-hb-less", warmLabels("vol-hb-less"), 5*time.Hour)},
		heartbeats: map[string]time.Time{},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Equal(t, []string{"warm-hb-less"}, mgr.destroyedContainers, "a missing heartbeat is treated as stale")
	assert.Equal(t, 1, result.Reconciled)
}

func TestReconciler_WarmPoolHeartbeatRefreshDoesNotResurrectDroppedContainer(t *testing.T) {
	// One tracked container (fresh heartbeat) and one the pool no longer tracks
	// (no heartbeat) despite the owner still being alive.
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{
			sandboxContainer("warm-tracked", warmLabels("vol-tracked"), 5*time.Hour),
			sandboxContainer("warm-dropped", warmLabels("vol-dropped"), 5*time.Hour),
		},
		heartbeats: map[string]time.Time{"warm-tracked": testNow.Add(-1 * time.Minute)},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	r.Reconcile(context.Background())

	assert.NotContains(t, mgr.destroyedContainers, "warm-tracked", "tracked container is spared")
	assert.Contains(t, mgr.destroyedContainers, "warm-dropped", "a dropped container must not be kept alive by the owner's heartbeat")
}

func TestReconciler_WarmPoolHeartbeatLookupFailure_FailsSafe(t *testing.T) {
	mgr := &fakeResourceManager{
		containers:        []SandboxContainerInfo{sandboxContainer("warm-peer", warmLabels("vol-peer"), 5*time.Hour)},
		listHeartbeatsErr: errors.New("daemon error"),
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedContainers, "when liveness cannot be read, warm-pool containers must be spared")
	assert.Equal(t, 1, result.Skipped)
}

func TestReconciler_NonWarmContainerIgnoresHeartbeat(t *testing.T) {
	// A non-warm ownerless container is not protected by any heartbeat.
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("plain", primaryLabels("vol-plain"), 5*time.Hour)},
		heartbeats: map[string]time.Time{"plain": testNow.Add(-1 * time.Minute)},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	r.Reconcile(context.Background())

	assert.Equal(t, []string{"plain"}, mgr.destroyedContainers)
}

// --- R2: fail-safe timestamps ---

func TestReconciler_VolumeWithZeroCreatedAt_Spared(t *testing.T) {
	mgr := &fakeResourceManager{
		volumes: []SandboxVolumeInfo{
			{
				Name:   "vol-unknown-age",
				Labels: map[string]string{defaultRuntimeLabel: "true", workspaceTypeLabel: "agent_sandbox"},
				// CreatedAt is the zero time (unparseable) → fail safe.
			},
		},
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedVolumes, "unknown creation time must spare the volume")
	assert.Equal(t, 1, result.Skipped)
}

func TestReconciler_ContainerWithZeroCreatedAt_Spared(t *testing.T) {
	c := sandboxContainer("c-unknown", primaryLabels("vol-unknown"), 0)
	c.CreatedAt = time.Time{}

	mgr := &fakeResourceManager{containers: []SandboxContainerInfo{c}}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedContainers)
}

// --- R2: concurrency guard ---

func TestReconciler_ConcurrentPassesDoNotOverlap(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("c1", primaryLabels("vol-1"), time.Hour)},
		started:    started,
		release:    release,
	}
	r := newTestReconciler(mgr, &fakeRefStore{}, 15*time.Minute)

	done := make(chan ReconcileResult, 1)
	go func() { done <- r.Reconcile(context.Background()) }()

	<-started // first pass is now in flight

	result := r.Reconcile(context.Background())
	assert.Equal(t, 0, result.Reconciled)
	assert.Equal(t, 0, result.Skipped)
	assert.Equal(t, 0, result.Failed)
	assert.Empty(t, mgr.destroyedContainers, "second concurrent pass must be a no-op")

	close(release)
	<-done

	assert.Equal(t, int64(1), mgr.listCalls.Load(), "manager should be enumerated once")
}

// --- R3: startup scheduling + configuration ---

type spyReconciler struct {
	calls  atomic.Int64
	result ReconcileResult
}

func (s *spyReconciler) Reconcile(_ context.Context) ReconcileResult {
	s.calls.Add(1)
	return s.result
}

func TestReconcileAtStartup_RunsWhenEnabled(t *testing.T) {
	spy := &spyReconciler{}
	cfg := &config.Config{Sandbox: config.SandboxConfig{Enabled: true, ReconcileEnabled: true}}

	reconcileAtStartup(cfg, spy, testLogger())

	assert.Equal(t, int64(1), spy.calls.Load())
}

func TestReconcileAtStartup_SkippedWhenSandboxesDisabled(t *testing.T) {
	spy := &spyReconciler{}
	cfg := &config.Config{Sandbox: config.SandboxConfig{Enabled: false, ReconcileEnabled: true}}

	reconcileAtStartup(cfg, spy, testLogger())

	assert.Equal(t, int64(0), spy.calls.Load())
}

func TestReconcileAtStartup_SkippedWhenReconcileDisabled(t *testing.T) {
	spy := &spyReconciler{}
	cfg := &config.Config{Sandbox: config.SandboxConfig{Enabled: true, ReconcileEnabled: false}}

	reconcileAtStartup(cfg, spy, testLogger())

	assert.Equal(t, int64(0), spy.calls.Load())
}

func TestNewReconciler_DefaultGracePeriod(t *testing.T) {
	r := NewReconciler(nil, nil, 0, testLogger())
	assert.Equal(t, defaultReconcileGracePeriod, r.grace)
}

func TestNewReconciler_GracePeriodFromConfig(t *testing.T) {
	cfg := &config.Config{Sandbox: config.SandboxConfig{ReconcileGraceMin: 42}}
	r := newReconciler(nil, nil, testLogger(), cfg)
	assert.Equal(t, 42*time.Minute, r.grace, "grace period must come from WORKSPACE_RECONCILE_GRACE_MIN")
}

func TestNewReconciler_HeartbeatTTLFromConfig(t *testing.T) {
	cfg := &config.Config{Sandbox: config.SandboxConfig{OwnerHeartbeatMin: 4}}
	r := newReconciler(nil, nil, testLogger(), cfg)
	assert.Equal(t, 12*time.Minute, r.heartbeatTTL, "heartbeat TTL must be 3 × WORKSPACE_OWNER_HEARTBEAT_MIN")
}

func TestNewReconciler_DefaultHeartbeatTTL(t *testing.T) {
	r := NewReconciler(nil, nil, time.Minute, testLogger())
	assert.Equal(t, 6*time.Minute, r.heartbeatTTL)
}

func TestReconciler_NoResourcesAvailable(t *testing.T) {
	r := NewReconciler(NewOrchestrator(testLogger()), nil, time.Minute, testLogger())
	// No provider registered → no resource manager → no-op, no panic.
	result := r.Reconcile(context.Background())
	assert.Equal(t, ReconcileResult{}, result)
}

func TestReconciler_ListActiveFailure_AbortsPass(t *testing.T) {
	mgr := &fakeResourceManager{
		containers: []SandboxContainerInfo{sandboxContainer("c1", primaryLabels("vol-1"), time.Hour)},
	}
	r := newTestReconciler(mgr, &fakeRefStore{err: errors.New("db down")}, 15*time.Minute)

	result := r.Reconcile(context.Background())

	assert.Empty(t, mgr.destroyedContainers, "no destruction when the DB cross-check fails")
	assert.Equal(t, ReconcileResult{}, result)
}

var _ ReconcilerRunner = (*Reconciler)(nil)

func TestReconciler_InterfaceSatisfied(t *testing.T) {
	require.NotNil(t, newTestReconciler(&fakeResourceManager{}, &fakeRefStore{}, time.Minute))
}
