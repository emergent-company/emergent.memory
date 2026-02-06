package health

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/internal/version"
)

// Handler handles health check requests
type Handler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	startAt time.Time
}

// NewHandler creates a new health handler
func NewHandler(pool *pgxpool.Pool, cfg *config.Config) *Handler {
	return &Handler{
		pool:    pool,
		cfg:     cfg,
		startAt: time.Now(),
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string           `json:"status"`
	Timestamp string           `json:"timestamp"`
	Uptime    string           `json:"uptime"`
	Version   string           `json:"version"`
	Checks    map[string]Check `json:"checks"`
}

// Check represents an individual health check result
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Health returns the overall service health
func (h *Handler) Health(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
	defer cancel()

	// Check database connectivity
	dbStatus := "healthy"
	dbMessage := ""
	if err := h.pool.Ping(ctx); err != nil {
		dbStatus = "unhealthy"
		dbMessage = err.Error()
	}

	// Determine overall status
	overallStatus := "healthy"
	if dbStatus == "unhealthy" {
		overallStatus = "unhealthy"
	}

	response := HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    time.Since(h.startAt).String(),
		Version:   version.Version,
		Checks: map[string]Check{
			"database": {
				Status:  dbStatus,
				Message: dbMessage,
			},
		},
	}

	statusCode := http.StatusOK
	if overallStatus == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	return c.JSON(statusCode, response)
}

// Healthz returns a simple health check (for k8s liveness probe)
func (h *Handler) Healthz(c echo.Context) error {
	return c.String(http.StatusOK, "OK")
}

// Ready returns readiness status (for k8s readiness probe)
func (h *Handler) Ready(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
	defer cancel()

	// Check database connectivity
	if err := h.pool.Ping(ctx); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]any{
			"status":  "not_ready",
			"message": "Database connection failed",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status": "ready",
	})
}

// Debug returns debug information (only in development)
func (h *Handler) Debug(c echo.Context) error {
	if h.cfg.Environment == "production" {
		return echo.NewHTTPError(http.StatusNotFound, "Not found")
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return c.JSON(http.StatusOK, map[string]any{
		"environment": h.cfg.Environment,
		"debug":       h.cfg.Debug,
		"go_version":  runtime.Version(),
		"goroutines":  runtime.NumGoroutine(),
		"memory": map[string]any{
			"alloc_mb":       mem.Alloc / 1024 / 1024,
			"total_alloc_mb": mem.TotalAlloc / 1024 / 1024,
			"sys_mb":         mem.Sys / 1024 / 1024,
			"num_gc":         mem.NumGC,
		},
		"database": map[string]any{
			"host":        h.cfg.Database.Host,
			"port":        h.cfg.Database.Port,
			"database":    h.cfg.Database.Database,
			"pool_total":  h.pool.Stat().TotalConns(),
			"pool_idle":   h.pool.Stat().IdleConns(),
			"pool_in_use": h.pool.Stat().AcquiredConns(),
		},
	})
}
