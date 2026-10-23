package extraction

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// AdminHandler handles HTTP requests for extraction jobs admin API
type AdminHandler struct {
	jobsService  *ObjectExtractionJobsService
	tempoBaseURL string       // internal Tempo query URL; empty when tracing disabled
	pricing      modelPricing // optional; nil when provider repo not available
}

// modelPricing resolves a model's per-1M-token text input/output price.
// Satisfied by *provider.Repository via the adapter in module.go. Nil (or
// returning found=false) means the cost estimate is omitted.
type modelPricing interface {
	LookupModelPricing(ctx context.Context, model string) (textIn, out float64, found bool)
}

// NewAdminHandler creates a new extraction jobs admin handler. tempoBaseURL and
// pricing are optional (empty/nil → trace enrichment is a no-op).
func NewAdminHandler(jobsService *ObjectExtractionJobsService, tempoBaseURL string, pricing modelPricing) *AdminHandler {
	return &AdminHandler{jobsService: jobsService, tempoBaseURL: tempoBaseURL, pricing: pricing}
}

// CreateJob handles POST /api/admin/extraction-jobs
// Creates a new extraction job
func (h *AdminHandler) CreateJob(c echo.Context) error {
	user := auth.MustGetUser(c)

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

	// Preserve the explicit 400 for a malformed project ID (previously enforced
	// by the handler-level model guard).
	if _, err := uuid.Parse(projectID); err != nil {
		return apperror.NewBadRequest("project_id must be a valid UUID")
	}

	// Enforce membership on the addressed project (issue #959). The project is
	// sourced from the body or the X-Project-ID header, not the :projectId path,
	// so the shared RequireProjectTokenScope/RequireProjectMember pair cannot see
	// it; enforce the equivalent handler-level ownership check here.
	if err := auth.RequireProjectMembership(c.Request().Context(), h.jobsService.DB(), projectID); err != nil {
		return err
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

	// When source_type is "document", set DocumentID for the worker.
	// Prefer explicit source_id; fall back to source_metadata["document_id"].
	if sourceType == "document" {
		if dto.SourceID != nil {
			opts.DocumentID = dto.SourceID
		} else if dto.SourceMetadata != nil {
			if raw, ok := dto.SourceMetadata["document_id"]; ok {
				if docID, ok := raw.(string); ok && docID != "" {
					opts.DocumentID = &docID
				}
			}
		}
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
		// The chokepoint gate rejects with ErrNoGenerativeModel; surface it as
		// the gateway contract's 409 no_generative_model rather than a 500.
		if errors.Is(err, ErrNoGenerativeModel) {
			return apperror.New(http.StatusConflict, "no_generative_model", noGenerativeModelMessage)
		}
		return apperror.NewInternal("failed to create extraction job", err)
	}

	return c.JSON(http.StatusCreated, SuccessResponse(job.ToDTO()))
}

// ListJobs handles GET /api/admin/extraction-jobs/projects/:projectId
// Lists extraction jobs for a project with pagination
func (h *AdminHandler) ListJobs(c echo.Context) error {
	user := auth.MustGetUser(c)

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

	// Enforce membership on the job's owning project (issue #959). The project
	// is sourced from the job record, not the :jobId path, so the shared pair
	// cannot see it; enforce the equivalent handler-level ownership check here.
	if err := auth.RequireProjectMembership(c.Request().Context(), h.jobsService.DB(), job.ProjectID); err != nil {
		return err
	}

	dto := job.ToDTO()
	h.enrichTrace(c.Request().Context(), dto)

	return c.JSON(http.StatusOK, SuccessResponse(dto))
}

// enrichTrace fills a single-job DTO's Spans and TraceCostUSD from Tempo when a
// trace id is present and tracing is enabled. Any failure (unreachable Tempo,
// missing pricing, malformed trace) leaves the fields empty and never changes
// the response status — the trace preview is best-effort. It is called only
// from GetJob; the list route keeps the DTOs cheap by skipping this step.
func (h *AdminHandler) enrichTrace(ctx context.Context, dto *ExtractionJobDTO) {
	if dto.TraceID == nil || *dto.TraceID == "" || h.tempoBaseURL == "" {
		return
	}

	spans, _, err := agents.GetTraceSpans(ctx, h.tempoBaseURL, *dto.TraceID)
	if err != nil || spans == nil {
		return
	}
	dto.Spans = spans

	if h.pricing == nil {
		return
	}

	// Aggregate input/output tokens per model over the flattened spans, then
	// cost each model independently against its pricing (per 1M tokens).
	type usage struct{ in, out int64 }
	byModel := make(map[string]*usage)
	for _, s := range spans {
		if s.Model == "" {
			continue
		}
		u, ok := byModel[s.Model]
		if !ok {
			u = &usage{}
			byModel[s.Model] = u
		}
		u.in += s.InputTokens
		u.out += s.OutputTokens
	}

	const perMillion = 1_000_000.0
	var total float64
	resolved := false
	for model, u := range byModel {
		textIn, out, found := h.lookupPricing(ctx, model)
		if !found {
			continue
		}
		total += float64(u.in)/perMillion*textIn + float64(u.out)/perMillion*out
		resolved = true
	}

	// Only surface a cost when at least one model resolved to a price.
	if resolved {
		cost := math.Round(total*1e6) / 1e6
		dto.TraceCostUSD = &cost
	}
}

// lookupPricing resolves a model's per-1M-token price, normalizing the model
// name first. call_llm span model attributes are frequently vendor-prefixed by
// the model factory (e.g. "openai/deepseek-v4-flash", "google/gemini-2.5-flash")
// while kb.provider_pricing stores bare model names, so an exact match would
// miss every row. The candidates tried in order are: the raw model, the
// substring after the last "/", then the substring after the first "/" — first
// hit wins, and an empty model never looks up.
//
// Note on approximation: the same bare model name can exist under several
// providers (e.g. "deepseek-v4-flash" under both "deepseek" and "aliyun" with
// different rates). GetPricingByModel returns the first match ordered by
// provider; we accept that first-match rate rather than resolving the actual
// provider — a deliberate approximation, not a correctness guarantee.
func (h *AdminHandler) lookupPricing(ctx context.Context, model string) (textIn, out float64, found bool) {
	for _, candidate := range modelCandidates(model) {
		if textIn, out, found = h.pricing.LookupModelPricing(ctx, candidate); found {
			return textIn, out, true
		}
	}
	return 0, 0, false
}

// modelCandidates returns the ordered, deduplicated model-name candidates for a
// pricing lookup: the raw string, then the substring after the last "/", then
// after the first "/". An empty model yields no candidates.
func modelCandidates(model string) []string {
	if model == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	add(model)
	if i := strings.LastIndex(model, "/"); i >= 0 && i+1 < len(model) {
		add(model[i+1:])
	}
	if i := strings.Index(model, "/"); i >= 0 && i+1 < len(model) {
		add(model[i+1:])
	}
	return out
}

// UpdateJob handles PATCH /api/admin/extraction-jobs/:jobId
// Updates an extraction job
func (h *AdminHandler) UpdateJob(c echo.Context) error {
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

	// Enforce membership on the job's owning project (issue #959).
	if err := auth.RequireProjectMembership(c.Request().Context(), h.jobsService.DB(), job.ProjectID); err != nil {
		return err
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

	// Enforce membership on the job's owning project (issue #959).
	if err := auth.RequireProjectMembership(c.Request().Context(), h.jobsService.DB(), job.ProjectID); err != nil {
		return err
	}

	projectID := job.ProjectID

	if err := h.jobsService.DeleteJob(c.Request().Context(), jobID, projectID); err != nil {
		return apperror.NewBadRequest(err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

// CancelJob handles POST /api/admin/extraction-jobs/:jobId/cancel
// Cancels a pending or running extraction job
func (h *AdminHandler) CancelJob(c echo.Context) error {
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

	// Enforce membership on the job's owning project (issue #959).
	if err := auth.RequireProjectMembership(c.Request().Context(), h.jobsService.DB(), job.ProjectID); err != nil {
		return err
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

	// Enforce membership on the job's owning project (issue #959).
	if err := auth.RequireProjectMembership(c.Request().Context(), h.jobsService.DB(), existingJob.ProjectID); err != nil {
		return err
	}

	projectID := existingJob.ProjectID

	job, err := h.jobsService.RetryJob(c.Request().Context(), jobID, projectID)
	if err != nil {
		return apperror.NewBadRequest(err.Error())
	}

	return c.JSON(http.StatusOK, SuccessResponse(job.ToDTO()))
}

// GetStatistics handles GET /api/admin/extraction-jobs/projects/:projectId/statistics
// Gets job statistics for a project
func (h *AdminHandler) GetStatistics(c echo.Context) error {
	user := auth.MustGetUser(c)

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
		TotalJobs:    int(stats.TotalJobs),
		JobsByStatus: jobsByStatus,
		// Stale-sweep reaps are kept out of JobsByStatus["failed"] and surfaced
		// separately so historical cleanup is not read as a current failure.
		StaleFailed:   int(stats.JobsByStatus["staleFailed"]),
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
	user := auth.MustGetUser(c)

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
	user := auth.MustGetUser(c)

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
	user := auth.MustGetUser(c)

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

	// Enforce membership on the job's owning project (issue #959).
	if err := auth.RequireProjectMembership(c.Request().Context(), h.jobsService.DB(), job.ProjectID); err != nil {
		return err
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
