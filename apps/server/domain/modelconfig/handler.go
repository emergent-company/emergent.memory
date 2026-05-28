package modelconfig

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Handler exposes model config endpoints.
type Handler struct {
	svc *Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// --- Project endpoints ---

// GetProjectModelConfig godoc
// @Summary Get project model config
// @Tags model-config
// @Produce json
// @Param projectId path string true "Project ID"
// @Success 200 {object} ModelConfigResponse
// @Router /projects/{projectId}/model-config [get]
func (h *Handler) GetProjectModelConfig(c echo.Context) error {
	projectID, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid project id")
	}
	resp, err := h.svc.GetProjectModelConfig(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	if resp == nil {
		return c.JSON(http.StatusOK, ModelConfigResponse{})
	}
	return c.JSON(http.StatusOK, resp)
}

// UpsertProjectModelConfig godoc
// @Summary Set project model config
// @Tags model-config
// @Accept json
// @Produce json
// @Param projectId path string true "Project ID"
// @Param body body UpsertModelConfigRequest true "Model config"
// @Success 200 {object} ModelConfigResponse
// @Router /projects/{projectId}/model-config [put]
func (h *Handler) UpsertProjectModelConfig(c echo.Context) error {
	projectID, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid project id")
	}
	var req UpsertModelConfigRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, err := h.svc.UpsertProjectModelConfig(c.Request().Context(), projectID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// DeleteProjectModelConfig godoc
// @Summary Clear project model config
// @Tags model-config
// @Param projectId path string true "Project ID"
// @Success 204
// @Router /projects/{projectId}/model-config [delete]
func (h *Handler) DeleteProjectModelConfig(c echo.Context) error {
	projectID, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid project id")
	}
	if err := h.svc.DeleteProjectModelConfig(c.Request().Context(), projectID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// GetEffectiveModelConfig godoc
// @Summary Get effective model config for a project (resolved)
// @Tags model-config
// @Produce json
// @Param projectId path string true "Project ID"
// @Success 200 {object} EffectiveModelConfig
// @Router /projects/{projectId}/model-config/effective [get]
func (h *Handler) GetEffectiveModelConfig(c echo.Context) error {
	projectID, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid project id")
	}
	resp, err := h.svc.ResolveEffectiveModels(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// --- Org endpoints ---

// GetOrgModelConfig godoc
// @Summary Get org model config
// @Tags model-config
// @Produce json
// @Param orgId path string true "Org ID"
// @Success 200 {object} ModelConfigResponse
// @Router /orgs/{orgId}/model-config [get]
func (h *Handler) GetOrgModelConfig(c echo.Context) error {
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid org id")
	}
	resp, err := h.svc.GetOrgModelConfig(c.Request().Context(), orgID)
	if err != nil {
		return err
	}
	if resp == nil {
		return c.JSON(http.StatusOK, ModelConfigResponse{})
	}
	return c.JSON(http.StatusOK, resp)
}

// UpsertOrgModelConfig godoc
// @Summary Set org model config
// @Tags model-config
// @Accept json
// @Produce json
// @Param orgId path string true "Org ID"
// @Param body body UpsertModelConfigRequest true "Model config"
// @Success 200 {object} ModelConfigResponse
// @Router /orgs/{orgId}/model-config [put]
func (h *Handler) UpsertOrgModelConfig(c echo.Context) error {
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid org id")
	}
	var req UpsertModelConfigRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, err := h.svc.UpsertOrgModelConfig(c.Request().Context(), orgID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// DeleteOrgModelConfig godoc
// @Summary Clear org model config
// @Tags model-config
// @Param orgId path string true "Org ID"
// @Success 204
// @Router /orgs/{orgId}/model-config [delete]
func (h *Handler) DeleteOrgModelConfig(c echo.Context) error {
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid org id")
	}
	if err := h.svc.DeleteOrgModelConfig(c.Request().Context(), orgID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
