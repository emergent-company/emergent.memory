# superadmin

Package `superadmin` provides a client for the Emergent Superadmin API.

The superadmin client is a **non-context client** — it requires no org or project context. All methods require **superadmin privileges**. The client provides platform-wide administration: user, organization, and project management; email, embedding, extraction, document-parsing, and sync job oversight.

## Import

```go
import "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/superadmin"
```

## Client

```go
type Client struct { /* unexported */ }
```

Obtained via `sdk.Client.Superadmin`. No `SetContext` call is needed or available.

> **All endpoints require superadmin role.** Calling these methods as a regular user returns `403 Forbidden`.

---

## Methods

### GetMe

```go
func (c *Client) GetMe(ctx context.Context) (*SuperadminMeResponse, error)
```

Returns whether the authenticated user has superadmin privileges.

**Endpoint:** `GET /api/superadmin/me`

---

### User Management

```go
func (c *Client) ListUsers(ctx context.Context, opts *ListUsersOptions) (*ListUsersResponse, error)
func (c *Client) DeleteUser(ctx context.Context, userID string) (*SuccessResponse, error)
```

**Endpoints:**
- `GET /api/superadmin/users`
- `DELETE /api/superadmin/users/{userID}`

`DeleteUser` performs a soft-delete.

---

### Organization Management

```go
func (c *Client) ListOrganizations(ctx context.Context, opts *PaginationOptions) (*ListOrganizationsResponse, error)
func (c *Client) DeleteOrganization(ctx context.Context, orgID string) (*SuccessResponse, error)
```

**Endpoints:**
- `GET /api/superadmin/organizations`
- `DELETE /api/superadmin/organizations/{orgID}`

---

### Project Management

```go
func (c *Client) ListProjects(ctx context.Context, opts *ListProjectsOptions) (*ListProjectsResponse, error)
func (c *Client) DeleteProject(ctx context.Context, projectID string) (*SuccessResponse, error)
```

**Endpoints:**
- `GET /api/superadmin/projects`
- `DELETE /api/superadmin/projects/{projectID}`

---

### Email Job Management

```go
func (c *Client) ListEmailJobs(ctx context.Context, opts *ListEmailJobsOptions) (*ListEmailJobsResponse, error)
func (c *Client) GetEmailJobPreview(ctx context.Context, jobID string) (*EmailJobPreviewResponse, error)
```

**Endpoints:**
- `GET /api/superadmin/email-jobs`
- `GET /api/superadmin/email-jobs/{jobID}/preview-json`

---

### Embedding Job Management

```go
func (c *Client) ListEmbeddingJobs(ctx context.Context, opts *ListEmbeddingJobsOptions) (*ListEmbeddingJobsResponse, error)
func (c *Client) DeleteEmbeddingJobs(ctx context.Context, req *DeleteJobsRequest) (*DeleteJobsResponse, error)
func (c *Client) CleanupOrphanEmbeddingJobs(ctx context.Context) (*CleanupOrphansResponse, error)
```

**Endpoints:**
- `GET /api/superadmin/embedding-jobs`
- `POST /api/superadmin/embedding-jobs/delete`
- `POST /api/superadmin/embedding-jobs/cleanup-orphans`

---

### Extraction Job Management

```go
func (c *Client) ListExtractionJobs(ctx context.Context, opts *ListExtractionJobsOptions) (*ListExtractionJobsResponse, error)
func (c *Client) DeleteExtractionJobs(ctx context.Context, req *DeleteJobsRequest) (*DeleteJobsResponse, error)
func (c *Client) CancelExtractionJobs(ctx context.Context, req *CancelJobsRequest) (*CancelJobsResponse, error)
```

**Endpoints:**
- `GET /api/superadmin/extraction-jobs`
- `POST /api/superadmin/extraction-jobs/delete`
- `POST /api/superadmin/extraction-jobs/cancel`

---

### Document Parsing Job Management

```go
func (c *Client) ListDocumentParsingJobs(ctx context.Context, opts *ListDocumentParsingJobsOptions) (*ListDocumentParsingJobsResponse, error)
func (c *Client) DeleteDocumentParsingJobs(ctx context.Context, req *DeleteJobsRequest) (*DeleteJobsResponse, error)
func (c *Client) RetryDocumentParsingJobs(ctx context.Context, req *RetryJobsRequest) (*RetryJobsResponse, error)
```

**Endpoints:**
- `GET /api/superadmin/document-parsing-jobs`
- `POST /api/superadmin/document-parsing-jobs/delete`
- `POST /api/superadmin/document-parsing-jobs/retry`

---

### Sync Job Management

```go
func (c *Client) ListSyncJobs(ctx context.Context, opts *ListSyncJobsOptions) (*ListSyncJobsResponse, error)
func (c *Client) GetSyncJobLogs(ctx context.Context, jobID string) (*SyncJobLogsResponse, error)
func (c *Client) DeleteSyncJobs(ctx context.Context, req *DeleteJobsRequest) (*DeleteJobsResponse, error)
func (c *Client) CancelSyncJobs(ctx context.Context, req *CancelJobsRequest) (*CancelJobsResponse, error)
```

**Endpoints:**
- `GET /api/superadmin/sync-jobs`
- `GET /api/superadmin/sync-jobs/{jobID}/logs`
- `POST /api/superadmin/sync-jobs/delete`
- `POST /api/superadmin/sync-jobs/cancel`

---

## Types

### PaginationMeta

```go
type PaginationMeta struct {
    Page       int  `json:"page"`
    Limit      int  `json:"limit"`
    Total      int  `json:"total"`
    TotalPages int  `json:"totalPages"`
    HasNext    bool `json:"hasNext"`
    HasPrev    bool `json:"hasPrev"`
}
```

---

### User

```go
type User struct {
    ID             string              `json:"id"`
    ZitadelUserID  string              `json:"zitadelUserId"`
    FirstName      *string             `json:"firstName,omitempty"`
    LastName       *string             `json:"lastName,omitempty"`
    DisplayName    *string             `json:"displayName,omitempty"`
    PrimaryEmail   *string             `json:"primaryEmail,omitempty"`
    LastActivityAt *time.Time          `json:"lastActivityAt,omitempty"`
    CreatedAt      time.Time           `json:"createdAt"`
    Organizations  []UserOrgMembership `json:"organizations"`
}
```

### UserOrgMembership

```go
type UserOrgMembership struct {
    OrgID    string    `json:"orgId"`
    OrgName  string    `json:"orgName"`
    Role     string    `json:"role"`
    JoinedAt time.Time `json:"joinedAt"`
}
```

---

### Organization

```go
type Organization struct {
    ID           string     `json:"id"`
    Name         string     `json:"name"`
    MemberCount  int        `json:"memberCount"`
    ProjectCount int        `json:"projectCount"`
    CreatedAt    time.Time  `json:"createdAt"`
    DeletedAt    *time.Time `json:"deletedAt,omitempty"`
}
```

---

### Project

```go
type Project struct {
    ID               string     `json:"id"`
    Name             string     `json:"name"`
    OrganizationID   string     `json:"organizationId"`
    OrganizationName string     `json:"organizationName"`
    DocumentCount    int        `json:"documentCount"`
    CreatedAt        time.Time  `json:"createdAt"`
    DeletedAt        *time.Time `json:"deletedAt,omitempty"`
}
```

---

### Options types

| Type | Key fields |
|---|---|
| `ListUsersOptions` | `Page`, `Limit`, `Search`, `OrgID` |
| `PaginationOptions` | `Page`, `Limit` |
| `ListProjectsOptions` | `Page`, `Limit`, `OrgID` |
| `ListEmailJobsOptions` | `Page`, `Limit`, `Status`, `Recipient`, `FromDate`, `ToDate` |
| `ListEmbeddingJobsOptions` | `Page`, `Limit`, `Status`, `HasError`, `ProjectID`, `Type` (`"graph"` or `"chunk"`) |
| `ListExtractionJobsOptions` | `Page`, `Limit`, `Status`, `JobType`, `ProjectID`, `HasError` |
| `ListDocumentParsingJobsOptions` | `Page`, `Limit`, `Status`, `ProjectID`, `HasError` |
| `ListSyncJobsOptions` | `Page`, `Limit`, `Status`, `ProjectID`, `HasError` |

---

### Bulk operation types

```go
// Bulk delete
type DeleteJobsRequest struct {
    IDs  []string `json:"ids"`
    Type string   `json:"type,omitempty"`
}
type DeleteJobsResponse struct {
    Success      bool   `json:"success"`
    DeletedCount int    `json:"deletedCount"`
    Message      string `json:"message"`
}

// Bulk cancel
type CancelJobsRequest struct {
    IDs []string `json:"ids"`
}
type CancelJobsResponse struct {
    Success        bool   `json:"success"`
    CancelledCount int    `json:"cancelledCount"`
    Message        string `json:"message"`
}

// Bulk retry (document parsing only)
type RetryJobsRequest struct {
    IDs []string `json:"ids"`
}
type RetryJobsResponse struct {
    Success      bool   `json:"success"`
    RetriedCount int    `json:"retriedCount"`
    Message      string `json:"message"`
}
```

---

## Example

```go
// Verify superadmin role
me, err := client.Superadmin.GetMe(ctx)
if err != nil || !me.IsSuperadmin {
    log.Fatal("not a superadmin")
}

// List failed extraction jobs platform-wide
hasErr := true
jobs, err := client.Superadmin.ListExtractionJobs(ctx, &superadmin.ListExtractionJobsOptions{
    HasError: &hasErr,
    Limit:    50,
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Failed extraction jobs: %d / %d total\n", jobs.Stats.Failed, jobs.Stats.Total)

// Cancel all queued jobs
var ids []string
for _, j := range jobs.Jobs {
    if j.Status == "queued" {
        ids = append(ids, j.ID)
    }
}
if len(ids) > 0 {
    result, err := client.Superadmin.CancelExtractionJobs(ctx, &superadmin.CancelJobsRequest{IDs: ids})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Cancelled %d jobs\n", result.CancelledCount)
}
```
