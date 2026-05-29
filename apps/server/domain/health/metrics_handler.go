package health

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

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

// queueDef describes a job queue and how to scope it.
type queueDef struct {
	name string
	// baseQuery is the full SELECT ... FROM ... (optionally with a WHERE placeholder).
	// Use a func so we can build the right query per-scope at runtime.
	projectQuery string // query when scoped to a project (includes project filter)
	globalQuery  string // query when not scoped (all projects)
	systemOnly   bool   // if true, only include when scope=account (e.g. email)
}

const selectCounts = `
	SELECT
		COUNT(*) FILTER (WHERE status = 'pending') as pending,
		COUNT(*) FILTER (WHERE status IN ('processing', 'running')) as processing,
		COUNT(*) FILTER (WHERE status = 'completed') as completed,
		COUNT(*) FILTER (WHERE status = 'failed') as failed,
		COUNT(*) as total,
		COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '1 hour') as last_hour,
		COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours') as last_24_hours
	FROM `

// JobMetrics returns metrics for all job queues
// @Summary      Get job queue metrics
// @Description  Returns processing pipeline metrics for all job queues. Project-scoped tokens see only their project's data. Account-level tokens see all projects (optionally filtered by project_id query param).
// @Tags         metrics
// @Produce      json
// @Param        project_id query string false "Filter by project ID (account-level tokens only; ignored for project-scoped tokens)"
// @Success      200 {object} AllJobMetrics "Job queue metrics"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/metrics/jobs [get]
// @Security     bearerAuth
func (h *MetricsHandler) JobMetrics(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	ctx := c.Request().Context()

	// Project-scoped tokens: enforce their bound project — cannot be overridden.
	// Account-level tokens/OAuth: use optional query param.
	projectID := c.QueryParam("project_id")
	if user.APITokenProjectID != "" {
		projectID = user.APITokenProjectID
	}

	scope := "account"
	if projectID != "" {
		scope = "project"
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
			query = selectCounts + `kb.document_parsing_jobs`
		case "chunk_embedding":
			query = selectCounts + `kb.chunk_embedding_jobs`
		case "graph_embedding":
			query = selectCounts + `kb.graph_embedding_jobs`
		case "object_extraction":
			query = selectCounts + `kb.object_extraction_jobs`
		case "email":
			query = selectCounts + `kb.email_jobs`
		default:
			return nil, nil
		}
	} else {
		// Project-scoped: tables with project_id filter directly;
		// tables without project_id join through their FK chain.
		switch name {
		case "document_parsing":
			query = selectCounts + `kb.document_parsing_jobs WHERE project_id = ?`
			args = append(args, projectID)
		case "chunk_embedding":
			// chunk_embedding_jobs → chunks → documents (has project_id)
			query = selectCounts + `kb.chunk_embedding_jobs cej
				JOIN kb.chunks c ON c.id = cej.chunk_id
				JOIN kb.documents d ON d.id = c.document_id
				WHERE d.project_id = ?`
			args = append(args, projectID)
		case "graph_embedding":
			// graph_embedding_jobs → graph_objects (has project_id)
			query = selectCounts + `kb.graph_embedding_jobs gej
				JOIN kb.graph_objects go ON go.id = gej.object_id
				WHERE go.project_id = ?`
			args = append(args, projectID)
		case "object_extraction":
			query = selectCounts + `kb.object_extraction_jobs WHERE project_id = ?`
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
		Total:       metrics.Total,
		LastHour:    metrics.LastHour,
		Last24Hours: metrics.Last24Hours,
	}, nil
}

// SchedulerMetrics returns metrics for scheduled tasks
// @Summary      Get scheduler metrics
// @Description  Returns metrics for scheduled background tasks
// @Tags         metrics
// @Produce      json
// @Success      200 {object} map[string]interface{} "Scheduler metrics"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/metrics/scheduler [get]
// @Security     bearerAuth
func (h *MetricsHandler) SchedulerMetrics(c echo.Context) error {
	// This would need to be wired up to the scheduler service
	// For now, return a placeholder
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Scheduler metrics endpoint - wire up to scheduler service for task info",
	})
}
