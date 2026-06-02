package extraction

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ProjectEmbeddingHandler exposes project-scoped embedding management endpoints.
// All operations are restricted to project admins.
type ProjectEmbeddingHandler struct {
	graphJobs   *GraphEmbeddingJobsService
	chunkJobs   *ChunkEmbeddingJobsService
	apitokenSvc *apitoken.Service
}

// NewProjectEmbeddingHandler creates a new project embedding handler.
func NewProjectEmbeddingHandler(
	graphJobs *GraphEmbeddingJobsService,
	chunkJobs *ChunkEmbeddingJobsService,
	apitokenSvc *apitoken.Service,
) *ProjectEmbeddingHandler {
	return &ProjectEmbeddingHandler{
		graphJobs:   graphJobs,
		chunkJobs:   chunkJobs,
		apitokenSvc: apitokenSvc,
	}
}

// requireProjectAdmin checks that the authenticated user is a project_admin for the given project.
func (h *ProjectEmbeddingHandler) requireProjectAdmin(c echo.Context, projectID string) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	role, err := h.apitokenSvc.GetUserProjectRole(c.Request().Context(), projectID, user.ID)
	if err != nil {
		return apperror.NewInternal("check project role", err)
	}
	if role != projects.RoleProjectAdmin {
		return apperror.ErrForbidden.WithMessage("project admin role required")
	}
	return nil
}

// ProjectEmbeddingProgressResponse is the response for the progress endpoint.
type ProjectEmbeddingProgressResponse struct {
	Objects       *GraphEmbeddingQueueStats `json:"objects"`
	Relationships *GraphEmbeddingQueueStats `json:"relationships"`
	Chunks        *ChunkEmbeddingQueueStats `json:"chunks"`
}

// Progress handles GET /api/projects/:id/embeddings/progress
// @Summary      Get embedding queue progress for a project
// @Description  Returns pending/processing/completed/failed counts for graph object, relationship, and chunk embedding jobs scoped to this project. Requires project_admin role.
// @Tags         embeddings
// @Produce      json
// @Param        id   path      string  true  "Project ID"
// @Success      200  {object}  ProjectEmbeddingProgressResponse
// @Failure      401  {object}  apperror.Error
// @Failure      403  {object}  apperror.Error
// @Router       /api/projects/{id}/embeddings/progress [get]
func (h *ProjectEmbeddingHandler) Progress(c echo.Context) error {
	projectID := c.Param("id")
	if err := h.requireProjectAdmin(c, projectID); err != nil {
		return err
	}

	ctx := c.Request().Context()

	objStats, err := h.graphJobs.StatsByProject(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("get object embedding stats", err)
	}

	// Graph relationship embedding jobs use the same table but object_id points to
	// relationship objects — they share graphJobs. Relationship-specific stats would
	// require filtering by object type; for now we surface total graph stats once.
	// A separate rel stats field is left nil to avoid double-counting.

	chunkStats, err := h.chunkJobs.StatsByProject(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("get chunk embedding stats", err)
	}

	return c.JSON(http.StatusOK, ProjectEmbeddingProgressResponse{
		Objects: objStats,
		Chunks:  chunkStats,
	})
}

// ProjectEmbeddingRetriggerResponse is the response for the retrigger endpoint.
type ProjectEmbeddingRetriggerResponse struct {
	Message      string `json:"message"`
	ObjectsReset int    `json:"objects_reset"`
	ChunksReset  int    `json:"chunks_reset"`
}

// Retrigger handles POST /api/projects/:id/embeddings/retrigger
// @Summary      Retrigger failed embedding jobs for a project
// @Description  Resets all failed and dead_letter embedding jobs (graph objects and chunks) for this project back to pending so they are retried. Requires project_admin role.
// @Tags         embeddings
// @Produce      json
// @Param        id   path      string  true  "Project ID"
// @Success      200  {object}  ProjectEmbeddingRetriggerResponse
// @Failure      401  {object}  apperror.Error
// @Failure      403  {object}  apperror.Error
// @Router       /api/projects/{id}/embeddings/retrigger [post]
func (h *ProjectEmbeddingHandler) Retrigger(c echo.Context) error {
	projectID := c.Param("id")
	if err := h.requireProjectAdmin(c, projectID); err != nil {
		return err
	}

	ctx := c.Request().Context()

	objN, err := h.graphJobs.RetriggerByProject(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("retrigger object embedding jobs", err)
	}

	chunkN, err := h.chunkJobs.RetriggerByProject(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("retrigger chunk embedding jobs", err)
	}

	return c.JSON(http.StatusOK, ProjectEmbeddingRetriggerResponse{
		Message:      "failed embedding jobs reset to pending",
		ObjectsReset: objN,
		ChunksReset:  chunkN,
	})
}

// ProjectEmbeddingCancelResponse is the response for the cancel endpoint.
type ProjectEmbeddingCancelResponse struct {
	Message          string `json:"message"`
	ObjectsCancelled int    `json:"objects_cancelled"`
	ChunksCancelled  int    `json:"chunks_cancelled"`
}

// Cancel handles DELETE /api/projects/:id/embeddings/queue
// @Summary      Cancel pending embedding jobs for a project
// @Description  Deletes all pending and processing embedding jobs (graph objects and chunks) for this project. Effectively stops queued embeddings. Requires project_admin role.
// @Tags         embeddings
// @Produce      json
// @Param        id   path      string  true  "Project ID"
// @Success      200  {object}  ProjectEmbeddingCancelResponse
// @Failure      401  {object}  apperror.Error
// @Failure      403  {object}  apperror.Error
// @Router       /api/projects/{id}/embeddings/queue [delete]
func (h *ProjectEmbeddingHandler) Cancel(c echo.Context) error {
	projectID := c.Param("id")
	if err := h.requireProjectAdmin(c, projectID); err != nil {
		return err
	}

	ctx := c.Request().Context()

	objN, err := h.graphJobs.CancelByProject(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("cancel object embedding jobs", err)
	}

	chunkN, err := h.chunkJobs.CancelByProject(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("cancel chunk embedding jobs", err)
	}

	return c.JSON(http.StatusOK, ProjectEmbeddingCancelResponse{
		Message:          "pending embedding jobs cancelled",
		ObjectsCancelled: objN,
		ChunksCancelled:  chunkN,
	})
}
