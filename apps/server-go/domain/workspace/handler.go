package workspace

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

// Handler handles workspace HTTP requests.
type Handler struct {
	svc          *Service
	orchestrator *Orchestrator
	checkoutSvc  *CheckoutService
	log          *slog.Logger
}

// NewHandler creates a new workspace handler.
func NewHandler(svc *Service, orchestrator *Orchestrator, log *slog.Logger) *Handler {
	return &Handler{svc: svc, orchestrator: orchestrator, log: log.With("component", "workspace-handler")}
}

// SetCheckoutService sets the checkout service (deferred injection to break circular deps).
func (h *Handler) SetCheckoutService(cs *CheckoutService) {
	h.checkoutSvc = cs
}

// provisionContainer asynchronously provisions a container for a workspace.
func (h *Handler) provisionContainer(ctx context.Context, ws *WorkspaceResponse, req *CreateWorkspaceRequest) {
	h.log.Info("starting async container provisioning", "workspace_id", ws.ID)

	// Get provider
	provider, err := h.orchestrator.GetProvider(ProviderType(ws.Provider))
	if err != nil {
		h.log.Error("failed to get provider", "workspace_id", ws.ID, "provider", ws.Provider, "error", err)
		_, _ = h.svc.UpdateStatus(ctx, ws.ID, StatusError)
		return
	}

	// Create container
	h.log.Info("creating container", "workspace_id", ws.ID, "provider", ws.Provider)
	containerReq := &CreateContainerRequest{
		ContainerType:  req.ContainerType,
		ResourceLimits: req.ResourceLimits,
		BaseImage:      "", // Will use default from provider
	}

	containerResult, err := provider.Create(ctx, containerReq)
	if err != nil {
		h.log.Error("failed to create container",
			"workspace_id", ws.ID,
			"provider", ws.Provider,
			"error", err,
		)
		_, _ = h.svc.UpdateStatus(ctx, ws.ID, StatusError)
		return
	}

	h.log.Info("container created successfully",
		"workspace_id", ws.ID,
		"provider_id", containerResult.ProviderID,
	)

	// Update workspace with provider ID
	wsEntity, err := h.svc.store.GetByID(ctx, ws.ID)
	if err != nil {
		h.log.Error("failed to get workspace entity", "workspace_id", ws.ID, "error", err)
		_ = provider.Destroy(ctx, containerResult.ProviderID)
		_, _ = h.svc.UpdateStatus(ctx, ws.ID, StatusError)
		return
	}

	wsEntity.ProviderWorkspaceID = containerResult.ProviderID
	_, err = h.svc.store.Update(ctx, wsEntity, "provider_workspace_id")
	if err != nil {
		h.log.Error("failed to update provider_workspace_id", "workspace_id", ws.ID, "error", err)
		_ = provider.Destroy(ctx, containerResult.ProviderID)
		_, _ = h.svc.UpdateStatus(ctx, ws.ID, StatusError)
		return
	}

	// Clone repository if needed
	if req.RepositoryURL != "" && h.checkoutSvc != nil {
		h.log.Info("cloning repository", "workspace_id", ws.ID, "repo_url", req.RepositoryURL)
		if cloneErr := h.checkoutSvc.CloneRepository(ctx, provider, containerResult.ProviderID, req.RepositoryURL, req.Branch); cloneErr != nil {
			h.log.Warn("repository clone failed",
				"workspace_id", ws.ID,
				"repo_url", req.RepositoryURL,
				"error", cloneErr,
			)
			// Don't fail on clone error - workspace is still usable
		} else {
			h.log.Info("repository cloned successfully", "workspace_id", ws.ID)
		}
	}

	// Mark workspace as ready
	h.log.Info("marking workspace as ready", "workspace_id", ws.ID)
	_, err = h.svc.UpdateStatus(ctx, ws.ID, StatusReady)
	if err != nil {
		h.log.Error("failed to mark workspace as ready", "workspace_id", ws.ID, "error", err)
		return
	}

	h.log.Info("workspace provisioned successfully", "workspace_id", ws.ID)
}

// CreateWorkspace handles POST /api/v1/agent/workspaces
// @Summary      Create agent workspace
// @Description  Creates a new isolated agent workspace (container/sandbox)
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        request body CreateWorkspaceRequest true "Workspace configuration"
// @Success      201 {object} WorkspaceResponse "Created workspace"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/agent/workspaces [post]
// @Security     bearerAuth
func (h *Handler) CreateWorkspace(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CreateWorkspaceRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	h.log.Info("creating workspace via API",
		"container_type", req.ContainerType,
		"provider", req.Provider,
		"repo_url", req.RepositoryURL,
	)

	ws, err := h.svc.Create(c.Request().Context(), &req)
	if err != nil {
		h.log.Error("failed to create workspace database record", "error", err)
		return err
	}

	h.log.Info("workspace database record created, now provisioning container",
		"workspace_id", ws.ID,
		"status", ws.Status,
	)

	// Trigger async container provisioning
	go h.provisionContainer(c.Request().Context(), ws, &req)

	return c.JSON(http.StatusCreated, ws)
}

// GetWorkspace handles GET /api/v1/agent/workspaces/:id
// @Summary      Get agent workspace
// @Description  Returns a single agent workspace by ID
// @Tags         agent-workspaces
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Success      200 {object} WorkspaceResponse "Workspace details"
// @Failure      400 {object} apperror.Error "Invalid workspace ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Workspace not found"
// @Router       /api/v1/agent/workspaces/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetWorkspace(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	ws, err := h.svc.GetByID(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ws)
}

// ListWorkspaces handles GET /api/v1/agent/workspaces
// @Summary      List agent workspaces
// @Description  Returns agent workspaces with optional filtering by type, provider, status, or session
// @Tags         agent-workspaces
// @Produce      json
// @Param        container_type query string false "Filter by container type"
// @Param        provider query string false "Filter by provider"
// @Param        status query string false "Filter by status"
// @Param        agent_session_id query string false "Filter by agent session ID"
// @Success      200 {array} WorkspaceResponse "List of workspaces"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/agent/workspaces [get]
// @Security     bearerAuth
func (h *Handler) ListWorkspaces(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	filters := &ListFilters{
		ContainerType:  ContainerType(c.QueryParam("container_type")),
		Provider:       ProviderType(c.QueryParam("provider")),
		Status:         Status(c.QueryParam("status")),
		AgentSessionID: c.QueryParam("agent_session_id"),
	}

	workspaces, err := h.svc.List(c.Request().Context(), filters)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, workspaces)
}

// DeleteWorkspace handles DELETE /api/v1/agent/workspaces/:id
// @Summary      Delete agent workspace
// @Description  Permanently deletes an agent workspace and its resources
// @Tags         agent-workspaces
// @Param        id path string true "Workspace ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Invalid workspace ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Workspace not found"
// @Router       /api/v1/agent/workspaces/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteWorkspace(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	err := h.svc.Delete(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// StopWorkspace handles POST /api/v1/agent/workspaces/:id/stop
// @Summary      Stop agent workspace
// @Description  Stops a running agent workspace
// @Tags         agent-workspaces
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Success      200 {object} WorkspaceResponse "Stopped workspace"
// @Failure      400 {object} apperror.Error "Invalid workspace ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Workspace not found"
// @Router       /api/v1/agent/workspaces/{id}/stop [post]
// @Security     bearerAuth
func (h *Handler) StopWorkspace(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	ws, err := h.svc.UpdateStatus(c.Request().Context(), id, StatusStopped)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ws)
}

// ResumeWorkspace handles POST /api/v1/agent/workspaces/:id/resume
// @Summary      Resume agent workspace
// @Description  Resumes a stopped agent workspace
// @Tags         agent-workspaces
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Success      200 {object} WorkspaceResponse "Resumed workspace"
// @Failure      400 {object} apperror.Error "Invalid workspace ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Workspace not found"
// @Router       /api/v1/agent/workspaces/{id}/resume [post]
// @Security     bearerAuth
func (h *Handler) ResumeWorkspace(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	ws, err := h.svc.UpdateStatus(c.Request().Context(), id, StatusReady)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ws)
}

// ListProviders handles GET /api/v1/agent/workspaces/providers
// @Summary      List workspace providers
// @Description  Returns available workspace providers and their capabilities
// @Tags         agent-workspaces
// @Produce      json
// @Success      200 {array} ProviderStatusResponse "List of providers"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/v1/agent/workspaces/providers [get]
// @Security     bearerAuth
func (h *Handler) ListProviders(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	providers := h.orchestrator.ListProviders()
	return c.JSON(http.StatusOK, providers)
}

// AttachSession handles POST /api/v1/agent/workspaces/:id/attach
// @Summary      Attach agent session to workspace
// @Description  Attaches an agent session to a workspace for sequential access. Rejects concurrent attachment.
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body AttachSessionRequest true "Session to attach"
// @Success      200 {object} WorkspaceResponse "Workspace with attached session"
// @Failure      400 {object} apperror.Error "Concurrent access or invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Workspace not found"
// @Router       /api/v1/agent/workspaces/{id}/attach [post]
// @Security     bearerAuth
func (h *Handler) AttachSession(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	var req AttachSessionRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.AgentSessionID == "" {
		return apperror.ErrBadRequest.WithMessage("agent_session_id is required")
	}

	ws, err := h.svc.AttachToSession(c.Request().Context(), id, req.AgentSessionID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ws)
}

// DetachSession handles POST /api/v1/agent/workspaces/:id/detach
// @Summary      Detach agent session from workspace
// @Description  Clears the agent session from a workspace, making it available for new attachments
// @Tags         agent-workspaces
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Success      200 {object} WorkspaceResponse "Workspace with session detached"
// @Failure      400 {object} apperror.Error "Invalid workspace ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Workspace not found"
// @Router       /api/v1/agent/workspaces/{id}/detach [post]
// @Security     bearerAuth
func (h *Handler) DetachSession(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	ws, err := h.svc.DetachSession(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ws)
}

// CreateSnapshot handles POST /api/v1/agent/workspaces/:id/snapshot
// @Summary      Create workspace snapshot
// @Description  Creates a point-in-time snapshot of a workspace's filesystem state
// @Tags         agent-workspaces
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Success      201 {object} SnapshotResponse "Snapshot created"
// @Failure      400 {object} apperror.Error "Invalid workspace state or provider"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Workspace not found"
// @Failure      500 {object} apperror.Error "Snapshot creation failed"
// @Router       /api/v1/agent/workspaces/{id}/snapshot [post]
// @Security     bearerAuth
func (h *Handler) CreateSnapshot(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	snap, err := h.svc.CreateSnapshot(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, snap)
}

// CreateFromSnapshot handles POST /api/v1/agent/workspaces/from-snapshot
// @Summary      Create workspace from snapshot
// @Description  Creates a new workspace with filesystem state restored from a snapshot
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        request body CreateFromSnapshotRequest true "Snapshot to restore from"
// @Success      201 {object} WorkspaceResponse "Workspace created from snapshot"
// @Failure      400 {object} apperror.Error "Invalid snapshot or provider"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Restore failed"
// @Router       /api/v1/agent/workspaces/from-snapshot [post]
// @Security     bearerAuth
func (h *Handler) CreateFromSnapshot(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CreateFromSnapshotRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.SnapshotID == "" {
		return apperror.ErrBadRequest.WithMessage("snapshot_id is required")
	}

	ws, err := h.svc.CreateFromSnapshot(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, ws)
}
