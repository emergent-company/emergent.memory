package workspace

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
}

// DefaultCleanupConfig returns the default cleanup configuration.
func DefaultCleanupConfig() CleanupConfig {
	return CleanupConfig{
		Interval:       1 * time.Hour,
		MaxConcurrent:  10,
		AlertThreshold: 0.8,
	}
}

// CleanupJob runs periodic scans for expired workspaces and destroys them.
// It also monitors aggregate resource usage and logs warnings when thresholds are exceeded.
type CleanupJob struct {
	store        *Store
	orchestrator *Orchestrator
	log          *slog.Logger
	config       CleanupConfig
	stopCh       chan struct{}
	doneCh       chan struct{} // closed when the goroutine exits
	mu           sync.Mutex
	running      bool
}

// NewCleanupJob creates a new cleanup job.
func NewCleanupJob(store *Store, orchestrator *Orchestrator, log *slog.Logger, config CleanupConfig) *CleanupJob {
	return &CleanupJob{
		store:        store,
		orchestrator: orchestrator,
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

		// Run an initial cleanup cycle on startup
		j.runCycle(ctx)

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

// runCycle performs a single cleanup cycle: destroy expired workspaces and check resource usage.
func (j *CleanupJob) runCycle(ctx context.Context) {
	j.cleanupExpired(ctx)
	j.checkResourceUsage(ctx)
}

// cleanupExpired finds and destroys all expired workspaces.
// Persistent MCP servers are automatically excluded because they have NULL expires_at,
// and ListExpired only returns rows where expires_at IS NOT NULL AND expires_at < NOW().
func (j *CleanupJob) cleanupExpired(ctx context.Context) {
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
func (j *CleanupJob) destroyWorkspace(ctx context.Context, ws *AgentWorkspace) error {
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
