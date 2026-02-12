// Package monitoring provides the Monitoring service client for the Emergent API SDK.
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Monitoring API.
// Requires authentication and extraction:read scope.
// Requires X-Project-ID header.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new monitoring client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider, orgID, projectID string) *Client {
	return &Client{
		http:      httpClient,
		base:      baseURL,
		auth:      authProvider,
		orgID:     orgID,
		projectID: projectID,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgID = orgID
	c.projectID = projectID
}

// --- Types ---

// ExtractionJobSummary represents a summary of an extraction job for list views.
type ExtractionJobSummary struct {
	ID                   string     `json:"id"`
	SourceType           string     `json:"source_type"`
	SourceID             string     `json:"source_id"`
	Status               string     `json:"status"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	DurationMs           *int       `json:"duration_ms,omitempty"`
	ObjectsCreated       *int       `json:"objects_created,omitempty"`
	RelationshipsCreated *int       `json:"relationships_created,omitempty"`
	SuggestionsCreated   *int       `json:"suggestions_created,omitempty"`
	TotalLLMCalls        *int       `json:"total_llm_calls,omitempty"`
	TotalCostUSD         *float64   `json:"total_cost_usd,omitempty"`
	ErrorMessage         *string    `json:"error_message,omitempty"`
}

// ExtractionJobDetail represents full details for an extraction job.
type ExtractionJobDetail struct {
	ID                   string                `json:"id"`
	SourceType           string                `json:"source_type"`
	SourceID             string                `json:"source_id"`
	Status               string                `json:"status"`
	StartedAt            *time.Time            `json:"started_at,omitempty"`
	CompletedAt          *time.Time            `json:"completed_at,omitempty"`
	DurationMs           *int                  `json:"duration_ms,omitempty"`
	ObjectsCreated       *int                  `json:"objects_created,omitempty"`
	RelationshipsCreated *int                  `json:"relationships_created,omitempty"`
	SuggestionsCreated   *int                  `json:"suggestions_created,omitempty"`
	ErrorMessage         *string               `json:"error_message,omitempty"`
	Logs                 []ProcessLog          `json:"logs"`
	LLMCalls             []LLMCallLog          `json:"llm_calls"`
	Metrics              *ExtractionJobMetrics `json:"metrics,omitempty"`
}

// ExtractionJobMetrics represents aggregated metrics for an extraction job.
type ExtractionJobMetrics struct {
	TotalLLMCalls     int     `json:"total_llm_calls"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	TotalTokens       int     `json:"total_tokens"`
	AvgCallDurationMs float64 `json:"avg_call_duration_ms"`
	SuccessRate       float64 `json:"success_rate"`
}

// ProcessLog represents a system process log entry.
type ProcessLog struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Level       string                 `json:"level"`
	Message     string                 `json:"message"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	ProcessType string                 `json:"processType,omitempty"`
}

// LLMCallLog represents an LLM API call log entry.
type LLMCallLog struct {
	ID              string                 `json:"id"`
	ModelName       string                 `json:"model_name"`
	Status          string                 `json:"status"`
	InputTokens     *int                   `json:"input_tokens,omitempty"`
	OutputTokens    *int                   `json:"output_tokens,omitempty"`
	TotalTokens     *int                   `json:"total_tokens,omitempty"`
	CostUSD         *float64               `json:"cost_usd,omitempty"`
	DurationMs      *int                   `json:"duration_ms,omitempty"`
	StartedAt       time.Time              `json:"started_at"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	RequestPayload  map[string]interface{} `json:"request_payload,omitempty"`
	ResponsePayload map[string]interface{} `json:"response_payload,omitempty"`
	ErrorMessage    *string                `json:"error_message,omitempty"`
}

// ExtractionJobListResponse represents a paginated list of extraction jobs.
type ExtractionJobListResponse struct {
	Items  []ExtractionJobSummary `json:"items"`
	Total  int                    `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

// ProcessLogListResponse wraps process logs for the response.
type ProcessLogListResponse struct {
	Logs []ProcessLog `json:"logs"`
}

// LLMCallListResponse wraps LLM call logs for the response.
type LLMCallListResponse struct {
	LLMCalls []LLMCallLog `json:"llm_calls"`
}

// --- Options ---

// ListExtractionJobsOptions holds options for listing extraction jobs.
type ListExtractionJobsOptions struct {
	Status     string // Filter by status: "pending", "running", "completed", "failed"
	SourceType string // Filter by source type
	DateFrom   string // Filter from date (RFC3339 format)
	DateTo     string // Filter to date (RFC3339 format)
	Limit      int    // Max results (1-100, default 50)
	Offset     int    // Pagination offset (default 0)
	SortBy     string // Sort field
	SortOrder  string // Sort order: "asc" or "desc"
}

// LogsOptions holds options for fetching extraction job logs.
type LogsOptions struct {
	Level  string // Filter by level: "debug", "info", "warn", "error"
	Limit  int    // Max results (1-500, default 100)
	Offset int    // Pagination offset (default 0)
}

// LLMCallsOptions holds options for fetching extraction job LLM calls.
type LLMCallsOptions struct {
	Limit  int // Max results (1-500, default 50)
	Offset int // Pagination offset (default 0)
}

// --- Internal helpers ---

func (c *Client) setHeaders(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	return nil
}

// --- API Methods ---

// ListExtractionJobs lists extraction jobs with filtering and pagination.
// GET /api/monitoring/extraction-jobs
func (c *Client) ListExtractionJobs(ctx context.Context, opts *ListExtractionJobsOptions) (*ExtractionJobListResponse, error) {
	u, err := url.Parse(c.base + "/api/monitoring/extraction-jobs")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.SourceType != "" {
			q.Set("source_type", opts.SourceType)
		}
		if opts.DateFrom != "" {
			q.Set("date_from", opts.DateFrom)
		}
		if opts.DateTo != "" {
			q.Set("date_to", opts.DateTo)
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", opts.Offset))
		}
		if opts.SortBy != "" {
			q.Set("sort_by", opts.SortBy)
		}
		if opts.SortOrder != "" {
			q.Set("sort_order", opts.SortOrder)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ExtractionJobListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetExtractionJobDetail retrieves full details for a specific extraction job,
// including logs, LLM calls, and metrics.
// GET /api/monitoring/extraction-jobs/:id
func (c *Client) GetExtractionJobDetail(ctx context.Context, jobID string) (*ExtractionJobDetail, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/monitoring/extraction-jobs/"+jobID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ExtractionJobDetail
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetExtractionJobLogs retrieves process logs for a specific extraction job.
// GET /api/monitoring/extraction-jobs/:id/logs
func (c *Client) GetExtractionJobLogs(ctx context.Context, jobID string, opts *LogsOptions) (*ProcessLogListResponse, error) {
	u, err := url.Parse(c.base + "/api/monitoring/extraction-jobs/" + jobID + "/logs")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Level != "" {
			q.Set("level", opts.Level)
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", opts.Offset))
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ProcessLogListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetExtractionJobLLMCalls retrieves LLM API call logs for a specific extraction job.
// GET /api/monitoring/extraction-jobs/:id/llm-calls
func (c *Client) GetExtractionJobLLMCalls(ctx context.Context, jobID string, opts *LLMCallsOptions) (*LLMCallListResponse, error) {
	u, err := url.Parse(c.base + "/api/monitoring/extraction-jobs/" + jobID + "/llm-calls")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", opts.Offset))
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result LLMCallListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
