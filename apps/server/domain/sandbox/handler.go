package sandbox

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles workspace HTTP requests.
type Handler struct {
	svc          *Service
	orchestrator *Orchestrator
	checkoutSvc  *CheckoutService
	warmPool     *WarmPool
	log          *slog.Logger
}

// NewHandler creates a new workspace handler.
func NewHandler(svc *Service, orchestrator *Orchestrator, checkoutSvc *CheckoutService, warmPool *WarmPool, log *slog.Logger) *Handler {
	return &Handler{
		svc:          svc,
		orchestrator: orchestrator,
		checkoutSvc:  checkoutSvc,
		warmPool:     warmPool,
		log:          log.With("component", "workspace-handler"),
	}
}

// provisionContainer asynchronously provisions a container for a workspace.
func (h *Handler) provisionContainer(ctx context.Context, ws *WorkspaceResponse, req *CreateWorkspaceRequest) {
	h.log.Info("starting async container provisioning", "workspace_id", ws.ID)

	providerType := ProviderType(ws.Provider)

	// Get provider
	provider, err := h.orchestrator.GetProvider(providerType)
	if err != nil {
		h.log.Error("failed to get provider", "workspace_id", ws.ID, "provider", ws.Provider, "error", err)
		_, _ = h.svc.UpdateStatus(ctx, ws.ID, StatusError)
		return
	}

	// Try warm pool first (if enabled and container matches)
	var providerID string
	if wc := h.warmPool.Acquire(providerType, ""); wc != nil {
		providerID = wc.ProviderID()
		h.log.Info("acquired warm container",
			"workspace_id", ws.ID,
			"provider_id", providerID,
			"provider", ws.Provider,
		)
	} else {
		// Cold start — create container from scratch
		h.log.Info("creating container (cold start)", "workspace_id", ws.ID, "provider", ws.Provider)
		containerReq := &CreateContainerRequest{
			ContainerType:  req.ContainerType,
			ResourceLimits: req.ResourceLimits,
			BaseImage:      "", // Will use default from provider
		}

		containerResult, createErr := provider.Create(ctx, containerReq)
		if createErr != nil {
			h.log.Error("failed to create container",
				"workspace_id", ws.ID,
				"provider", ws.Provider,
				"error", createErr,
			)
			_, _ = h.svc.UpdateStatus(ctx, ws.ID, StatusError)
			return
		}
		providerID = containerResult.ProviderID
	}

	h.log.Info("container created successfully",
		"workspace_id", ws.ID,
		"provider_id", providerID,
	)

	// Update workspace with provider ID
	wsEntity, err := h.svc.store.GetByID(ctx, ws.ID)
	if err != nil {
		h.log.Error("failed to get workspace entity", "workspace_id", ws.ID, "error", err)
		_ = provider.Destroy(ctx, providerID)
		_, _ = h.svc.UpdateStatus(ctx, ws.ID, StatusError)
		return
	}

	wsEntity.ProviderWorkspaceID = providerID
	_, err = h.svc.store.Update(ctx, wsEntity, "provider_workspace_id")
	if err != nil {
		h.log.Error("failed to update provider_workspace_id", "workspace_id", ws.ID, "error", err)
		_ = provider.Destroy(ctx, providerID)
		_, _ = h.svc.UpdateStatus(ctx, ws.ID, StatusError)
		return
	}

	// Clone repository if needed
	if req.RepositoryURL != "" && h.checkoutSvc != nil {
		h.log.Info("cloning repository", "workspace_id", ws.ID, "repo_url", req.RepositoryURL)
		if cloneErr := h.checkoutSvc.CloneRepository(ctx, provider, providerID, req.RepositoryURL, req.Branch, "/workspace"); cloneErr != nil {
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

// CreateWorkspace handles POST /api/v1/agent/sandboxes
// @Summary      Create agent sandbox
// @Description  Creates a new isolated agent sandbox (container/sandbox)
// @Tags         agent-sandboxes
// @Accept       json
// @Produce      json
// @Param        request body CreateWorkspaceRequest true "Sandbox configuration"
// @Success      201 {object} WorkspaceResponse "Created sandbox"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/agent/sandboxes [post]
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

	// Trigger async container provisioning (use background context to avoid cancellation)
	go h.provisionContainer(context.Background(), ws, &req)

	return c.JSON(http.StatusCreated, ws)
}

// GetWorkspace handles GET /api/v1/agent/sandboxes/:id
// @Summary      Get agent sandbox
// @Description  Returns a single agent sandbox by ID
// @Tags         agent-sandboxes
// @Produce      json
// @Param        id path string true "Sandbox ID (UUID)"
// @Success      200 {object} WorkspaceResponse "Sandbox details"
// @Failure      400 {object} apperror.Error "Invalid sandbox ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Sandbox not found"
// @Router       /api/v1/agent/sandboxes/{id} [get]
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

// ListWorkspaces handles GET /api/v1/agent/sandboxes
// @Summary      List agent sandboxes
// @Description  Returns agent sandboxes with optional filtering by type, provider, status, or session
// @Tags         agent-sandboxes
// @Produce      json
// @Param        container_type query string false "Filter by container type"
// @Param        provider query string false "Filter by provider"
// @Param        status query string false "Filter by status"
// @Param        agent_session_id query string false "Filter by agent session ID"
// @Success      200 {array} WorkspaceResponse "List of sandboxes"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/agent/sandboxes [get]
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

// DeleteWorkspace handles DELETE /api/v1/agent/sandboxes/:id
// @Summary      Delete agent sandbox
// @Description  Permanently deletes an agent sandbox and its resources
// @Tags         agent-sandboxes
// @Param        id path string true "Sandbox ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Invalid sandbox ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Sandbox not found"
// @Router       /api/v1/agent/sandboxes/{id} [delete]
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

// StopWorkspace handles POST /api/v1/agent/sandboxes/:id/stop
// @Summary      Stop agent sandbox
// @Description  Stops a running agent sandbox
// @Tags         agent-sandboxes
// @Produce      json
// @Param        id path string true "Sandbox ID (UUID)"
// @Success      200 {object} WorkspaceResponse "Stopped sandbox"
// @Failure      400 {object} apperror.Error "Invalid sandbox ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Sandbox not found"
// @Router       /api/v1/agent/sandboxes/{id}/stop [post]
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

// ResumeWorkspace handles POST /api/v1/agent/sandboxes/:id/resume
// @Summary      Resume agent sandbox
// @Description  Resumes a stopped agent sandbox
// @Tags         agent-sandboxes
// @Produce      json
// @Param        id path string true "Sandbox ID (UUID)"
// @Success      200 {object} WorkspaceResponse "Resumed sandbox"
// @Failure      400 {object} apperror.Error "Invalid sandbox ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Sandbox not found"
// @Router       /api/v1/agent/sandboxes/{id}/resume [post]
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

// ListProviders handles GET /api/v1/agent/sandboxes/providers
// @Summary      List sandbox providers
// @Description  Returns available sandbox providers and their capabilities
// @Tags         agent-sandboxes
// @Produce      json
// @Success      200 {array} ProviderStatusResponse "List of providers"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/v1/agent/sandboxes/providers [get]
// @Security     bearerAuth
func (h *Handler) ListProviders(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	providers := h.orchestrator.ListProviders()
	return c.JSON(http.StatusOK, providers)
}

// AttachSession handles POST /api/v1/agent/sandboxes/:id/attach
// @Summary      Attach agent session to sandbox
// @Description  Attaches an agent session to a sandbox for sequential access. Rejects concurrent attachment.
// @Tags         agent-sandboxes
// @Accept       json
// @Produce      json
// @Param        id path string true "Sandbox ID (UUID)"
// @Param        request body AttachSessionRequest true "Session to attach"
// @Success      200 {object} WorkspaceResponse "Sandbox with attached session"
// @Failure      400 {object} apperror.Error "Concurrent access or invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Sandbox not found"
// @Router       /api/v1/agent/sandboxes/{id}/attach [post]
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

// DetachSession handles POST /api/v1/agent/sandboxes/:id/detach
// @Summary      Detach agent session from sandbox
// @Description  Clears the agent session from a sandbox, making it available for new attachments
// @Tags         agent-sandboxes
// @Produce      json
// @Param        id path string true "Sandbox ID (UUID)"
// @Success      200 {object} WorkspaceResponse "Sandbox with session detached"
// @Failure      400 {object} apperror.Error "Invalid sandbox ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Sandbox not found"
// @Router       /api/v1/agent/sandboxes/{id}/detach [post]
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

// CreateSnapshot handles POST /api/v1/agent/sandboxes/:id/snapshot
// @Summary      Create sandbox snapshot
// @Description  Creates a point-in-time snapshot of a sandbox's filesystem state
// @Tags         agent-sandboxes
// @Produce      json
// @Param        id path string true "Sandbox ID (UUID)"
// @Success      201 {object} SnapshotResponse "Snapshot created"
// @Failure      400 {object} apperror.Error "Invalid sandbox state or provider"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Sandbox not found"
// @Failure      500 {object} apperror.Error "Snapshot creation failed"
// @Router       /api/v1/agent/sandboxes/{id}/snapshot [post]
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

// CreateFromSnapshot handles POST /api/v1/agent/sandboxes/from-snapshot
// @Summary      Create sandbox from snapshot
// @Description  Creates a new sandbox with filesystem state restored from a snapshot
// @Tags         agent-sandboxes
// @Accept       json
// @Produce      json
// @Param        request body CreateFromSnapshotRequest true "Snapshot to restore from"
// @Success      201 {object} WorkspaceResponse "Sandbox created from snapshot"
// @Failure      400 {object} apperror.Error "Invalid snapshot or provider"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Restore failed"
// @Router       /api/v1/agent/sandboxes/from-snapshot [post]
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
