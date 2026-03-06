package integrations

import (
	"time"
)

// IntegrationCapabilitiesDTO describes what an integration can do
type IntegrationCapabilitiesDTO struct {
	SupportsImport            bool `json:"supportsImport"`
	SupportsWebhooks          bool `json:"supportsWebhooks"`
	SupportsBidirectionalSync bool `json:"supportsBidirectionalSync"`
	RequiresOAuth             bool `json:"requiresOAuth"`
	SupportsIncrementalSync   bool `json:"supportsIncrementalSync"`
}

// AvailableIntegrationDTO represents an available integration type from the registry
type AvailableIntegrationDTO struct {
	Name             string                     `json:"name"`
	DisplayName      string                     `json:"displayName"`
	Description      string                     `json:"description,omitempty"`
	Capabilities     IntegrationCapabilitiesDTO `json:"capabilities"`
	RequiredSettings []string                   `json:"requiredSettings"`
	OptionalSettings map[string]interface{}     `json:"optionalSettings,omitempty"`
}

// IntegrationDTO represents a configured integration instance
type IntegrationDTO struct {
	ID             string                 `json:"id"`
	OrgID          string                 `json:"org_id"`
	ProjectID      string                 `json:"project_id"`
	Name           string                 `json:"name"`
	DisplayName    string                 `json:"display_name"`
	Description    *string                `json:"description,omitempty"`
	Settings       map[string]interface{} `json:"settings,omitempty"`
	Enabled        bool                   `json:"enabled"`
	LastSyncAt     *time.Time             `json:"last_sync_at,omitempty"`
	LastSyncStatus *string                `json:"last_sync_status,omitempty"`
	ErrorMessage   *string                `json:"error_message,omitempty"`
	LogoURL        *string                `json:"logo_url,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// PublicIntegrationDTO represents non-sensitive integration info
type PublicIntegrationDTO struct {
	Name             string  `json:"name"`
	DisplayName      string  `json:"display_name"`
	Description      *string `json:"description,omitempty"`
	Enabled          bool    `json:"enabled"`
	LogoURL          *string `json:"logo_url,omitempty"`
	HasConfiguration bool    `json:"has_configuration"`
}

// CreateIntegrationDTO represents the payload for creating an integration
type CreateIntegrationDTO struct {
	Name        string                 `json:"name" validate:"required"`
	DisplayName string                 `json:"display_name" validate:"required"`
	Description *string                `json:"description,omitempty"`
	Settings    map[string]interface{} `json:"settings,omitempty"`
	Enabled     *bool                  `json:"enabled,omitempty"`
	LogoURL     *string                `json:"logo_url,omitempty"`
	CreatedBy   *string                `json:"created_by,omitempty"`
}

// UpdateIntegrationDTO represents the payload for updating an integration
type UpdateIntegrationDTO struct {
	DisplayName *string                `json:"display_name,omitempty"`
	Description *string                `json:"description,omitempty"`
	Settings    map[string]interface{} `json:"settings,omitempty"`
	Enabled     *bool                  `json:"enabled,omitempty"`
	LogoURL     *string                `json:"logo_url,omitempty"`
}

// TestConnectionResponseDTO represents the response from testing a connection
type TestConnectionResponseDTO struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// TriggerSyncResponseDTO represents the response from triggering a sync
type TriggerSyncResponseDTO struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	JobID   *string `json:"job_id,omitempty"`
}

// TriggerSyncConfigDTO represents configuration for triggering a sync
type TriggerSyncConfigDTO struct {
	FullSync        *bool    `json:"full_sync,omitempty"`
	SourceTypes     []string `json:"source_types,omitempty"`
	SpaceIDs        []string `json:"space_ids,omitempty"`
	IncludeArchived *bool    `json:"includeArchived,omitempty"`
	BatchSize       *int     `json:"batchSize,omitempty"`
}

// ListIntegrationsParams represents query parameters for listing integrations
type ListIntegrationsParams struct {
	Name    *string `query:"name"`
	Enabled *bool   `query:"enabled"`
}

// ToDTO converts an Integration entity to DTO (without decrypted settings)
func (i *Integration) ToDTO() IntegrationDTO {
	return IntegrationDTO{
		ID:          i.ID,
		OrgID:       i.OrgID,
		ProjectID:   i.ProjectID,
		Name:        i.Name,
		DisplayName: i.DisplayName,
		Description: i.Description,
		Enabled:     i.Enabled,
		LogoURL:     i.LogoURL,
		CreatedAt:   i.CreatedAt,
		UpdatedAt:   i.UpdatedAt,
	}
}

// ToPublicDTO converts an Integration entity to a public DTO
func (i *Integration) ToPublicDTO() PublicIntegrationDTO {
	return PublicIntegrationDTO{
		Name:             i.Name,
		DisplayName:      i.DisplayName,
		Description:      i.Description,
		Enabled:          i.Enabled,
		LogoURL:          i.LogoURL,
		HasConfiguration: i.SettingsEncrypted != nil && len(i.SettingsEncrypted) > 0,
	}
}
