// Package sources implements the data-source-integrations domain: a durable,
// project-scoped source model (kb.sources) plus the provider catalog, config
// schema, connection testing, and CRUD REST surface that backs
// /api/data-source-integrations/* (matching pkg/sdk/datasources/client.go).
//
// Credentials (OAuth2 client id/secret + refresh token) are encrypted at rest
// via pkg/crypto, following the provider.EncryptedCredential pattern, and are
// never stored in the plaintext config or returned in any response.
package sources

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Sync job lifecycle states (kb.source_sync_jobs.status). `pending` and
// `processing` are the active states guarded by the partial unique admission
// index (design D6); the rest are terminal.
const (
	SyncStatusPending    = "pending"
	SyncStatusProcessing = "processing"
	SyncStatusCompleted  = "completed"
	SyncStatusFailed     = "failed"
	SyncStatusCancelled  = "cancelled"
)

// Sync trigger origins (kb.source_sync_jobs.trigger_type).
const (
	TriggerTypeManual    = "manual"
	TriggerTypeScheduled = "scheduled"
)

// Source lifecycle status values (kb.sources.status). A source is only admitted
// for scheduled sync while `active`; `disabled` is the operator kill switch.
const (
	SourceStatusActive   = "active"
	SourceStatusDisabled = "disabled"
)

// Provider type identifiers.
const (
	// ProviderTypeGoogleDrive syncs documents from a Google Drive folder.
	ProviderTypeGoogleDrive = "google_drive"

	// ProviderTypeGitHub syncs documentation from a GitHub repository. The
	// provider is a readiness stub in this iteration: catalog, config schema,
	// credential handling, and OAuth are wired, while enumeration/sync ship in a
	// follow-up change (§7.3).
	ProviderTypeGitHub = "github"

	// SourceTypeDrive is the source-type category advertised for the Google
	// Drive provider and stamped on the SDK Integration.sourceType field.
	SourceTypeDrive = "drive"

	// SourceTypeRepo is the source-type category advertised for the GitHub
	// provider (and future repository-backed providers).
	SourceTypeRepo = "repo"
)

// Source is the kb.sources row model. EncryptedCredentials is deliberately
// `json:"-"` so it can never be serialized into a response even if a store
// query returns it.
type Source struct {
	bun.BaseModel `bun:"table:kb.sources,alias:s"`

	ID                   uuid.UUID      `bun:"id,pk,type:uuid" json:"id"`
	ProjectID            uuid.UUID      `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Provider             string         `bun:"provider,notnull" json:"provider"`
	DisplayName          string         `bun:"display_name" json:"displayName,omitempty"`
	Config               map[string]any `bun:"config,type:jsonb,notnull" json:"config"`
	EncryptedCredentials []byte         `bun:"encrypted_credentials,type:bytea" json:"-"`
	SyncPolicy           string         `bun:"sync_policy,notnull" json:"syncPolicy"`
	SyncIntervalMinutes  *int           `bun:"sync_interval_minutes" json:"syncIntervalMinutes,omitempty"`
	Cursor               map[string]any `bun:"cursor,type:jsonb,notnull" json:"cursor"`
	Status               string         `bun:"status,notnull" json:"status"`
	ErrorCount           int            `bun:"error_count,notnull" json:"errorCount"`
	LastError            *string        `bun:"last_error" json:"lastError,omitempty"`
	LastErrorAt          *time.Time     `bun:"last_error_at" json:"lastErrorAt,omitempty"`
	LastSyncedAt         *time.Time     `bun:"last_synced_at" json:"lastSyncedAt,omitempty"`
	CreatedAt            time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt            time.Time      `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// Provider mirrors pkg/sdk/datasources.Provider — the provider-catalog entry.
type Provider struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SourceType  string `json:"sourceType"`
	// Vendor is the stable vendor/app identity behind the provider (e.g.
	// "google", "github"). An OAuth app configured for a provider is a
	// vendor-level app: the same Google Cloud client can later back several
	// Google-backed providers, so the credentials UI labels it by VendorName
	// and shows the data source (Name) separately.
	Vendor      string   `json:"vendor"`
	VendorName  string   `json:"vendorName"`
	IconURL     string   `json:"iconUrl,omitempty"`
	Available   bool     `json:"available"`
	AuthMethods []string `json:"authMethods,omitempty"`
	// Strategies advertises the org-selectable OAuth scope strategies (design
	// D6); empty when the provider's scopes are fixed.
	Strategies []string `json:"strategies,omitempty"`
}

// ProviderSchema mirrors pkg/sdk/datasources.ProviderSchema.
type ProviderSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

// Integration mirrors pkg/sdk/datasources.Integration — the read/write shape of
// a data-source integration. It carries no credential material.
type Integration struct {
	ID                  string     `json:"id"`
	ProjectID           string     `json:"projectId"`
	Name                string     `json:"name"`
	Description         *string    `json:"description,omitempty"`
	ProviderType        string     `json:"providerType"`
	SourceType          string     `json:"sourceType"`
	SyncMode            string     `json:"syncMode"`
	SyncIntervalMinutes *int       `json:"syncIntervalMinutes,omitempty"`
	LastSyncedAt        *time.Time `json:"lastSyncedAt,omitempty"`
	NextSyncAt          *time.Time `json:"nextSyncAt,omitempty"`
	Status              string     `json:"status"`
	ErrorMessage        *string    `json:"errorMessage,omitempty"`
	ErrorCount          int        `json:"errorCount"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

// SourceType mirrors pkg/sdk/datasources.SourceType — document counts grouped
// by source type.
type SourceType struct {
	SourceType    string `json:"sourceType"`
	DocumentCount int    `json:"documentCount"`
}

// TestConnectionResponse mirrors pkg/sdk/datasources.TestConnectionResponse.
type TestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// CreateIntegrationRequest mirrors pkg/sdk/datasources.CreateIntegrationRequest.
// Credentials are carried inside Config (client_id/client_secret/refresh_token)
// and are stripped + encrypted before persistence.
type CreateIntegrationRequest struct {
	Name                string         `json:"name"`
	Description         *string        `json:"description,omitempty"`
	ProviderType        string         `json:"providerType"`
	SourceType          string         `json:"sourceType"`
	Config              map[string]any `json:"config"`
	SyncMode            *string        `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int           `json:"syncIntervalMinutes,omitempty"`
}

// UpdateIntegrationRequest mirrors pkg/sdk/datasources.UpdateIntegrationRequest.
type UpdateIntegrationRequest struct {
	Name                *string        `json:"name,omitempty"`
	Description         *string        `json:"description,omitempty"`
	Config              map[string]any `json:"config,omitempty"`
	SyncMode            *string        `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int           `json:"syncIntervalMinutes,omitempty"`
	Enabled             *bool          `json:"enabled,omitempty"`
}

// TestConfigRequest mirrors pkg/sdk/datasources.TestConfigRequest.
type TestConfigRequest struct {
	ProviderType string         `json:"providerType"`
	Config       map[string]any `json:"config"`
}

// SyncJob is the kb.source_sync_jobs row model. It carries the full
// internal/jobs.Queue column contract (id, status, scheduled_at, priority,
// started_at, updated_at, attempt_count, last_error, completed_at) plus the
// source/progress columns. Fields that are not part of the SDK sync-job shape
// are `json:"-"` so the store model and the SDK DTO stay separate.
type SyncJob struct {
	bun.BaseModel `bun:"table:kb.source_sync_jobs,alias:sj"`

	// Queue contract columns.
	ID           uuid.UUID  `bun:"id,pk,type:uuid" json:"-"`
	Status       string     `bun:"status,notnull" json:"-"`
	ScheduledAt  time.Time  `bun:"scheduled_at,notnull" json:"-"`
	Priority     int        `bun:"priority,notnull" json:"-"`
	StartedAt    *time.Time `bun:"started_at" json:"-"`
	UpdatedAt    time.Time  `bun:"updated_at,notnull" json:"-"`
	AttemptCount int        `bun:"attempt_count,notnull" json:"-"`
	LastError    *string    `bun:"last_error" json:"-"`
	CompletedAt  *time.Time `bun:"completed_at" json:"-"`

	// Source/progress columns.
	ProjectID       uuid.UUID      `bun:"project_id,type:uuid,notnull" json:"-"`
	SourceID        uuid.UUID      `bun:"source_id,type:uuid,notnull" json:"-"`
	TriggerType     string         `bun:"trigger_type,notnull" json:"-"`
	TotalItems      int            `bun:"total_items,notnull" json:"-"`
	ProcessedItems  int            `bun:"processed_items,notnull" json:"-"`
	SuccessfulItems int            `bun:"successful_items,notnull" json:"-"`
	FailedItems     int            `bun:"failed_items,notnull" json:"-"`
	SkippedItems    int            `bun:"skipped_items,notnull" json:"-"`
	CurrentPhase    *string        `bun:"current_phase" json:"-"`
	StatusMessage   *string        `bun:"status_message" json:"-"`
	Error           map[string]any `bun:"error,type:jsonb" json:"-"`
	CursorBefore    map[string]any `bun:"cursor_before,type:jsonb" json:"-"`
	CursorAfter     map[string]any `bun:"cursor_after,type:jsonb" json:"-"`
	CreatedAt       time.Time      `bun:"created_at,notnull,default:now()" json:"-"`
}

// SyncJobDTO is the SDK-shaped sync-job response (mirrors
// pkg/sdk/datasources.SyncJob). It is derived from SyncJob via toSyncJobDTO and
// never carries queue internals.
type SyncJobDTO struct {
	ID              string     `json:"id"`
	IntegrationID   string     `json:"integrationId"`
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
	RetryCount      int        `json:"retryCount"`
	MaxRetries      int        `json:"maxRetries"`
	CreatedAt       time.Time  `json:"createdAt"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

// maxSyncRetries is the advertised max-retry count. The sync queue follows the
// internal/jobs default of unlimited retries (MaxAttempts=0), so 0 reports
// "unlimited" to SDK consumers.
const maxSyncRetries = 0

// TriggerSyncResponse mirrors pkg/sdk/datasources.TriggerSyncResponse.
type TriggerSyncResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	JobID   *string `json:"jobId,omitempty"`
}

// CancelSyncJobResponse mirrors pkg/sdk/datasources.CancelSyncJobResponse.
type CancelSyncJobResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ListSyncJobsResponse mirrors pkg/sdk/datasources.ListSyncJobsResponse.
type ListSyncJobsResponse struct {
	Items []SyncJobDTO `json:"items"`
	Total int          `json:"total"`
}
