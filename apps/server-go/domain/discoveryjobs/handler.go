package discoveryjobs

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for discovery jobs
type Handler struct {
	svc *Service
}

// NewHandler creates a new discovery jobs handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// StartDiscovery handles POST /discovery-jobs/projects/:projectId/start
// @Summary      Start discovery job
// @Description  Initiates a discovery job to analyze documents and extract graph object types and relationships
// @Tags         discovery-jobs
// @Accept       json
// @Produce      json
// @Param        X-Org-ID header string true "Organization ID"
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body StartDiscoveryRequest true "Discovery configuration"
// @Success      200 {object} StartDiscoveryResponse "Discovery job started"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /discovery-jobs/projects/{projectId}/start [post]
// @Security     bearerAuth
func (h *Handler) StartDiscovery(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectIDStr := c.Param("projectId")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project ID")
	}

	// Get org ID from header
	orgIDStr := c.Request().Header.Get("X-Org-ID")
	if orgIDStr == "" {
		return apperror.ErrBadRequest.WithMessage("X-Org-ID header required")
	}
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid org ID")
	}

	var req StartDiscoveryRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if len(req.DocumentIDs) == 0 {
		return apperror.ErrBadRequest.WithMessage("document_ids array is required and cannot be empty")
	}

	result, err := h.svc.StartDiscovery(c.Request().Context(), projectID, orgID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetJobStatus handles GET /discovery-jobs/:jobId
// @Summary      Get discovery job status
// @Description  Retrieves the current status, progress, and results of a discovery job
// @Tags         discovery-jobs
// @Accept       json
// @Produce      json
// @Param        jobId path string true "Job ID (UUID)"
// @Success      200 {object} JobStatusResponse "Job status details"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /discovery-jobs/{jobId} [get]
// @Security     bearerAuth
func (h *Handler) GetJobStatus(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobIDStr := c.Param("jobId")
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid job ID")
	}

	result, err := h.svc.GetJobStatus(c.Request().Context(), jobID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// ListJobs handles GET /discovery-jobs/projects/:projectId
// @Summary      List discovery jobs
// @Description  Returns all discovery jobs for a project
// @Tags         discovery-jobs
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} JobListItem "List of jobs"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /discovery-jobs/projects/{projectId} [get]
// @Security     bearerAuth
func (h *Handler) ListJobs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectIDStr := c.Param("projectId")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project ID")
	}

	result, err := h.svc.ListJobsForProject(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// CancelJob handles DELETE /discovery-jobs/:jobId
// @Summary      Cancel discovery job
// @Description  Cancels a running discovery job
// @Tags         discovery-jobs
// @Accept       json
// @Produce      json
// @Param        jobId path string true "Job ID (UUID)"
// @Success      200 {object} CancelJobResponse "Job cancelled"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /discovery-jobs/{jobId} [delete]
// @Security     bearerAuth
func (h *Handler) CancelJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobIDStr := c.Param("jobId")
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid job ID")
	}

	if err := h.svc.CancelJob(c.Request().Context(), jobID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, CancelJobResponse{
		Message: "Discovery job cancelled",
	})
}

// FinalizeDiscovery handles POST /discovery-jobs/:jobId/finalize
// @Summary      Finalize discovery job
// @Description  Creates or extends a template pack from discovered types and relationships
// @Tags         discovery-jobs
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        X-Org-ID header string true "Organization ID"
// @Param        jobId path string true "Job ID (UUID)"
// @Param        request body FinalizeDiscoveryRequest true "Finalization configuration"
// @Success      200 {object} FinalizeDiscoveryResponse "Template pack created"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /discovery-jobs/{jobId}/finalize [post]
// @Security     bearerAuth
func (h *Handler) FinalizeDiscovery(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobIDStr := c.Param("jobId")
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid job ID")
	}

	// Get project and org IDs from headers
	projectIDStr := c.Request().Header.Get("X-Project-ID")
	if projectIDStr == "" {
		return apperror.ErrBadRequest.WithMessage("X-Project-ID header required")
	}
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project ID")
	}

	orgIDStr := c.Request().Header.Get("X-Org-ID")
	if orgIDStr == "" {
		return apperror.ErrBadRequest.WithMessage("X-Org-ID header required")
	}
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid org ID")
	}

	var req FinalizeDiscoveryRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.PackName == "" {
		return apperror.ErrBadRequest.WithMessage("packName is required")
	}
	if req.Mode != "create" && req.Mode != "extend" {
		return apperror.ErrBadRequest.WithMessage("mode must be 'create' or 'extend'")
	}
	if len(req.IncludedTypes) == 0 {
		return apperror.ErrBadRequest.WithMessage("includedTypes is required and cannot be empty")
	}

	result, err := h.svc.FinalizeDiscovery(c.Request().Context(), jobID, projectID, orgID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}
