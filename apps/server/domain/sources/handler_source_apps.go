package sources

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

type registerSourceAppDTO struct {
	Provider string `json:"provider"`
	// Scope is "org" (org-wide, org admin) or "project" (project-owned,
	// project admin). Only the project-scoped route reads it; the org route is
	// always org-wide.
	Scope         string `json:"scope,omitempty"`
	AppID         int64  `json:"appId"`
	Slug          string `json:"slug"`
	PrivateKey    string `json:"privateKey"`
	WebhookSecret string `json:"webhookSecret"`
	WebhookKey    string `json:"webhookKey,omitempty"`
}

type updateSourceAppDTO struct {
	Slug          string `json:"slug"`
	PrivateKey    string `json:"privateKey"`
	WebhookSecret string `json:"webhookSecret"`
}

type sourceTriggerDTO struct {
	EventType   string         `json:"eventType"`
	AgentID     string         `json:"agentId"`
	Queue       string         `json:"queue"`
	EventFilter map[string]any `json:"eventFilter"`
	Enabled     *bool          `json:"enabled"`
}

// RegisterSourceApp handles POST /api/orgs/:orgId/source-apps.
// @Summary      Register a source App
// @Description  Stores org-level App credentials (encrypted). Org admin only.
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        body body registerSourceAppDTO true "App credentials"
// @Success      201 {object} SourceApp
// @Failure      400 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Router       /api/orgs/{orgId}/source-apps [post]
// @Security     bearerAuth
func (h *Handler) RegisterSourceApp(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	var dto registerSourceAppDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	app, err := h.svc.RegisterSourceApp(c.Request().Context(), orgID, user.ID, RegisterSourceAppRequest{
		Provider: dto.Provider, AppID: dto.AppID, Slug: dto.Slug, PrivateKey: dto.PrivateKey, WebhookSecret: dto.WebhookSecret, WebhookKey: dto.WebhookKey,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, app)
}

// ListSourceApps handles GET /api/orgs/:orgId/source-apps.
// @Summary      List source Apps
// @Description  Returns org Apps without credentials. Org admin only.
// @Tags         data-source-integrations
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Success      200 {array} SourceApp
// @Router       /api/orgs/{orgId}/source-apps [get]
// @Security     bearerAuth
func (h *Handler) ListSourceApps(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)
	apps, err := h.svc.ListSourceApps(c.Request().Context(), orgID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, apps)
}

// DeleteSourceApp handles DELETE /api/orgs/:orgId/source-apps/:appId.
// @Summary      Delete a source App
// @Description  Removes an org App credential. Refused with 409 while project sources are still bound to the App; the error names them. Org admin only.
// @Tags         data-source-integrations
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        appId path string true "App row ID (UUID)"
// @Success      204
// @Failure      403 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      409 {object} apperror.Error "App still bound to project sources"
// @Router       /api/orgs/{orgId}/source-apps/{appId} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteSourceApp(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("appId"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	if err := h.svc.DeleteSourceApp(c.Request().Context(), orgID, user.ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// UpdateSourceApp handles PATCH /api/orgs/:orgId/source-apps/:appId.
// @Summary      Update a source App
// @Description  Renames an App and optionally rotates its private key and webhook secret. A blank secret keeps the stored one. The provider and GitHub App id are immutable. Org admin only.
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        appId path string true "App row ID (UUID)"
// @Param        body body updateSourceAppDTO true "App label; omit or blank a secret to keep the stored value"
// @Success      200 {object} SourceApp
// @Failure      400 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/orgs/{orgId}/source-apps/{appId} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateSourceApp(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("appId"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	var dto updateSourceAppDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	app, err := h.svc.UpdateSourceApp(c.Request().Context(), orgID, user.ID, id, UpdateSourceAppRequest(dto))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, app)
}

// ListSourceTriggers handles GET /api/data-source-integrations/:id/triggers.
// @Summary      List source triggers
// @Description  Returns a source's event-to-agent bindings.
// @Tags         data-source-integrations
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Success      200 {array} SourceTrigger
// @Router       /api/data-source-integrations/{id}/triggers [get]
// @Security     bearerAuth
func (h *Handler) ListSourceTriggers(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	sourceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	triggers, err := h.svc.ListSourceTriggers(c.Request().Context(), projectID, sourceID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, triggers)
}

// CreateSourceTrigger handles POST /api/data-source-integrations/:id/triggers.
// @Summary      Create a source trigger
// @Description  Binds an event type to an agent and queue. agentId may be a runtime agent id or an agent-definition id; the binding stores the runtime kb.agents id either way.
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Param        body body sourceTriggerDTO true "Trigger binding"
// @Success      201 {object} SourceTrigger
// @Router       /api/data-source-integrations/{id}/triggers [post]
// @Security     bearerAuth
func (h *Handler) CreateSourceTrigger(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	sourceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	var dto sourceTriggerDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	trigger, err := h.svc.CreateSourceTrigger(c.Request().Context(), projectID, sourceID, SourceTriggerInput(dto))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, trigger)
}

// UpdateSourceTrigger handles PATCH /api/data-source-integrations/:id/triggers/:triggerId.
// @Summary      Update a source trigger
// @Description  Patches the event, agent, queue, filter, or enabled state.
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Param        triggerId path string true "Trigger ID (UUID)"
// @Param        body body sourceTriggerDTO true "Patch"
// @Success      200 {object} SourceTrigger
// @Router       /api/data-source-integrations/{id}/triggers/{triggerId} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateSourceTrigger(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	sourceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	triggerID, err := uuid.Parse(c.Param("triggerId"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	var dto sourceTriggerDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	trigger, err := h.svc.UpdateSourceTrigger(c.Request().Context(), projectID, sourceID, triggerID, SourceTriggerInput(dto))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, trigger)
}

// DeleteSourceTrigger handles DELETE /api/data-source-integrations/:id/triggers/:triggerId.
// @Summary      Delete a source trigger
// @Description  Removes the binding.
// @Tags         data-source-integrations
// @Param        id path string true "Source ID (UUID)"
// @Param        triggerId path string true "Trigger ID (UUID)"
// @Success      204
// @Router       /api/data-source-integrations/{id}/triggers/{triggerId} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteSourceTrigger(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	sourceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	triggerID, err := uuid.Parse(c.Param("triggerId"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	if err := h.svc.DeleteSourceTrigger(c.Request().Context(), projectID, sourceID, triggerID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// projectIDFromCaller resolves the X-Project-ID for the project-scoped group.
func (h *Handler) projectIDFromCaller(c echo.Context) (uuid.UUID, error) {
	raw, err := auth.GetProjectID(c)
	if err != nil {
		return uuid.Nil, apperror.ErrBadRequest
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.ErrBadRequest
	}
	return id, nil
}

// ListProjectSourceApps handles GET /api/data-source-integrations/source-apps.
// @Summary      List Apps available to the project
// @Description  Returns org-wide Apps plus Apps owned by the current project, without credentials.
// @Tags         data-source-integrations
// @Produce      json
// @Success      200 {array} SourceApp
// @Router       /api/data-source-integrations/source-apps [get]
// @Security     bearerAuth
func (h *Handler) ListProjectSourceApps(c echo.Context) error {
	projectID, err := h.projectIDFromCaller(c)
	if err != nil {
		return err
	}
	apps, err := h.svc.ListSourceAppsForProject(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, apps)
}

// RegisterProjectSourceApp handles POST /api/data-source-integrations/source-apps.
// @Summary      Register a source App for the project
// @Description  Org admins may register org-wide (scope=org) or project-owned (scope=project); project admins only project-owned.
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        body body registerSourceAppDTO true "App credentials"
// @Success      201 {object} SourceApp
// @Router       /api/data-source-integrations/source-apps [post]
// @Security     bearerAuth
func (h *Handler) RegisterProjectSourceApp(c echo.Context) error {
	projectID, err := h.projectIDFromCaller(c)
	if err != nil {
		return err
	}
	var dto registerSourceAppDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	scope := strings.ToLower(strings.TrimSpace(dto.Scope))
	if scope == "" {
		scope = "project"
	}
	if scope != "org" && scope != "project" {
		return apperror.NewBadRequest("scope must be project or org")
	}
	app, err := h.svc.RegisterProjectSourceApp(c.Request().Context(), projectID, user.ID, RegisterSourceAppRequest{
		Provider: dto.Provider, AppID: dto.AppID, Slug: dto.Slug, PrivateKey: dto.PrivateKey, WebhookSecret: dto.WebhookSecret, WebhookKey: dto.WebhookKey,
	}, scope == "org")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, app)
}

// UpdateProjectSourceApp handles PATCH /api/data-source-integrations/source-apps/:appId.
// @Summary      Update a source App from the project surface
// @Description  Project-owned Apps require project admin; org-wide Apps require org admin. Secrets are optional rotations.
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        appId path string true "App row ID (UUID)"
// @Param        body body updateSourceAppDTO true "Patch"
// @Success      200 {object} SourceApp
// @Router       /api/data-source-integrations/source-apps/{appId} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateProjectSourceApp(c echo.Context) error {
	projectID, err := h.projectIDFromCaller(c)
	if err != nil {
		return err
	}
	appID, err := uuid.Parse(c.Param("appId"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	var dto updateSourceAppDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	app, err := h.svc.UpdateSourceAppForProject(c.Request().Context(), projectID, user.ID, appID, UpdateSourceAppRequest{
		Slug: dto.Slug, PrivateKey: dto.PrivateKey, WebhookSecret: dto.WebhookSecret,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, app)
}

// DeleteProjectSourceApp handles DELETE /api/data-source-integrations/source-apps/:appId.
// @Summary      Delete a source App from the project surface
// @Description  Project-owned Apps require project admin; org-wide Apps require org admin. Refused with 409 while bound sources exist.
// @Tags         data-source-integrations
// @Param        appId path string true "App row ID (UUID)"
// @Success      204
// @Router       /api/data-source-integrations/source-apps/{appId} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteProjectSourceApp(c echo.Context) error {
	projectID, err := h.projectIDFromCaller(c)
	if err != nil {
		return err
	}
	appID, err := uuid.Parse(c.Param("appId"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	if err := h.svc.DeleteSourceAppForProject(c.Request().Context(), projectID, user.ID, appID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
