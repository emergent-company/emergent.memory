package documentation

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
)

// Handler handles HTTP requests for documentation
type Handler struct {
	svc *Service
}

// NewHandler creates a new documentation handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListDocuments handles GET /api/docs
// @Summary      List all documentation
// @Description  Returns a list of all available documentation files with metadata (title, category, description, tags, etc.) but without content
// @Tags         documentation
// @Produce      json
// @Success      200 {object} DocumentList "List of documentation with metadata"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/docs [get]
func (h *Handler) ListDocuments(c echo.Context) error {
	docs, err := h.svc.ListDocuments()
	if err != nil {
		return apperror.ErrInternalServerError.WithMessage("failed to list documents").WithDebug(err.Error())
	}

	return c.JSON(http.StatusOK, DocumentList{
		Documents: docs,
		Total:     len(docs),
	})
}

// GetDocument handles GET /api/docs/:slug
// @Summary      Get documentation by slug
// @Description  Returns the full documentation document including markdown content for the specified slug (e.g., "template-pack-creation")
// @Tags         documentation
// @Produce      json
// @Param        slug path string true "Document slug (e.g., template-pack-creation)"
// @Success      200 {object} Document "Full document with markdown content"
// @Failure      404 {object} apperror.Error "Document not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/docs/{slug} [get]
func (h *Handler) GetDocument(c echo.Context) error {
	slug := c.Param("slug")
	if slug == "" {
		return apperror.ErrBadRequest.WithMessage("slug is required")
	}

	doc, err := h.svc.GetDocument(slug)
	if err != nil {
		// Check if it's a "not found" error
		if err.Error() == "document not found: "+slug {
			return apperror.ErrNotFound.WithMessage("document not found")
		}
		return apperror.ErrInternalServerError.WithMessage("failed to get document").WithDebug(err.Error())
	}

	return c.JSON(http.StatusOK, doc)
}

// GetCategories handles GET /api/docs/categories
// @Summary      Get documentation categories
// @Description  Returns all documentation categories with their metadata (name, description, icon) from index.json
// @Tags         documentation
// @Produce      json
// @Success      200 {object} CategoriesResponse "List of categories"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/docs/categories [get]
func (h *Handler) GetCategories(c echo.Context) error {
	categories, err := h.svc.GetCategories()
	if err != nil {
		return apperror.ErrInternalServerError.WithMessage("failed to get categories").WithDebug(err.Error())
	}

	return c.JSON(http.StatusOK, CategoriesResponse{
		Categories: categories,
		Total:      len(categories),
	})
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
// @Summary      Get service health
// @Description  Returns detailed health status including database connectivity and system uptime
// @Tags         health
// @Accept       json
// @Produce      json
// @Success      200 {object} HealthResponse "Service is healthy"
// @Success      503 {object} HealthResponse "Service is unhealthy"
// @Router       /health [get]
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
// @Summary      Liveness probe
// @Description  Simple health check endpoint for Kubernetes liveness probes
// @Tags         health
// @Produce      plain
// @Success      200 {string} string "OK"
// @Router       /healthz [get]
func (h *Handler) Healthz(c echo.Context) error {
	return c.String(http.StatusOK, "OK")
}

// Ready returns readiness status (for k8s readiness probe)
// @Summary      Readiness probe
// @Description  Returns readiness status based on database connectivity (for Kubernetes readiness probes)
// @Tags         health
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string]any "Service is ready"
// @Success      503 {object} map[string]any "Service is not ready"
// @Router       /ready [get]
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
// @Summary      Get debug information
// @Description  Returns debug information including memory stats and database pool stats (development only)
// @Tags         health
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string]any "Debug information"
// @Failure      404 {object} map[string]any "Not found in production"
// @Router       /debug [get]
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
