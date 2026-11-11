package sources

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

type registerSourceAppDTO struct {
	Provider      string `json:"provider"`
	AppID         int64  `json:"appId"`
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
	app, err := h.svc.RegisterSourceApp(c.Request().Context(), orgID, user.ID, RegisterSourceAppRequest(dto))
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
// @Description  Binds an event type to a runtime agent and queue.
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
