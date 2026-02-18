package workspace

import (
	"context"
	"fmt"
	"log/slog"
)

// ProvisioningResult holds the result of auto-provisioning a workspace for an agent session.
type ProvisioningResult struct {
	Workspace    *AgentWorkspace
	RepoURL      string
	Branch       string
	ProviderType ProviderType // Provider used for this attempt
	Degraded     bool         // True if provisioning failed and session started without workspace
	Error        error        // Non-nil if provisioning failed
}

// AutoProvisioner handles automatic workspace provisioning for agent sessions.
type AutoProvisioner struct {
	service      *Service
	orchestrator *Orchestrator
	checkoutSvc  *CheckoutService
	setupExec    *SetupExecutor
	log          *slog.Logger
}

// NewAutoProvisioner creates a new auto-provisioning service.
func NewAutoProvisioner(
	service *Service,
	orchestrator *Orchestrator,
	checkoutSvc *CheckoutService,
	setupExec *SetupExecutor,
	log *slog.Logger,
) *AutoProvisioner {
	return &AutoProvisioner{
		service:      service,
		orchestrator: orchestrator,
		checkoutSvc:  checkoutSvc,
		setupExec:    setupExec,
		log:          log.With("component", "workspace-auto-provisioner"),
	}
}

// ProvisionForSession provisions a workspace based on an agent definition's workspace config.
// This is called when an agent session starts.
//
// Flow:
// 1. Parse workspace config from agent definition
// 2. Resolve repo source (task_context, fixed, or none)
// 3. Create workspace via orchestrator with provider selection
// 4. Clone repository if needed
// 5. Run setup commands
// 6. Mark workspace as ready
//
// On failure, retries once with a fallback provider. If still failing, returns a degraded result.
func (ap *AutoProvisioner) ProvisionForSession(
	ctx context.Context,
	agentDefID string,
	workspaceConfig map[string]any,
	taskMetadata map[string]any,
) (*ProvisioningResult, error) {
	// Parse workspace config
	cfg, err := ParseAgentWorkspaceConfig(workspaceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse workspace config: %w", err)
	}

	if cfg == nil || !cfg.Enabled {
		return nil, nil // Workspace not configured or disabled
	}

	// Resolve repo source
	taskCtx := ExtractTaskContext(taskMetadata)
	repoURL, branch, shouldCheckout := ResolveRepoSource(cfg, taskCtx)

	ap.log.Info("provisioning workspace for agent session",
		"agent_definition_id", agentDefID,
		"repo_url", repoURL,
		"branch", branch,
		"should_checkout", shouldCheckout,
	)

	// Attempt provisioning (with one retry on fallback provider)
	result, err := ap.attemptProvision(ctx, cfg, repoURL, branch, shouldCheckout)
	if err != nil {
		ap.log.Warn("first provisioning attempt failed, retrying with fallback",
			"agent_definition_id", agentDefID,
			"error", err,
		)

		// Mark the failed provider as unhealthy temporarily to force fallback selection
		if result != nil && result.ProviderType != "" {
			ap.orchestrator.UpdateHealth(result.ProviderType, false, fmt.Sprintf("provisioning failed: %v", err))
		}

		// Retry once with fallback
		result, err = ap.attemptProvision(ctx, cfg, repoURL, branch, shouldCheckout)
		if err != nil {
			ap.log.Error("workspace provisioning failed after retry, starting in degraded mode",
				"agent_definition_id", agentDefID,
				"error", err,
			)
			return &ProvisioningResult{
				Degraded: true,
				Error:    err,
				RepoURL:  repoURL,
				Branch:   branch,
			}, nil
		}
	}

	result.RepoURL = repoURL
	result.Branch = branch

	return result, nil
}

// LinkToRun associates a provisioned workspace with an agent run by setting
// the agent_session_id column to the run ID.
func (ap *AutoProvisioner) LinkToRun(ctx context.Context, workspace *AgentWorkspace, runID string) error {
	workspace.AgentSessionID = &runID
	_, err := ap.service.store.Update(ctx, workspace, "agent_session_id")
	if err != nil {
		return fmt.Errorf("failed to link workspace %s to run %s: %w", workspace.ID, runID, err)
	}
	ap.log.Info("workspace linked to agent run",
		"workspace_id", workspace.ID,
		"run_id", runID,
	)
	return nil
}

// TeardownWorkspace destroys an ephemeral workspace after an agent run completes.
// For persistent workspaces, it marks them as stopped but does not destroy them.
// This is best-effort — errors are logged but not returned to avoid masking run results.
func (ap *AutoProvisioner) TeardownWorkspace(ctx context.Context, workspace *AgentWorkspace) {
	if workspace == nil {
		return
	}

	ap.log.Info("tearing down workspace after agent run",
		"workspace_id", workspace.ID,
		"provider", workspace.Provider,
	)

	// Get the provider to destroy the container
	provider, err := ap.orchestrator.GetProvider(workspace.Provider)
	if err != nil {
		ap.log.Warn("failed to get provider for workspace teardown",
			"workspace_id", workspace.ID,
			"provider", workspace.Provider,
			"error", err,
		)
		// Still update the DB status even if we can't destroy the container
		ap.updateStatusBestEffort(ctx, workspace.ID)
		return
	}

	// Destroy the container
	if err := provider.Destroy(ctx, workspace.ProviderWorkspaceID); err != nil {
		ap.log.Warn("failed to destroy workspace container",
			"workspace_id", workspace.ID,
			"provider_workspace_id", workspace.ProviderWorkspaceID,
			"error", err,
		)
	}

	// Update status to stopped
	ap.updateStatusBestEffort(ctx, workspace.ID)
}

// updateStatusBestEffort attempts to update the workspace status to stopped.
// Errors and panics are logged but never propagated — teardown must not crash.
func (ap *AutoProvisioner) updateStatusBestEffort(ctx context.Context, workspaceID string) {
	if ap.service == nil || ap.service.store == nil {
		return
	}
	if _, err := ap.service.UpdateStatus(ctx, workspaceID, StatusStopped); err != nil {
		ap.log.Warn("failed to update workspace status to stopped",
			"workspace_id", workspaceID,
			"error", err,
		)
	}
}

// attemptProvision performs a single provisioning attempt.
func (ap *AutoProvisioner) attemptProvision(
	ctx context.Context,
	cfg *AgentWorkspaceConfig,
	repoURL, branch string,
	shouldCheckout bool,
) (*ProvisioningResult, error) {
	ap.log.Info("starting workspace provisioning attempt",
		"repo_url", repoURL,
		"branch", branch,
		"should_checkout", shouldCheckout,
		"base_image", cfg.BaseImage,
	)

	// Select provider
	ap.log.Info("selecting provider with fallback")
	provider, providerType, err := ap.orchestrator.SelectProviderWithFallback(
		ContainerTypeAgentWorkspace,
		DeploymentSelfHosted,
		"", // auto-select
	)
	if err != nil {
		ap.log.Error("no provider available", "error", err)
		return nil, fmt.Errorf("no provider available: %w", err)
	}
	ap.log.Info("provider selected", "provider_type", providerType)

	// Create container
	ap.log.Info("creating container via provider", "provider_type", providerType)
	containerReq := &CreateContainerRequest{
		ContainerType:  ContainerTypeAgentWorkspace,
		ResourceLimits: cfg.ResourceLimits,
		BaseImage:      cfg.BaseImage,
	}

	containerResult, err := provider.Create(ctx, containerReq)
	if err != nil {
		ap.log.Error("failed to create container", "provider_type", providerType, "error", err)
		return &ProvisioningResult{ProviderType: providerType}, fmt.Errorf("failed to create container via %s: %w", providerType, err)
	}
	ap.log.Info("container created successfully",
		"provider_type", providerType,
		"provider_id", containerResult.ProviderID,
	)

	// Create workspace record in DB
	ap.log.Info("creating workspace database record")
	ws, err := ap.service.Create(ctx, &CreateWorkspaceRequest{
		ContainerType:  ContainerTypeAgentWorkspace,
		Provider:       string(providerType),
		RepositoryURL:  repoURL,
		Branch:         branch,
		ResourceLimits: cfg.ResourceLimits,
	})
	if err != nil {
		ap.log.Error("failed to create workspace record, cleaning up container",
			"provider_id", containerResult.ProviderID,
			"error", err,
		)
		// Try to clean up the container
		_ = provider.Destroy(ctx, containerResult.ProviderID)
		return &ProvisioningResult{ProviderType: providerType}, fmt.Errorf("failed to create workspace record: %w", err)
	}
	ap.log.Info("workspace database record created", "workspace_id", ws.ID)

	// Build the entity for internal use
	wsEntity := &AgentWorkspace{
		ID:                  ws.ID,
		Provider:            providerType,
		ProviderWorkspaceID: containerResult.ProviderID,
		Status:              StatusCreating,
	}

	// Update provider workspace ID
	ap.log.Info("updating provider workspace ID",
		"workspace_id", ws.ID,
		"provider_id", containerResult.ProviderID,
	)
	wsEntity.ProviderWorkspaceID = containerResult.ProviderID
	_, err = ap.service.store.Update(ctx, wsEntity, "provider_workspace_id")
	if err != nil {
		ap.log.Warn("failed to update provider_workspace_id", "workspace_id", ws.ID, "error", err)
	}

	// Clone repository if needed
	if shouldCheckout && repoURL != "" && ap.checkoutSvc != nil {
		ap.log.Info("cloning repository",
			"workspace_id", ws.ID,
			"repo_url", repoURL,
			"branch", branch,
		)
		if cloneErr := ap.checkoutSvc.CloneRepository(ctx, provider, containerResult.ProviderID, repoURL, branch); cloneErr != nil {
			ap.log.Warn("repository clone failed",
				"workspace_id", ws.ID,
				"repo_url", repoURL,
				"error", cloneErr,
			)
			// Don't fail provisioning on clone error — workspace is still usable
		} else {
			ap.log.Info("repository cloned successfully", "workspace_id", ws.ID)
		}
	}

	// Run setup commands
	if len(cfg.SetupCommands) > 0 && ap.setupExec != nil {
		ap.log.Info("running setup commands",
			"workspace_id", ws.ID,
			"num_commands", len(cfg.SetupCommands),
		)
		completed, setupErr := ap.setupExec.RunSetupCommands(ctx, wsEntity, cfg.SetupCommands)
		if setupErr != nil {
			ap.log.Warn("setup commands partially failed",
				"workspace_id", ws.ID,
				"completed", completed,
				"total", len(cfg.SetupCommands),
				"error", setupErr,
			)
			// Don't fail provisioning on setup error — workspace is still usable
		} else {
			ap.log.Info("all setup commands completed successfully",
				"workspace_id", ws.ID,
				"num_commands", len(cfg.SetupCommands),
			)
		}
	}

	// Mark workspace as ready
	ap.log.Info("marking workspace as ready", "workspace_id", ws.ID)
	_, err = ap.service.UpdateStatus(ctx, ws.ID, StatusReady)
	if err != nil {
		ap.log.Warn("failed to mark workspace as ready", "workspace_id", ws.ID, "error", err)
	}

	ap.log.Info("workspace provisioned successfully",
		"workspace_id", ws.ID,
		"provider", providerType,
	)

	return &ProvisioningResult{
		Workspace:    wsEntity,
		ProviderType: providerType,
	}, nil
}

// GetProviderForWorkspace returns the provider instance for a given workspace.
// This allows the agent executor to build workspace tools that delegate to the provider.
func (ap *AutoProvisioner) GetProviderForWorkspace(ws *AgentWorkspace) (Provider, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	return ap.orchestrator.GetProvider(ws.Provider)
}
