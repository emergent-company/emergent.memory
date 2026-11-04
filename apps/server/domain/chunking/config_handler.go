package chunking

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// GetChunkingConfig handles GET /api/chunking/config
// @Summary      Get project chunking configuration
// @Description  Returns the active project's chunking strategy and parameters (or defaults)
// @Tags         chunking
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} ChunkingConfigResponse
// @Failure      401 {object} apperror.Error
// @Router       /api/chunking/config [get]
// @Security     bearerAuth
func (h *Handler) GetChunkingConfig(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("x-project-id header required")
	}
	resp, err := h.svc.GetChunkingConfig(c.Request().Context(), user.ProjectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// UpdateChunkingConfig handles PUT /api/chunking/config
// @Summary      Update project chunking configuration
// @Description  Validates and persists the active project's chunking strategy and parameters
// @Tags         chunking
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        body body UpdateChunkingConfigRequest true "Chunking configuration"
// @Success      200 {object} ChunkingConfigResponse
// @Failure      400 {object} apperror.Error
// @Router       /api/chunking/config [put]
// @Security     bearerAuth
func (h *Handler) UpdateChunkingConfig(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("x-project-id header required")
	}
	var req UpdateChunkingConfigRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	resp, err := h.svc.UpdateChunkingConfig(c.Request().Context(), user.ProjectID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
