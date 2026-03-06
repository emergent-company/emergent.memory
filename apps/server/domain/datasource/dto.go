package datasource

import (
	"time"
)

// ------------------------------------------------------------------
// Provider DTOs
// ------------------------------------------------------------------

// ProviderDTO represents an available data source provider
type ProviderDTO struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SourceType  string `json:"sourceType"`
	IconURL     string `json:"iconUrl,omitempty"`
	Available   bool   `json:"available"`
}

// ProviderSchemaDTO represents the configuration schema for a provider
type ProviderSchemaDTO struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// ------------------------------------------------------------------
// Integration DTOs
// ------------------------------------------------------------------

// DataSourceIntegrationDTO represents a data source integration for API responses
type DataSourceIntegrationDTO struct {
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

// CreateDataSourceIntegrationDTO represents request to create a new integration
type CreateDataSourceIntegrationDTO struct {
	Name                string                 `json:"name"`
	Description         *string                `json:"description,omitempty"`
	ProviderType        string                 `json:"providerType"`
	SourceType          string                 `json:"sourceType"`
	Config              map[string]interface{} `json:"config"`
	SyncMode            *string                `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int                   `json:"syncIntervalMinutes,omitempty"`
}

// UpdateDataSourceIntegrationDTO represents request to update an integration
type UpdateDataSourceIntegrationDTO struct {
	Name                *string                `json:"name,omitempty"`
	Description         *string                `json:"description,omitempty"`
	Config              map[string]interface{} `json:"config,omitempty"`
	SyncMode            *string                `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int                   `json:"syncIntervalMinutes,omitempty"`
	Enabled             *bool                  `json:"enabled,omitempty"`
}

// TestConfigDTO represents request to test a provider configuration
type TestConfigDTO struct {
	ProviderType string                 `json:"providerType"`
	Config       map[string]interface{} `json:"config"`
}

// TestConnectionResponseDTO represents the response from connection test
type TestConnectionResponseDTO struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ------------------------------------------------------------------
// Sync Job DTOs
// ------------------------------------------------------------------

// SyncJobDTO represents a sync job for API responses
type SyncJobDTO struct {
	ID                string     `json:"id"`
	IntegrationID     string     `json:"integrationId"`
	ConfigurationID   *string    `json:"configurationId,omitempty"`
	ConfigurationName *string    `json:"configurationName,omitempty"`
	Status            string     `json:"status"`
	TotalItems        int        `json:"totalItems"`
	ProcessedItems    int        `json:"processedItems"`
	SuccessfulItems   int        `json:"successfulItems"`
	FailedItems       int        `json:"failedItems"`
	SkippedItems      int        `json:"skippedItems"`
	CurrentPhase      *string    `json:"currentPhase,omitempty"`
	StatusMessage     *string    `json:"statusMessage,omitempty"`
	ErrorMessage      *string    `json:"errorMessage,omitempty"`
	TriggerType       string     `json:"triggerType"`
	RetryCount        int        `json:"retryCount"`
	MaxRetries        int        `json:"maxRetries"`
	CreatedAt         time.Time  `json:"createdAt"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
}

// TriggerSyncDTO represents request to trigger a sync
type TriggerSyncDTO struct {
	ConfigurationID *string                `json:"configurationId,omitempty"`
	FullSync        bool                   `json:"fullSync,omitempty"`
	Options         map[string]interface{} `json:"options,omitempty"`
}

// TriggerSyncResponseDTO represents the response from triggering a sync
type TriggerSyncResponseDTO struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	JobID   *string `json:"jobId,omitempty"`
}

// ------------------------------------------------------------------
// Sync Configuration DTOs
// ------------------------------------------------------------------

// SyncConfigurationDTO represents a stored sync configuration
type SyncConfigurationDTO struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Enabled     bool                   `json:"enabled"`
	Schedule    string                 `json:"schedule,omitempty"`
	IntervalMin int                    `json:"intervalMin,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
	LastRunAt   *time.Time             `json:"lastRunAt,omitempty"`
	NextRunAt   *time.Time             `json:"nextRunAt,omitempty"`
}

// CreateSyncConfigurationDTO represents request to create a sync configuration
type CreateSyncConfigurationDTO struct {
	Name        string                 `json:"name"`
	Enabled     *bool                  `json:"enabled,omitempty"`
	Schedule    string                 `json:"schedule,omitempty"`
	IntervalMin int                    `json:"intervalMin,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// UpdateSyncConfigurationDTO represents request to update a sync configuration
type UpdateSyncConfigurationDTO struct {
	Name        *string                `json:"name,omitempty"`
	Enabled     *bool                  `json:"enabled,omitempty"`
	Schedule    *string                `json:"schedule,omitempty"`
	IntervalMin *int                   `json:"intervalMin,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// ------------------------------------------------------------------
// Source Type DTOs
// ------------------------------------------------------------------

// SourceTypeDTO represents document counts by source type
type SourceTypeDTO struct {
	SourceType    string `json:"sourceType"`
	DocumentCount int    `json:"documentCount"`
}

// ------------------------------------------------------------------
// List Response DTOs
// ------------------------------------------------------------------

// ListIntegrationsParams represents query parameters for listing integrations
type ListIntegrationsParams struct {
	ProviderType *string `query:"providerType"`
	SourceType   *string `query:"sourceType"`
	Status       *string `query:"status"`
}

// ListSyncJobsParams represents query parameters for listing sync jobs
type ListSyncJobsParams struct {
	Status *string `query:"status"`
	Limit  int     `query:"limit"`
	Offset int     `query:"offset"`
}

// ------------------------------------------------------------------
// Conversion helpers
// ------------------------------------------------------------------

// ToDTO converts a DataSourceIntegration to DataSourceIntegrationDTO
func (i *DataSourceIntegration) ToDTO() DataSourceIntegrationDTO {
	return DataSourceIntegrationDTO{
		ID:                  i.ID,
		ProjectID:           i.ProjectID,
		Name:                i.Name,
		Description:         i.Description,
		ProviderType:        i.ProviderType,
		SourceType:          i.SourceType,
		SyncMode:            string(i.SyncMode),
		SyncIntervalMinutes: i.SyncIntervalMinutes,
		LastSyncedAt:        i.LastSyncedAt,
		NextSyncAt:          i.NextSyncAt,
		Status:              string(i.Status),
		ErrorMessage:        i.ErrorMessage,
		ErrorCount:          i.ErrorCount,
		CreatedAt:           i.CreatedAt,
		UpdatedAt:           i.UpdatedAt,
	}
}

// ToDTO converts a DataSourceSyncJob to SyncJobDTO
func (j *DataSourceSyncJob) ToDTO() SyncJobDTO {
	return SyncJobDTO{
		ID:                j.ID,
		IntegrationID:     j.IntegrationID,
		ConfigurationID:   j.ConfigurationID,
		ConfigurationName: j.ConfigurationName,
		Status:            string(j.Status),
		TotalItems:        j.TotalItems,
		ProcessedItems:    j.ProcessedItems,
		SuccessfulItems:   j.SuccessfulItems,
		FailedItems:       j.FailedItems,
		SkippedItems:      j.SkippedItems,
		CurrentPhase:      j.CurrentPhase,
		StatusMessage:     j.StatusMessage,
		ErrorMessage:      j.ErrorMessage,
		TriggerType:       string(j.TriggerType),
		RetryCount:        j.RetryCount,
		MaxRetries:        j.MaxRetries,
		CreatedAt:         j.CreatedAt,
		StartedAt:         j.StartedAt,
		CompletedAt:       j.CompletedAt,
	}
}
