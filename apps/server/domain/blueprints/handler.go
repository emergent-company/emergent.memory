package blueprints

import (
	"net/http"
	"strings"

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

// ImportBlueprint handles POST /api/blueprints/import
// @Summary      Import blueprint from a GitHub URL
// @Description  Fetches a blueprint from https://github.com/<org>/<repo>[#ref], extracts and validates its manifest, and creates + publishes a blueprint in the caller's scope. The optional token is used only for private-repo fetch and is never stored or returned.
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        request body ImportGitHubRequest true "GitHub source"
// @Success      201 {object} Blueprint "Created blueprint"
// @Failure      400 {object} apperror.Error "Invalid URL or manifest"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      413 {object} apperror.Error "Archive too large"
// @Failure      502 {object} apperror.Error "Fetch failed"
// @Router       /api/blueprints/import [post]
// @Security     bearerAuth
func (h *Handler) ImportBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	var req ImportGitHubRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if strings.TrimSpace(req.URL) == "" {
		return apperror.NewBadRequest("url is required")
	}

	// Same scoping/authority as create: a project context produces a
	// project-private blueprint; a global (no project) import is a write to the
	// platform-global catalogue and requires superadmin_full.
	projectID := user.ProjectID
	if projectID == "" {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(c.Request().Context()); err != nil {
			return err
		}
	}

	bp, err := h.svc.ImportFromGitHub(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, bp)
}

// PreviewImport handles POST /api/blueprints/import/preview
// @Summary      Preview blueprint import from a GitHub URL
// @Description  Fetches a blueprint from https://github.com/<org>/<repo>[#ref], extracts and validates its manifest (applying any modelOverrides), and returns its metadata and manifest WITHOUT creating anything. The optional token is used only for private-repo fetch and is never stored or returned.
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        request body ImportGitHubRequest true "GitHub source"
// @Success      200 {object} BlueprintPreview "Import preview"
// @Failure      400 {object} apperror.Error "Invalid URL or manifest"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      413 {object} apperror.Error "Archive too large"
// @Failure      502 {object} apperror.Error "Fetch failed"
// @Router       /api/blueprints/import/preview [post]
// @Security     bearerAuth
func (h *Handler) PreviewImport(c echo.Context) error {
	user := auth.MustGetUser(c)

	var req ImportGitHubRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if strings.TrimSpace(req.URL) == "" {
		return apperror.NewBadRequest("url is required")
	}

	// Same scoping/authority as create/import: a global (no project) preview is
	// a read of the platform-global catalogue surface and requires superadmin.
	projectID := user.ProjectID
	if projectID == "" {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(c.Request().Context()); err != nil {
			return err
		}
	}

	preview, err := h.svc.PreviewFromGitHub(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	// Project-scoped previews also report object-type collisions an apply would
	// hit. Global previews (no project) have no registry to compare against.
	if projectID != "" {
		conflicts, cerr := h.svc.PreviewManifestConflicts(c.Request().Context(), projectID, preview.Manifest)
		if cerr != nil {
			return cerr
		}
		preview.TypeConflicts = conflicts
	}
	return c.JSON(http.StatusOK, preview)
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

// PreviewApplyBlueprint handles POST /api/blueprints/:id/apply/preview
// @Summary      Preview blueprint apply
// @Description  Read-only preview of the object-type name collisions that would block (or require merge confirmation for) applying this blueprint to the caller's project, including property-level diff detail. Materializes nothing.
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        id path string true "Blueprint ID (UUID)"
// @Success      200 {object} ApplyPreview "Apply preview"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Blueprint not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{id}/apply/preview [post]
// @Security     bearerAuth
func (h *Handler) PreviewApplyBlueprint(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("id is required")
	}

	projectID := user.ProjectID
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project context required (X-Project-ID header or API token)")
	}

	preview, err := h.svc.PreviewApply(c.Request().Context(), id, projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, preview)
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

// SetGithubCredentialRequest is the body for PUT /api/blueprints/github-credentials.
type SetGithubCredentialRequest struct {
	Token string `json:"token"`
}

// ModelOverridesResponse is the body for GET/PUT /api/blueprints/model-overrides:
// the project-scoped model overrides for one blueprint name as an agent-name ->
// model map.
type ModelOverridesResponse struct {
	ModelOverrides map[string]string `json:"modelOverrides"`
}

// UpsertModelOverridesRequest is the body for PUT /api/blueprints/model-overrides.
type UpsertModelOverridesRequest struct {
	Name           string            `json:"name"`
	ModelOverrides map[string]string `json:"modelOverrides"`
}

// GetModelOverrides handles GET /api/blueprints/model-overrides?name=<name>.
// @Summary      Get blueprint model overrides
// @Description  Returns the caller's project-scoped model overrides for a blueprint name (agent-name -> provider/model map). Requires project context.
// @Tags         blueprints
// @Produce      json
// @Param        name query string true "Blueprint name"
// @Success      200 {object} ModelOverridesResponse "Stored model overrides"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/model-overrides [get]
// @Security     bearerAuth
func (h *Handler) GetModelOverrides(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := user.ProjectID
	if projectID == "" {
		return apperror.NewBadRequest("project context required (X-Project-ID header or API token)")
	}
	name := strings.TrimSpace(c.QueryParam("name"))
	if name == "" {
		return apperror.NewBadRequest("name is required")
	}

	overrides, err := h.svc.GetModelOverrides(c.Request().Context(), projectID, name)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, ModelOverridesResponse{ModelOverrides: overrides})
}

// UpsertModelOverrides handles PUT /api/blueprints/model-overrides.
// @Summary      Set blueprint model overrides
// @Description  Upserts the caller's project-scoped model overrides for a blueprint name (agent-name -> provider/model map) and returns the resulting stored map. Requires project context.
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        request body UpsertModelOverridesRequest true "Blueprint name and overrides"
// @Success      200 {object} ModelOverridesResponse "Resulting stored model overrides"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/model-overrides [put]
// @Security     bearerAuth
func (h *Handler) UpsertModelOverrides(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := user.ProjectID
	if projectID == "" {
		return apperror.NewBadRequest("project context required (X-Project-ID header or API token)")
	}

	var req UpsertModelOverridesRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if strings.TrimSpace(req.Name) == "" {
		return apperror.NewBadRequest("name is required")
	}

	overrides, err := h.svc.UpsertModelOverrides(c.Request().Context(), projectID, req.Name, req.ModelOverrides)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, ModelOverridesResponse{ModelOverrides: overrides})
}

// CheckBlueprintUpdates handles POST /api/blueprints/:name/updates/check.
// @Summary      Check blueprint for updates
// @Description  Enqueues an update-check job for the caller's applied blueprint by name. The check runs asynchronously; returns the queued job.
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        name path string true "Blueprint name"
// @Success      202 {object} BlueprintUpdateJob "Queued update-check job"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{name}/updates/check [post]
// @Security     bearerAuth
func (h *Handler) CheckBlueprintUpdates(c echo.Context) error {
	user := auth.MustGetUser(c)

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("name is required")
	}
	projectID := user.ProjectID
	if projectID == "" {
		return apperror.NewBadRequest("project context required (X-Project-ID header or API token)")
	}

	job, err := h.svc.EnqueueUpdateCheck(c.Request().Context(), projectID, name)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, job)
}

// GetBlueprintUpdates handles GET /api/blueprints/:name/updates.
// @Summary      Get blueprint update status
// @Description  Returns the latest update-check result for the caller's applied blueprint by name, plus the latest update job (check or apply), when one exists.
// @Tags         blueprints
// @Produce      json
// @Param        name path string true "Blueprint name"
// @Success      200 {object} BlueprintUpdateStatus "Latest update-check result and job"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "No update status recorded"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{name}/updates [get]
// @Security     bearerAuth
func (h *Handler) GetBlueprintUpdates(c echo.Context) error {
	user := auth.MustGetUser(c)

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("name is required")
	}
	projectID := user.ProjectID
	if projectID == "" {
		return apperror.NewBadRequest("project context required (X-Project-ID header or API token)")
	}

	status, err := h.svc.GetUpdateStatus(c.Request().Context(), projectID, name)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, status)
}

// ApplyBlueprintUpdates handles POST /api/blueprints/:name/updates/apply.
// @Summary      Apply a blueprint update
// @Description  Enqueues an update (apply) job for the caller's applied blueprint by name. The job asynchronously fetches the upstream source, creates + publishes + applies a new version when upstream is newer, and returns the queued job. Rejected with 409 while an update for the same blueprint is already pending or running.
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        name path string true "Blueprint name"
// @Success      202 {object} BlueprintUpdateJob "Queued update job"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      409 {object} apperror.Error "An update is already pending or running"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{name}/updates/apply [post]
// @Security     bearerAuth
func (h *Handler) ApplyBlueprintUpdates(c echo.Context) error {
	user := auth.MustGetUser(c)

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("name is required")
	}
	projectID := user.ProjectID
	if projectID == "" {
		return apperror.NewBadRequest("project context required (X-Project-ID header or API token)")
	}

	var req ApplyBlueprintUpdateRequest
	if c.Request().ContentLength != 0 {
		if err := c.Bind(&req); err != nil {
			return apperror.NewBadRequest("invalid request body")
		}
	}

	job, err := h.svc.EnqueueBlueprintUpdateWithOptions(c.Request().Context(), projectID, name, UpdateJobOptions{
		MergeTypeConflicts: req.MergeTypeConflicts,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, job)
}

// ApplyBlueprintUpdateRequest is the optional body for
// POST /api/blueprints/:name/updates/apply.
type ApplyBlueprintUpdateRequest struct {
	MergeTypeConflicts bool `json:"mergeTypeConflicts,omitempty"`
}

// PreviewBlueprintUpdates handles POST /api/blueprints/:name/updates/preview.
// @Summary      Preview a blueprint update
// @Description  Fetches the upstream manifest for the caller's applied GitHub blueprint and reports the object-type collisions an update apply would hit, WITHOUT creating, publishing, or applying a new version.
// @Tags         blueprints
// @Produce      json
// @Param        name path string true "Blueprint name"
// @Success      200 {object} BlueprintUpdatePreview "Update preview"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Applied GitHub blueprint not found"
// @Failure      502 {object} apperror.Error "Fetch failed"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/{name}/updates/preview [post]
// @Security     bearerAuth
func (h *Handler) PreviewBlueprintUpdates(c echo.Context) error {
	user := auth.MustGetUser(c)

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("name is required")
	}
	projectID := user.ProjectID
	if projectID == "" {
		return apperror.NewBadRequest("project context required (X-Project-ID header or API token)")
	}

	preview, err := h.svc.PreviewBlueprintUpdate(c.Request().Context(), projectID, name)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, preview)
}

// SaveGithubCredentials handles PUT /api/blueprints/github-credentials.
// @Summary      Set project GitHub credential
// @Description  Stores (or replaces) the caller's project-scoped GitHub personal access token, encrypted at rest. The token is never echoed.
// @Tags         blueprints
// @Accept       json
// @Produce      json
// @Param        request body SetGithubCredentialRequest true "GitHub token"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Bad request / project context required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      409 {object} apperror.Error "Encryption key not configured"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/blueprints/github-credentials [put]
// @Security     bearerAuth
func (h *Handler) SaveGithubCredentials(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := user.ProjectID
	if projectID == "" {
		return apperror.NewBadRequest("project context required (X-Project-ID header or API token)")
	}

	var req SetGithubCredentialRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if err := h.svc.SaveGithubCredential(c.Request().Context(), projectID, req.Token); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
