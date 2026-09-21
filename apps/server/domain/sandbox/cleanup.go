package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// CleanupConfig holds configuration for the cleanup job.
type CleanupConfig struct {
	Interval       time.Duration // How often to scan for expired workspaces (default: 1 hour)
	MaxConcurrent  int           // Maximum concurrent active workspaces (for resource alerts)
	AlertThreshold float64       // Usage threshold for resource alerts (default: 0.8 = 80%)
	// PersistentIdleTTLDays enables idle reclamation of persistent MCP servers.
	// 0 (the default) disables the policy.
	PersistentIdleTTLDays int
}

// DefaultCleanupConfig returns the default cleanup configuration.
func DefaultCleanupConfig() CleanupConfig {
	return CleanupConfig{
		Interval:              1 * time.Hour,
		MaxConcurrent:         10,
		AlertThreshold:        0.8,
		PersistentIdleTTLDays: 0, // disabled by default — preserves persistent semantics
	}
}

// CleanupJob runs periodic scans for expired workspaces and destroys them.
// It also monitors aggregate resource usage and logs warnings when thresholds are exceeded.
type CleanupJob struct {
	store        *Store
	orchestrator *Orchestrator
	mcpHosting   *MCPHostingService // may be nil; idle reclamation is skipped when nil
	log          *slog.Logger
	config       CleanupConfig
	stopCh       chan struct{}
	doneCh       chan struct{} // closed when the goroutine exits
	mu           sync.Mutex
	running      bool

	// reconciler runs label-driven orphan reconciliation alongside TTL expiry.
	// It is optional so existing construction/tests keep working.
	reconciler       ReconcilerRunner
	reconcileEnabled bool
}

// SetReconciler wires orphan reconciliation into the cleanup cycle. When enabled is
// false, reconciliation is skipped even if a reconciler is present.
func (j *CleanupJob) SetReconciler(r ReconcilerRunner, enabled bool) {
	j.reconciler = r
	j.reconcileEnabled = enabled
}

// NewCleanupJob creates a new cleanup job.
func NewCleanupJob(store *Store, orchestrator *Orchestrator, mcpHosting *MCPHostingService, log *slog.Logger, config CleanupConfig) *CleanupJob {
	return &CleanupJob{
		store:        store,
		orchestrator: orchestrator,
		mcpHosting:   mcpHosting,
		log:          log.With("component", "workspace-cleanup"),
		config:       config,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start begins the periodic cleanup goroutine.
func (j *CleanupJob) Start(ctx context.Context) {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return
	}
	j.running = true
	j.mu.Unlock()

	go func() {
		defer close(j.doneCh)

		ticker := time.NewTicker(j.config.Interval)
		defer ticker.Stop()

		// Initial pass on startup: TTL expiry + resource check. Reconciliation is
		// run exactly once at startup by the module (after providers register), so
		// it is deliberately NOT repeated here.
		j.runInitialCycle(ctx)

		for {
			select {
			case <-ticker.C:
				j.runCycle(ctx)
			case <-j.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	j.log.Info("workspace cleanup job started",
		"interval", j.config.Interval,
		"max_concurrent", j.config.MaxConcurrent,
		"alert_threshold", j.config.AlertThreshold,
	)
}

// Stop signals the cleanup goroutine to stop and waits for it to finish.
func (j *CleanupJob) Stop() {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return
	}
	j.running = false
	close(j.stopCh)
	// Release the lock before waiting to avoid blocking Start() or other callers.
	j.mu.Unlock()

	// Wait for the goroutine to finish its current cycle and exit.
	<-j.doneCh
}

// runCycle performs a single cleanup cycle: reconcile ownerless sandbox resources,
// destroy expired workspaces, reclaim idle persistent MCP servers, and check
// resource usage.
func (j *CleanupJob) runCycle(ctx context.Context) {
	j.cleanupExpired(ctx)
	j.reconcileOrphans(ctx)
	j.reclaimIdleMCPServers(ctx)
	j.checkResourceUsage(ctx)
}

// runInitialCycle is the startup pass. It intentionally omits reconciliation, which
// is run once at startup (after provider registration) by reconcileAtStartup. This
// keeps the startup reconciliation single-shot per the spec.
func (j *CleanupJob) runInitialCycle(ctx context.Context) {
	j.cleanupExpired(ctx)
	j.checkResourceUsage(ctx)
}

// reconcileOrphans runs one orphan reconciliation pass when enabled. CleanupJob is
// only started when sandboxes are enabled (module guard), and reconcileEnabled
// carries the WORKSPACE_RECONCILE_ENABLED toggle.
func (j *CleanupJob) reconcileOrphans(ctx context.Context) {
	if j.reconciler == nil || !j.reconcileEnabled {
		return
	}
	result := j.reconciler.Reconcile(ctx)
	j.log.Debug("cleanup cycle: reconciliation complete",
		"reconciled", result.Reconciled,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
}

// reclaimIdleMCPServers runs the persistent-MCP idle policy if it is enabled.
// It is a no-op when the policy is disabled (the default) or the hosting
// service is unavailable.
func (j *CleanupJob) reclaimIdleMCPServers(ctx context.Context) {
	days := j.config.PersistentIdleTTLDays
	if days <= 0 {
		return
	}
	if j.mcpHosting == nil {
		j.log.Warn("persistent MCP idle reclamation is enabled but the MCP hosting service is unavailable; skipping")
		return
	}

	rt := &hostedMCPRuntime{orchestrator: j.orchestrator, hosting: j.mcpHosting}
	res := runIdleReclamation(ctx, j.store, rt, days, j.log)
	j.log.Info("idle MCP reclamation cycle complete",
		"reclaimed", res.Reclaimed,
		"skipped", res.Skipped,
		"failed", res.Failed,
	)
}

// idleReclaimResult reports the outcome of one idle reclamation pass.
type idleReclaimResult struct {
	Reclaimed int
	Skipped   int
	Failed    int
}

// idleServerLister is the persistence surface the idle pass needs.
type idleServerLister interface {
	// ListPersistentMCPServers returns the idle-reclamation candidate set: all
	// persistent MCP server rows, narrowed only by container type and lifecycle.
	// The idle window and in-flight status policy are applied by the caller so
	// each skip decision is observable.
	ListPersistentMCPServers(ctx context.Context) ([]*AgentSandbox, error)
	// GetIdlePersistentMCPServer re-reads a candidate row and returns it only
	// if it is still eligible (present, not in-flight, still idle). It returns
	// (nil, nil) when the row vanished, was touched inside the window, or
	// entered an in-flight state.
	GetIdlePersistentMCPServer(ctx context.Context, id string, idleBefore time.Time) (*AgentSandbox, error)
	Delete(ctx context.Context, id string) (bool, error)
}

// idleServerReclaimer is the container surface the idle pass needs.
type idleServerReclaimer interface {
	// StopRuntime halts in-process monitoring/bridging for a server so it is
	// not auto-restarted after its container is reclaimed.
	StopRuntime(workspaceID string)
	// DestroyContainer destroys the provider container. It must complete
	// successfully before the row is deleted, so a failed destroy leaves the
	// row in place for a later retry rather than orphaning a running container.
	DestroyContainer(ctx context.Context, ws *AgentSandbox) error
}

// idleEligible reports whether a persistent MCP server row is eligible for
// idle reclamation at the given cutoff. The candidate query narrows by
// container type and lifecycle, so this applies the idle window and in-flight
// status policy; it is also exercised directly by tests.
func idleEligible(ws *AgentSandbox, idleBefore time.Time) bool {
	if ws == nil {
		return false
	}
	if ws.ContainerType != ContainerTypeMCPServer || ws.Lifecycle != LifecyclePersistent {
		return false
	}
	if ws.Status == StatusCreating || ws.Status == StatusStopping {
		return false
	}
	return ws.LastUsedAt.Before(idleBefore)
}

// idleSkipReason returns the operator-facing reason a persistent MCP server was
// not reclaimed.
func idleSkipReason(ws *AgentSandbox, idleBefore time.Time) string {
	if ws.ContainerType != ContainerTypeMCPServer || ws.Lifecycle != LifecyclePersistent {
		return "not a persistent MCP server"
	}
	if ws.Status == StatusCreating || ws.Status == StatusStopping {
		return "in-flight lifecycle state"
	}
	if !ws.LastUsedAt.Before(idleBefore) {
		return "used within idle window"
	}
	return "not eligible"
}

// idleReclaimTimeout bounds each per-server destroy/delete during idle
// reclamation so a blocked provider cannot hang the cleanup goroutine (and thus
// Stop()/graceful shutdown).
const idleReclaimTimeout = 30 * time.Second

// runIdleReclamation destroys persistent MCP servers idle beyond the configured
// window. It reuses the existing provider Destroy + row deletion path (the same
// path as explicit removal), never deletes a row before a successful container
// destroy, and continues past individual failures. The idle window and in-flight
// status policy are applied here (not in the candidate query) so each skip is
// observable with its reason. A window of <= 0 disables the pass entirely.
func runIdleReclamation(ctx context.Context, lister idleServerLister, rt idleServerReclaimer, idleTTLDays int, log *slog.Logger) idleReclaimResult {
	res := idleReclaimResult{}
	if idleTTLDays <= 0 || lister == nil || rt == nil {
		return res
	}

	idleBefore := time.Now().AddDate(0, 0, -idleTTLDays)
	candidates, err := lister.ListPersistentMCPServers(ctx)
	if err != nil {
		log.Error("failed to list persistent MCP servers for idle reclamation", "error", err)
		return res
	}

	for _, ws := range candidates {
		if ws == nil {
			continue
		}
		idleAge := time.Since(ws.LastUsedAt)

		if !idleEligible(ws, idleBefore) {
			log.Info("skipping persistent MCP server for idle reclamation",
				"sandbox_id", ws.ID,
				"idle_age", idleAge.String(),
				"reason", idleSkipReason(ws, idleBefore),
			)
			res.Skipped++
			continue
		}

		// Re-validate against fresh store state immediately before reclaiming:
		// a call that started after the candidate SELECT may have touched
		// last_used_at, or the row may have vanished / entered an in-flight
		// state. Skip rather than killing an in-flight call.
		fresh, err := lister.GetIdlePersistentMCPServer(ctx, ws.ID, idleBefore)
		if err != nil {
			log.Warn("failed to re-read idle persistent MCP server before reclamation; skipping",
				"sandbox_id", ws.ID,
				"idle_age", idleAge.String(),
				"error", err,
			)
			res.Failed++
			continue
		}
		if fresh == nil {
			log.Info("skipping persistent MCP server for idle reclamation",
				"sandbox_id", ws.ID,
				"idle_age", idleAge.String(),
				"reason", "touched or changed state since selection",
			)
			res.Skipped++
			continue
		}

		// Stop the in-process monitor/bridge before destroying the container so
		// it cannot auto-restart the server behind the cleanup pass's back.
		rt.StopRuntime(fresh.ID)

		if fresh.ProviderWorkspaceID == "" {
			log.Warn("idle persistent MCP server has no provider container; deleting row without destroy",
				"sandbox_id", fresh.ID,
				"idle_age", idleAge.String(),
			)
		} else {
			destroyCtx, cancel := context.WithTimeout(ctx, idleReclaimTimeout)
			err = rt.DestroyContainer(destroyCtx, fresh)
			cancel()
			if err != nil {
				log.Warn("failed to destroy idle persistent MCP server; row kept for retry",
					"sandbox_id", fresh.ID,
					"provider_workspace_id", fresh.ProviderWorkspaceID,
					"idle_age", idleAge.String(),
					"error", err,
				)
				res.Failed++
				continue
			}
		}

		delCtx, cancel := context.WithTimeout(ctx, idleReclaimTimeout)
		_, err = lister.Delete(delCtx, fresh.ID)
		cancel()
		if err != nil {
			log.Warn("failed to delete idle persistent MCP server row after destroy",
				"sandbox_id", fresh.ID,
				"idle_age", idleAge.String(),
				"error", err,
			)
			res.Failed++
			continue
		}

		log.Info("reclaimed idle persistent MCP server",
			"sandbox_id", fresh.ID,
			"idle_days", int(idleAge.Hours()/24),
		)
		res.Reclaimed++
	}
	return res
}

// hostedMCPRuntime adapts the orchestrator and MCP hosting service to the idle
// pass's container surface.
type hostedMCPRuntime struct {
	orchestrator *Orchestrator
	hosting      *MCPHostingService
}

// StopRuntime halts in-process monitoring for a server if the hosting service
// is present.
func (r *hostedMCPRuntime) StopRuntime(workspaceID string) {
	if r.hosting != nil {
		r.hosting.stopServer(workspaceID)
	}
}

// DestroyContainer destroys the server's provider container via the same
// provider Destroy path used by explicit removal.
func (r *hostedMCPRuntime) DestroyContainer(ctx context.Context, ws *AgentSandbox) error {
	if ws == nil || ws.ProviderWorkspaceID == "" {
		return nil
	}
	if r.orchestrator == nil {
		return fmt.Errorf("orchestrator unavailable for provider %s", ws.Provider)
	}
	provider, err := r.orchestrator.GetProvider(ws.Provider)
	if err != nil {
		return fmt.Errorf("provider %s unavailable: %w", ws.Provider, err)
	}
	if err := provider.Destroy(ctx, ws.ProviderWorkspaceID); err != nil {
		return fmt.Errorf("destroy container %s: %w", ws.ProviderWorkspaceID, err)
	}
	return nil
}

// cleanupExpired finds and destroys all expired workspaces.
// Persistent MCP servers are automatically excluded because they have NULL expires_at,
// and ListExpired only returns rows where expires_at IS NOT NULL AND expires_at < NOW().
func (j *CleanupJob) cleanupExpired(ctx context.Context) {
	if j.store == nil {
		return
	}
	expired, err := j.store.ListExpired(ctx)
	if err != nil {
		j.log.Error("failed to list expired workspaces", "error", err)
		return
	}

	if len(expired) == 0 {
		j.log.Debug("cleanup cycle: no expired workspaces found")
		return
	}

	j.log.Info("cleanup cycle: found expired workspaces", "count", len(expired))

	destroyed := 0
	failed := 0
	for _, ws := range expired {
		if err := j.destroyWorkspace(ctx, ws); err != nil {
			j.log.Error("failed to destroy expired workspace",
				"workspace_id", ws.ID,
				"provider", ws.Provider,
				"error", err,
			)
			failed++
			continue
		}
		destroyed++
	}

	j.log.Info("cleanup cycle complete",
		"destroyed", destroyed,
		"failed", failed,
		"total_expired", len(expired),
	)
}

// destroyWorkspace destroys a single workspace via its provider and updates the DB status.
func (j *CleanupJob) destroyWorkspace(ctx context.Context, ws *AgentSandbox) error {
	// Try to destroy via provider (if the container still exists)
	if ws.ProviderWorkspaceID != "" {
		provider, err := j.orchestrator.GetProvider(ws.Provider)
		if err != nil {
			j.log.Warn("provider not available for cleanup, marking as stopped",
				"workspace_id", ws.ID,
				"provider", ws.Provider,
				"error", err,
			)
		} else {
			destroyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			if err := provider.Destroy(destroyCtx, ws.ProviderWorkspaceID); err != nil {
				// Log but don't fail — the container may already be gone
				j.log.Warn("provider destroy returned error (container may already be removed)",
					"workspace_id", ws.ID,
					"provider_workspace_id", ws.ProviderWorkspaceID,
					"error", err,
				)
			}
		}
	}

	// Mark workspace as stopped in DB regardless of provider destroy outcome
	ws.Status = StatusStopped
	_, err := j.store.Update(ctx, ws, "status")
	if err != nil {
		return fmt.Errorf("failed to update workspace status: %w", err)
	}

	j.log.Info("expired workspace destroyed",
		"workspace_id", ws.ID,
		"provider", ws.Provider,
		"container_type", ws.ContainerType,
		"expired_at", ws.ExpiresAt,
	)

	return nil
}

// checkResourceUsage monitors aggregate resource usage and logs warnings when thresholds are exceeded.
func (j *CleanupJob) checkResourceUsage(ctx context.Context) {
	if j.store == nil {
		return
	}
	activeCount, err := j.store.CountActive(ctx)
	if err != nil {
		j.log.Error("failed to count active workspaces for resource monitoring", "error", err)
		return
	}

	if j.config.MaxConcurrent <= 0 {
		return
	}

	usageRatio := float64(activeCount) / float64(j.config.MaxConcurrent)

	j.log.Debug("resource usage check",
		"active_workspaces", activeCount,
		"max_concurrent", j.config.MaxConcurrent,
		"usage_percent", fmt.Sprintf("%.1f%%", usageRatio*100),
	)

	if usageRatio >= 1.0 {
		j.log.Error("workspace resource exhaustion: at maximum capacity",
			"active_workspaces", activeCount,
			"max_concurrent", j.config.MaxConcurrent,
			"usage_percent", fmt.Sprintf("%.1f%%", usageRatio*100),
		)
	} else if usageRatio >= j.config.AlertThreshold {
		j.log.Warn("workspace resource usage high: approaching capacity",
			"active_workspaces", activeCount,
			"max_concurrent", j.config.MaxConcurrent,
			"usage_percent", fmt.Sprintf("%.1f%%", usageRatio*100),
			"alert_threshold", fmt.Sprintf("%.0f%%", j.config.AlertThreshold*100),
		)
	}
}
