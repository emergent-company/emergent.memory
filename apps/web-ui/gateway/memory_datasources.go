package main

import (
	"context"
	"net/http"
	"net/url"
)

// --- data-source integrations (Data Source Integrations API) ---

// DSProvider is one entry of the data-source provider catalog
// (GET /api/data-source-integrations/providers).
type DSProvider struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SourceType  string `json:"sourceType"`
	IconURL     string `json:"iconUrl,omitempty"`
	Available   bool   `json:"available"`
}

// DSProviderSchema is a provider's JSON config schema
// (GET /api/data-source-integrations/providers/:providerType/schema). Properties
// are the raw schema objects, so the connect form can be rendered from the
// server's declared contract rather than a hard-coded field list.
type DSProviderSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

// DSIntegration is a data-source integration (GET /api/data-source-integrations).
// It carries no credential material — the server never returns it.
type DSIntegration struct {
	ID                  string  `json:"id"`
	ProjectID           string  `json:"projectId"`
	Name                string  `json:"name"`
	Description         *string `json:"description,omitempty"`
	ProviderType        string  `json:"providerType"`
	SourceType          string  `json:"sourceType"`
	SyncMode            string  `json:"syncMode"`
	SyncIntervalMinutes *int    `json:"syncIntervalMinutes,omitempty"`
	LastSyncedAt        *string `json:"lastSyncedAt,omitempty"`
	NextSyncAt          *string `json:"nextSyncAt,omitempty"`
	Status              string  `json:"status"`
	ErrorMessage        *string `json:"errorMessage,omitempty"`
	ErrorCount          int     `json:"errorCount"`
	CreatedAt           string  `json:"createdAt"`
	UpdatedAt           string  `json:"updatedAt"`
}

// DSSyncJob is one sync job of a source. It mirrors the SDK SyncJob shape so the
// full progress contract (phase, message, item counts, trigger, timings) is
// available to the UI.
type DSSyncJob struct {
	ID              string  `json:"id"`
	IntegrationID   string  `json:"integrationId"`
	Status          string  `json:"status"`
	TotalItems      int     `json:"totalItems"`
	ProcessedItems  int     `json:"processedItems"`
	SuccessfulItems int     `json:"successfulItems"`
	FailedItems     int     `json:"failedItems"`
	SkippedItems    int     `json:"skippedItems"`
	CurrentPhase    *string `json:"currentPhase,omitempty"`
	StatusMessage   *string `json:"statusMessage,omitempty"`
	ErrorMessage    *string `json:"errorMessage,omitempty"`
	TriggerType     string  `json:"triggerType"`
	RetryCount      int     `json:"retryCount"`
	MaxRetries      int     `json:"maxRetries"`
	CreatedAt       string  `json:"createdAt"`
	StartedAt       *string `json:"startedAt,omitempty"`
	CompletedAt     *string `json:"completedAt,omitempty"`
}

// DSListSyncJobsResponse is the sync-jobs list envelope.
type DSListSyncJobsResponse struct {
	Items []DSSyncJob `json:"items"`
	Total int         `json:"total"`
}

// DSSourceType is a document count grouped by source type.
type DSSourceType struct {
	SourceType    string `json:"sourceType"`
	DocumentCount int    `json:"documentCount"`
}

// DSTestConnectionResponse is the result of a config or connection test.
type DSTestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DSTriggerSyncResponse is the result of a manual sync trigger.
type DSTriggerSyncResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	JobID   *string `json:"jobId,omitempty"`
}

// DSCancelSyncJobResponse is the result of cancelling a sync job.
type DSCancelSyncJobResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DSCreateIntegrationRequest is the create body. Credentials travel inside
// Config (client_id/client_secret/refresh_token) and are stripped + encrypted by
// the server before persistence.
type DSCreateIntegrationRequest struct {
	Name                string         `json:"name"`
	Description         *string        `json:"description,omitempty"`
	ProviderType        string         `json:"providerType"`
	SourceType          string         `json:"sourceType"`
	Config              map[string]any `json:"config"`
	SyncMode            *string        `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int           `json:"syncIntervalMinutes,omitempty"`
}

// DSUpdateIntegrationRequest is the partial update body. Omitted fields are left
// unchanged by the server; Config is only sent when the form supplied it.
type DSUpdateIntegrationRequest struct {
	Name                *string        `json:"name,omitempty"`
	Description         *string        `json:"description,omitempty"`
	Config              map[string]any `json:"config,omitempty"`
	SyncMode            *string        `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int           `json:"syncIntervalMinutes,omitempty"`
}

// DSTestConfigRequest tests an unsaved provider config.
type DSTestConfigRequest struct {
	ProviderType string         `json:"providerType"`
	Config       map[string]any `json:"config"`
}

// ListDSProviders returns the available data-source providers.
func (m *MemoryClient) ListDSProviders(ctx context.Context) ([]DSProvider, error) {
	var out []DSProvider
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations/providers", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDSProviderSchema returns a provider's config schema.
func (m *MemoryClient) GetDSProviderSchema(ctx context.Context, providerType string) (*DSProviderSchema, error) {
	var out DSProviderSchema
	path := "/api/data-source-integrations/providers/" + url.PathEscape(providerType) + "/schema"
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDSSourceTypes returns document counts grouped by source type.
func (m *MemoryClient) ListDSSourceTypes(ctx context.Context) ([]DSSourceType, error) {
	var out []DSSourceType
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations/source-types", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TestDSConfig tests an unsaved provider config (POST
// /api/data-source-integrations/test-config).
func (m *MemoryClient) TestDSConfig(ctx context.Context, req *DSTestConfigRequest) (*DSTestConnectionResponse, error) {
	var out DSTestConnectionResponse
	if err := m.doH(ctx, http.MethodPost, "/api/data-source-integrations/test-config", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TestDSConnection tests an existing source's stored credentials (POST
// /api/data-source-integrations/:id/test-connection).
func (m *MemoryClient) TestDSConnection(ctx context.Context, id string) (*DSTestConnectionResponse, error) {
	var out DSTestConnectionResponse
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/test-connection"
	if err := m.doH(ctx, http.MethodPost, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDSIntegrations lists the project's data sources.
func (m *MemoryClient) ListDSIntegrations(ctx context.Context) ([]DSIntegration, error) {
	var out []DSIntegration
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDSIntegration fetches one data source by id.
func (m *MemoryClient) GetDSIntegration(ctx context.Context, id string) (*DSIntegration, error) {
	var out DSIntegration
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations/"+url.PathEscape(id), nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDSIntegration creates a data source.
func (m *MemoryClient) CreateDSIntegration(ctx context.Context, req *DSCreateIntegrationRequest) (*DSIntegration, error) {
	var out DSIntegration
	if err := m.doH(ctx, http.MethodPost, "/api/data-source-integrations", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDSIntegration applies a partial update to a data source (PATCH).
func (m *MemoryClient) UpdateDSIntegration(ctx context.Context, id string, req *DSUpdateIntegrationRequest) (*DSIntegration, error) {
	var out DSIntegration
	path := "/api/data-source-integrations/" + url.PathEscape(id)
	if err := m.doH(ctx, http.MethodPatch, path, req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDSIntegration deletes a data source.
func (m *MemoryClient) DeleteDSIntegration(ctx context.Context, id string) error {
	path := "/api/data-source-integrations/" + url.PathEscape(id)
	return m.doH(ctx, http.MethodDelete, path, nil, m.documentHeaders(ctx), nil)
}

// TriggerDSSync admits a manual sync job for a source. When a job is already
// active the server returns it in the response rather than erroring.
func (m *MemoryClient) TriggerDSSync(ctx context.Context, id string) (*DSTriggerSyncResponse, error) {
	var out DSTriggerSyncResponse
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync"
	if err := m.doH(ctx, http.MethodPost, path, map[string]any{}, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDSSyncJobs lists a source's sync jobs (optionally filtered by status).
func (m *MemoryClient) ListDSSyncJobs(ctx context.Context, id, status string) (*DSListSyncJobsResponse, error) {
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync-jobs"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var out DSListSyncJobsResponse
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLatestDSSyncJob returns a source's most recent sync job, or nil when it has
// never synced (the server returns JSON null).
func (m *MemoryClient) GetLatestDSSyncJob(ctx context.Context, id string) (*DSSyncJob, error) {
	var out *DSSyncJob
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync-jobs/latest"
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDSSyncJob fetches one sync job of a source.
func (m *MemoryClient) GetDSSyncJob(ctx context.Context, id, jobID string) (*DSSyncJob, error) {
	var out DSSyncJob
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync-jobs/" + url.PathEscape(jobID)
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelDSSyncJob cancels an active sync job.
func (m *MemoryClient) CancelDSSyncJob(ctx context.Context, id, jobID string) (*DSCancelSyncJobResponse, error) {
	var out DSCancelSyncJobResponse
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync-jobs/" + url.PathEscape(jobID) + "/cancel"
	if err := m.doH(ctx, http.MethodPost, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- document version history ---

// DocumentVersion is one version of a document
// (GET /api/documents/:id/versions), ordered by version number.
type DocumentVersion struct {
	ID               string  `json:"id"`
	CanonicalID      string  `json:"canonicalId"`
	Version          int     `json:"version"`
	SupersedesID     *string `json:"supersedesId,omitempty"`
	SourceStatus     string  `json:"sourceStatus,omitempty"`
	SourceRevisionID *string `json:"sourceRevisionId,omitempty"`
	SourceDeletedAt  *string `json:"sourceDeletedAt,omitempty"`
	CreatedAt        string  `json:"createdAt"`
}

// ListDocumentVersions returns a document's full version history.
func (m *MemoryClient) ListDocumentVersions(ctx context.Context, documentID string) ([]DocumentVersion, error) {
	var out struct {
		Versions []DocumentVersion `json:"versions"`
	}
	path := "/api/documents/" + url.PathEscape(documentID) + "/versions"
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.Versions, nil
}
