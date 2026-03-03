# useractivity

Package `useractivity` provides a client for the Emergent User Activity API.

The user activity client tracks recently accessed resources per user — such as documents, graphs, and agents — enabling "recently viewed" UI features and navigation shortcuts.

## Import

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/useractivity"
```

## Client

```go
type Client struct { /* unexported */ }
```

Obtained via `sdk.Client.UserActivity` after calling `client.SetContext(orgID, projectID)`.

### SetContext

```go
func (c *Client) SetContext(orgID, projectID string)
```

Updates the `X-Org-ID` and `X-Project-ID` headers sent with every request. Thread-safe.

---

## Methods

### Record

```go
func (c *Client) Record(ctx context.Context, projectID string, recordReq *RecordActivityRequest) error
```

Records a user activity event for a resource in a specific project. `projectID` is required and is passed as a query parameter (not a header).

**Endpoint:** `POST /api/user-activity/record`

---

### GetRecent

```go
func (c *Client) GetRecent(ctx context.Context, opts *ListOptions) (*RecentItemsResponse, error)
```

Returns recently accessed resources across all resource types for the authenticated user, ordered most-recent first.

**Endpoint:** `GET /api/user-activity/recent`

---

### GetRecentByType

```go
func (c *Client) GetRecentByType(ctx context.Context, resourceType string, opts *ListOptions) (*RecentItemsResponse, error)
```

Returns recently accessed resources filtered to a specific `resourceType`.

**Endpoint:** `GET /api/user-activity/recent/{resourceType}`

---

### DeleteAll

```go
func (c *Client) DeleteAll(ctx context.Context) error
```

Clears all recent activity history for the authenticated user.

**Endpoint:** `DELETE /api/user-activity/recent`

---

### DeleteByResource

```go
func (c *Client) DeleteByResource(ctx context.Context, resourceType, resourceID string) error
```

Removes a specific resource from the user's recent activity history.

**Endpoint:** `DELETE /api/user-activity/recent/{resourceType}/{resourceID}`

---

## Types

### RecentItem

```go
type RecentItem struct {
    ID              string    `json:"id"`
    ResourceType    string    `json:"resourceType"`
    ResourceID      string    `json:"resourceId"`
    ResourceName    *string   `json:"resourceName,omitempty"`
    ResourceSubtype *string   `json:"resourceSubtype,omitempty"`
    ActionType      string    `json:"actionType"`
    AccessedAt      time.Time `json:"accessedAt"`
    ProjectID       string    `json:"projectId"`
}
```

| Field | Description |
|---|---|
| `ResourceType` | Type of resource, e.g., `"document"`, `"graph-object"`, `"agent"` |
| `ResourceID` | The ID of the accessed resource |
| `ResourceSubtype` | Optional sub-classification (e.g., object type) |
| `ActionType` | The action performed, e.g., `"view"`, `"edit"` |
| `AccessedAt` | Timestamp of the access event |

---

### RecentItemsResponse

```go
type RecentItemsResponse struct {
    Data []RecentItem `json:"data"`
}
```

---

### RecordActivityRequest

```go
type RecordActivityRequest struct {
    ResourceType    string  `json:"resourceType"`
    ResourceID      string  `json:"resourceId"`
    ResourceName    *string `json:"resourceName,omitempty"`
    ResourceSubtype *string `json:"resourceSubtype,omitempty"`
    ActionType      string  `json:"actionType"`
}
```

---

### ListOptions

```go
type ListOptions struct {
    Limit int // Max results (default 20, max 100)
}
```

---

## Example

```go
// Record a "view" event when the user opens a document
subtype := "pdf"
err := client.UserActivity.Record(ctx, projectID, &useractivity.RecordActivityRequest{
    ResourceType:    "document",
    ResourceID:      docID,
    ResourceName:    &docName,
    ResourceSubtype: &subtype,
    ActionType:      "view",
})
if err != nil {
    log.Printf("record activity: %v", err)
}

// Fetch the 10 most recently viewed documents
recent, err := client.UserActivity.GetRecentByType(ctx, "document", &useractivity.ListOptions{
    Limit: 10,
})
if err != nil {
    log.Fatal(err)
}
for _, item := range recent.Data {
    fmt.Printf("%s — viewed %s\n", *item.ResourceName, item.AccessedAt.Format(time.RFC3339))
}
```
