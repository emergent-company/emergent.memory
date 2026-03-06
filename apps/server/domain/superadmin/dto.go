package superadmin

import "time"

// PaginationMeta contains pagination metadata
type PaginationMeta struct {
	Page       int  `json:"page"`
	Limit      int  `json:"limit"`
	Total      int  `json:"total"`
	TotalPages int  `json:"totalPages"`
	HasNext    bool `json:"hasNext"`
	HasPrev    bool `json:"hasPrev"`
}

// NewPaginationMeta creates pagination metadata
func NewPaginationMeta(page, limit, total int) PaginationMeta {
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}
	return PaginationMeta{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
		HasPrev:    page > 1,
	}
}

// SuperadminMeResponse is the response for GET /api/superadmin/me
type SuperadminMeResponse struct {
	IsSuperadmin bool `json:"isSuperadmin"`
}

// UserOrgMembershipDTO represents a user's org membership
type UserOrgMembershipDTO struct {
	OrgID    string    `json:"orgId"`
	OrgName  string    `json:"orgName"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

// SuperadminUserDTO represents a user in the superadmin list
type SuperadminUserDTO struct {
	ID             string                 `json:"id"`
	ZitadelUserID  string                 `json:"zitadelUserId"`
	FirstName      *string                `json:"firstName,omitempty"`
	LastName       *string                `json:"lastName,omitempty"`
	DisplayName    *string                `json:"displayName,omitempty"`
	PrimaryEmail   *string                `json:"primaryEmail,omitempty"`
	LastActivityAt *time.Time             `json:"lastActivityAt,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
	Organizations  []UserOrgMembershipDTO `json:"organizations"`
}

// ListUsersResponse is the response for GET /api/superadmin/users
type ListUsersResponse struct {
	Users []SuperadminUserDTO `json:"users"`
	Meta  PaginationMeta      `json:"meta"`
}

// SuperadminOrgDTO represents an org in the superadmin list
type SuperadminOrgDTO struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	MemberCount  int        `json:"memberCount"`
	ProjectCount int        `json:"projectCount"`
	CreatedAt    time.Time  `json:"createdAt"`
	DeletedAt    *time.Time `json:"deletedAt,omitempty"`
}

// ListOrganizationsResponse is the response for GET /api/superadmin/organizations
type ListOrganizationsResponse struct {
	Organizations []SuperadminOrgDTO `json:"organizations"`
	Meta          PaginationMeta     `json:"meta"`
}

// SuperadminProjectDTO represents a project in the superadmin list
type SuperadminProjectDTO struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	OrganizationID   string     `json:"organizationId"`
	OrganizationName string     `json:"organizationName"`
	DocumentCount    int        `json:"documentCount"`
	CreatedAt        time.Time  `json:"createdAt"`
	DeletedAt        *time.Time `json:"deletedAt,omitempty"`
}

// ListProjectsResponse is the response for GET /api/superadmin/projects
type ListProjectsResponse struct {
	Projects []SuperadminProjectDTO `json:"projects"`
	Meta     PaginationMeta         `json:"meta"`
}

// SuperadminEmailJobDTO represents an email job in the superadmin list
type SuperadminEmailJobDTO struct {
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

// ListEmailJobsResponse is the response for GET /api/superadmin/email-jobs
type ListEmailJobsResponse struct {
	EmailJobs []SuperadminEmailJobDTO `json:"emailJobs"`
	Meta      PaginationMeta          `json:"meta"`
}

// EmailJobPreviewResponse is the response for GET /api/superadmin/email-jobs/:id/preview-json
type EmailJobPreviewResponse struct {
	HTML    string  `json:"html"`
	Subject string  `json:"subject"`
	ToEmail string  `json:"toEmail"`
	ToName  *string `json:"toName,omitempty"`
}

// EmbeddingJobDTO represents an embedding job (graph or chunk)
type EmbeddingJobDTO struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"` // "graph" or "chunk"
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

// EmbeddingJobStatsDTO contains stats for embedding jobs
type EmbeddingJobStatsDTO struct {
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

// ListEmbeddingJobsResponse is the response for GET /api/superadmin/embedding-jobs
type ListEmbeddingJobsResponse struct {
	Jobs  []EmbeddingJobDTO    `json:"jobs"`
	Stats EmbeddingJobStatsDTO `json:"stats"`
	Meta  PaginationMeta       `json:"meta"`
}

// DeleteJobsRequest is the request body for bulk delete operations
type DeleteJobsRequest struct {
	IDs  []string `json:"ids" validate:"required,min=1"`
	Type string   `json:"type,omitempty"` // For embedding jobs: "graph" or "chunk"
}

// DeleteJobsResponse is the response for bulk delete operations
type DeleteJobsResponse struct {
	Success      bool   `json:"success"`
	DeletedCount int    `json:"deletedCount"`
	Message      string `json:"message"`
}

// CleanupOrphansResponse is the response for cleanup-orphans
type CleanupOrphansResponse struct {
	Success      bool   `json:"success"`
	DeletedCount int    `json:"deletedCount"`
	Message      string `json:"message"`
}

// ExtractionJobDTO represents an extraction job
type ExtractionJobDTO struct {
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

// ExtractionJobStatsDTO contains stats for extraction jobs
type ExtractionJobStatsDTO struct {
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

// ListExtractionJobsResponse is the response for GET /api/superadmin/extraction-jobs
type ListExtractionJobsResponse struct {
	Jobs  []ExtractionJobDTO    `json:"jobs"`
	Stats ExtractionJobStatsDTO `json:"stats"`
	Meta  PaginationMeta        `json:"meta"`
}

// CancelJobsRequest is the request body for bulk cancel operations
type CancelJobsRequest struct {
	IDs []string `json:"ids" validate:"required,min=1"`
}

// CancelJobsResponse is the response for bulk cancel operations
type CancelJobsResponse struct {
	Success        bool   `json:"success"`
	CancelledCount int    `json:"cancelledCount"`
	Message        string `json:"message"`
}

// DocumentParsingJobDTO represents a document parsing job
type DocumentParsingJobDTO struct {
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

// DocumentParsingJobStatsDTO contains stats for document parsing jobs
type DocumentParsingJobStatsDTO struct {
	Total              int   `json:"total"`
	Pending            int   `json:"pending"`
	Processing         int   `json:"processing"`
	Completed          int   `json:"completed"`
	Failed             int   `json:"failed"`
	RetryPending       int   `json:"retryPending"`
	WithErrors         int   `json:"withErrors"`
	TotalFileSizeBytes int64 `json:"totalFileSizeBytes"`
}

// ListDocumentParsingJobsResponse is the response for GET /api/superadmin/document-parsing-jobs
type ListDocumentParsingJobsResponse struct {
	Jobs  []DocumentParsingJobDTO    `json:"jobs"`
	Stats DocumentParsingJobStatsDTO `json:"stats"`
	Meta  PaginationMeta             `json:"meta"`
}

// RetryJobsRequest is the request body for bulk retry operations
type RetryJobsRequest struct {
	IDs []string `json:"ids" validate:"required,min=1"`
}

// RetryJobsResponse is the response for bulk retry operations
type RetryJobsResponse struct {
	Success      bool   `json:"success"`
	RetriedCount int    `json:"retriedCount"`
	Message      string `json:"message"`
}

// SyncJobDTO represents a data source sync job
type SyncJobDTO struct {
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

// SyncJobStatsDTO contains stats for sync jobs
type SyncJobStatsDTO struct {
	Total              int `json:"total"`
	Pending            int `json:"pending"`
	Running            int `json:"running"`
	Completed          int `json:"completed"`
	Failed             int `json:"failed"`
	Cancelled          int `json:"cancelled"`
	WithErrors         int `json:"withErrors"`
	TotalItemsImported int `json:"totalItemsImported"`
}

// ListSyncJobsResponse is the response for GET /api/superadmin/sync-jobs
type ListSyncJobsResponse struct {
	Jobs  []SyncJobDTO    `json:"jobs"`
	Stats SyncJobStatsDTO `json:"stats"`
	Meta  PaginationMeta  `json:"meta"`
}

// SyncJobLogsResponse is the response for GET /api/superadmin/sync-jobs/:id/logs
type SyncJobLogsResponse struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	Logs         any        `json:"logs"`
	ErrorMessage *string    `json:"errorMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

// SuccessResponse is a generic success response
type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
