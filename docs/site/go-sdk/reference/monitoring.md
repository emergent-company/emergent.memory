# monitoring

Package `monitoring` provides a client for the Emergent Monitoring API.

The monitoring client surfaces detailed telemetry for extraction jobs — the background processes that parse documents and populate the knowledge graph. It exposes per-job process logs, LLM call traces, and aggregated metrics.

## Import

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/monitoring"
```

## Client

```go
type Client struct { /* unexported */ }
```

Obtained via `sdk.Client.Monitoring` after calling `client.SetContext(orgID, projectID)`.

Requires `extraction:read` scope. `X-Project-ID` header is required for all methods.

### SetContext

```go
func (c *Client) SetContext(orgID, projectID string)
```

Updates the `X-Org-ID` and `X-Project-ID` headers sent with every request. Thread-safe.

---

## Methods

### ListExtractionJobs

```go
func (c *Client) ListExtractionJobs(ctx context.Context, opts *ListExtractionJobsOptions) (*ExtractionJobListResponse, error)
```

Returns a paginated list of extraction job summaries for the current project.

**Endpoint:** `GET /api/monitoring/extraction-jobs`

---

### GetExtractionJobDetail

```go
func (c *Client) GetExtractionJobDetail(ctx context.Context, jobID string) (*ExtractionJobDetail, error)
```

Returns full details for a specific extraction job including its process logs, LLM call log, and aggregated metrics.

**Endpoint:** `GET /api/monitoring/extraction-jobs/{jobID}`

---

### GetExtractionJobLogs

```go
func (c *Client) GetExtractionJobLogs(ctx context.Context, jobID string, opts *LogsOptions) (*ProcessLogListResponse, error)
```

Returns process log entries for an extraction job. Supports filtering by log level.

**Endpoint:** `GET /api/monitoring/extraction-jobs/{jobID}/logs`

---

### GetExtractionJobLLMCalls

```go
func (c *Client) GetExtractionJobLLMCalls(ctx context.Context, jobID string, opts *LLMCallsOptions) (*LLMCallListResponse, error)
```

Returns LLM API call logs for an extraction job. Useful for cost analysis and debugging prompts.

**Endpoint:** `GET /api/monitoring/extraction-jobs/{jobID}/llm-calls`

---

## Types

### ExtractionJobSummary

```go
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
```

Returned in list views. `Status` values: `"pending"`, `"running"`, `"completed"`, `"failed"`.

---

### ExtractionJobDetail

```go
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
```

---

### ExtractionJobMetrics

```go
type ExtractionJobMetrics struct {
    TotalLLMCalls     int     `json:"total_llm_calls"`
    TotalCostUSD      float64 `json:"total_cost_usd"`
    TotalTokens       int     `json:"total_tokens"`
    AvgCallDurationMs float64 `json:"avg_call_duration_ms"`
    SuccessRate       float64 `json:"success_rate"`
}
```

---

### ProcessLog

```go
type ProcessLog struct {
    ID          string                 `json:"id"`
    Timestamp   time.Time              `json:"timestamp"`
    Level       string                 `json:"level"` // "debug", "info", "warn", "error"
    Message     string                 `json:"message"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
    ProcessType string                 `json:"processType,omitempty"`
}
```

---

### LLMCallLog

```go
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
```

---

### ExtractionJobListResponse

```go
type ExtractionJobListResponse struct {
    Items  []ExtractionJobSummary `json:"items"`
    Total  int                    `json:"total"`
    Limit  int                    `json:"limit"`
    Offset int                    `json:"offset"`
}
```

---

### ProcessLogListResponse / LLMCallListResponse

```go
type ProcessLogListResponse struct {
    Logs []ProcessLog `json:"logs"`
}

type LLMCallListResponse struct {
    LLMCalls []LLMCallLog `json:"llm_calls"`
}
```

---

### ListExtractionJobsOptions

```go
type ListExtractionJobsOptions struct {
    Status     string // "pending", "running", "completed", "failed"
    SourceType string // Filter by source type
    DateFrom   string // RFC3339 lower bound
    DateTo     string // RFC3339 upper bound
    Limit      int    // 1–100, default 50
    Offset     int    // Pagination offset
    SortBy     string // Field to sort by
    SortOrder  string // "asc" or "desc"
}
```

---

### LogsOptions

```go
type LogsOptions struct {
    Level  string // "debug", "info", "warn", "error"
    Limit  int    // 1–500, default 100
    Offset int
}
```

---

### LLMCallsOptions

```go
type LLMCallsOptions struct {
    Limit  int // 1–500, default 50
    Offset int
}
```

---

## Example

```go
// List recently failed extraction jobs
jobs, err := client.Monitoring.ListExtractionJobs(ctx, &monitoring.ListExtractionJobsOptions{
    Status: "failed",
    Limit:  20,
})
if err != nil {
    log.Fatal(err)
}

for _, job := range jobs.Items {
    fmt.Printf("Job %s failed: %v\n", job.ID, job.ErrorMessage)

    // Get full detail with logs and LLM call trace
    detail, err := client.Monitoring.GetExtractionJobDetail(ctx, job.ID)
    if err != nil {
        continue
    }
    fmt.Printf("  Metrics: %d LLM calls, $%.4f USD\n",
        detail.Metrics.TotalLLMCalls, detail.Metrics.TotalCostUSD)

    // Print error-level logs
    logs, err := client.Monitoring.GetExtractionJobLogs(ctx, job.ID, &monitoring.LogsOptions{
        Level: "error",
    })
    if err != nil {
        continue
    }
    for _, l := range logs.Logs {
        fmt.Printf("  [%s] %s\n", l.Timestamp.Format(time.RFC3339), l.Message)
    }
}
```
