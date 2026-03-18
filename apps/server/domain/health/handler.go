package health

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/internal/version"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/kreuzberg"
	"github.com/emergent-company/emergent.memory/pkg/whisper"
)

// Handler handles health check requests
type Handler struct {
	pool       *pgxpool.Pool
	cfg        *config.Config
	storage    *storage.Service
	kreuzberg  *kreuzberg.Client
	whisper    *whisper.Client
	embeddings *embeddings.Service
	startAt    time.Time
}

// NewHandler creates a new health handler
func NewHandler(
	pool *pgxpool.Pool,
	cfg *config.Config,
	storageSvc *storage.Service,
	kreuzbergClient *kreuzberg.Client,
	whisperClient *whisper.Client,
	embeddingsSvc *embeddings.Service,
) *Handler {
	return &Handler{
		pool:       pool,
		cfg:        cfg,
		storage:    storageSvc,
		kreuzberg:  kreuzbergClient,
		whisper:    whisperClient,
		embeddings: embeddingsSvc,
		startAt:    time.Now(),
	}
}

// TracingInfo holds tracing configuration exposed in the health response.
type TracingInfo struct {
	Enabled   bool   `json:"enabled"`
	TracesURL string `json:"traces_url,omitempty"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string           `json:"status"`
	Timestamp string           `json:"timestamp"`
	Uptime    string           `json:"uptime"`
	Version   string           `json:"version"`
	Checks    map[string]Check `json:"checks"`
	Tracing   *TracingInfo     `json:"tracing,omitempty"`
}

// Check represents an individual health check result
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Health returns the overall service health
// @Summary      Get service health
// @Description  Returns detailed health status including database, storage, auth, and service connectivity
// @Tags         health
// @Accept       json
// @Produce      json
// @Success      200 {object} HealthResponse "Service is healthy or degraded"
// @Success      503 {object} HealthResponse "Service is unhealthy"
// @Router       /health [get]
func (h *Handler) Health(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
	defer cancel()

	checks := h.runChecks(ctx)

	// Critical components: database, storage, auth — 503 if any are unhealthy
	// Optional components: kreuzberg, whisper, embeddings — 200 even if degraded
	criticalComponents := []string{"database", "storage", "auth"}
	optionalComponents := []string{"kreuzberg", "whisper", "embeddings"}

	overallStatus := "healthy"

	for _, name := range optionalComponents {
		if chk, ok := checks[name]; ok && chk.Status == "unhealthy" {
			overallStatus = "degraded"
		}
	}
	for _, name := range criticalComponents {
		if chk, ok := checks[name]; ok && chk.Status == "unhealthy" {
			overallStatus = "unhealthy"
			break
		}
	}

	var tracingInfo *TracingInfo
	if h.cfg.Otel.Enabled() {
		tracingInfo = &TracingInfo{
			Enabled:   true,
			TracesURL: "/api/traces",
		}
	}

	response := HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    time.Since(h.startAt).String(),
		Version:   version.Version,
		Checks:    checks,
		Tracing:   tracingInfo,
	}

	statusCode := http.StatusOK
	if overallStatus == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	return c.JSON(statusCode, response)
}

// runChecks executes all health checks concurrently and returns the results.
func (h *Handler) runChecks(ctx context.Context) map[string]Check {
	type result struct {
		name  string
		check Check
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]Check, 6)
	)

	emit := func(name string, chk Check) {
		mu.Lock()
		results[name] = chk
		mu.Unlock()
	}

	// Database
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := h.pool.Ping(ctx); err != nil {
			emit("database", Check{Status: "unhealthy", Message: err.Error()})
		} else {
			emit("database", Check{Status: "healthy"})
		}
	}()

	// Storage (MinIO/S3)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !h.storage.Enabled() {
			emit("storage", Check{Status: "unhealthy", Message: "not configured"})
			return
		}
		if err := h.storage.Ping(ctx); err != nil {
			emit("storage", Check{Status: "unhealthy", Message: err.Error()})
		} else {
			emit("storage", Check{Status: "healthy"})
		}
	}()

	// Auth (Zitadel OIDC discovery)
	wg.Add(1)
	go func() {
		defer wg.Done()
		// In standalone mode the server authenticates via API key; Zitadel is
		// not required, so report auth as healthy to avoid a spurious 503.
		if h.cfg.Standalone.Enabled {
			emit("auth", Check{Status: "healthy", Message: "standalone mode"})
			return
		}
		issuer := h.cfg.Zitadel.GetIssuer()
		if issuer == "" || h.cfg.Zitadel.Domain == "" {
			emit("auth", Check{Status: "unhealthy", Message: "not configured"})
			return
		}

		url := issuer + "/.well-known/openid-configuration"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			emit("auth", Check{Status: "unhealthy", Message: err.Error()})
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			emit("auth", Check{Status: "unhealthy", Message: err.Error()})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			emit("auth", Check{Status: "unhealthy", Message: "OIDC discovery returned " + resp.Status})
		} else {
			emit("auth", Check{Status: "healthy"})
		}
	}()

	// Kreuzberg (document extraction)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !h.kreuzberg.IsEnabled() {
			emit("kreuzberg", Check{Status: "healthy", Message: "disabled"})
			return
		}
		healthResp, _ := h.kreuzberg.HealthCheck(ctx)
		if healthResp == nil || healthResp.Status != "healthy" {
			msg := "unreachable"
			if healthResp != nil {
				if errDetail, ok := healthResp.Details["error"]; ok {
					msg = errDetail.(string)
				}
			}
			emit("kreuzberg", Check{Status: "unhealthy", Message: msg})
		} else {
			emit("kreuzberg", Check{Status: "healthy"})
		}
	}()

	// Whisper (audio transcription)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !h.whisper.IsEnabled() {
			emit("whisper", Check{Status: "healthy", Message: "disabled"})
			return
		}
		if err := h.whisper.HealthCheck(ctx); err != nil {
			emit("whisper", Check{Status: "unhealthy", Message: err.Error()})
		} else {
			emit("whisper", Check{Status: "healthy"})
		}
	}()

	// Embeddings (config-only, no live ping)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if h.embeddings.IsEnabled() {
			emit("embeddings", Check{Status: "healthy", Message: "enabled"})
		} else {
			emit("embeddings", Check{Status: "healthy", Message: "disabled"})
		}
	}()

	wg.Wait()
	return results
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

// Diagnose returns detailed DB and server diagnostics
// @Router /api/diagnostics [get]
func (h *Handler) Diagnose(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	result := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    time.Since(h.startAt).String(),
		"server":    make(map[string]any),
		"database":  make(map[string]any),
	}

	// Server Stats
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	result["server"] = map[string]any{
		"goroutines":   runtime.NumGoroutine(),
		"memory_alloc": mem.Alloc / 1024 / 1024,
		"memory_sys":   mem.Sys / 1024 / 1024,
		"num_cpu":      runtime.NumCPU(),
		"go_version":   runtime.Version(),
	}

	// DB Pool Stats
	stats := h.pool.Stat()
	result["database"].(map[string]any)["pool"] = map[string]any{
		"total_conns":       stats.TotalConns(),
		"acquired_conns":    stats.AcquiredConns(),
		"idle_conns":        stats.IdleConns(),
		"max_conns":         stats.MaxConns(),
		"canceled_acquires": stats.CanceledAcquireCount(),
		"empty_acquires":    stats.EmptyAcquireCount(),
	}

	// DB Connections from pg_stat_activity
	var connStatesJSON []byte
	err := h.pool.QueryRow(ctx, "SELECT COALESCE(json_agg(json_build_object('state', COALESCE(state, 'unknown'), 'count', count)), '[]'::json) FROM (SELECT state, count(*) as count FROM pg_stat_activity GROUP BY state) s").Scan(&connStatesJSON)
	if err != nil {
		result["database"].(map[string]any)["error"] = err.Error()
		return c.JSON(http.StatusOK, result)
	}
	var connStates []map[string]any
	_ = json.Unmarshal(connStatesJSON, &connStates)
	result["database"].(map[string]any)["connections"] = connStates

	// DB Long Running Queries
	var longQueriesJSON []byte
	_ = h.pool.QueryRow(ctx, "SELECT COALESCE(json_agg(json_build_object('pid', pid, 'query', left(query, 100), 'duration', age(clock_timestamp(), query_start), 'state', state)), '[]'::json) FROM pg_stat_activity WHERE state != 'idle' AND query_start < clock_timestamp() - interval '2 seconds' AND pid <> pg_backend_pid()").Scan(&longQueriesJSON)
	var longQueries []map[string]any
	_ = json.Unmarshal(longQueriesJSON, &longQueries)
	result["database"].(map[string]any)["long_queries"] = longQueries

	// DB Settings
	var dbSettingsJSON []byte
	_ = h.pool.QueryRow(ctx, "SELECT json_agg(json_build_object('name', name, 'setting', setting)) FROM pg_settings WHERE name IN ('max_connections', 'shared_buffers', 'work_mem', 'idle_in_transaction_session_timeout', 'statement_timeout')").Scan(&dbSettingsJSON)
	var dbSettings []map[string]any
	_ = json.Unmarshal(dbSettingsJSON, &dbSettings)
	result["database"].(map[string]any)["settings"] = dbSettings

	// DB Table Sizes
	var tableStatsJSON []byte
	_ = h.pool.QueryRow(ctx, "SELECT COALESCE(json_agg(json_build_object('table', c.relname, 'size', pg_size_pretty(pg_total_relation_size(c.oid)), 'rows', COALESCE(s.n_live_tup,0))), '[]'::json) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_stat_user_tables s ON s.relname = c.relname AND s.schemaname = n.nspname WHERE n.nspname IN ('kb', 'core') AND c.relkind = 'r' ORDER BY pg_total_relation_size(c.oid) DESC LIMIT 10").Scan(&tableStatsJSON)
	var tableStats []map[string]any
	_ = json.Unmarshal(tableStatsJSON, &tableStats)
	result["database"].(map[string]any)["tables"] = tableStats

	return c.JSON(http.StatusOK, result)
}
