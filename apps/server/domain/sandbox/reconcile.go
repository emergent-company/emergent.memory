package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

const (
	// defaultReconcileGracePeriod is how long an ownerless sandbox resource must
	// have existed before reconciliation may destroy it. It protects resources
	// created by a starting peer process from being reaped mid-startup.
	defaultReconcileGracePeriod = 15 * time.Minute

	// reconcileTimeout bounds a single reconciliation pass so a slow Docker daemon
	// cannot wedge the cleanup cycle.
	reconcileTimeout = 2 * time.Minute

	// defaultOwnerHeartbeatInterval is the default warm-pool heartbeat refresh
	// interval (WORKSPACE_OWNER_HEARTBEAT_MIN, default 2m).
	defaultOwnerHeartbeatInterval = 2 * time.Minute

	// ownerHeartbeatTTLMultiplier is how many missed intervals before an owner is
	// considered dead (3 × WORKSPACE_OWNER_HEARTBEAT_MIN).
	ownerHeartbeatTTLMultiplier = 3
)

// ReconcileResult reports the outcome of a reconciliation pass.
type ReconcileResult struct {
	Reconciled int `json:"reconciled"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

// ReconcilerRunner is the subset of *Reconciler used by CleanupJob. It exists so
// the cleanup cycle can be unit-tested with a spy.
type ReconcilerRunner interface {
	Reconcile(ctx context.Context) ReconcileResult
}

// WorkspaceRefStore looks up workspaces that still hold a live reference to a
// container. *Store implements it; tests may inject a fake.
type WorkspaceRefStore interface {
	ListActive(ctx context.Context) ([]*AgentSandbox, error)
}

// Reconciler destroys sandbox containers and volumes that no live process owns
// and no active workspace references. It is label-driven (per design D1): the
// container/volume labels written at create time are the durable ground truth.
type Reconciler struct {
	orchestrator *Orchestrator
	store        WorkspaceRefStore
	log          *slog.Logger
	grace        time.Duration
	owner        string

	// heartbeatTTL is how long a warm-pool container's liveness lease is trusted.
	// A lease older than this (or missing) means the owner is presumed dead.
	heartbeatTTL time.Duration

	// resources overrides provider resolution. When nil, the gVisor provider is
	// resolved from the orchestrator. Tests set it directly.
	resources SandboxResourceManager

	// now is overridable for deterministic tests.
	now func() time.Time

	// running guards against overlapping passes (spec: concurrent passes do not overlap).
	running atomic.Bool
}

// NewReconciler creates a reconciler. A zero/negative grace period falls back to
// the default. The owner identity is captured now and stays stable for this process.
func NewReconciler(orchestrator *Orchestrator, store WorkspaceRefStore, grace time.Duration, log *slog.Logger) *Reconciler {
	if grace <= 0 {
		grace = defaultReconcileGracePeriod
	}
	if log == nil {
		log = slog.Default()
	}
	return &Reconciler{
		orchestrator: orchestrator,
		store:        store,
		log:          log.With("component", "sandbox-reconcile"),
		grace:        grace,
		owner:        sandboxOwnerIdentity(),
		heartbeatTTL: ownerHeartbeatTTLMultiplier * defaultOwnerHeartbeatInterval,
		now:          time.Now,
	}
}

// Reconcile runs a single reconciliation pass. A second concurrent invocation is a
// no-op. Individual destroy failures are logged and do not abort the pass.
func (r *Reconciler) Reconcile(ctx context.Context) ReconcileResult {
	var result ReconcileResult

	if !r.running.CompareAndSwap(false, true) {
		r.log.Debug("reconciliation already running, skipping concurrent pass")
		return result
	}
	defer r.running.Store(false)

	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	mgr, err := r.resourceManager()
	if err != nil {
		r.log.Warn("reconciliation skipped: sandbox resource manager unavailable", "error", err)
		return result
	}

	// Cross-check against the DB: any container referenced by a workspace record
	// that is not stopped/errored is live work and must not be touched. A nil store
	// cannot prove anything, so it must abort the pass — an empty reference set
	// would otherwise let reconciliation destroy a live DB-referenced container.
	if r.store == nil {
		r.log.Error("reconciliation aborted: workspace reference store unavailable")
		return result
	}
	active, err := r.store.ListActive(ctx)
	if err != nil {
		r.log.Error("reconciliation aborted: failed to list active workspaces", "error", err)
		return result
	}
	refs := map[string]*AgentSandbox{}
	for _, ws := range active {
		if ws != nil && ws.ProviderWorkspaceID != "" {
			refs[ws.ProviderWorkspaceID] = ws
		}
	}

	now := r.now()

	// The container list is mandatory: it is what builds the protected-volume set
	// below. If enumeration fails we must abort before the volume sweep, otherwise
	// we could destroy the workspace volume of a live container.
	containers, err := mgr.ListSandboxContainers(ctx)
	if err != nil {
		r.log.Error("reconciliation aborted: failed to enumerate sandbox containers", "error", err)
		return result
	}

	// Warm-pool owner liveness leases. A missing/stale lease means the owner is
	// presumed dead; if the lookup itself fails we fail safe and spare warm-pool
	// containers rather than risk reaping a live peer's pool.
	leases, hbErr := mgr.ListHeartbeatLeases(ctx)
	heartbeatsOK := hbErr == nil
	heartbeats := map[string]time.Time{}
	if hbErr != nil {
		r.log.Warn("reconciliation: failed to read owner heartbeats, sparing warm-pool containers", "error", hbErr)
	} else {
		for _, lease := range leases {
			if existing, ok := heartbeats[lease.ContainerID]; !ok || lease.CreatedAt.After(existing) {
				heartbeats[lease.ContainerID] = lease.CreatedAt
			}
		}
	}

	// Volumes belonging to containers we keep must never be reaped, even if the
	// container's owner is a previous process (e.g. an active workspace across a restart).
	protectedVolumes := map[string]bool{}

	for _, c := range containers {
		volumeName := c.Labels[workspaceVolumeLabel]
		reason := r.decide(c, refs, heartbeats, heartbeatsOK, now)
		if reason == "" && c.Labels[warmPoolLabel] == "true" {
			// The pass snapshotted leases earlier; a peer may have created a fresh
			// lease since, so re-validate liveness immediately before destroying a
			// warm-pool container (stale-snapshot TOCTOU).
			reason = r.recheckWarmPoolLiveness(ctx, mgr, c.ID, now)
		}
		if reason != "" {
			result.Skipped++
			if volumeName != "" {
				protectedVolumes[volumeName] = true
			}
			r.log.Debug("reconciliation skipped container",
				"container_id", c.ID,
				"name", c.Name,
				"reason", reason,
			)
			continue
		}

		if err := mgr.DestroySandboxContainer(ctx, c.ID, volumeName); err != nil {
			result.Failed++
			r.log.Error("reconciliation failed to destroy orphan container",
				"container_id", c.ID,
				"name", c.Name,
				"labels", c.Labels,
				"age", now.Sub(c.CreatedAt).Round(time.Second).String(),
				"error", err,
			)
			continue
		}

		result.Reconciled++
		r.log.Info("reconciliation destroyed orphan container",
			"container_id", c.ID,
			"name", c.Name,
			"labels", c.Labels,
			"age", now.Sub(c.CreatedAt).Round(time.Second).String(),
			"reason", "ownerless",
		)
	}

	// Remove liveness leases whose container no longer exists, so a dead owner's
	// leftovers do not accumulate. Only heartbeat-lease volumes are touched here,
	// and only when their container is absent from the container list, so a live
	// container's lease is never removed.
	if heartbeatsOK {
		present := make(map[string]bool, len(containers))
		for _, c := range containers {
			present[c.ID] = true
		}
		for _, lease := range leases {
			if lease.ContainerID == "" || lease.Volume == "" {
				continue
			}
			if present[lease.ContainerID] {
				result.Skipped++
				r.log.Debug("reconciliation skipped heartbeat lease",
					"volume", lease.Volume, "container_id", lease.ContainerID, "reason", "container_present")
				continue
			}
			if err := mgr.DestroyHeartbeatLease(ctx, lease.Volume); err != nil {
				result.Failed++
				r.log.Error("reconciliation failed to destroy orphan heartbeat lease",
					"volume", lease.Volume, "container_id", lease.ContainerID, "error", err)
				continue
			}
			result.Reconciled++
			r.log.Info("reconciliation destroyed orphan heartbeat lease",
				"volume", lease.Volume, "container_id", lease.ContainerID, "reason", "container_gone")
		}
	}

	volumes, err := mgr.ListSandboxVolumes(ctx)
	if err != nil {
		r.log.Error("reconciliation: failed to enumerate sandbox volumes", "error", err)
	}

	for _, v := range volumes {
		switch {
		case v.Labels[heartbeatLabel] != "":
			// Defensive: lease volumes are cleaned up explicitly above and must
			// never be treated as orphan workspace volumes.
			result.Skipped++
			r.log.Debug("reconciliation skipped volume", "volume", v.Name, "reason", "heartbeat_lease")
			continue
		case protectedVolumes[v.Name]:
			result.Skipped++
			r.log.Debug("reconciliation skipped volume", "volume", v.Name, "reason", "in_use")
			continue
		case v.Labels[sandboxOwnerLabel] == r.owner:
			result.Skipped++
			r.log.Debug("reconciliation skipped volume", "volume", v.Name, "reason", "owned")
			continue
		case !isPrimaryWorkspaceVolume(v.Labels):
			result.Skipped++
			r.log.Debug("reconciliation skipped volume", "volume", v.Name, "reason", "not_primary")
			continue
		case r.withinGrace(v.CreatedAt, now):
			result.Skipped++
			r.log.Debug("reconciliation skipped volume", "volume", v.Name, "reason", "grace_period")
			continue
		}

		if err := mgr.DestroySandboxVolume(ctx, v.Name); err != nil {
			result.Failed++
			r.log.Error("reconciliation failed to destroy orphan volume",
				"volume", v.Name,
				"labels", v.Labels,
				"error", err,
			)
			continue
		}

		result.Reconciled++
		r.log.Info("reconciliation destroyed orphan volume",
			"volume", v.Name,
			"labels", v.Labels,
			"reason", "ownerless",
		)
	}

	r.log.Info("reconciliation complete",
		"reconciled", result.Reconciled,
		"skipped", result.Skipped,
		"failed", result.Failed,
		"grace_period", r.grace.String(),
	)
	return result
}

// decide returns a non-empty skip reason when a container must be kept, or "" when
// it is an ownerless orphan eligible for destruction.
func (r *Reconciler) decide(c SandboxContainerInfo, refs map[string]*AgentSandbox, heartbeats map[string]time.Time, heartbeatsOK bool, now time.Time) string {
	// D2: never touch our own containers (the warm pool is not in the DB).
	if c.Labels[sandboxOwnerLabel] == r.owner {
		return "owned"
	}

	// A DB row that is not stopped/errored means live work.
	if ws, ok := refs[c.ID]; ok {
		if ws.Lifecycle == LifecyclePersistent || ws.ContainerType == ContainerTypeMCPServer {
			return "persistent"
		}
		return "active"
	}

	// D6: persistent MCP containers are excluded explicitly, even without a DB row.
	if c.Labels[workspaceTypeLabel] == string(ContainerTypeMCPServer) {
		return "persistent"
	}

	// Warm-pool containers are never in the DB, so ownership alone cannot tell a
	// live peer from a dead predecessor. Use the owner's liveness lease: a fresh
	// heartbeat means the peer is alive and its pool must be spared. A stale or
	// missing lease means the owner is presumed dead and the container is
	// reapable (subject to the grace window below).
	if c.Labels[warmPoolLabel] == "true" {
		if !heartbeatsOK {
			return "heartbeat_unknown"
		}
		if last, ok := heartbeats[c.ID]; ok {
			// A zero lease timestamp is unknown, not stale: fail safe and spare.
			if last.IsZero() {
				return "heartbeat_unknown"
			}
			if now.Sub(last) < r.heartbeatTTL {
				return "peer_live"
			}
		}
	}

	// D3: a recently created container may belong to a starting peer.
	if r.withinGrace(c.CreatedAt, now) {
		return "grace_period"
	}

	return ""
}

// recheckWarmPoolLiveness re-reads the liveness lease for a warm-pool container
// immediately before it is destroyed. The pass snapshotted leases earlier, so a
// peer that created a fresh lease in the interim is invisible to the cached
// snapshot; this closes that TOCTOU. It returns a non-empty skip reason when the
// owner appears alive now (or liveness cannot be confirmed), sparing the container.
func (r *Reconciler) recheckWarmPoolLiveness(ctx context.Context, mgr SandboxResourceManager, containerID string, now time.Time) string {
	leases, err := mgr.ListHeartbeatLeases(ctx)
	if err != nil {
		// Re-read failed: fail safe and spare the container rather than risk
		// reaping a live peer's pool on a transient Docker error.
		return "peer_live_recheck"
	}

	var newest time.Time
	found := false
	for _, lease := range leases {
		if lease.ContainerID != containerID {
			continue
		}
		found = true
		if lease.CreatedAt.After(newest) {
			newest = lease.CreatedAt
		}
	}

	// No lease at all means the owner is confirmed absent (matches the snapshot's
	// "missing heartbeat" verdict), so the container may still be reaped. A lease
	// whose timestamp is zero/unknown, or fresher than the TTL, means the owner
	// looks alive now: spare the container.
	if found && (newest.IsZero() || now.Sub(newest) < r.heartbeatTTL) {
		return "peer_live_recheck"
	}
	return ""
}

// withinGrace reports whether t is inside the configured grace window. A zero time
// (unknown or unparseable) is treated as inside the window so the resource is
// spared — the fail-safe direction required by design D3.
func (r *Reconciler) withinGrace(t, now time.Time) bool {
	if t.IsZero() {
		return true
	}
	return now.Sub(t) < r.grace
}

// resourceManager resolves the provider that can enumerate sandbox resources.
func (r *Reconciler) resourceManager() (SandboxResourceManager, error) {
	if r.resources != nil {
		return r.resources, nil
	}
	if r.orchestrator == nil {
		return nil, fmt.Errorf("no orchestrator configured")
	}
	p, err := r.orchestrator.GetProvider(ProviderGVisor)
	if err != nil {
		return nil, err
	}
	mgr, ok := p.(SandboxResourceManager)
	if !ok {
		return nil, fmt.Errorf("provider %T does not support sandbox resource enumeration", p)
	}
	return mgr, nil
}

// reconcileAtStartup runs one reconciliation pass when sandboxes and reconciliation
// are enabled. It is called from provider registration so predecessors' orphans are
// reclaimed as soon as the server has started, without waiting for the cleanup interval.
func reconcileAtStartup(cfg *config.Config, rec ReconcilerRunner, log *slog.Logger) ReconcileResult {
	if cfg == nil || !cfg.Sandbox.IsEnabled() || !cfg.Sandbox.ReconcileEnabled || rec == nil {
		return ReconcileResult{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()
	result := rec.Reconcile(ctx)
	log.Info("startup reconciliation complete",
		"reconciled", result.Reconciled,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
	return result
}
