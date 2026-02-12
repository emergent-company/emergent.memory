// Package superadmin provides the Superadmin service client for the Emergent API SDK.
// All endpoints require superadmin privileges.
package superadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Superadmin API.
type Client struct {
	http *http.Client
	base string
	auth auth.Provider
}

// NewClient creates a new superadmin client.
// This is a non-context client (no org/project required).
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// --- Types ---

// PaginationMeta contains pagination metadata.
type PaginationMeta struct {
	Page       int  `json:"page"`
	Limit      int  `json:"limit"`
	Total      int  `json:"total"`
	TotalPages int  `json:"totalPages"`
	HasNext    bool `json:"hasNext"`
	HasPrev    bool `json:"hasPrev"`
}

// PaginationOptions holds common pagination parameters.
type PaginationOptions struct {
	Page  int
	Limit int
}

// SuperadminMeResponse is the response for GET /api/superadmin/me.
type SuperadminMeResponse struct {
	IsSuperadmin bool `json:"isSuperadmin"`
}

// UserOrgMembership represents a user's org membership.
type UserOrgMembership struct {
	OrgID    string    `json:"orgId"`
	OrgName  string    `json:"orgName"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

// User represents a user in the superadmin list.
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

// ListUsersResponse is the response for listing users.
type ListUsersResponse struct {
	Users []User         `json:"users"`
	Meta  PaginationMeta `json:"meta"`
}

// ListUsersOptions holds query parameters for listing users.
type ListUsersOptions struct {
	Page   int
	Limit  int
	Search string
	OrgID  string
}

// Organization represents an org in the superadmin list.
type Organization struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	MemberCount  int        `json:"memberCount"`
	ProjectCount int        `json:"projectCount"`
	CreatedAt    time.Time  `json:"createdAt"`
	DeletedAt    *time.Time `json:"deletedAt,omitempty"`
}

// ListOrganizationsResponse is the response for listing organizations.
type ListOrganizationsResponse struct {
	Organizations []Organization `json:"organizations"`
	Meta          PaginationMeta `json:"meta"`
}

// Project represents a project in the superadmin list.
type Project struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	OrganizationID   string     `json:"organizationId"`
	OrganizationName string     `json:"organizationName"`
	DocumentCount    int        `json:"documentCount"`
	CreatedAt        time.Time  `json:"createdAt"`
	DeletedAt        *time.Time `json:"deletedAt,omitempty"`
}

// ListProjectsResponse is the response for listing projects.
type ListProjectsResponse struct {
	Projects []Project      `json:"projects"`
	Meta     PaginationMeta `json:"meta"`
}

// ListProjectsOptions holds query parameters for listing projects.
type ListProjectsOptions struct {
	Page  int
	Limit int
	OrgID string
}

// SuccessResponse is a generic success response.
type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// EmailJob represents an email job.
type EmailJob struct {
	ID               string     `json:"id"`
	TemplateName     string     `json:"templateName"`
	ToEmail          string     `json:"toEmail"`
	ToName           *string    `json:"toName,omitempty"`
	Subject          string     `json:"subject"`
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	MaxAttempts      int        `json:"maxAttempts"`
	LastError        *string    `json:"lastError,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	ProcessedAt      *time.Time `json:"processedAt,omitempty"`
	SourceType       *string    `json:"sourceType,omitempty"`
	SourceID         *string    `json:"sourceId,omitempty"`
	DeliveryStatus   *string    `json:"deliveryStatus,omitempty"`
	DeliveryStatusAt *time.Time `json:"deliveryStatusAt,omitempty"`
}

// ListEmailJobsResponse is the response for listing email jobs.
type ListEmailJobsResponse struct {
	EmailJobs []EmailJob     `json:"emailJobs"`
	Meta      PaginationMeta `json:"meta"`
}

// ListEmailJobsOptions holds query parameters for listing email jobs.
type ListEmailJobsOptions struct {
	Page      int
	Limit     int
	Status    string
	Recipient string
	FromDate  string
	ToDate    string
}

// EmailJobPreviewResponse is the response for previewing an email job.
type EmailJobPreviewResponse struct {
	HTML    string  `json:"html"`
	Subject string  `json:"subject"`
	ToEmail string  `json:"toEmail"`
	ToName  *string `json:"toName,omitempty"`
}

// EmbeddingJob represents an embedding job.
type EmbeddingJob struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	TargetID     string     `json:"targetId"`
	ProjectID    *string    `json:"projectId,omitempty"`
	ProjectName  *string    `json:"projectName,omitempty"`
	Status       string     `json:"status"`
	AttemptCount int        `json:"attemptCount"`
	LastError    *string    `json:"lastError,omitempty"`
	Priority     int        `json:"priority"`
	ScheduledAt  time.Time  `json:"scheduledAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// EmbeddingJobStats contains stats for embedding jobs.
type EmbeddingJobStats struct {
	GraphTotal      int `json:"graphTotal"`
	GraphPending    int `json:"graphPending"`
	GraphCompleted  int `json:"graphCompleted"`
	GraphFailed     int `json:"graphFailed"`
	GraphWithErrors int `json:"graphWithErrors"`
	ChunkTotal      int `json:"chunkTotal"`
	ChunkPending    int `json:"chunkPending"`
	ChunkCompleted  int `json:"chunkCompleted"`
	ChunkFailed     int `json:"chunkFailed"`
	ChunkWithErrors int `json:"chunkWithErrors"`
}

// ListEmbeddingJobsResponse is the response for listing embedding jobs.
type ListEmbeddingJobsResponse struct {
	Jobs  []EmbeddingJob    `json:"jobs"`
	Stats EmbeddingJobStats `json:"stats"`
	Meta  PaginationMeta    `json:"meta"`
}

// ListEmbeddingJobsOptions holds query parameters for listing embedding jobs.
type ListEmbeddingJobsOptions struct {
	Page      int
	Limit     int
	Status    string
	HasError  *bool
	ProjectID string
	Type      string // "graph" or "chunk"
}

// DeleteJobsRequest is the request for bulk delete operations.
type DeleteJobsRequest struct {
	IDs  []string `json:"ids"`
	Type string   `json:"type,omitempty"`
}

// DeleteJobsResponse is the response for bulk delete operations.
type DeleteJobsResponse struct {
	Success      bool   `json:"success"`
	DeletedCount int    `json:"deletedCount"`
	Message      string `json:"message"`
}

// CleanupOrphansResponse is the response for cleanup-orphans.
type CleanupOrphansResponse struct {
	Success      bool   `json:"success"`
	DeletedCount int    `json:"deletedCount"`
	Message      string `json:"message"`
}

// ExtractionJob represents an extraction job.
type ExtractionJob struct {
	ID                   string     `json:"id"`
	ProjectID            string     `json:"projectId"`
	ProjectName          *string    `json:"projectName,omitempty"`
	DocumentID           *string    `json:"documentId,omitempty"`
	DocumentName         *string    `json:"documentName,omitempty"`
	ChunkID              *string    `json:"chunkId,omitempty"`
	JobType              string     `json:"jobType"`
	Status               string     `json:"status"`
	ObjectsCreated       int        `json:"objectsCreated"`
	RelationshipsCreated int        `json:"relationshipsCreated"`
	RetryCount           int        `json:"retryCount"`
	MaxRetries           int        `json:"maxRetries"`
	ErrorMessage         *string    `json:"errorMessage,omitempty"`
	StartedAt            *time.Time `json:"startedAt,omitempty"`
	CompletedAt          *time.Time `json:"completedAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	TotalItems           int        `json:"totalItems"`
	ProcessedItems       int        `json:"processedItems"`
	SuccessfulItems      int        `json:"successfulItems"`
	FailedItems          int        `json:"failedItems"`
}

// ExtractionJobStats contains stats for extraction jobs.
type ExtractionJobStats struct {
	Total                     int `json:"total"`
	Queued                    int `json:"queued"`
	Processing                int `json:"processing"`
	Completed                 int `json:"completed"`
	Failed                    int `json:"failed"`
	Cancelled                 int `json:"cancelled"`
	WithErrors                int `json:"withErrors"`
	TotalObjectsCreated       int `json:"totalObjectsCreated"`
	TotalRelationshipsCreated int `json:"totalRelationshipsCreated"`
}

// ListExtractionJobsResponse is the response for listing extraction jobs.
type ListExtractionJobsResponse struct {
	Jobs  []ExtractionJob    `json:"jobs"`
	Stats ExtractionJobStats `json:"stats"`
	Meta  PaginationMeta     `json:"meta"`
}

// ListExtractionJobsOptions holds query parameters for listing extraction jobs.
type ListExtractionJobsOptions struct {
	Page      int
	Limit     int
	Status    string
	JobType   string
	ProjectID string
	HasError  *bool
}

// CancelJobsRequest is the request for bulk cancel operations.
type CancelJobsRequest struct {
	IDs []string `json:"ids"`
}

// CancelJobsResponse is the response for bulk cancel operations.
type CancelJobsResponse struct {
	Success        bool   `json:"success"`
	CancelledCount int    `json:"cancelledCount"`
	Message        string `json:"message"`
}

// DocumentParsingJob represents a document parsing job.
type DocumentParsingJob struct {
	ID                  string     `json:"id"`
	OrganizationID      string     `json:"organizationId"`
	OrganizationName    *string    `json:"organizationName,omitempty"`
	ProjectID           string     `json:"projectId"`
	ProjectName         *string    `json:"projectName,omitempty"`
	Status              string     `json:"status"`
	SourceType          string     `json:"sourceType"`
	SourceFilename      *string    `json:"sourceFilename,omitempty"`
	MimeType            *string    `json:"mimeType,omitempty"`
	FileSizeBytes       *int64     `json:"fileSizeBytes,omitempty"`
	StorageKey          *string    `json:"storageKey,omitempty"`
	DocumentID          *string    `json:"documentId,omitempty"`
	ExtractionJobID     *string    `json:"extractionJobId,omitempty"`
	ParsedContentLength *int       `json:"parsedContentLength,omitempty"`
	ErrorMessage        *string    `json:"errorMessage,omitempty"`
	RetryCount          int        `json:"retryCount"`
	MaxRetries          int        `json:"maxRetries"`
	NextRetryAt         *time.Time `json:"nextRetryAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	StartedAt           *time.Time `json:"startedAt,omitempty"`
	CompletedAt         *time.Time `json:"completedAt,omitempty"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	Metadata            any        `json:"metadata,omitempty"`
}

// DocumentParsingJobStats contains stats for document parsing jobs.
type DocumentParsingJobStats struct {
	Total              int   `json:"total"`
	Pending            int   `json:"pending"`
	Processing         int   `json:"processing"`
	Completed          int   `json:"completed"`
	Failed             int   `json:"failed"`
	RetryPending       int   `json:"retryPending"`
	WithErrors         int   `json:"withErrors"`
	TotalFileSizeBytes int64 `json:"totalFileSizeBytes"`
}

// ListDocumentParsingJobsResponse is the response for listing document parsing jobs.
type ListDocumentParsingJobsResponse struct {
	Jobs  []DocumentParsingJob    `json:"jobs"`
	Stats DocumentParsingJobStats `json:"stats"`
	Meta  PaginationMeta          `json:"meta"`
}

// ListDocumentParsingJobsOptions holds query parameters for listing document parsing jobs.
type ListDocumentParsingJobsOptions struct {
	Page      int
	Limit     int
	Status    string
	ProjectID string
	HasError  *bool
}

// RetryJobsRequest is the request for bulk retry operations.
type RetryJobsRequest struct {
	IDs []string `json:"ids"`
}

// RetryJobsResponse is the response for bulk retry operations.
type RetryJobsResponse struct {
	Success      bool   `json:"success"`
	RetriedCount int    `json:"retriedCount"`
	Message      string `json:"message"`
}

// SyncJob represents a data source sync job.
type SyncJob struct {
	ID              string     `json:"id"`
	IntegrationID   string     `json:"integrationId"`
	IntegrationName *string    `json:"integrationName,omitempty"`
	ProjectID       string     `json:"projectId"`
	ProjectName     *string    `json:"projectName,omitempty"`
	ProviderType    *string    `json:"providerType,omitempty"`
	Status          string     `json:"status"`
	TotalItems      int        `json:"totalItems"`
	ProcessedItems  int        `json:"processedItems"`
	SuccessfulItems int        `json:"successfulItems"`
	FailedItems     int        `json:"failedItems"`
	SkippedItems    int        `json:"skippedItems"`
	CurrentPhase    *string    `json:"currentPhase,omitempty"`
	StatusMessage   *string    `json:"statusMessage,omitempty"`
	ErrorMessage    *string    `json:"errorMessage,omitempty"`
	TriggerType     string     `json:"triggerType"`
	CreatedAt       time.Time  `json:"createdAt"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

// SyncJobStats contains stats for sync jobs.
type SyncJobStats struct {
	Total              int `json:"total"`
	Pending            int `json:"pending"`
	Running            int `json:"running"`
	Completed          int `json:"completed"`
	Failed             int `json:"failed"`
	Cancelled          int `json:"cancelled"`
	WithErrors         int `json:"withErrors"`
	TotalItemsImported int `json:"totalItemsImported"`
}

// ListSyncJobsResponse is the response for listing sync jobs.
type ListSyncJobsResponse struct {
	Jobs  []SyncJob      `json:"jobs"`
	Stats SyncJobStats   `json:"stats"`
	Meta  PaginationMeta `json:"meta"`
}

// ListSyncJobsOptions holds query parameters for listing sync jobs.
type ListSyncJobsOptions struct {
	Page      int
	Limit     int
	Status    string
	ProjectID string
	HasError  *bool
}

// SyncJobLogsResponse is the response for getting sync job logs.
type SyncJobLogsResponse struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	Logs         any        `json:"logs"`
	ErrorMessage *string    `json:"errorMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

// --- Helper ---

func addPaginationParams(q url.Values, page, limit int) {
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
}

func (c *Client) doGet(ctx context.Context, path string, result any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if err := c.auth.Authenticate(httpReq); err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) doPost(ctx context.Context, path string, reqBody any, result any) error {
	var bodyReader *bytes.Reader
	if reqBody != nil {
		body, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.auth.Authenticate(httpReq); err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) doDelete(ctx context.Context, path string, result any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if err := c.auth.Authenticate(httpReq); err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// --- Methods ---

// GetMe returns whether the authenticated user is a superadmin.
// GET /api/superadmin/me
func (c *Client) GetMe(ctx context.Context) (*SuperadminMeResponse, error) {
	var result SuperadminMeResponse
	if err := c.doGet(ctx, "/api/superadmin/me", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListUsers lists all users with optional filtering.
// GET /api/superadmin/users
func (c *Client) ListUsers(ctx context.Context, opts *ListUsersOptions) (*ListUsersResponse, error) {
	q := url.Values{}
	if opts != nil {
		addPaginationParams(q, opts.Page, opts.Limit)
		if opts.Search != "" {
			q.Set("search", opts.Search)
		}
		if opts.OrgID != "" {
			q.Set("orgId", opts.OrgID)
		}
	}

	path := "/api/superadmin/users"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListUsersResponse
	if err := c.doGet(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteUser soft-deletes a user.
// DELETE /api/superadmin/users/:id
func (c *Client) DeleteUser(ctx context.Context, userID string) (*SuccessResponse, error) {
	var result SuccessResponse
	if err := c.doDelete(ctx, "/api/superadmin/users/"+userID, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListOrganizations lists all organizations.
// GET /api/superadmin/organizations
func (c *Client) ListOrganizations(ctx context.Context, opts *PaginationOptions) (*ListOrganizationsResponse, error) {
	q := url.Values{}
	if opts != nil {
		addPaginationParams(q, opts.Page, opts.Limit)
	}

	path := "/api/superadmin/organizations"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListOrganizationsResponse
	if err := c.doGet(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteOrganization soft-deletes an organization.
// DELETE /api/superadmin/organizations/:id
func (c *Client) DeleteOrganization(ctx context.Context, orgID string) (*SuccessResponse, error) {
	var result SuccessResponse
	if err := c.doDelete(ctx, "/api/superadmin/organizations/"+orgID, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListProjects lists all projects with optional filtering.
// GET /api/superadmin/projects
func (c *Client) ListProjects(ctx context.Context, opts *ListProjectsOptions) (*ListProjectsResponse, error) {
	q := url.Values{}
	if opts != nil {
		addPaginationParams(q, opts.Page, opts.Limit)
		if opts.OrgID != "" {
			q.Set("orgId", opts.OrgID)
		}
	}

	path := "/api/superadmin/projects"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListProjectsResponse
	if err := c.doGet(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteProject soft-deletes a project.
// DELETE /api/superadmin/projects/:id
func (c *Client) DeleteProject(ctx context.Context, projectID string) (*SuccessResponse, error) {
	var result SuccessResponse
	if err := c.doDelete(ctx, "/api/superadmin/projects/"+projectID, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListEmailJobs lists email jobs with optional filtering.
// GET /api/superadmin/email-jobs
func (c *Client) ListEmailJobs(ctx context.Context, opts *ListEmailJobsOptions) (*ListEmailJobsResponse, error) {
	q := url.Values{}
	if opts != nil {
		addPaginationParams(q, opts.Page, opts.Limit)
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.Recipient != "" {
			q.Set("recipient", opts.Recipient)
		}
		if opts.FromDate != "" {
			q.Set("fromDate", opts.FromDate)
		}
		if opts.ToDate != "" {
			q.Set("toDate", opts.ToDate)
		}
	}

	path := "/api/superadmin/email-jobs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListEmailJobsResponse
	if err := c.doGet(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetEmailJobPreview gets the preview of an email job.
// GET /api/superadmin/email-jobs/:id/preview-json
func (c *Client) GetEmailJobPreview(ctx context.Context, jobID string) (*EmailJobPreviewResponse, error) {
	var result EmailJobPreviewResponse
	if err := c.doGet(ctx, "/api/superadmin/email-jobs/"+jobID+"/preview-json", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListEmbeddingJobs lists embedding jobs with optional filtering.
// GET /api/superadmin/embedding-jobs
func (c *Client) ListEmbeddingJobs(ctx context.Context, opts *ListEmbeddingJobsOptions) (*ListEmbeddingJobsResponse, error) {
	q := url.Values{}
	if opts != nil {
		addPaginationParams(q, opts.Page, opts.Limit)
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.HasError != nil {
			q.Set("hasError", strconv.FormatBool(*opts.HasError))
		}
		if opts.ProjectID != "" {
			q.Set("projectId", opts.ProjectID)
		}
		if opts.Type != "" {
			q.Set("type", opts.Type)
		}
	}

	path := "/api/superadmin/embedding-jobs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListEmbeddingJobsResponse
	if err := c.doGet(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteEmbeddingJobs bulk deletes embedding jobs.
// POST /api/superadmin/embedding-jobs/delete
func (c *Client) DeleteEmbeddingJobs(ctx context.Context, req *DeleteJobsRequest) (*DeleteJobsResponse, error) {
	var result DeleteJobsResponse
	if err := c.doPost(ctx, "/api/superadmin/embedding-jobs/delete", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CleanupOrphanEmbeddingJobs removes orphan embedding jobs.
// POST /api/superadmin/embedding-jobs/cleanup-orphans
func (c *Client) CleanupOrphanEmbeddingJobs(ctx context.Context) (*CleanupOrphansResponse, error) {
	var result CleanupOrphansResponse
	if err := c.doPost(ctx, "/api/superadmin/embedding-jobs/cleanup-orphans", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListExtractionJobs lists extraction jobs with optional filtering.
// GET /api/superadmin/extraction-jobs
func (c *Client) ListExtractionJobs(ctx context.Context, opts *ListExtractionJobsOptions) (*ListExtractionJobsResponse, error) {
	q := url.Values{}
	if opts != nil {
		addPaginationParams(q, opts.Page, opts.Limit)
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.JobType != "" {
			q.Set("jobType", opts.JobType)
		}
		if opts.ProjectID != "" {
			q.Set("projectId", opts.ProjectID)
		}
		if opts.HasError != nil {
			q.Set("hasError", strconv.FormatBool(*opts.HasError))
		}
	}

	path := "/api/superadmin/extraction-jobs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListExtractionJobsResponse
	if err := c.doGet(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteExtractionJobs bulk deletes extraction jobs.
// POST /api/superadmin/extraction-jobs/delete
func (c *Client) DeleteExtractionJobs(ctx context.Context, req *DeleteJobsRequest) (*DeleteJobsResponse, error) {
	var result DeleteJobsResponse
	if err := c.doPost(ctx, "/api/superadmin/extraction-jobs/delete", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelExtractionJobs bulk cancels extraction jobs.
// POST /api/superadmin/extraction-jobs/cancel
func (c *Client) CancelExtractionJobs(ctx context.Context, req *CancelJobsRequest) (*CancelJobsResponse, error) {
	var result CancelJobsResponse
	if err := c.doPost(ctx, "/api/superadmin/extraction-jobs/cancel", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListDocumentParsingJobs lists document parsing jobs with optional filtering.
// GET /api/superadmin/document-parsing-jobs
func (c *Client) ListDocumentParsingJobs(ctx context.Context, opts *ListDocumentParsingJobsOptions) (*ListDocumentParsingJobsResponse, error) {
	q := url.Values{}
	if opts != nil {
		addPaginationParams(q, opts.Page, opts.Limit)
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.ProjectID != "" {
			q.Set("projectId", opts.ProjectID)
		}
		if opts.HasError != nil {
			q.Set("hasError", strconv.FormatBool(*opts.HasError))
		}
	}

	path := "/api/superadmin/document-parsing-jobs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListDocumentParsingJobsResponse
	if err := c.doGet(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteDocumentParsingJobs bulk deletes document parsing jobs.
// POST /api/superadmin/document-parsing-jobs/delete
func (c *Client) DeleteDocumentParsingJobs(ctx context.Context, req *DeleteJobsRequest) (*DeleteJobsResponse, error) {
	var result DeleteJobsResponse
	if err := c.doPost(ctx, "/api/superadmin/document-parsing-jobs/delete", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RetryDocumentParsingJobs re-queues failed document parsing jobs for retry.
// POST /api/superadmin/document-parsing-jobs/retry
func (c *Client) RetryDocumentParsingJobs(ctx context.Context, req *RetryJobsRequest) (*RetryJobsResponse, error) {
	var result RetryJobsResponse
	if err := c.doPost(ctx, "/api/superadmin/document-parsing-jobs/retry", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListSyncJobs lists data source sync jobs with optional filtering.
// GET /api/superadmin/sync-jobs
func (c *Client) ListSyncJobs(ctx context.Context, opts *ListSyncJobsOptions) (*ListSyncJobsResponse, error) {
	q := url.Values{}
	if opts != nil {
		addPaginationParams(q, opts.Page, opts.Limit)
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.ProjectID != "" {
			q.Set("projectId", opts.ProjectID)
		}
		if opts.HasError != nil {
			q.Set("hasError", strconv.FormatBool(*opts.HasError))
		}
	}

	path := "/api/superadmin/sync-jobs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListSyncJobsResponse
	if err := c.doGet(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSyncJobLogs gets the logs for a specific sync job.
// GET /api/superadmin/sync-jobs/:id/logs
func (c *Client) GetSyncJobLogs(ctx context.Context, jobID string) (*SyncJobLogsResponse, error) {
	var result SyncJobLogsResponse
	if err := c.doGet(ctx, "/api/superadmin/sync-jobs/"+jobID+"/logs", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteSyncJobs bulk deletes sync jobs.
// POST /api/superadmin/sync-jobs/delete
func (c *Client) DeleteSyncJobs(ctx context.Context, req *DeleteJobsRequest) (*DeleteJobsResponse, error) {
	var result DeleteJobsResponse
	if err := c.doPost(ctx, "/api/superadmin/sync-jobs/delete", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelSyncJobs bulk cancels sync jobs.
// POST /api/superadmin/sync-jobs/cancel
func (c *Client) CancelSyncJobs(ctx context.Context, req *CancelJobsRequest) (*CancelJobsResponse, error) {
	var result CancelJobsResponse
	if err := c.doPost(ctx, "/api/superadmin/sync-jobs/cancel", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
