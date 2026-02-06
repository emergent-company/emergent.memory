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
