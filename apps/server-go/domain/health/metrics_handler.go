package health

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
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
	Timestamp string            `json:"timestamp"`
}

// JobMetrics returns metrics for all job queues
func (h *MetricsHandler) JobMetrics(c echo.Context) error {
	ctx := c.Request().Context()

	// Define job queue tables
	queues := []struct {
		name  string
		table string
	}{
		{"document_parsing", "kb.document_parsing_jobs"},
		{"chunk_embedding", "kb.chunk_embedding_jobs"},
		{"graph_embedding", "kb.graph_embedding_jobs"},
		{"object_extraction", "kb.object_extraction_jobs"},
		{"data_source_sync", "kb.data_source_sync_jobs"},
		{"email", "kb.email_jobs"},
	}

	var allMetrics []JobQueueMetrics

	for _, q := range queues {
		metrics, err := h.getQueueMetrics(ctx, q.name, q.table)
		if err != nil {
			// Log error but continue with other queues
			continue
		}
		allMetrics = append(allMetrics, *metrics)
	}

	return c.JSON(http.StatusOK, AllJobMetrics{
		Queues:    allMetrics,
		Timestamp: c.Request().Header.Get("Date"),
	})
}

// getQueueMetrics retrieves metrics for a specific job queue
func (h *MetricsHandler) getQueueMetrics(ctx context.Context, name, table string) (*JobQueueMetrics, error) {
	// Use raw SQL since we need to query multiple tables with different schemas
	query := `
		SELECT 
			COUNT(*) FILTER (WHERE status = 'pending') as pending,
			COUNT(*) FILTER (WHERE status IN ('processing', 'running')) as processing,
			COUNT(*) FILTER (WHERE status = 'completed') as completed,
			COUNT(*) FILTER (WHERE status = 'failed') as failed,
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '1 hour') as last_hour,
			COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours') as last_24_hours
		FROM ` + table

	var metrics struct {
		Pending     int64 `bun:"pending"`
		Processing  int64 `bun:"processing"`
		Completed   int64 `bun:"completed"`
		Failed      int64 `bun:"failed"`
		Total       int64 `bun:"total"`
		LastHour    int64 `bun:"last_hour"`
		Last24Hours int64 `bun:"last_24_hours"`
	}

	err := h.db.NewRaw(query).Scan(ctx, &metrics)
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
func (h *MetricsHandler) SchedulerMetrics(c echo.Context) error {
	// This would need to be wired up to the scheduler service
	// For now, return a placeholder
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Scheduler metrics endpoint - wire up to scheduler service for task info",
	})
}
