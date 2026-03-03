# tasks

Package `tasks` provides a client for the Emergent Tasks API.

Tasks are human-review items — such as AI-generated suggestions or flagged content — that require explicit acceptance, rejection, or cancellation. They are project-scoped and tied to a review/resolution workflow.

> **Note:** This client tracks reviewable work items, not background job execution. See [monitoring](monitoring.md) for extraction job monitoring.

## Import

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/tasks"
```

## Client

```go
type Client struct { /* unexported */ }
```

Obtained via `sdk.Client.Tasks` after calling `client.SetContext(orgID, projectID)`.

### SetContext

```go
func (c *Client) SetContext(orgID, projectID string)
```

Updates the `X-Org-ID` and `X-Project-ID` headers sent with every request. Thread-safe.

---

## Methods

### List

```go
func (c *Client) List(ctx context.Context, opts *ListOptions) (*TaskListResponse, error)
```

Returns paginated tasks for the context project. Pass `opts.ProjectID` to override the header-based project.

**Endpoint:** `GET /api/tasks`

---

### GetCounts

```go
func (c *Client) GetCounts(ctx context.Context, projectID string) (*TaskCounts, error)
```

Returns task counts broken down by status for a specific project.

**Endpoint:** `GET /api/tasks/counts`

---

### ListAll

```go
func (c *Client) ListAll(ctx context.Context, opts *ListOptions) (*TaskListResponse, error)
```

Returns paginated tasks across **all** projects accessible to the authenticated user. Useful for global dashboards.

**Endpoint:** `GET /api/tasks/all`

---

### GetAllCounts

```go
func (c *Client) GetAllCounts(ctx context.Context) (*TaskCounts, error)
```

Returns aggregated task counts across all projects for the authenticated user.

**Endpoint:** `GET /api/tasks/all/counts`

---

### GetByID

```go
func (c *Client) GetByID(ctx context.Context, taskID, projectID string) (*TaskResponse, error)
```

Returns a specific task by ID. `projectID` may be provided as a query param or left empty to use the context header.

**Endpoint:** `GET /api/tasks/{taskID}`

---

### Resolve

```go
func (c *Client) Resolve(ctx context.Context, taskID, projectID string, resolveReq *ResolveTaskRequest) error
```

Marks a task as accepted or rejected. `Resolution` must be `"accepted"` or `"rejected"`.

**Endpoint:** `POST /api/tasks/{taskID}/resolve`

---

### Cancel

```go
func (c *Client) Cancel(ctx context.Context, taskID, projectID string) error
```

Cancels a pending task. Only pending tasks may be cancelled.

**Endpoint:** `POST /api/tasks/{taskID}/cancel`

---

## Types

### Task

```go
type Task struct {
    ID              string          `json:"id"`
    ProjectID       string          `json:"projectId"`
    Title           string          `json:"title"`
    Description     *string         `json:"description,omitempty"`
    Type            string          `json:"type"`
    Status          string          `json:"status"`
    ResolvedAt      *time.Time      `json:"resolvedAt,omitempty"`
    ResolvedBy      *string         `json:"resolvedBy,omitempty"`
    ResolutionNotes *string         `json:"resolutionNotes,omitempty"`
    SourceType      *string         `json:"sourceType,omitempty"`
    SourceID        *string         `json:"sourceId,omitempty"`
    Metadata        json.RawMessage `json:"metadata,omitempty"`
    CreatedAt       time.Time       `json:"createdAt"`
    UpdatedAt       time.Time       `json:"updatedAt"`
}
```

| Field | Description |
|---|---|
| `Type` | Category of task (e.g., `"suggestion"`, `"review"`) |
| `Status` | `"pending"`, `"accepted"`, `"rejected"`, `"cancelled"` |
| `SourceType` / `SourceID` | The resource that generated this task |
| `ResolvedBy` | User ID who resolved the task |
| `ResolutionNotes` | Optional notes attached at resolution |

---

### TaskCounts

```go
type TaskCounts struct {
    Pending   int64 `json:"pending"`
    Accepted  int64 `json:"accepted"`
    Rejected  int64 `json:"rejected"`
    Cancelled int64 `json:"cancelled"`
}
```

---

### TaskListResponse

```go
type TaskListResponse struct {
    Data  []Task `json:"data"`
    Total int    `json:"total"`
}
```

---

### TaskResponse

```go
type TaskResponse struct {
    Data Task `json:"data"`
}
```

---

### ResolveTaskRequest

```go
type ResolveTaskRequest struct {
    Resolution      string  `json:"resolution"`        // "accepted" or "rejected"
    ResolutionNotes *string `json:"resolutionNotes,omitempty"`
}
```

---

### ListOptions

```go
type ListOptions struct {
    ProjectID string // Override context project; passed as query param
    Status    string // "pending", "accepted", "rejected", "cancelled"
    Type      string // Filter by task type
    Limit     int    // Max results (1–100)
    Offset    int    // Pagination offset
}
```

---

## Example

```go
// Count pending tasks in the current project
counts, err := client.Tasks.GetCounts(ctx, "")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Pending review items: %d\n", counts.Pending)

// List and resolve each pending task
list, err := client.Tasks.List(ctx, &tasks.ListOptions{Status: "pending"})
if err != nil {
    log.Fatal(err)
}
for _, t := range list.Data {
    fmt.Printf("Reviewing: %s (%s)\n", t.Title, t.Type)
    notes := "Looks good"
    err := client.Tasks.Resolve(ctx, t.ID, t.ProjectID, &tasks.ResolveTaskRequest{
        Resolution:      "accepted",
        ResolutionNotes: &notes,
    })
    if err != nil {
        log.Printf("failed to resolve task %s: %v", t.ID, err)
    }
}
```
