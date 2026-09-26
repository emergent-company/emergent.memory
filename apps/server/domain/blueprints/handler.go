package blueprints

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles HTTP requests for blueprints
type Handler struct {
	svc *Service
}

// NewHandler creates a new blueprints handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// CreateBlueprint handles POST /api/blueprints
// @Summary      Create blueprint
// @Description  Creates a new blueprint draft with the given name, version, and manifest
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        request body CreateBlueprintRequest true "Blueprint definition"
// @Success      201 {object} Blueprint "Created blueprint"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      409 {object} apperror.Error "Blueprint with name and version already exists"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints [post]
// @Security     bearerAuth
func (h *Handler) CreateBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	var req CreateBlueprintRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Private by default: when the caller has a project context and did not
	// explicitly request global scope, scope the new blueprint to the caller's
	// project. A caller without a project can only create global blueprints.
	projectID := user.ProjectID
	if req.ProjectID == nil && projectID != "" {
		req.ProjectID = &projectID
	}

	// A global-scope creation (project_id IS NULL) is a write to the
	// platform-global catalogue and requires superadmin_full; a project member
	// may only author project-private drafts (issue #1021/#1041). The authority
	// decision lives in the shared Service helper so the REST and MCP entrypoints
	// cannot drift.
	if req.ProjectID == nil {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(c.Request().Context()); err != nil {
			return err
		}
	}

	bp, err := h.svc.CreateBlueprint(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, bp)
}

// ListBlueprints handles GET /api/blueprints
// @Summary      List blueprints
// @Description  Returns blueprints, optionally filtered by name via the ?name= query parameter
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        name query string false "Filter by blueprint name"
// @Success      200 {array} Blueprint "Blueprints"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints [get]
// @Security     bearerAuth
func (h *Handler) ListBlueprints(c echo.Context) error {
	user := auth.MustGetUser(c)

	nameFilter := c.QueryParam("name")
	blueprints, err := h.svc.ListBlueprints(c.Request().Context(), user.ProjectID, nameFilter)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, blueprints)
}

// GetBlueprint handles GET /api/blueprints/:id
// @Summary      Get blueprint
// @Description  Returns a blueprint by ID
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Success      200 {object} Blueprint "Blueprint"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	bp, err := h.svc.GetBlueprint(c.Request().Context(), user.ProjectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, bp)
}

// UpdateBlueprint handles PUT /api/blueprints/:id
// @Summary      Update blueprint
// @Description  Updates a draft blueprint's description, author, and manifest
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Param        request body UpdateBlueprintRequest true "Fields to update"
// @Success      200 {object} Blueprint "Updated blueprint"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      409 {object} apperror.Error "Blueprint is not a draft"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id} [put]
// @Security     bearerAuth
func (h *Handler) UpdateBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	var req UpdateBlueprintRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	bp, err := h.svc.UpdateBlueprint(c.Request().Context(), user.ProjectID, id, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, bp)
}

// PublishBlueprint handles POST /api/blueprints/:id/publish
// @Summary      Publish blueprint
// @Description  Transitions a draft blueprint to published and computes a sha256 checksum of its manifest
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Success      200 {object} Blueprint "Published blueprint"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      409 {object} apperror.Error "Blueprint is not a draft"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id}/publish [post]
// @Security     bearerAuth
func (h *Handler) PublishBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	// Publishing a global blueprint is a platform-global catalogue write
	// (superadmin_full); publishing the caller's own private draft stays
	// project-tier and unchanged (issue #1021/#1041). Shared Service helper.
	target, err := h.svc.GetBlueprint(c.Request().Context(), user.ProjectID, id)
	if err != nil {
		return err
	}
	if target.ProjectID == nil {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(c.Request().Context()); err != nil {
			return err
		}
	}

	bp, err := h.svc.PublishBlueprint(c.Request().Context(), user.ProjectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, bp)
}

// DeprecateBlueprint handles POST /api/blueprints/:id/deprecate
// @Summary      Deprecate blueprint
// @Description  Transitions a blueprint to deprecated (from any status)
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Success      200 {object} Blueprint "Deprecated blueprint"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id}/deprecate [post]
// @Security     bearerAuth
func (h *Handler) DeprecateBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	// Deprecating a global blueprint is a platform-global catalogue write
	// (superadmin_full); deprecating the caller's own private blueprint stays
	// project-tier and unchanged (issue #1021/#1041). Shared Service helper.
	target, err := h.svc.GetBlueprint(c.Request().Context(), user.ProjectID, id)
	if err != nil {
		return err
	}
	if target.ProjectID == nil {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(c.Request().Context()); err != nil {
			return err
		}
	}

	bp, err := h.svc.DeprecateBlueprint(c.Request().Context(), user.ProjectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, bp)
}

// NewVersion handles POST /api/blueprints/:id/versions
// @Summary      Create new blueprint version
// @Description  Clones a blueprint into a new draft row with the given version
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Param        request body NewVersionRequest true "New version"
// @Success      201 {object} Blueprint "New blueprint version"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      409 {object} apperror.Error "Blueprint with name and version already exists"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id}/versions [post]
// @Security     bearerAuth
func (h *Handler) NewVersion(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	var req NewVersionRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Forking a blueprint with no project context clones it as a NEW global
	// version — a platform-global catalogue write requiring superadmin_full.
	// With a project context the clone is project-private (the documented
	// "fork a version" flow) and unchanged (issue #1021/#1041). Shared Service
	// helper.
	if user.ProjectID == "" {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(c.Request().Context()); err != nil {
			return err
		}
	}

	bp, err := h.svc.NewVersion(c.Request().Context(), user.ProjectID, id, req.Version)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, bp)
}

// ApplyBlueprint handles POST /api/blueprints/:id/apply
// @Summary      Apply blueprint
// @Description  Materializes a blueprint's manifest into the project: schema packs, agent definitions, global skills, and seed graph objects/relationships. Idempotent create-or-update by name.
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Param        request body ApplyOptions false "Apply options"
// @Success      200 {object} ApplyResult "Apply result"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      409 {object} apperror.Error "Blueprint is deprecated"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id}/apply [post]
// @Security     bearerAuth
func (h *Handler) ApplyBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	projectID := user.ProjectID
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project context required (X-Project-ID header or API token)")
	}

	var req ApplyOptions
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	result, err := h.svc.Apply(c.Request().Context(), id, projectID, user.ID, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// DeleteBlueprint handles DELETE /api/blueprints/:id
// @Summary      Delete blueprint
// @Description  Deletes a draft blueprint
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Success      204 "No content"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      409 {object} apperror.Error "Blueprint is not a draft"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	if err := h.svc.DeleteBlueprint(c.Request().Context(), user.ProjectID, id); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// ListVersionsByName handles GET /api/blueprints/name/:name/versions
// @Summary      List blueprint versions
// @Description  Returns all versions of a blueprint name
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        name path string true "Blueprint name"
// @Success      200 {array} Blueprint "Blueprint versions"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/name/{name}/versions [get]
// @Security     bearerAuth
func (h *Handler) ListVersionsByName(c echo.Context) error {
	user := auth.MustGetUser(c)

	name := c.Param("name")
	if name == "" {
		return apperror.ErrBadRequest.WithMessage("name is required")
	}

	blueprints, err := h.svc.ListVersions(c.Request().Context(), user.ProjectID, name)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, blueprints)
}

// ListAppliedBlueprints handles GET /api/blueprints/applied.
// @Summary      List applied blueprints
// @Description  Returns the blueprints applied to the caller's project, joined with blueprint metadata and the last-applied manifest checksum
// @Tags         blueprints
// @Produce      json
// @Success      200 {array} AppliedBlueprint "Applied blueprints"
// @Failure      400 {object} apperror.Error "Project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/applied [get]
// @Security     bearerAuth
func (h *Handler) ListAppliedBlueprints(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := user.ProjectID
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project context required (X-Project-ID header or API token)")
	}

	out, err := h.svc.ListApplied(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// UnapplyBlueprint handles POST /api/blueprints/:id/unapply.
// @Summary      Unapply blueprint
// @Description  Reverses a blueprint's apply for the caller's project: deletes its agents, soft-removes its pack assignments, and marks the application unapplied. Skills and seed objects are skipped (global/shared and unattributed respectively).
// @Tags         blueprints
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Success      200 {object} UnapplyResult "Unapply result"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      409 {object} apperror.Error "Blueprint not applied to this project"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id}/unapply [post]
// @Security     bearerAuth
func (h *Handler) UnapplyBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	projectID := user.ProjectID
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project context required (X-Project-ID header or API token)")
	}

	result, err := h.svc.Unapply(c.Request().Context(), id, projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}
