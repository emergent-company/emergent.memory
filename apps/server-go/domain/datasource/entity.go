package datasource

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// ------------------------------------------------------------------
// Status and Type Enums
// ------------------------------------------------------------------

// JobStatus represents the processing status of a sync job
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusRunning    JobStatus = "running"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusCancelled  JobStatus = "cancelled"
	JobStatusDeadLetter JobStatus = "dead_letter" // Permanently failed after max retries
)

// TriggerType represents what triggered the sync
type TriggerType string

const (
	TriggerTypeManual    TriggerType = "manual"
	TriggerTypeScheduled TriggerType = "scheduled"
	TriggerTypeWebhook   TriggerType = "webhook"
)

// IntegrationStatus represents the status of an integration
type IntegrationStatus string

const (
	IntegrationStatusActive   IntegrationStatus = "active"
	IntegrationStatusError    IntegrationStatus = "error"
	IntegrationStatusDisabled IntegrationStatus = "disabled"
)

// SyncMode represents how syncing is triggered
type SyncMode string

const (
	SyncModeManual    SyncMode = "manual"
	SyncModeRecurring SyncMode = "recurring"
)

// ------------------------------------------------------------------
// DataSourceIntegration - External data source configuration
// ------------------------------------------------------------------

// DataSourceIntegration represents an integration with an external data source.
// Maps to kb.data_source_integrations table.
type DataSourceIntegration struct {
	bun.BaseModel `bun:"table:kb.data_source_integrations,alias:dsi"`

	ID                  string            `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	OrganizationID      string            `bun:"organization_id,notnull,type:uuid"`
	ProjectID           string            `bun:"project_id,notnull,type:uuid"`
	Name                string            `bun:"name,notnull"`
	Description         *string           `bun:"description"`
	ProviderType        string            `bun:"provider_type,notnull"` // 'imap', 'gmail_oauth', 'google_drive', 'clickup'
	SourceType          string            `bun:"source_type,notnull"`   // 'email', 'drive', 'clickup-document'
	ConfigEncrypted     *string           `bun:"config_encrypted"`      // AES-256-GCM encrypted config
	SyncMode            SyncMode          `bun:"sync_mode,notnull,default:'manual'"`
	SyncIntervalMinutes *int              `bun:"sync_interval_minutes"`
	LastSyncedAt        *time.Time        `bun:"last_synced_at"`
	NextSyncAt          *time.Time        `bun:"next_sync_at"`
	Status              IntegrationStatus `bun:"status,notnull,default:'active'"`
	ErrorMessage        *string           `bun:"error_message"`
	ErrorCount          int               `bun:"error_count,notnull,default:0"`
	Metadata            JSON              `bun:"metadata,type:jsonb,notnull,default:'{}'"`
	CreatedBy           *string           `bun:"created_by,type:uuid"`
	CreatedAt           time.Time         `bun:"created_at,notnull,default:now()"`
	UpdatedAt           time.Time         `bun:"updated_at,notnull,default:now()"`
}

// ------------------------------------------------------------------
// DataSourceSyncJob - Tracks async sync operations
// ------------------------------------------------------------------

// DataSourceSyncJob represents a data source sync job in kb.data_source_sync_jobs.
// These jobs sync data from external integrations (ClickUp, Gmail, etc.).
type DataSourceSyncJob struct {
	bun.BaseModel `bun:"table:kb.data_source_sync_jobs,alias:dssj"`

	ID                string      `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	IntegrationID     string      `bun:"integration_id,notnull,type:uuid"`
	ProjectID         string      `bun:"project_id,notnull,type:uuid"`
	ConfigurationID   *string     `bun:"configuration_id,type:uuid"`
	ConfigurationName *string     `bun:"configuration_name"`
	Status            JobStatus   `bun:"status,notnull,default:'pending'"`
	TotalItems        int         `bun:"total_items,notnull,default:0"`
	ProcessedItems    int         `bun:"processed_items,notnull,default:0"`
	SuccessfulItems   int         `bun:"successful_items,notnull,default:0"`
	FailedItems       int         `bun:"failed_items,notnull,default:0"`
	SkippedItems      int         `bun:"skipped_items,notnull,default:0"`
	CurrentPhase      *string     `bun:"current_phase"` // 'initializing', 'discovering', 'importing', 'syncing', 'completed'
	StatusMessage     *string     `bun:"status_message"`
	SyncOptions       JSON        `bun:"sync_options,type:jsonb,notnull,default:'{}'"`
	DocumentIDs       JSONArray   `bun:"document_ids,type:jsonb,notnull,default:'[]'"`
	Logs              JSONArray   `bun:"logs,type:jsonb,notnull,default:'[]'"`
	ErrorMessage      *string     `bun:"error_message"`
	ErrorDetails      JSON        `bun:"error_details,type:jsonb"`
	RetryCount        int         `bun:"retry_count,notnull,default:0"`
	MaxRetries        int         `bun:"max_retries,notnull,default:3"`
	NextRetryAt       *time.Time  `bun:"next_retry_at"`
	TriggeredBy       *string     `bun:"triggered_by,type:uuid"`
	TriggerType       TriggerType `bun:"trigger_type,notnull,default:'manual'"`
	CreatedAt         time.Time   `bun:"created_at,notnull,default:now()"`
	StartedAt         *time.Time  `bun:"started_at"`
	CompletedAt       *time.Time  `bun:"completed_at"`
	UpdatedAt         time.Time   `bun:"updated_at,notnull,default:now()"`
}

// ------------------------------------------------------------------
// SyncConfiguration - Stored sync configurations
// ------------------------------------------------------------------

// SyncConfiguration represents a stored sync configuration
// Stored in integration.metadata.syncConfigurations
type SyncConfiguration struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Schedule    string    `json:"schedule,omitempty"`    // Cron expression
	IntervalMin int       `json:"intervalMin,omitempty"` // Minutes between runs
	Options     JSON      `json:"options,omitempty"`
	LastRunAt   time.Time `json:"lastRunAt,omitempty"`
	NextRunAt   time.Time `json:"nextRunAt,omitempty"`
}

// ------------------------------------------------------------------
// SyncJobLogEntry - Log entries for job progress
// ------------------------------------------------------------------

// SyncJobLogEntry represents a single log entry in a sync job
type SyncJobLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // 'info', 'warn', 'error'
	Message   string    `json:"message"`
	Details   JSON      `json:"details,omitempty"`
}

// ------------------------------------------------------------------
// Helper types for JSONB columns
// ------------------------------------------------------------------

// JSON is a helper type for JSONB columns (objects)
type JSON map[string]interface{}

// Value implements driver.Valuer for JSON
func (j JSON) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements sql.Scanner for JSON
func (j *JSON) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

// JSONArray is a helper type for JSONB columns that store arrays
type JSONArray []interface{}

// Value implements driver.Valuer for JSONArray
func (j JSONArray) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements sql.Scanner for JSONArray
func (j *JSONArray) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

// StringArray is a helper for arrays of strings
type StringArray []string

// Value implements driver.Valuer for StringArray
func (s StringArray) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan implements sql.Scanner for StringArray
func (s *StringArray) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, s)
}
