package extraction

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// AdminHandler handles HTTP requests for extraction jobs admin API
type AdminHandler struct {
	jobsService *ObjectExtractionJobsService
}

// NewAdminHandler creates a new extraction jobs admin handler
func NewAdminHandler(jobsService *ObjectExtractionJobsService) *AdminHandler {
	return &AdminHandler{jobsService: jobsService}
}

// CreateJob handles POST /api/admin/extraction-jobs
// Creates a new extraction job
func (h *AdminHandler) CreateJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var dto CreateExtractionJobDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Resolve project ID from body or header
	projectID := ""
	if dto.ProjectID != nil && *dto.ProjectID != "" {
		projectID = *dto.ProjectID
	} else if user.ProjectID != "" {
		projectID = user.ProjectID
	}

	if projectID == "" {
		return apperror.NewBadRequest("project_id is required")
	}

	// Validate project ID matches header if both are provided
	if user.ProjectID != "" && dto.ProjectID != nil && *dto.ProjectID != "" && *dto.ProjectID != user.ProjectID {
		return apperror.NewBadRequest("project_id mismatch between body and X-Project-ID header")
	}

	// Convert source type to string
	sourceType := string(dto.SourceType)
	if sourceType == "" {
		sourceType = "document"
	}

	// Create job
	opts := CreateObjectExtractionJobOptions{
		ProjectID:        projectID,
		SourceType:       &sourceType,
		SourceID:         dto.SourceID,
		SourceMetadata:   dto.SourceMetadata,
		ExtractionConfig: dto.ExtractionConfig,
		CreatedBy:        dto.SubjectID,
	}

	// When source_type is "document", also set DocumentID for the worker
	if sourceType == "document" && dto.SourceID != nil {
		opts.DocumentID = dto.SourceID
	}

	// Handle extraction config target_types -> enabled_types
	if dto.ExtractionConfig != nil {
		if targetTypes, ok := dto.ExtractionConfig["target_types"].([]interface{}); ok {
			enabledTypes := make([]string, 0, len(targetTypes))
			for _, t := range targetTypes {
				if s, ok := t.(string); ok {
					enabledTypes = append(enabledTypes, s)
				}
			}
			opts.EnabledTypes = enabledTypes
		}
	}

	job, err := h.jobsService.CreateJob(c.Request().Context(), opts)
	if err != nil {
		return apperror.NewInternal("failed to create extraction job", err)
	}

	return c.JSON(http.StatusCreated, SuccessResponse(job.ToDTO()))
}

// ListJobs handles GET /api/admin/extraction-jobs/projects/:projectId
// Lists extraction jobs for a project with pagination
func (h *AdminHandler) ListJobs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	// Validate project ID matches header
	if user.ProjectID != "" && projectID != user.ProjectID {
		return apperror.NewBadRequest("projectId mismatch with X-Project-ID header")
	}

	// Parse query parameters
	page := 1
	if pageStr := c.QueryParam("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	limit := 20
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	var status *JobStatus
	if statusStr := c.QueryParam("status"); statusStr != "" {
		dtoStatus := ExtractionJobStatusDTO(statusStr)
		s := mapJobStatusFromDTO(dtoStatus)
		status = &s
	}

	var sourceType *string
	if st := c.QueryParam("source_type"); st != "" {
		sourceType = &st
	}

	var sourceID *string
	if sid := c.QueryParam("source_id"); sid != "" {
		sourceID = &sid
	}

	jobs, total, err := h.jobsService.FindByProjectPaginated(c.Request().Context(), projectID, status, sourceType, sourceID, page, limit)
	if err != nil {
		return apperror.NewInternal("failed to list extraction jobs", err)
	}

	// Convert to DTOs
	dtos := make([]*ExtractionJobDTO, len(jobs))
	for i, job := range jobs {
		dtos[i] = job.ToDTO()
	}

	totalPages := (total + limit - 1) / limit

	return c.JSON(http.StatusOK, SuccessResponse(ExtractionJobListDTO{
		Jobs:       dtos,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	}))
}

// GetJob handles GET /api/admin/extraction-jobs/:jobId
// Gets a single extraction job by ID
func (h *AdminHandler) GetJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobID := c.Param("jobId")
	if jobID == "" {
		return apperror.NewBadRequest("jobId is required")
	}

	job, err := h.jobsService.FindByID(c.Request().Context(), jobID)
	if err != nil {
		return apperror.NewInternal("failed to get extraction job", err)
	}
	if job == nil {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	// Verify project access if project header is set
	if user.ProjectID != "" && job.ProjectID != user.ProjectID {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	return c.JSON(http.StatusOK, SuccessResponse(job.ToDTO()))
}

// UpdateJob handles PATCH /api/admin/extraction-jobs/:jobId
// Updates an extraction job
func (h *AdminHandler) UpdateJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobID := c.Param("jobId")
	if jobID == "" {
		return apperror.NewBadRequest("jobId is required")
	}

	var dto UpdateExtractionJobDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Get existing job
	job, err := h.jobsService.FindByID(c.Request().Context(), jobID)
	if err != nil {
		return apperror.NewInternal("failed to get extraction job", err)
	}
	if job == nil {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	// Verify project access if project header is set
	if user.ProjectID != "" && job.ProjectID != user.ProjectID {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	// Apply updates
	if dto.Status != nil {
		job.Status = mapJobStatusFromDTO(*dto.Status)
	}
	if dto.TotalItems != nil {
		job.TotalItems = *dto.TotalItems
	}
	if dto.ProcessedItems != nil {
		job.ProcessedItems = *dto.ProcessedItems
	}
	if dto.SuccessfulItems != nil {
		job.SuccessfulItems = *dto.SuccessfulItems
	}
	if dto.FailedItems != nil {
		job.FailedItems = *dto.FailedItems
	}
	if dto.DiscoveredTypes != nil {
		arr := make(JSONArray, len(dto.DiscoveredTypes))
		for i, t := range dto.DiscoveredTypes {
			arr[i] = t
		}
		job.DiscoveredTypes = arr
	}
	if dto.CreatedObjects != nil {
		arr := make(JSONArray, len(dto.CreatedObjects))
		for i, o := range dto.CreatedObjects {
			arr[i] = o
		}
		job.CreatedObjects = arr
	}
	if dto.ErrorMessage != nil {
		job.ErrorMessage = dto.ErrorMessage
	}
	if dto.ErrorDetails != nil {
		job.ErrorDetails = dto.ErrorDetails
	}
	if dto.DebugInfo != nil {
		job.DebugInfo = dto.DebugInfo
	}

	if err := h.jobsService.UpdateJob(c.Request().Context(), job); err != nil {
		return apperror.NewInternal("failed to update extraction job", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(job.ToDTO()))
}

// DeleteJob handles DELETE /api/admin/extraction-jobs/:jobId
// Deletes an extraction job
func (h *AdminHandler) DeleteJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobID := c.Param("jobId")
	if jobID == "" {
		return apperror.NewBadRequest("jobId is required")
	}

	// Get existing job to verify project access
	job, err := h.jobsService.FindByID(c.Request().Context(), jobID)
	if err != nil {
		return apperror.NewInternal("failed to get extraction job", err)
	}
	if job == nil {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	// Verify project access if project header is set
	projectID := job.ProjectID
	if user.ProjectID != "" && projectID != user.ProjectID {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	if err := h.jobsService.DeleteJob(c.Request().Context(), jobID, projectID); err != nil {
		return apperror.NewBadRequest(err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

// CancelJob handles POST /api/admin/extraction-jobs/:jobId/cancel
// Cancels a pending or running extraction job
func (h *AdminHandler) CancelJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobID := c.Param("jobId")
	if jobID == "" {
		return apperror.NewBadRequest("jobId is required")
	}

	// Get existing job to verify project access
	job, err := h.jobsService.FindByID(c.Request().Context(), jobID)
	if err != nil {
		return apperror.NewInternal("failed to get extraction job", err)
	}
	if job == nil {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	// Verify project access if project header is set
	if user.ProjectID != "" && job.ProjectID != user.ProjectID {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	if err := h.jobsService.CancelJob(c.Request().Context(), jobID); err != nil {
		return apperror.NewBadRequest(err.Error())
	}

	// Fetch updated job
	job, _ = h.jobsService.FindByID(c.Request().Context(), jobID)
	return c.JSON(http.StatusOK, SuccessResponse(job.ToDTO()))
}

// RetryJob handles POST /api/admin/extraction-jobs/:jobId/retry
// Retries a failed or stuck extraction job
func (h *AdminHandler) RetryJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobID := c.Param("jobId")
	if jobID == "" {
		return apperror.NewBadRequest("jobId is required")
	}

	// Get existing job to verify project access
	existingJob, err := h.jobsService.FindByID(c.Request().Context(), jobID)
	if err != nil {
		return apperror.NewInternal("failed to get extraction job", err)
	}
	if existingJob == nil {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	// Verify project access if project header is set
	projectID := existingJob.ProjectID
	if user.ProjectID != "" && projectID != user.ProjectID {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	job, err := h.jobsService.RetryJob(c.Request().Context(), jobID, projectID)
	if err != nil {
		return apperror.NewBadRequest(err.Error())
	}

	return c.JSON(http.StatusOK, SuccessResponse(job.ToDTO()))
}

// GetStatistics handles GET /api/admin/extraction-jobs/projects/:projectId/statistics
// Gets job statistics for a project
func (h *AdminHandler) GetStatistics(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	// Validate project ID matches header
	if user.ProjectID != "" && projectID != user.ProjectID {
		return apperror.NewBadRequest("projectId mismatch with X-Project-ID header")
	}

	stats, err := h.jobsService.GetStatistics(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to get statistics", err)
	}

	// Convert to DTO format expected by frontend
	jobsByStatus := make(map[ExtractionJobStatusDTO]int)
	for status, count := range stats.JobsByStatus {
		switch status {
		case "pending":
			jobsByStatus[StatusQueued] = int(count)
		case "processing":
			jobsByStatus[StatusRunning] = int(count)
		case "completed":
			jobsByStatus[StatusCompleted] = int(count)
		case "failed":
			jobsByStatus[StatusFailed] = int(count)
		case "cancelled":
			jobsByStatus[StatusCancelled] = int(count)
		}
	}

	response := ExtractionJobStatisticsDTO{
		TotalJobs:     int(stats.TotalJobs),
		JobsByStatus:  jobsByStatus,
		SuccessRate:   stats.SuccessRate,
		JobsThisWeek:  int(stats.JobsThisWeek),
		JobsThisMonth: int(stats.JobsThisMonth),
	}

	if stats.AverageProcessingTimeMs != nil {
		response.AverageProcessingTimeMs = stats.AverageProcessingTimeMs
	}

	return c.JSON(http.StatusOK, SuccessResponse(response))
}

// BulkCancelJobs handles POST /api/admin/extraction-jobs/projects/:projectId/bulk-cancel
// Cancels all pending/running jobs for a project
func (h *AdminHandler) BulkCancelJobs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	// Validate project ID matches header
	if user.ProjectID != "" && projectID != user.ProjectID {
		return apperror.NewBadRequest("projectId mismatch with X-Project-ID header")
	}

	count, err := h.jobsService.BulkCancelByProject(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to bulk cancel jobs", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(BulkCancelResponseDTO{
		Cancelled: count,
		Message:   fmt.Sprintf("Cancelled %d job%s", count, pluralize(count)),
	}))
}

// BulkDeleteJobs handles DELETE /api/admin/extraction-jobs/projects/:projectId/bulk-delete
// Deletes all completed/failed/cancelled jobs for a project
func (h *AdminHandler) BulkDeleteJobs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	// Validate project ID matches header
	if user.ProjectID != "" && projectID != user.ProjectID {
		return apperror.NewBadRequest("projectId mismatch with X-Project-ID header")
	}

	count, err := h.jobsService.DeleteCompleted(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to bulk delete jobs", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(BulkDeleteResponseDTO{
		Deleted: count,
		Message: fmt.Sprintf("Deleted %d job%s", count, pluralize(count)),
	}))
}

// BulkRetryJobs handles POST /api/admin/extraction-jobs/projects/:projectId/bulk-retry
// Retries all failed jobs for a project
func (h *AdminHandler) BulkRetryJobs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	// Validate project ID matches header
	if user.ProjectID != "" && projectID != user.ProjectID {
		return apperror.NewBadRequest("projectId mismatch with X-Project-ID header")
	}

	count, err := h.jobsService.BulkRetryFailed(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to bulk retry jobs", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(BulkRetryResponseDTO{
		Retried: count,
		Message: fmt.Sprintf("Retrying %d job%s", count, pluralize(count)),
	}))
}

// GetLogs handles GET /api/admin/extraction-jobs/:jobId/logs
// Gets detailed extraction logs for a job
func (h *AdminHandler) GetLogs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	jobID := c.Param("jobId")
	if jobID == "" {
		return apperror.NewBadRequest("jobId is required")
	}

	// Get job to verify access and get debug_info for timeline
	job, err := h.jobsService.FindByID(c.Request().Context(), jobID)
	if err != nil {
		return apperror.NewInternal("failed to get extraction job", err)
	}
	if job == nil {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	// Verify project access if project header is set
	if user.ProjectID != "" && job.ProjectID != user.ProjectID {
		return apperror.NewNotFound("ExtractionJob", jobID)
	}

	// Get logs and summary in parallel
	logs, err := h.jobsService.GetJobLogs(c.Request().Context(), jobID)
	if err != nil {
		return apperror.NewInternal("failed to get extraction logs", err)
	}

	summary, err := h.jobsService.GetJobLogSummary(c.Request().Context(), jobID)
	if err != nil {
		return apperror.NewInternal("failed to get log summary", err)
	}

	// Convert logs to DTOs
	logDTOs := make([]*ExtractionLogDTO, len(logs))
	for i, log := range logs {
		logDTOs[i] = log.ToDTO()
	}

	// Extract timeline from debug_info if available
	var timeline []TimelineEventDTO
	if job.DebugInfo != nil {
		if timelineRaw, ok := job.DebugInfo["timeline"]; ok {
			if timelineArr, ok := timelineRaw.([]interface{}); ok {
				timeline = make([]TimelineEventDTO, 0, len(timelineArr))
				for _, item := range timelineArr {
					if event, ok := item.(map[string]interface{}); ok {
						timeline = append(timeline, TimelineEventDTO(event))
					}
				}
			}
		}
	}

	return c.JSON(http.StatusOK, SuccessResponse(ExtractionLogsResponseDTO{
		Logs:     logDTOs,
		Summary:  *summary,
		Timeline: timeline,
	}))
}

// pluralize returns "s" if count != 1, empty string otherwise
func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
