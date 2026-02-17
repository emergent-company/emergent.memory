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
}

// Service handles business logic for agent workspaces.
type Service struct {
	store  *Store
	log    *slog.Logger
	config ServiceConfig
}

// NewService creates a new workspace service.
func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{
		store: store,
		log:   log.With("component", "workspace-service"),
		config: ServiceConfig{
			MaxConcurrent:   10,
			DefaultTTLDays:  30,
			DefaultProvider: ProviderGVisor,
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
		ResourceLimits:      req.ResourceLimits,
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
