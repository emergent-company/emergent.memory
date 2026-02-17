package monitoring

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/domain/extraction"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

// Handler handles HTTP requests for monitoring endpoints
type Handler struct {
	repo *Repository
}

// NewHandler creates a new monitoring handler
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// ListExtractionJobs handles GET /api/monitoring/extraction-jobs
// @Summary      List extraction jobs
// @Description  Lists extraction jobs with filtering and pagination support
// @Tags         monitoring
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        status query string false "Filter by status" Enums(pending,running,completed,failed)
// @Param        source_type query string false "Filter by source type"
// @Param        date_from query string false "Filter from date (RFC3339)"
// @Param        date_to query string false "Filter to date (RFC3339)"
// @Param        limit query int false "Max results (1-100)" minimum(1) maximum(100) default(50)
// @Param        offset query int false "Pagination offset" minimum(0) default(0)
// @Param        sort_by query string false "Sort field"
// @Param        sort_order query string false "Sort order" Enums(asc,desc)
// @Success      200 {object} ExtractionJobListResponseDTO "List of jobs"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/monitoring/extraction-jobs [get]
// @Security     bearerAuth
func (h *Handler) ListExtractionJobs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	// Parse query parameters
	params := ListExtractionJobsQueryParams{
		ProjectID: user.ProjectID,
		Limit:     50,
		Offset:    0,
	}

	if status := c.QueryParam("status"); status != "" {
		params.Status = &status
	}

	if sourceType := c.QueryParam("source_type"); sourceType != "" {
		params.SourceType = &sourceType
	}

	if dateFrom := c.QueryParam("date_from"); dateFrom != "" {
		if t, err := time.Parse(time.RFC3339, dateFrom); err == nil {
			params.DateFrom = &t
		}
	}

	if dateTo := c.QueryParam("date_to"); dateTo != "" {
		if t, err := time.Parse(time.RFC3339, dateTo); err == nil {
			params.DateTo = &t
		}
	}

	if limit := c.QueryParam("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 100 {
			params.Limit = l
		}
	}

	if offset := c.QueryParam("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil && o >= 0 {
			params.Offset = o
		}
	}

	params.SortBy = c.QueryParam("sort_by")
	params.SortOrder = c.QueryParam("sort_order")

	jobs, total, err := h.repo.ListExtractionJobs(c.Request().Context(), params)
	if err != nil {
		return apperror.NewInternal("failed to list extraction jobs", err)
	}

	// Convert to DTOs
	items := make([]ExtractionJobSummaryDTO, len(jobs))
	for i, job := range jobs {
		items[i] = h.jobToSummaryDTO(&job)
	}

	return c.JSON(http.StatusOK, ExtractionJobListResponseDTO{
		Items:  items,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	})
}

// GetExtractionJobDetail handles GET /api/monitoring/extraction-jobs/:id
// @Summary      Get extraction job details
// @Description  Retrieves full details including logs, LLM calls, and metrics for a specific extraction job
// @Tags         monitoring
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Job ID (UUID)"
// @Success      200 {object} ExtractionJobDetailDTO "Job details"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/monitoring/extraction-jobs/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetExtractionJobDetail(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	jobID := c.Param("id")
	if jobID == "" {
		return apperror.NewBadRequest("job id is required")
	}

	ctx := c.Request().Context()

	// Get the job
	job, err := h.repo.GetExtractionJobByID(ctx, jobID, user.ProjectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperror.NewNotFound("ExtractionJob", jobID)
		}
		return apperror.NewInternal("failed to get extraction job", err)
	}

	// Get logs (limited to 100)
	logs, err := h.repo.GetLogsForJob(ctx, jobID, nil, 100, 0)
	if err != nil {
		return apperror.NewInternal("failed to get job logs", err)
	}

	// Get LLM calls
	llmCalls, err := h.repo.GetLLMCallsForJob(ctx, jobID, 100, 0)
	if err != nil {
		return apperror.NewInternal("failed to get LLM calls", err)
	}

	// Get metrics
	metrics, err := h.repo.GetLLMCallMetricsForJob(ctx, jobID)
	if err != nil {
		return apperror.NewInternal("failed to get job metrics", err)
	}

	// Convert to DTOs
	logDTOs := make([]ProcessLogDTO, len(logs))
	for i, log := range logs {
		logDTOs[i] = log.ToDTO()
	}

	llmCallDTOs := make([]LLMCallLogDTO, len(llmCalls))
	for i, call := range llmCalls {
		llmCallDTOs[i] = call.ToDTO()
	}

	detail := h.jobToDetailDTO(job, logDTOs, llmCallDTOs, metrics)

	return c.JSON(http.StatusOK, detail)
}

// GetExtractionJobLogs handles GET /api/monitoring/extraction-jobs/:id/logs
// @Summary      Get job process logs
// @Description  Retrieves system process logs for a specific extraction job with optional filtering by level
// @Tags         monitoring
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Job ID (UUID)"
// @Param        level query string false "Filter by log level" Enums(debug,info,warn,error)
// @Param        limit query int false "Max results (1-500)" minimum(1) maximum(500) default(100)
// @Param        offset query int false "Pagination offset" minimum(0) default(0)
// @Success      200 {object} ProcessLogListDTO "Process logs"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/monitoring/extraction-jobs/{id}/logs [get]
// @Security     bearerAuth
func (h *Handler) GetExtractionJobLogs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	jobID := c.Param("id")
	if jobID == "" {
		return apperror.NewBadRequest("job id is required")
	}

	// Verify job belongs to project
	_, err := h.repo.GetExtractionJobByID(c.Request().Context(), jobID, user.ProjectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperror.NewNotFound("ExtractionJob", jobID)
		}
		return apperror.NewInternal("failed to verify extraction job", err)
	}

	// Parse query params
	var level *string
	if l := c.QueryParam("level"); l != "" {
		level = &l
	}

	limit := 100
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	offset := 0
	if o := c.QueryParam("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	logs, err := h.repo.GetLogsForJob(c.Request().Context(), jobID, level, limit, offset)
	if err != nil {
		return apperror.NewInternal("failed to get job logs", err)
	}

	// Convert to DTOs
	logDTOs := make([]ProcessLogDTO, len(logs))
	for i, log := range logs {
		logDTOs[i] = log.ToDTO()
	}

	return c.JSON(http.StatusOK, ProcessLogListDTO{Logs: logDTOs})
}

// GetExtractionJobLLMCalls handles GET /api/monitoring/extraction-jobs/:id/llm-calls
// @Summary      Get job LLM calls
// @Description  Retrieves LLM API call logs for a specific extraction job including tokens, cost, and duration
// @Tags         monitoring
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Job ID (UUID)"
// @Param        limit query int false "Max results (1-500)" minimum(1) maximum(500) default(50)
// @Param        offset query int false "Pagination offset" minimum(0) default(0)
// @Success      200 {object} LLMCallListDTO "LLM call logs"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/monitoring/extraction-jobs/{id}/llm-calls [get]
// @Security     bearerAuth
func (h *Handler) GetExtractionJobLLMCalls(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	jobID := c.Param("id")
	if jobID == "" {
		return apperror.NewBadRequest("job id is required")
	}

	// Verify job belongs to project
	_, err := h.repo.GetExtractionJobByID(c.Request().Context(), jobID, user.ProjectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperror.NewNotFound("ExtractionJob", jobID)
		}
		return apperror.NewInternal("failed to verify extraction job", err)
	}

	// Parse query params
	limit := 50
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	offset := 0
	if o := c.QueryParam("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	calls, err := h.repo.GetLLMCallsForJob(c.Request().Context(), jobID, limit, offset)
	if err != nil {
		return apperror.NewInternal("failed to get LLM calls", err)
	}

	// Convert to DTOs
	callDTOs := make([]LLMCallLogDTO, len(calls))
	for i, call := range calls {
		callDTOs[i] = call.ToDTO()
	}

	return c.JSON(http.StatusOK, LLMCallListDTO{LLMCalls: callDTOs})
}

// jobToSummaryDTO converts an extraction job to a summary DTO
func (h *Handler) jobToSummaryDTO(job *extraction.ObjectExtractionJob) ExtractionJobSummaryDTO {
	dto := ExtractionJobSummaryDTO{
		ID:     job.ID,
		Status: mapInternalStatusToFrontend(string(job.Status)),
	}

	// Handle pointer fields
	if job.SourceType != nil {
		dto.SourceType = *job.SourceType
	} else {
		dto.SourceType = "unknown"
	}

	if job.SourceID != nil {
		dto.SourceID = *job.SourceID
	} else {
		dto.SourceID = ""
	}

	if job.StartedAt != nil {
		dto.StartedAt = job.StartedAt
	}

	if job.CompletedAt != nil {
		dto.CompletedAt = job.CompletedAt
	}

	// Calculate duration if both timestamps are available
	if job.StartedAt != nil && job.CompletedAt != nil {
		duration := int(job.CompletedAt.Sub(*job.StartedAt).Milliseconds())
		dto.DurationMs = &duration
	}

	if job.SuccessfulItems > 0 {
		dto.ObjectsCreated = &job.SuccessfulItems
	}

	if job.ErrorMessage != nil {
		dto.ErrorMessage = job.ErrorMessage
	}

	return dto
}

// jobToDetailDTO converts an extraction job to a detail DTO
func (h *Handler) jobToDetailDTO(job *extraction.ObjectExtractionJob, logs []ProcessLogDTO, llmCalls []LLMCallLogDTO, metrics *ExtractionJobMetricsDTO) ExtractionJobDetailDTO {
	dto := ExtractionJobDetailDTO{
		ID:       job.ID,
		Status:   mapInternalStatusToFrontend(string(job.Status)),
		Logs:     logs,
		LLMCalls: llmCalls,
		Metrics:  metrics,
	}

	// Handle pointer fields
	if job.SourceType != nil {
		dto.SourceType = *job.SourceType
	} else {
		dto.SourceType = "unknown"
	}

	if job.SourceID != nil {
		dto.SourceID = *job.SourceID
	} else {
		dto.SourceID = ""
	}

	if job.StartedAt != nil {
		dto.StartedAt = job.StartedAt
	}

	if job.CompletedAt != nil {
		dto.CompletedAt = job.CompletedAt
	}

	// Calculate duration if both timestamps are available
	if job.StartedAt != nil && job.CompletedAt != nil {
		duration := int(job.CompletedAt.Sub(*job.StartedAt).Milliseconds())
		dto.DurationMs = &duration
	}

	if job.SuccessfulItems > 0 {
		dto.ObjectsCreated = &job.SuccessfulItems
	}

	if job.ErrorMessage != nil {
		dto.ErrorMessage = job.ErrorMessage
	}

	return dto
}
