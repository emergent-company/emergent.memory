package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emergent/emergent-core/pkg/apperror"
)

// ServiceConfig holds configuration for the workspace service.
type ServiceConfig struct {
	MaxConcurrent   int          `json:"max_concurrent"`   // Maximum concurrent active workspaces (default: 10)
	DefaultTTLDays  int          `json:"default_ttl_days"` // Default TTL for ephemeral workspaces (default: 30)
	DefaultProvider ProviderType `json:"default_provider"` // Default provider (default: gvisor)
	DefaultCPU      string       `json:"default_cpu"`      // Default CPU limit (default: "2")
	DefaultMemory   string       `json:"default_memory"`   // Default memory limit (default: "4G")
	DefaultDisk     string       `json:"default_disk"`     // Default disk limit (default: "10G")
}

// Service handles business logic for agent workspaces.
type Service struct {
	store        *Store
	orchestrator *Orchestrator
	log          *slog.Logger
	config       ServiceConfig
}

// NewService creates a new workspace service.
func NewService(store *Store, orchestrator *Orchestrator, log *slog.Logger) *Service {
	return &Service{
		store:        store,
		orchestrator: orchestrator,
		log:          log.With("component", "workspace-service"),
		config: ServiceConfig{
			MaxConcurrent:   10,
			DefaultTTLDays:  30,
			DefaultProvider: ProviderGVisor,
			DefaultCPU:      "2",
			DefaultMemory:   "4G",
			DefaultDisk:     "10G",
		},
	}
}

// Create creates a new workspace record.
func (s *Service) Create(ctx context.Context, req *CreateWorkspaceRequest) (*WorkspaceResponse, error) {
	// Validate container type
	if req.ContainerType != ContainerTypeAgentWorkspace && req.ContainerType != ContainerTypeMCPServer {
		return nil, apperror.NewBadRequest("container_type must be 'agent_workspace' or 'mcp_server'")
	}

	// Resolve provider
	provider := s.resolveProvider(req.Provider)

	// Check concurrency limit for agent workspaces
	if req.ContainerType == ContainerTypeAgentWorkspace {
		activeCount, err := s.store.CountActive(ctx)
		if err != nil {
			s.log.Error("failed to count active workspaces", "error", err)
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		if activeCount >= s.config.MaxConcurrent {
			return nil, apperror.NewBadRequest(
				fmt.Sprintf("maximum concurrent workspace limit reached (%d)", s.config.MaxConcurrent),
			)
		}
	}

	// Determine lifecycle
	lifecycle := LifecycleEphemeral
	if req.ContainerType == ContainerTypeMCPServer {
		lifecycle = LifecyclePersistent
	}

	// Determine deployment mode
	deploymentMode := DeploymentSelfHosted
	if req.DeploymentMode == string(DeploymentManaged) {
		deploymentMode = DeploymentManaged
	}

	// Build workspace entity
	ws := &AgentWorkspace{
		ContainerType:       req.ContainerType,
		Provider:            provider,
		ProviderWorkspaceID: "", // Will be set after container creation
		DeploymentMode:      deploymentMode,
		Lifecycle:           lifecycle,
		Status:              StatusCreating,
		ResourceLimits:      s.applyDefaultLimits(req.ResourceLimits),
		MCPConfig:           req.MCPConfig,
		Metadata:            map[string]any{},
	}

	if req.RepositoryURL != "" {
		ws.RepositoryURL = &req.RepositoryURL
	}
	if req.Branch != "" {
		ws.Branch = &req.Branch
	}

	// Set TTL for ephemeral workspaces
	if lifecycle == LifecycleEphemeral {
		expiresAt := time.Now().AddDate(0, 0, s.config.DefaultTTLDays)
		ws.ExpiresAt = &expiresAt
	}

	created, err := s.store.Create(ctx, ws)
	if err != nil {
		s.log.Error("failed to create workspace", "error", err)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	s.log.Info("workspace created",
		"id", created.ID,
		"container_type", created.ContainerType,
		"provider", created.Provider,
		"lifecycle", created.Lifecycle,
	)

	return ToResponse(created), nil
}

// GetByID returns a workspace by ID.
func (s *Service) GetByID(ctx context.Context, id string) (*WorkspaceResponse, error) {
	ws, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return nil, apperror.NewNotFound("workspace", id)
	}
	return ToResponse(ws), nil
}

// List returns workspaces matching optional filters.
func (s *Service) List(ctx context.Context, filters *ListFilters) ([]*WorkspaceResponse, error) {
	workspaces, err := s.store.List(ctx, filters)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return ToResponseList(workspaces), nil
}

// UpdateStatus updates the status of a workspace.
func (s *Service) UpdateStatus(ctx context.Context, id string, status Status) (*WorkspaceResponse, error) {
	ws, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return nil, apperror.NewNotFound("workspace", id)
	}

	ws.Status = status
	updated, err := s.store.Update(ctx, ws, "status")
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return ToResponse(updated), nil
}

// Delete removes a workspace record.
func (s *Service) Delete(ctx context.Context, id string) error {
	ws, err := s.store.GetByID(ctx, id)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return apperror.NewNotFound("workspace", id)
	}

	deleted, err := s.store.Delete(ctx, id)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	if !deleted {
		return apperror.NewNotFound("workspace", id)
	}

	s.log.Info("workspace deleted", "id", id)
	return nil
}

// TouchLastUsed updates the last_used_at timestamp and optionally extends TTL.
func (s *Service) TouchLastUsed(ctx context.Context, id string) error {
	ws, err := s.store.GetByID(ctx, id)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return apperror.NewNotFound("workspace", id)
	}

	var extendTTL *time.Time
	if ws.ExpiresAt != nil {
		extended := time.Now().AddDate(0, 0, s.config.DefaultTTLDays)
		extendTTL = &extended
	}

	return s.store.TouchLastUsed(ctx, id, extendTTL)
}

// applyDefaultLimits fills in missing resource limits from config defaults.
func (s *Service) applyDefaultLimits(limits *ResourceLimits) *ResourceLimits {
	if limits == nil {
		return &ResourceLimits{
			CPU:    s.config.DefaultCPU,
			Memory: s.config.DefaultMemory,
			Disk:   s.config.DefaultDisk,
		}
	}
	result := *limits
	if result.CPU == "" {
		result.CPU = s.config.DefaultCPU
	}
	if result.Memory == "" {
		result.Memory = s.config.DefaultMemory
	}
	if result.Disk == "" {
		result.Disk = s.config.DefaultDisk
	}
	return &result
}

// resolveProvider selects the provider type based on the requested provider string.
func (s *Service) resolveProvider(requested string) ProviderType {
	switch strings.ToLower(requested) {
	case string(ProviderFirecracker):
		return ProviderFirecracker
	case string(ProviderE2B):
		return ProviderE2B
	case string(ProviderGVisor):
		return ProviderGVisor
	case "", "auto":
		return s.config.DefaultProvider
	default:
		return s.config.DefaultProvider
	}
}

// AttachToSession attaches an agent session to a workspace.
// It validates that no other active session is currently attached (sequential-only access).
func (s *Service) AttachToSession(ctx context.Context, workspaceID string, sessionID string) (*WorkspaceResponse, error) {
	ws, err := s.store.GetByID(ctx, workspaceID)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return nil, apperror.NewNotFound("workspace", workspaceID)
	}

	// Reject if workspace is in error or creating state
	if ws.Status == StatusError || ws.Status == StatusCreating {
		return nil, apperror.NewBadRequest(
			fmt.Sprintf("workspace is in %s state; cannot attach", ws.Status),
		)
	}

	// Reject concurrent attachment: if there's already an active session and workspace is ready
	if ws.AgentSessionID != nil && *ws.AgentSessionID != "" && ws.Status == StatusReady {
		return nil, apperror.NewBadRequest(
			fmt.Sprintf("workspace is currently in use by session %s; detach first or wait for session to end", *ws.AgentSessionID),
		)
	}

	// Log session transition for audit
	previousSessionID := ""
	if ws.AgentSessionID != nil {
		previousSessionID = *ws.AgentSessionID
	}

	s.log.Info("attaching session to workspace",
		"workspace_id", workspaceID,
		"new_session_id", sessionID,
		"previous_session_id", previousSessionID,
	)

	// Update the session ID
	ws.AgentSessionID = &sessionID

	// If workspace was stopped, resume it
	if ws.Status == StatusStopped {
		ws.Status = StatusReady
	}

	updated, err := s.store.Update(ctx, ws, "agent_session_id", "status")
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Touch last used
	_ = s.store.TouchLastUsed(ctx, workspaceID, nil)

	return ToResponse(updated), nil
}

// DetachSession clears the agent session ID from a workspace (marks it available for new attachment).
func (s *Service) DetachSession(ctx context.Context, workspaceID string) (*WorkspaceResponse, error) {
	ws, err := s.store.GetByID(ctx, workspaceID)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return nil, apperror.NewNotFound("workspace", workspaceID)
	}

	previousSessionID := ""
	if ws.AgentSessionID != nil {
		previousSessionID = *ws.AgentSessionID
	}

	s.log.Info("detaching session from workspace",
		"workspace_id", workspaceID,
		"previous_session_id", previousSessionID,
	)

	ws.AgentSessionID = nil
	updated, err := s.store.Update(ctx, ws, "agent_session_id")
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return ToResponse(updated), nil
}

// CreateSnapshot creates a point-in-time snapshot of a workspace's filesystem state.
func (s *Service) CreateSnapshot(ctx context.Context, workspaceID string) (*SnapshotResponse, error) {
	ws, err := s.store.GetByID(ctx, workspaceID)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return nil, apperror.NewNotFound("workspace", workspaceID)
	}

	// Snapshots only allowed on ready or stopped workspaces
	if ws.Status != StatusReady && ws.Status != StatusStopped {
		return nil, apperror.NewBadRequest(
			fmt.Sprintf("workspace must be in ready or stopped state for snapshot; current state: %s", ws.Status),
		)
	}

	if ws.ProviderWorkspaceID == "" {
		return nil, apperror.NewBadRequest("workspace has no provider container; cannot snapshot")
	}

	// Get the provider
	provider, err := s.orchestrator.GetProvider(ws.Provider)
	if err != nil {
		return nil, apperror.NewBadRequest(fmt.Sprintf("provider %s not available: %v", ws.Provider, err))
	}

	// Check if provider supports snapshots
	caps := provider.Capabilities()
	if !caps.SupportsSnapshots {
		return nil, apperror.NewBadRequest(
			fmt.Sprintf("provider %s does not support snapshots", ws.Provider),
		)
	}

	// Create the snapshot
	snapshotID, err := provider.Snapshot(ctx, ws.ProviderWorkspaceID)
	if err != nil {
		s.log.Error("failed to create snapshot",
			"workspace_id", workspaceID,
			"provider", ws.Provider,
			"error", err,
		)
		return nil, apperror.NewInternal("failed to create snapshot", err)
	}

	// Store snapshot ID in workspace
	ws.SnapshotID = &snapshotID
	_, err = s.store.Update(ctx, ws, "snapshot_id")
	if err != nil {
		s.log.Error("failed to store snapshot ID", "error", err)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	s.log.Info("snapshot created",
		"workspace_id", workspaceID,
		"snapshot_id", snapshotID,
		"provider", ws.Provider,
	)

	return &SnapshotResponse{
		SnapshotID:  snapshotID,
		WorkspaceID: workspaceID,
		Provider:    string(ws.Provider),
		CreatedAt:   time.Now().Format(time.RFC3339Nano),
	}, nil
}

// CreateFromSnapshot creates a new workspace from a previously-taken snapshot.
func (s *Service) CreateFromSnapshot(ctx context.Context, req *CreateFromSnapshotRequest) (*WorkspaceResponse, error) {
	if req.SnapshotID == "" {
		return nil, apperror.NewBadRequest("snapshot_id is required")
	}

	// We need to determine the provider. If specified, use it. Otherwise, try to infer from
	// existing workspaces that used this snapshot.
	providerType := s.resolveProvider(req.Provider)

	// Get the provider
	provider, err := s.orchestrator.GetProvider(providerType)
	if err != nil {
		return nil, apperror.NewBadRequest(fmt.Sprintf("provider %s not available: %v", providerType, err))
	}

	// Check snapshot support
	caps := provider.Capabilities()
	if !caps.SupportsSnapshots {
		return nil, apperror.NewBadRequest(
			fmt.Sprintf("provider %s does not support snapshots", providerType),
		)
	}

	// Create container from snapshot
	containerReq := &CreateContainerRequest{
		ContainerType:  ContainerTypeAgentWorkspace,
		ResourceLimits: req.ResourceLimits,
	}

	result, err := provider.CreateFromSnapshot(ctx, req.SnapshotID, containerReq)
	if err != nil {
		s.log.Error("failed to create workspace from snapshot",
			"snapshot_id", req.SnapshotID,
			"provider", providerType,
			"error", err,
		)
		return nil, apperror.NewInternal("failed to create workspace from snapshot", err)
	}

	// Determine deployment mode
	deploymentMode := DeploymentSelfHosted
	if req.DeploymentMode == string(DeploymentManaged) {
		deploymentMode = DeploymentManaged
	}

	// Build workspace entity
	ws := &AgentWorkspace{
		ContainerType:       ContainerTypeAgentWorkspace,
		Provider:            providerType,
		ProviderWorkspaceID: result.ProviderID,
		DeploymentMode:      deploymentMode,
		Lifecycle:           LifecycleEphemeral,
		Status:              StatusReady,
		ResourceLimits:      req.ResourceLimits,
		SnapshotID:          &req.SnapshotID,
		Metadata:            map[string]any{"created_from_snapshot": req.SnapshotID},
	}

	// Set TTL
	expiresAt := time.Now().AddDate(0, 0, s.config.DefaultTTLDays)
	ws.ExpiresAt = &expiresAt

	created, err := s.store.Create(ctx, ws)
	if err != nil {
		s.log.Error("failed to store workspace from snapshot", "error", err)
		// Attempt to clean up the container
		_ = provider.Destroy(ctx, result.ProviderID)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	s.log.Info("workspace created from snapshot",
		"workspace_id", created.ID,
		"snapshot_id", req.SnapshotID,
		"provider", providerType,
	)

	return ToResponse(created), nil
}
