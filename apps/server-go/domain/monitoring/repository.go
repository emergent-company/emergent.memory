package monitoring

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/domain/extraction"
)

// Repository handles database queries for monitoring data
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new monitoring repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{db: db, log: log}
}

// ListExtractionJobsParams contains filtering options for listing extraction jobs
type ListExtractionJobsQueryParams struct {
	ProjectID  string
	Status     *string
	SourceType *string
	DateFrom   *time.Time
	DateTo     *time.Time
	Limit      int
	Offset     int
	SortBy     string
	SortOrder  string
}

// ListExtractionJobs retrieves extraction jobs with filtering and pagination
func (r *Repository) ListExtractionJobs(ctx context.Context, params ListExtractionJobsQueryParams) ([]extraction.ObjectExtractionJob, int, error) {
	var jobs []extraction.ObjectExtractionJob

	query := r.db.NewSelect().
		Model(&jobs).
		Where("project_id = ?", params.ProjectID)

	// Apply filters
	if params.Status != nil && *params.Status != "" {
		// Map frontend status to internal status
		internalStatus := mapFrontendStatusToInternal(*params.Status)
		query = query.Where("status = ?", internalStatus)
	}

	if params.SourceType != nil && *params.SourceType != "" {
		query = query.Where("source_type = ?", *params.SourceType)
	}

	if params.DateFrom != nil {
		query = query.Where("created_at >= ?", params.DateFrom)
	}

	if params.DateTo != nil {
		query = query.Where("created_at <= ?", params.DateTo)
	}

	// Get total count
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Apply sorting
	sortBy := "created_at"
	if params.SortBy != "" {
		switch params.SortBy {
		case "started_at":
			sortBy = "started_at"
		case "duration_ms":
			sortBy = "EXTRACT(EPOCH FROM (completed_at - started_at)) * 1000"
		case "total_cost_usd":
			// This would require a subquery or join with llm_call_logs
			sortBy = "created_at"
		default:
			sortBy = "created_at"
		}
	}

	sortOrder := "DESC"
	if params.SortOrder == "asc" {
		sortOrder = "ASC"
	}

	query = query.OrderExpr(sortBy + " " + sortOrder)

	// Apply pagination
	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	} else {
		query = query.Limit(50)
	}

	if params.Offset > 0 {
		query = query.Offset(params.Offset)
	}

	err = query.Scan(ctx)
	if err != nil {
		return nil, 0, err
	}

	return jobs, total, nil
}

// GetExtractionJobByID retrieves a single extraction job by ID
func (r *Repository) GetExtractionJobByID(ctx context.Context, jobID, projectID string) (*extraction.ObjectExtractionJob, error) {
	var job extraction.ObjectExtractionJob

	err := r.db.NewSelect().
		Model(&job).
		Where("id = ?", jobID).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		return nil, err
	}

	return &job, nil
}

// GetLogsForJob retrieves system process logs for a specific job
func (r *Repository) GetLogsForJob(ctx context.Context, jobID string, level *string, limit, offset int) ([]SystemProcessLog, error) {
	var logs []SystemProcessLog

	query := r.db.NewSelect().
		Model(&logs).
		Where("process_id = ?", jobID).
		Order("timestamp DESC")

	if level != nil && *level != "" {
		query = query.Where("level = ?", *level)
	}

	if limit > 0 {
		query = query.Limit(limit)
	} else {
		query = query.Limit(100)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	err := query.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return logs, nil
}

// GetLLMCallsForJob retrieves LLM call logs for a specific job
func (r *Repository) GetLLMCallsForJob(ctx context.Context, jobID string, limit, offset int) ([]LLMCallLog, error) {
	var calls []LLMCallLog

	query := r.db.NewSelect().
		Model(&calls).
		Where("process_id = ?", jobID).
		Order("started_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	} else {
		query = query.Limit(50)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	err := query.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return calls, nil
}

// GetLLMCallMetricsForJob retrieves aggregated LLM call metrics for a job
func (r *Repository) GetLLMCallMetricsForJob(ctx context.Context, jobID string) (*ExtractionJobMetricsDTO, error) {
	var result struct {
		TotalCalls      int     `bun:"total_calls"`
		SuccessfulCalls int     `bun:"successful_calls"`
		TotalCost       float64 `bun:"total_cost"`
		TotalTokens     int     `bun:"total_tokens"`
		AvgDuration     float64 `bun:"avg_duration"`
	}

	err := r.db.NewSelect().
		TableExpr("kb.llm_call_logs").
		ColumnExpr("COUNT(*) AS total_calls").
		ColumnExpr("COUNT(*) FILTER (WHERE status = 'success') AS successful_calls").
		ColumnExpr("COALESCE(SUM(cost_usd), 0) AS total_cost").
		ColumnExpr("COALESCE(SUM(total_tokens), 0) AS total_tokens").
		ColumnExpr("COALESCE(AVG(duration_ms), 0) AS avg_duration").
		Where("process_id = ?", jobID).
		Scan(ctx, &result)

	if err != nil {
		return nil, err
	}

	successRate := float64(0)
	if result.TotalCalls > 0 {
		successRate = float64(result.SuccessfulCalls) / float64(result.TotalCalls)
	}

	return &ExtractionJobMetricsDTO{
		TotalLLMCalls:     result.TotalCalls,
		TotalCostUSD:      result.TotalCost,
		TotalTokens:       result.TotalTokens,
		AvgCallDurationMs: result.AvgDuration,
		SuccessRate:       successRate,
	}, nil
}

// mapFrontendStatusToInternal maps frontend status values to internal status
func mapFrontendStatusToInternal(status string) string {
	switch status {
	case "pending":
		return "pending"
	case "in_progress":
		return "processing"
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	default:
		return status
	}
}

// mapInternalStatusToFrontend maps internal status to frontend status
func mapInternalStatusToFrontend(status string) string {
	switch status {
	case "pending":
		return "pending"
	case "processing":
		return "in_progress"
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	default:
		return status
	}
}
