package health

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/jobs"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Keep apperror imported for swagger type resolution (@Failure {object} apperror.Error).
var _ = apperror.Error{}

// MetricsHandler handles job metrics requests
type MetricsHandler struct {
	db *bun.DB
}

// NewMetricsHandler creates a new metrics handler
func NewMetricsHandler(db *bun.DB) *MetricsHandler {
	return &MetricsHandler{
		db: db,
	}
}

// JobQueueMetrics represents metrics for a single job queue
type JobQueueMetrics struct {
	Queue       string `json:"queue"`
	Pending     int64  `json:"pending"`
	Processing  int64  `json:"processing"`
	Completed   int64  `json:"completed"`
	Failed      int64  `json:"failed"`
	StaleFailed int64  `json:"stale_failed"`
	Total       int64  `json:"total"`
	LastHour    int64  `json:"last_hour"`
	Last24Hours int64  `json:"last_24_hours"`
}

// AllJobMetrics contains metrics for all job queues
type AllJobMetrics struct {
	Queues    []JobQueueMetrics `json:"queues"`
	Scope     string            `json:"scope"`                // "project" or "account"
	ProjectID string            `json:"project_id,omitempty"` // set when scope=project
	Timestamp string            `json:"timestamp"`
}

// selectCounts builds the per-queue status aggregate for errorColumn (which
// must be qualified when the caller joins other tables). failed counts only
// genuine failures: rows terminal-failed by the stale-job sweep carry
// jobs.StaleJobMessage and are reported separately as stale_failed, so
// historical cleanup is not presented as current breakage.
func selectCounts(errorColumn string) string {
	return `
	SELECT
		COUNT(*) FILTER (WHERE status = 'pending') as pending,
		COUNT(*) FILTER (WHERE status IN ('processing', 'running')) as processing,
		COUNT(*) FILTER (WHERE status = 'completed') as completed,
		COUNT(*) FILTER (WHERE status = 'failed' AND COALESCE(` + errorColumn + `, '') <> '` + jobs.StaleJobMessage + `') as failed,
		COUNT(*) FILTER (WHERE status = 'failed' AND ` + errorColumn + ` = '` + jobs.StaleJobMessage + `') as stale_failed,
		COUNT(*) as total,
		COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '1 hour') as last_hour,
		COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours') as last_24_hours
	FROM `
}

// JobMetrics returns metrics for all job queues
// @Summary      Get job queue metrics
// @Description  Returns processing pipeline metrics. The effective project is resolved server-side (project token binding or X-Project-ID header) and validated against membership; project_id is a filter only and cannot widen access. An instance-wide aggregate (no project context) requires superadmin_full.
// @Tags         metrics
// @Produce      json
// @Param        project_id query string false "Filter by project ID (must match the caller's project, or any project for a superadmin_full aggregate)"
// @Success      200 {object} AllJobMetrics "Job queue metrics"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Router       /api/metrics/jobs [get]
// @Security     bearerAuth
func (h *MetricsHandler) JobMetrics(c echo.Context) error {
	user := auth.MustGetUser(c)

	ctx := c.Request().Context()

	// Resolve the effective project server-side. RequireAuth normalises the
	// project-scoped token binding and the X-Project-ID header onto
	// user.ProjectID, and RequireProjectMember has already validated the
	// caller's membership in that project's owning org. A client-supplied
	// ?project_id is ONLY a filter and can never widen access (issue #994
	// mechanism 3/4).
	serverProject := user.ProjectID
	filterProject := c.QueryParam("project_id")

	scope := "project"
	projectID := serverProject

	switch {
	case serverProject != "":
		// Project-scoped caller: a filter for a different project is refused.
		if filterProject != "" && filterProject != serverProject {
			return apperror.NewForbidden("project_id filter does not match the caller's project")
		}
	default:
		// No server-side project context: this is an instance-wide aggregate.
		// It is a superadmin_full-only surface, so a client-supplied ?project_id
		// cannot smuggle a project read past the membership gate.
		isSuperadmin, err := auth.IsSuperadminFull(ctx, h.db)
		if err != nil {
			return err
		}
		if !isSuperadmin {
			return apperror.NewForbidden("superadmin privilege required")
		}
		// A superadmin may filter the aggregate to a single project.
		if filterProject != "" {
			projectID = filterProject
			scope = "project"
		} else {
			projectID = ""
			scope = "account"
		}
	}

	queues := []struct {
		name       string
		systemOnly bool // only shown for account-level (no project_id)
	}{
		{"document_parsing", false},
		{"chunk_embedding", false},
		{"graph_embedding", false},
		{"object_extraction", false},
		{"email", true}, // system queue — not project-scoped
	}

	var allMetrics []JobQueueMetrics

	for _, q := range queues {
		// Skip system-only queues when showing project-scoped data
		if q.systemOnly && projectID != "" {
			continue
		}

		metrics, err := h.getQueueMetrics(ctx, q.name, projectID)
		if err != nil {
			// Log error but continue with other queues
			continue
		}
		allMetrics = append(allMetrics, *metrics)
	}

	resp := AllJobMetrics{
		Queues:    allMetrics,
		Scope:     scope,
		Timestamp: c.Request().Header.Get("Date"),
	}
	if projectID != "" {
		resp.ProjectID = projectID
	}

	return c.JSON(http.StatusOK, resp)
}

// getQueueMetrics retrieves metrics for a specific job queue.
// It uses JOIN-based queries for tables that lack a project_id column.
func (h *MetricsHandler) getQueueMetrics(ctx context.Context, name, projectID string) (*JobQueueMetrics, error) {
	var query string
	var args []interface{}

	if projectID == "" {
		// No project filter — simple aggregate over the full table
		switch name {
		case "document_parsing":
			query = selectCounts("error_message") + `kb.document_parsing_jobs`
		case "chunk_embedding":
			query = selectCounts("last_error") + `kb.chunk_embedding_jobs`
		case "graph_embedding":
			query = selectCounts("last_error") + `kb.graph_embedding_jobs`
		case "object_extraction":
			query = selectCounts("error_message") + `kb.object_extraction_jobs`
		case "email":
			query = selectCounts("last_error") + `kb.email_jobs`
		default:
			return nil, nil
		}
	} else {
		// Project-scoped: tables with project_id filter directly;
		// tables without project_id join through their FK chain.
		switch name {
		case "document_parsing":
			query = selectCounts("error_message") + `kb.document_parsing_jobs WHERE project_id = ?`
			args = append(args, projectID)
		case "chunk_embedding":
			// chunk_embedding_jobs → chunks → documents (has project_id)
			query = selectCounts("cej.last_error") + `kb.chunk_embedding_jobs cej
				JOIN kb.chunks c ON c.id = cej.chunk_id
				JOIN kb.documents d ON d.id = c.document_id
				WHERE d.project_id = ?`
			args = append(args, projectID)
		case "graph_embedding":
			// graph_embedding_jobs → graph_objects (has project_id)
			query = selectCounts("gej.last_error") + `kb.graph_embedding_jobs gej
				JOIN kb.graph_objects go ON go.id = gej.object_id
				WHERE go.project_id = ?`
			args = append(args, projectID)
		case "object_extraction":
			query = selectCounts("error_message") + `kb.object_extraction_jobs WHERE project_id = ?`
			args = append(args, projectID)
		default:
			return nil, nil
		}
	}

	var metrics struct {
		Pending     int64 `bun:"pending"`
		Processing  int64 `bun:"processing"`
		Completed   int64 `bun:"completed"`
		Failed      int64 `bun:"failed"`
		StaleFailed int64 `bun:"stale_failed"`
		Total       int64 `bun:"total"`
		LastHour    int64 `bun:"last_hour"`
		Last24Hours int64 `bun:"last_24_hours"`
	}

	err := h.db.NewRaw(query, args...).Scan(ctx, &metrics)
	if err != nil {
		return nil, err
	}

	return &JobQueueMetrics{
		Queue:       name,
		Pending:     metrics.Pending,
		Processing:  metrics.Processing,
		Completed:   metrics.Completed,
		Failed:      metrics.Failed,
		StaleFailed: metrics.StaleFailed,
		Total:       metrics.Total,
		LastHour:    metrics.LastHour,
		Last24Hours: metrics.Last24Hours,
	}, nil
}

// SchedulerMetrics returns metrics for scheduled tasks
// @Summary      Get scheduler metrics
// @Description  Returns metrics for scheduled background tasks (instance-wide; superadmin_full only)
// @Tags         metrics
// @Produce      json
// @Success      200 {object} map[string]interface{} "Scheduler metrics"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Router       /api/metrics/scheduler [get]
// @Security     bearerAuth
func (h *MetricsHandler) SchedulerMetrics(c echo.Context) error {
	// This would need to be wired up to the scheduler service
	// For now, return a placeholder
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Scheduler metrics endpoint - wire up to scheduler service for task info",
	})
}
