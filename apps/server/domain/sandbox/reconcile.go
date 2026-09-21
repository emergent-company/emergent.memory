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
	// that is not stopped/errored is live work and must not be touched.
	refs := map[string]*AgentSandbox{}
	if r.store != nil {
		active, err := r.store.ListActive(ctx)
		if err != nil {
			r.log.Error("reconciliation aborted: failed to list active workspaces", "error", err)
			return result
		}
		for _, ws := range active {
			if ws != nil && ws.ProviderWorkspaceID != "" {
				refs[ws.ProviderWorkspaceID] = ws
			}
		}
	}

	now := r.now()

	containers, err := mgr.ListSandboxContainers(ctx)
	if err != nil {
		r.log.Error("reconciliation: failed to enumerate sandbox containers", "error", err)
	}

	// Volumes belonging to containers we keep must never be reaped, even if the
	// container's owner is a previous process (e.g. an active workspace across a restart).
	protectedVolumes := map[string]bool{}

	for _, c := range containers {
		volumeName := c.Labels[workspaceVolumeLabel]
		reason := r.decide(c, refs, now)
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

	volumes, err := mgr.ListSandboxVolumes(ctx)
	if err != nil {
		r.log.Error("reconciliation: failed to enumerate sandbox volumes", "error", err)
	}

	for _, v := range volumes {
		switch {
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
func (r *Reconciler) decide(c SandboxContainerInfo, refs map[string]*AgentSandbox, now time.Time) string {
	// D2: never touch our own containers (the warm pool is not in the DB).
	if c.Labels[sandboxOwnerLabel] == r.owner {
		return "owned"
	}

	// D2: a DB row that is not stopped/errored means live work.
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
	if c.Labels[workspaceLifecycleLabel] == string(LifecyclePersistent) {
		return "persistent"
	}

	// D3: a recently created container may belong to a starting peer.
	if r.withinGrace(c.CreatedAt, now) {
		return "grace_period"
	}

	return ""
}

// withinGrace reports whether t is more recent than the configured grace window.
// A zero time is treated as unknown-and-old (eligible), matching Docker always
// populating creation timestamps for containers.
func (r *Reconciler) withinGrace(t, now time.Time) bool {
	if t.IsZero() {
		return false
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
