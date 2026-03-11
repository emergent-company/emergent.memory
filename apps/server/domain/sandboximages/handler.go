package sandboximages

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler provides HTTP endpoints for workspace image management.
type Handler struct {
	svc *Service
}

// NewHandler creates a new workspace images handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List returns all sandbox images for the current project.
// @Summary      List sandbox images
// @Description  Returns all registered sandbox images (built-in and custom) for the project
// @Tags         sandbox-images
// @Produce      json
// @Success      200 {object} ListResponse[SandboxImageDTO]
// @Failure      401 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/sandbox-images [get]
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

	dtos := make([]SandboxImageDTO, len(images))
	for i, img := range images {
		dtos[i] = img.ToDTO()
	}
	return c.JSON(http.StatusOK, ListResponse[SandboxImageDTO]{Data: dtos})
}

// Get returns a single sandbox image by ID.
// @Summary      Get sandbox image
// @Description  Returns details of a specific sandbox image
// @Tags         sandbox-images
// @Produce      json
// @Param        id path string true "Image ID"
// @Success      200 {object} APIResponse[SandboxImageDTO]
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/admin/sandbox-images/{id} [get]
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

	return c.JSON(http.StatusOK, APIResponse[SandboxImageDTO]{Data: img.ToDTO()})
}

// Create registers a new sandbox image.
// @Summary      Create sandbox image
// @Description  Registers a new sandbox image. For Docker refs, triggers background pull.
// @Tags         sandbox-images
// @Accept       json
// @Produce      json
// @Param        body body CreateSandboxImageRequest true "Image details"
// @Success      201 {object} APIResponse[SandboxImageDTO]
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      409 {object} apperror.Error
// @Router       /api/admin/sandbox-images [post]
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	var req CreateSandboxImageRequest
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

	return c.JSON(http.StatusCreated, APIResponse[SandboxImageDTO]{Data: img.ToDTO()})
}

// Delete removes a custom sandbox image.
// @Summary      Delete sandbox image
// @Description  Removes a custom sandbox image. Built-in images cannot be deleted.
// @Tags         sandbox-images
// @Param        id path string true "Image ID"
// @Success      204
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/admin/sandbox-images/{id} [delete]
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
