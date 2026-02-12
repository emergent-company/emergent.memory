// Package health provides the Health service client for the Emergent API SDK.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Health API.
type Client struct {
	http *http.Client
	base string
}

// NewClient creates a new health client.
func NewClient(httpClient *http.Client, baseURL string) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
	}
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status    string           `json:"status"`
	Timestamp string           `json:"timestamp"`
	Uptime    string           `json:"uptime"`
	Version   string           `json:"version"`
	Checks    map[string]Check `json:"checks"`
}

// Check represents an individual health check result.
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ReadyResponse represents the readiness probe response.
type ReadyResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// DebugResponse represents the debug information response.
type DebugResponse struct {
	Environment string         `json:"environment"`
	Debug       bool           `json:"debug"`
	GoVersion   string         `json:"go_version"`
	Goroutines  int            `json:"goroutines"`
	Memory      DebugMemory    `json:"memory"`
	Database    DebugDatabase  `json:"database"`
	Extra       map[string]any `json:"-"` // catch-all for unexpected fields
}

// DebugMemory represents memory statistics in the debug response.
type DebugMemory struct {
	AllocMB      uint64 `json:"alloc_mb"`
	TotalAllocMB uint64 `json:"total_alloc_mb"`
	SysMB        uint64 `json:"sys_mb"`
	NumGC        uint32 `json:"num_gc"`
}

// DebugDatabase represents database pool info in the debug response.
type DebugDatabase struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Database  string `json:"database"`
	PoolTotal int32  `json:"pool_total"`
	PoolIdle  int32  `json:"pool_idle"`
	PoolInUse int32  `json:"pool_in_use"`
}

// JobQueueMetrics represents metrics for a single job queue.
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

// AllJobMetrics contains metrics for all job queues.
type AllJobMetrics struct {
	Queues    []JobQueueMetrics `json:"queues"`
	Timestamp string            `json:"timestamp"`
}

// SchedulerMetrics represents scheduler metrics (placeholder on server).
type SchedulerMetrics struct {
	Message string         `json:"message,omitempty"`
	Extra   map[string]any `json:"-"` // catch-all for future fields
}

// ---------------------------------------------------------------------------
// Health endpoints (no auth required)
// ---------------------------------------------------------------------------

// Health returns the overall service health.
// GET /health
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 503 is acceptable (unhealthy state) — only treat 4xx as errors
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusServiceUnavailable {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &health, nil
}

// APIHealth returns the service health via the /api/health route.
// GET /api/health
func (c *Client) APIHealth(ctx context.Context) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/health", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusServiceUnavailable {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &health, nil
}

// Ready returns readiness status (for k8s readiness probes).
// GET /ready
func (c *Client) Ready(ctx context.Context) (*ReadyResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/ready", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 503 means "not ready" — still parse the response
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusServiceUnavailable {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var ready ReadyResponse
	if err := json.NewDecoder(resp.Body).Decode(&ready); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &ready, nil
}

// IsReady is a convenience method that returns true if the service is ready.
// GET /ready
func (c *Client) IsReady(ctx context.Context) (bool, error) {
	r, err := c.Ready(ctx)
	if err != nil {
		return false, err
	}
	return r.Status == "ready", nil
}

// Healthz returns a simple health check (for k8s liveness probe).
// Returns nil if alive, error otherwise.
// GET /healthz
func (c *Client) Healthz(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz check failed with status %d", resp.StatusCode)
	}

	return nil
}

// Debug returns debug information (only available in non-production environments).
// GET /debug
func (c *Client) Debug(ctx context.Context) (*DebugResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/debug", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var debug DebugResponse
	if err := json.NewDecoder(resp.Body).Decode(&debug); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &debug, nil
}

// ---------------------------------------------------------------------------
// Metrics endpoints (no auth required)
// ---------------------------------------------------------------------------

// JobMetrics returns metrics for all job queues.
// GET /api/metrics/jobs
func (c *Client) JobMetrics(ctx context.Context) (*AllJobMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/metrics/jobs", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var metrics AllJobMetrics
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &metrics, nil
}

// SchedulerStatus returns metrics for scheduled tasks.
// GET /api/metrics/scheduler
func (c *Client) SchedulerStatus(ctx context.Context) (*SchedulerMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/metrics/scheduler", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Server returns a generic map — decode into our type
	var metrics SchedulerMetrics
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &metrics, nil
}
