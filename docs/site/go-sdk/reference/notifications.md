# notifications

Package `notifications` provides a client for the Emergent Notifications API.

Notifications inform users of events such as completed jobs, system alerts, and review requests. They are scoped to the authenticated user and optionally to an organization and project.

## Import

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/notifications"
```

## Client

```go
type Client struct { /* unexported */ }
```

Obtained via `sdk.Client.Notifications` after calling `client.SetContext(orgID, projectID)`.

### SetContext

```go
func (c *Client) SetContext(orgID, projectID string)
```

Updates the `X-Org-ID` and `X-Project-ID` headers sent with every request. Thread-safe.

---

## Methods

### GetStats

```go
func (c *Client) GetStats(ctx context.Context) (*NotificationStats, error)
```

Returns aggregated unread/dismissed/total counts for the current user.

**Endpoint:** `GET /api/notifications/stats`

---

### GetCounts

```go
func (c *Client) GetCounts(ctx context.Context) (*NotificationCountsResponse, error)
```

Returns notification counts broken down by UI tab (All, Important, Other, Snoozed, Cleared).

**Endpoint:** `GET /api/notifications/counts`

---

### List

```go
func (c *Client) List(ctx context.Context, opts *ListOptions) (*NotificationListResponse, error)
```

Returns a filtered list of notifications for the current user.

**Endpoint:** `GET /api/notifications`

**Parameters:**

| Field | Type | Description |
|---|---|---|
| `opts` | `*ListOptions` | Filtering options; pass `nil` for defaults |

---

### MarkRead

```go
func (c *Client) MarkRead(ctx context.Context, notificationID string) error
```

Marks a single notification as read.

**Endpoint:** `PATCH /api/notifications/{id}/read`

---

### MarkAllRead

```go
func (c *Client) MarkAllRead(ctx context.Context) (*MarkAllReadResponse, error)
```

Marks all unread notifications as read for the current user. Returns the count of affected notifications.

**Endpoint:** `POST /api/notifications/mark-all-read`

---

### Dismiss

```go
func (c *Client) Dismiss(ctx context.Context, notificationID string) error
```

Dismisses a notification. Dismissed notifications appear in the "Cleared" tab.

**Endpoint:** `DELETE /api/notifications/{id}/dismiss`

---

## Types

### Notification

```go
type Notification struct {
    ID                  string          `json:"id"`
    ProjectID           *string         `json:"projectId,omitempty"`
    UserID              string          `json:"userId"`
    Title               string          `json:"title"`
    Message             string          `json:"message"`
    Type                *string         `json:"type,omitempty"`
    Severity            string          `json:"severity"`
    RelatedResourceType *string         `json:"relatedResourceType,omitempty"`
    RelatedResourceID   *string         `json:"relatedResourceId,omitempty"`
    Read                bool            `json:"read"`
    Dismissed           bool            `json:"dismissed"`
    DismissedAt         *time.Time      `json:"dismissedAt,omitempty"`
    Actions             json.RawMessage `json:"actions"`
    ExpiresAt           *time.Time      `json:"expiresAt,omitempty"`
    ReadAt              *time.Time      `json:"readAt,omitempty"`
    Importance          string          `json:"importance"`
    ClearedAt           *time.Time      `json:"clearedAt,omitempty"`
    SnoozedUntil        *time.Time      `json:"snoozedUntil,omitempty"`
    Category            *string         `json:"category,omitempty"`
    SourceType          *string         `json:"sourceType,omitempty"`
    SourceID            *string         `json:"sourceId,omitempty"`
    ActionURL           *string         `json:"actionUrl,omitempty"`
    ActionLabel         *string         `json:"actionLabel,omitempty"`
    GroupKey            *string         `json:"groupKey,omitempty"`
    Details             json.RawMessage `json:"details,omitempty"`
    CreatedAt           time.Time       `json:"createdAt"`
    UpdatedAt           time.Time       `json:"updatedAt"`
    TaskID              *string         `json:"taskId,omitempty"`
}
```

| Field | Description |
|---|---|
| `ID` | Unique notification ID |
| `UserID` | Owner of the notification |
| `Title` / `Message` | Display text |
| `Severity` | `"info"`, `"warning"`, `"error"`, `"critical"` |
| `Importance` | Controls which UI tab the notification appears in |
| `Read` / `Dismissed` | Read/dismissed state |
| `RelatedResourceType` / `RelatedResourceID` | Linked resource (e.g., a document or job) |
| `Actions` | JSON array of action buttons |
| `SnoozedUntil` | Non-nil when snoozed |
| `TaskID` | Associated review task, if any |

---

### NotificationStats

```go
type NotificationStats struct {
    Unread    int64 `json:"unread"`
    Dismissed int64 `json:"dismissed"`
    Total     int64 `json:"total"`
}
```

---

### NotificationCounts

```go
type NotificationCounts struct {
    All       int64 `json:"all"`
    Important int64 `json:"important"`
    Other     int64 `json:"other"`
    Snoozed   int64 `json:"snoozed"`
    Cleared   int64 `json:"cleared"`
}
```

Counts per UI tab.

### NotificationCountsResponse

```go
type NotificationCountsResponse struct {
    Data NotificationCounts `json:"data"`
}
```

---

### NotificationListResponse

```go
type NotificationListResponse struct {
    Data []Notification `json:"data"`
}
```

---

### MarkAllReadResponse

```go
type MarkAllReadResponse struct {
    Status string `json:"status"`
    Count  int    `json:"count"`
}
```

`Count` is the number of notifications that were marked as read.

---

### ListOptions

```go
type ListOptions struct {
    Tab        string // "all", "important", "other", "snoozed", "cleared"
    Category   string // Filter by notification category
    UnreadOnly bool   // Show only unread notifications
    Search     string // Search by title or message
}
```

---

## Example

```go
// Poll unread count
stats, err := client.Notifications.GetStats(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Unread: %d\n", stats.Unread)

// List important unread notifications
list, err := client.Notifications.List(ctx, &notifications.ListOptions{
    Tab:        "important",
    UnreadOnly: true,
})
if err != nil {
    log.Fatal(err)
}
for _, n := range list.Data {
    fmt.Printf("[%s] %s: %s\n", n.Severity, n.Title, n.Message)

    // Mark as read
    if err := client.Notifications.MarkRead(ctx, n.ID); err != nil {
        log.Printf("mark read failed: %v", err)
    }
}

// Dismiss all and get how many were marked
resp, err := client.Notifications.MarkAllRead(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Marked %d notifications as read\n", resp.Count)
```
