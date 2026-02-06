package superadmin

import (
	"time"

	"github.com/uptrace/bun"
)

// Superadmin represents the core.superadmins table
type Superadmin struct {
	bun.BaseModel `bun:"table:core.superadmins,alias:sa"`

	UserID    string     `bun:"user_id,pk,type:uuid" json:"userId"`
	GrantedBy *string    `bun:"granted_by,type:uuid" json:"grantedBy,omitempty"`
	GrantedAt time.Time  `bun:"granted_at,notnull,default:now()" json:"grantedAt"`
	RevokedAt *time.Time `bun:"revoked_at" json:"revokedAt,omitempty"`
	RevokedBy *string    `bun:"revoked_by,type:uuid" json:"revokedBy,omitempty"`
	Notes     *string    `bun:"notes" json:"notes,omitempty"`
}

// IsActive returns true if the superadmin grant is currently active
func (s *Superadmin) IsActive() bool {
	return s.RevokedAt == nil
}

// UserProfile for superadmin queries (core.user_profiles)
type UserProfile struct {
	bun.BaseModel `bun:"table:core.user_profiles,alias:up"`

	ID             string     `bun:"id,pk,type:uuid" json:"id"`
	ZitadelUserID  string     `bun:"zitadel_user_id" json:"zitadelUserId"`
	FirstName      *string    `bun:"first_name" json:"firstName,omitempty"`
	LastName       *string    `bun:"last_name" json:"lastName,omitempty"`
	DisplayName    *string    `bun:"display_name" json:"displayName,omitempty"`
	LastActivityAt *time.Time `bun:"last_activity_at" json:"lastActivityAt,omitempty"`
	CreatedAt      time.Time  `bun:"created_at" json:"createdAt"`
	UpdatedAt      time.Time  `bun:"updated_at" json:"updatedAt"`
	DeletedAt      *time.Time `bun:"deleted_at" json:"deletedAt,omitempty"`
	DeletedBy      *string    `bun:"deleted_by,type:uuid" json:"deletedBy,omitempty"`

	// Relations
	Emails []UserEmail `bun:"rel:has-many,join:id=user_id" json:"emails,omitempty"`
}

// UserEmail for superadmin queries (core.user_emails)
type UserEmail struct {
	bun.BaseModel `bun:"table:core.user_emails,alias:ue"`

	ID        string    `bun:"id,pk,type:uuid" json:"id"`
	UserID    string    `bun:"user_id,type:uuid" json:"userId"`
	Email     string    `bun:"email" json:"email"`
	Verified  bool      `bun:"verified" json:"verified"`
	CreatedAt time.Time `bun:"created_at" json:"createdAt"`
}

// Org for superadmin queries (kb.orgs)
type Org struct {
	bun.BaseModel `bun:"table:kb.orgs,alias:o"`

	ID        string     `bun:"id,pk,type:uuid" json:"id"`
	Name      string     `bun:"name" json:"name"`
	CreatedAt time.Time  `bun:"created_at" json:"createdAt"`
	DeletedAt *time.Time `bun:"deleted_at" json:"deletedAt,omitempty"`
	DeletedBy *string    `bun:"deleted_by,type:uuid" json:"deletedBy,omitempty"`
}

// Project for superadmin queries (kb.projects)
type Project struct {
	bun.BaseModel `bun:"table:kb.projects,alias:p"`

	ID             string     `bun:"id,pk,type:uuid" json:"id"`
	Name           string     `bun:"name" json:"name"`
	OrganizationID string     `bun:"organization_id,type:uuid" json:"organizationId"`
	CreatedAt      time.Time  `bun:"created_at" json:"createdAt"`
	DeletedAt      *time.Time `bun:"deleted_at" json:"deletedAt,omitempty"`
	DeletedBy      *string    `bun:"deleted_by,type:uuid" json:"deletedBy,omitempty"`

	// Relations
	Organization *Org `bun:"rel:belongs-to,join:organization_id=id" json:"organization,omitempty"`
}

// OrganizationMembership for superadmin queries
type OrganizationMembership struct {
	bun.BaseModel `bun:"table:kb.organization_memberships,alias:om"`

	ID             string    `bun:"id,pk,type:uuid" json:"id"`
	OrganizationID string    `bun:"organization_id,type:uuid" json:"organizationId"`
	UserID         string    `bun:"user_id,type:uuid" json:"userId"`
	Role           string    `bun:"role" json:"role"`
	CreatedAt      time.Time `bun:"created_at" json:"createdAt"`

	// Relations
	Organization *Org `bun:"rel:belongs-to,join:organization_id=id" json:"organization,omitempty"`
}

// EmailJob for superadmin queries (kb.email_jobs)
type EmailJob struct {
	bun.BaseModel `bun:"table:kb.email_jobs,alias:ej"`

	ID               string     `bun:"id,pk,type:uuid" json:"id"`
	TemplateName     string     `bun:"template_name" json:"templateName"`
	ToEmail          string     `bun:"to_email" json:"toEmail"`
	ToName           *string    `bun:"to_name" json:"toName,omitempty"`
	Subject          string     `bun:"subject" json:"subject"`
	TemplateData     any        `bun:"template_data,type:jsonb" json:"templateData,omitempty"`
	Status           string     `bun:"status" json:"status"`
	Attempts         int        `bun:"attempts" json:"attempts"`
	MaxAttempts      int        `bun:"max_attempts" json:"maxAttempts"`
	LastError        *string    `bun:"last_error" json:"lastError,omitempty"`
	ProcessedAt      *time.Time `bun:"processed_at" json:"processedAt,omitempty"`
	SourceType       *string    `bun:"source_type" json:"sourceType,omitempty"`
	SourceID         *string    `bun:"source_id" json:"sourceId,omitempty"`
	DeliveryStatus   *string    `bun:"delivery_status" json:"deliveryStatus,omitempty"`
	DeliveryStatusAt *time.Time `bun:"delivery_status_at" json:"deliveryStatusAt,omitempty"`
	CreatedAt        time.Time  `bun:"created_at" json:"createdAt"`
	UpdatedAt        time.Time  `bun:"updated_at" json:"updatedAt"`
}

// GraphEmbeddingJob for superadmin queries (kb.graph_embedding_jobs)
type GraphEmbeddingJob struct {
	bun.BaseModel `bun:"table:kb.graph_embedding_jobs,alias:gej"`

	ID           string     `bun:"id,pk,type:uuid" json:"id"`
	ObjectID     string     `bun:"object_id,type:uuid" json:"objectId"`
	Status       string     `bun:"status" json:"status"`
	AttemptCount int        `bun:"attempt_count" json:"attemptCount"`
	LastError    *string    `bun:"last_error" json:"lastError,omitempty"`
	Priority     int        `bun:"priority" json:"priority"`
	ScheduledAt  time.Time  `bun:"scheduled_at" json:"scheduledAt"`
	StartedAt    *time.Time `bun:"started_at" json:"startedAt,omitempty"`
	CompletedAt  *time.Time `bun:"completed_at" json:"completedAt,omitempty"`
	CreatedAt    time.Time  `bun:"created_at" json:"createdAt"`
	UpdatedAt    time.Time  `bun:"updated_at" json:"updatedAt"`
}

// ChunkEmbeddingJob for superadmin queries (kb.chunk_embedding_jobs)
type ChunkEmbeddingJob struct {
	bun.BaseModel `bun:"table:kb.chunk_embedding_jobs,alias:cej"`

	ID           string     `bun:"id,pk,type:uuid" json:"id"`
	ChunkID      string     `bun:"chunk_id,type:uuid" json:"chunkId"`
	Status       string     `bun:"status" json:"status"`
	AttemptCount int        `bun:"attempt_count" json:"attemptCount"`
	LastError    *string    `bun:"last_error" json:"lastError,omitempty"`
	Priority     int        `bun:"priority" json:"priority"`
	ScheduledAt  time.Time  `bun:"scheduled_at" json:"scheduledAt"`
	StartedAt    *time.Time `bun:"started_at" json:"startedAt,omitempty"`
	CompletedAt  *time.Time `bun:"completed_at" json:"completedAt,omitempty"`
	CreatedAt    time.Time  `bun:"created_at" json:"createdAt"`
	UpdatedAt    time.Time  `bun:"updated_at" json:"updatedAt"`
}

// ObjectExtractionJob for superadmin queries (kb.object_extraction_jobs)
type ObjectExtractionJob struct {
	bun.BaseModel `bun:"table:kb.object_extraction_jobs,alias:oej"`

	ID                    string     `bun:"id,pk,type:uuid" json:"id"`
	ProjectID             string     `bun:"project_id,type:uuid" json:"projectId"`
	DocumentID            *string    `bun:"document_id,type:uuid" json:"documentId,omitempty"`
	ChunkID               *string    `bun:"chunk_id,type:uuid" json:"chunkId,omitempty"`
	JobType               string     `bun:"job_type" json:"jobType"`
	Status                string     `bun:"status" json:"status"`
	ObjectsCreated        int        `bun:"objects_created" json:"objectsCreated"`
	RelationshipsCreated  int        `bun:"relationships_created" json:"relationshipsCreated"`
	RetryCount            int        `bun:"retry_count" json:"retryCount"`
	MaxRetries            int        `bun:"max_retries" json:"maxRetries"`
	ErrorMessage          *string    `bun:"error_message" json:"errorMessage,omitempty"`
	StartedAt             *time.Time `bun:"started_at" json:"startedAt,omitempty"`
	CompletedAt           *time.Time `bun:"completed_at" json:"completedAt,omitempty"`
	SourceType            *string    `bun:"source_type" json:"sourceType,omitempty"`
	SourceID              *string    `bun:"source_id" json:"sourceId,omitempty"`
	SourceMetadata        any        `bun:"source_metadata,type:jsonb" json:"sourceMetadata,omitempty"`
	TotalItems            int        `bun:"total_items" json:"totalItems"`
	ProcessedItems        int        `bun:"processed_items" json:"processedItems"`
	SuccessfulItems       int        `bun:"successful_items" json:"successfulItems"`
	FailedItems           int        `bun:"failed_items" json:"failedItems"`
	CreatedObjectsDetails any        `bun:"created_objects,type:jsonb" json:"createdObjectsDetails,omitempty"`
	CreatedAt             time.Time  `bun:"created_at" json:"createdAt"`
	UpdatedAt             time.Time  `bun:"updated_at" json:"updatedAt"`

	// Relations
	Project *Project `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
}

// DocumentParsingJob for superadmin queries (kb.document_parsing_jobs)
type DocumentParsingJob struct {
	bun.BaseModel `bun:"table:kb.document_parsing_jobs,alias:dpj"`

	ID              string     `bun:"id,pk,type:uuid" json:"id"`
	OrganizationID  string     `bun:"organization_id,type:uuid" json:"organizationId"`
	ProjectID       string     `bun:"project_id,type:uuid" json:"projectId"`
	Status          string     `bun:"status" json:"status"`
	SourceType      string     `bun:"source_type" json:"sourceType"`
	SourceFilename  *string    `bun:"source_filename" json:"sourceFilename,omitempty"`
	MimeType        *string    `bun:"mime_type" json:"mimeType,omitempty"`
	FileSizeBytes   *int64     `bun:"file_size_bytes" json:"fileSizeBytes,omitempty"`
	StorageKey      *string    `bun:"storage_key" json:"storageKey,omitempty"`
	DocumentID      *string    `bun:"document_id,type:uuid" json:"documentId,omitempty"`
	ExtractionJobID *string    `bun:"extraction_job_id,type:uuid" json:"extractionJobId,omitempty"`
	ParsedContent   *string    `bun:"parsed_content" json:"-"` // Don't include full content in list
	ErrorMessage    *string    `bun:"error_message" json:"errorMessage,omitempty"`
	RetryCount      int        `bun:"retry_count" json:"retryCount"`
	MaxRetries      int        `bun:"max_retries" json:"maxRetries"`
	NextRetryAt     *time.Time `bun:"next_retry_at" json:"nextRetryAt,omitempty"`
	Metadata        any        `bun:"metadata,type:jsonb" json:"metadata,omitempty"`
	CreatedAt       time.Time  `bun:"created_at" json:"createdAt"`
	StartedAt       *time.Time `bun:"started_at" json:"startedAt,omitempty"`
	CompletedAt     *time.Time `bun:"completed_at" json:"completedAt,omitempty"`
	UpdatedAt       time.Time  `bun:"updated_at" json:"updatedAt"`

	// Relations
	Project      *Project `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
	Organization *Org     `bun:"rel:belongs-to,join:organization_id=id" json:"organization,omitempty"`
}

// DataSourceSyncJob for superadmin queries (kb.data_source_sync_jobs)
type DataSourceSyncJob struct {
	bun.BaseModel `bun:"table:kb.data_source_sync_jobs,alias:dssj"`

	ID              string     `bun:"id,pk,type:uuid" json:"id"`
	IntegrationID   string     `bun:"integration_id,type:uuid" json:"integrationId"`
	ProjectID       string     `bun:"project_id,type:uuid" json:"projectId"`
	Status          string     `bun:"status" json:"status"`
	TotalItems      int        `bun:"total_items" json:"totalItems"`
	ProcessedItems  int        `bun:"processed_items" json:"processedItems"`
	SuccessfulItems int        `bun:"successful_items" json:"successfulItems"`
	FailedItems     int        `bun:"failed_items" json:"failedItems"`
	SkippedItems    int        `bun:"skipped_items" json:"skippedItems"`
	CurrentPhase    *string    `bun:"current_phase" json:"currentPhase,omitempty"`
	StatusMessage   *string    `bun:"status_message" json:"statusMessage,omitempty"`
	ErrorMessage    *string    `bun:"error_message" json:"errorMessage,omitempty"`
	TriggerType     string     `bun:"trigger_type" json:"triggerType"`
	Logs            any        `bun:"logs,type:jsonb" json:"logs,omitempty"`
	CreatedAt       time.Time  `bun:"created_at" json:"createdAt"`
	StartedAt       *time.Time `bun:"started_at" json:"startedAt,omitempty"`
	CompletedAt     *time.Time `bun:"completed_at" json:"completedAt,omitempty"`

	// Relations
	Project     *Project               `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
	Integration *DataSourceIntegration `bun:"rel:belongs-to,join:integration_id=id" json:"integration,omitempty"`
}

// DataSourceIntegration for join purposes
type DataSourceIntegration struct {
	bun.BaseModel `bun:"table:kb.data_source_integrations,alias:dsi"`

	ID           string `bun:"id,pk,type:uuid" json:"id"`
	Name         string `bun:"name" json:"name"`
	ProviderType string `bun:"provider_type" json:"providerType"`
}
