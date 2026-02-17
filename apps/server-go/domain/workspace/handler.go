package workspace

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
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

	ws, err := h.svc.Create(c.Request().Context(), &req)
	if err != nil {
		return err
	}

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
