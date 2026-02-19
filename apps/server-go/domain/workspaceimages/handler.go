package workspaceimages

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

// Handler provides HTTP endpoints for workspace image management.
type Handler struct {
	svc *Service
}

// NewHandler creates a new workspace images handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List returns all workspace images for the current project.
// @Summary      List workspace images
// @Description  Returns all registered workspace images (built-in and custom) for the project
// @Tags         workspace-images
// @Produce      json
// @Success      200 {object} ListResponse[WorkspaceImageDTO]
// @Failure      401 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/workspace-images [get]
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	images, err := h.svc.List(c.Request().Context(), user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to list workspace images", err)
	}

	dtos := make([]WorkspaceImageDTO, len(images))
	for i, img := range images {
		dtos[i] = img.ToDTO()
	}
	return c.JSON(http.StatusOK, ListResponse[WorkspaceImageDTO]{Data: dtos})
}

// Get returns a single workspace image by ID.
// @Summary      Get workspace image
// @Description  Returns details of a specific workspace image
// @Tags         workspace-images
// @Produce      json
// @Param        id path string true "Image ID"
// @Success      200 {object} APIResponse[WorkspaceImageDTO]
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/admin/workspace-images/{id} [get]
func (h *Handler) Get(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("image ID is required")
	}

	img, err := h.svc.Get(c.Request().Context(), id)
	if err != nil {
		return apperror.NewInternal("failed to get workspace image", err)
	}
	if img == nil {
		return apperror.NewNotFound("workspace_image", id)
	}

	return c.JSON(http.StatusOK, APIResponse[WorkspaceImageDTO]{Data: img.ToDTO()})
}

// Create registers a new workspace image.
// @Summary      Create workspace image
// @Description  Registers a new workspace image. For Docker refs, triggers background pull.
// @Tags         workspace-images
// @Accept       json
// @Produce      json
// @Param        body body CreateWorkspaceImageRequest true "Image details"
// @Success      201 {object} APIResponse[WorkspaceImageDTO]
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      409 {object} apperror.Error
// @Router       /api/admin/workspace-images [post]
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	var req CreateWorkspaceImageRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	img, err := h.svc.Create(c.Request().Context(), user.ProjectID, &req)
	if err != nil {
		if err == ErrNameRequired {
			return apperror.NewBadRequest(err.Error())
		}
		if err == ErrNameConflict {
			return c.JSON(http.StatusConflict, map[string]string{
				"error":   "conflict",
				"message": err.Error(),
			})
		}
		return apperror.NewInternal("failed to create workspace image", err)
	}

	return c.JSON(http.StatusCreated, APIResponse[WorkspaceImageDTO]{Data: img.ToDTO()})
}

// Delete removes a custom workspace image.
// @Summary      Delete workspace image
// @Description  Removes a custom workspace image. Built-in images cannot be deleted.
// @Tags         workspace-images
// @Param        id path string true "Image ID"
// @Success      204
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/admin/workspace-images/{id} [delete]
func (h *Handler) Delete(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("image ID is required")
	}

	err := h.svc.Delete(c.Request().Context(), id)
	if err != nil {
		if err == ErrNotFound {
			return apperror.NewNotFound("workspace_image", id)
		}
		if err == ErrBuiltInImmutable {
			return apperror.NewBadRequest(err.Error())
		}
		return apperror.NewInternal("failed to delete workspace image", err)
	}

	return c.NoContent(http.StatusNoContent)
}
