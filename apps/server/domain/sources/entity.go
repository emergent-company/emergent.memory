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
//
// `needs_reconnect` parks a source whose stored credential the provider no longer
// accepts. Retrying cannot recover it — only a fresh consent can — so the sweep
// does not admit it and the failure is not retried automatically. It is cleared
// by reconnect or by a successful sync, never by a `disabled` source being
// reconnected (a pause is not a parked state).
const (
	SourceStatusActive         = "active"
	SourceStatusDisabled       = "disabled"
	SourceStatusNeedsReconnect = "needs_reconnect"
)

// Sync policy values (kb.sources.sync_policy). The policy — not the presence of
// an interval — is what makes a source eligible for the scheduler sweep:
// `manual` sources are only ever synced by an explicit trigger, `scheduled`
// sources are admitted when their interval has elapsed.
const (
	SyncPolicyManual    = "manual"
	SyncPolicyScheduled = "scheduled"
)

// Provider type identifiers.
const (
	// ProviderTypeGoogleDrive syncs documents from a Google Drive folder.
	ProviderTypeGoogleDrive = "google_drive"

	// ProviderTypeGmail syncs email threads from a Gmail mailbox. It is an
	// OAuth-only provider sharing the Google vendor/app identity with google_drive.
	ProviderTypeGmail = "gmail"

	// SourceTypeEmail is the source-type category advertised for the Gmail
	// provider (and future email-backed providers).
	SourceTypeEmail = "email"

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

	// ProviderTypeTrale syncs meeting intelligence (transcripts/summaries) from
	// Trale via its remote MCP server, connected with OAuth 2.1 + dynamic client
	// registration.
	ProviderTypeTrale = "trale"

	// SourceTypeMeeting is the source-type category advertised for the Trale
	// provider (and future meeting-intelligence providers).
	SourceTypeMeeting = "meeting"

	// ProviderTypeGitHubApp is the GitHub App bot: an installation-bound source
	// that receives webhooks and triggers agent runs. It is distinct from the
	// docs-sync ProviderTypeGitHub (OAuth/PAT, poll).
	ProviderTypeGitHubApp = "github_app"

	// SourceTypeApp is the source-type category advertised for the GitHub App bot
	// and future webhook-backed providers.
	SourceTypeApp = "app"

	// ProviderTypeWeb crawls a website and syncs its pages into the knowledge
	// base. It is config-only: no credentials are collected or stored, and every
	// fetch runs through the deployment's SSRF guard.
	ProviderTypeWeb = "web"

	// SourceTypeWeb is the source-type category advertised for the web provider
	// (and future crawl-backed providers).
	SourceTypeWeb = "web"
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
	// CredentialID references a project-scoped saved credential. When set, the
	// source uses that credential instead of its legacy inline blob.
	CredentialID *uuid.UUID `bun:"credential_id,type:uuid" json:"credentialId,omitempty"`
	// AppID references the org-level source App credential for a trigger-backed
	// source (github_app).
	AppID *uuid.UUID `bun:"app_id,type:uuid" json:"appId,omitempty"`
	// ExternalAccountID is the provider's installation/account id (GitHub
	// installation id) for a trigger-backed source.
	ExternalAccountID *string `bun:"external_account_id" json:"externalAccountId,omitempty"`
	// McpSlug is the immutable MCP server/tool-name segment for this source.
	// Null for providers that do not expose source-backed MCP tools.
	McpSlug *string `bun:"mcp_slug" json:"mcpSlug,omitempty"`
	// OAuthAppID references the named, vendor-level OAuth app this source uses.
	// NULL means resolve from the sole project/org app or the deployment default.
	OAuthAppID          *uuid.UUID     `bun:"oauth_app_id,type:uuid" json:"oauthAppId,omitempty"`
	SyncPolicy          string         `bun:"sync_policy,notnull" json:"syncPolicy"`
	SyncIntervalMinutes *int           `bun:"sync_interval_minutes" json:"syncIntervalMinutes,omitempty"`
	Cursor              map[string]any `bun:"cursor,type:jsonb,notnull" json:"cursor"`
	Status              string         `bun:"status,notnull" json:"status"`
	// CredentialsUpdatedAt is the grant epoch: when the credential this source
	// reads was last replaced. It is deliberately not `UpdatedAt` — token rotation
	// writes the row on nearly every run for a rotating provider, and a sync job
	// uses this value to tell "the user repaired this while I was running" from
	// "I failed against the current grant" (#2425).
	CredentialsUpdatedAt time.Time  `bun:"credentials_updated_at,notnull,default:now()" json:"-"`
	ErrorCount           int        `bun:"error_count,notnull" json:"errorCount"`
	LastError            *string    `bun:"last_error" json:"lastError,omitempty"`
	LastErrorAt          *time.Time `bun:"last_error_at" json:"lastErrorAt,omitempty"`
	// LastProbedAt is when the proactive grant probe last asked the provider
	// whether the stored credential is still accepted. It is not a sync: the
	// scheduler does not read it, and a probe does not move LastSyncedAt.
	LastProbedAt *time.Time `bun:"last_probed_at" json:"-"`
	LastSyncedAt *time.Time `bun:"last_synced_at" json:"lastSyncedAt,omitempty"`
	CreatedAt    time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt    time.Time  `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// SourceObject maps an external source item to the graph object created from
// it. It is the idempotency key and deletion-reconciliation ledger for typed
// source sync.
type SourceObject struct {
	bun.BaseModel `bun:"table:kb.source_objects,alias:so"`

	ID                uuid.UUID  `bun:"id,pk,type:uuid" json:"id"`
	ProjectID         uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"projectId"`
	SourceID          uuid.UUID  `bun:"source_id,type:uuid,notnull" json:"sourceId"`
	ExternalID        string     `bun:"external_id,notnull" json:"externalId"`
	ObjectType        string     `bun:"object_type,notnull" json:"objectType"`
	ObjectKey         string     `bun:"object_key,notnull" json:"objectKey"`
	ObjectID          uuid.UUID  `bun:"object_id,type:uuid,notnull" json:"objectId"`
	ObjectCanonicalID uuid.UUID  `bun:"object_canonical_id,type:uuid,notnull" json:"objectCanonicalId"`
	DocumentID        *uuid.UUID `bun:"document_id,type:uuid" json:"documentId,omitempty"`
	RevisionID        *string    `bun:"revision_id" json:"revisionId,omitempty"`
	Status            string     `bun:"status,notnull" json:"status"`
	SyncedAt          time.Time  `bun:"synced_at,notnull" json:"syncedAt"`
	CreatedAt         time.Time  `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt         time.Time  `bun:"updated_at,notnull" json:"updatedAt"`
}

// SourceObject status values.
const (
	SourceObjectStatusActive            = "active"
	SourceObjectStatusDeletedFromSource = "deleted_from_source"
)

// SourceCredential is the kb.source_credentials row model. A saved credential
// is a reusable, project-scoped credential payload for one provider. The
// encrypted blob is deliberately `json:"-"` so it can never be serialised.
type SourceCredential struct {
	bun.BaseModel `bun:"table:kb.source_credentials,alias:sc"`

	ID                   uuid.UUID `bun:"id,pk,type:uuid" json:"id"`
	ProjectID            uuid.UUID `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Provider             string    `bun:"provider,notnull" json:"provider"`
	Name                 string    `bun:"name,notnull" json:"name"`
	EncryptedCredentials []byte    `bun:"encrypted_credentials,type:bytea,notnull" json:"-"`
	CreatedAt            time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt            time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// SavedCredential is the public, secret-free view of a saved credential.
type SavedCredential struct {
	ID           string    `json:"id"`
	ProviderType string    `json:"providerType"`
	Name         string    `json:"name"`
	UsedByCount  int       `json:"usedByCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// CreateSavedCredentialRequest is the write shape for a reusable credential.
type CreateSavedCredentialRequest struct {
	ProviderType string         `json:"providerType"`
	Name         string         `json:"name"`
	Credentials  map[string]any `json:"credentials"`
}

// UpdateSavedCredentialRequest is the partial-update shape for a reusable
// credential. An empty Credentials map keeps the stored secret.
type UpdateSavedCredentialRequest struct {
	Name        *string        `json:"name,omitempty"`
	Credentials map[string]any `json:"credentials,omitempty"`
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
	Vendor     string `json:"vendor"`
	VendorName string `json:"vendorName"`
	IconURL    string `json:"iconUrl,omitempty"`
	Available  bool   `json:"available"`
	// SelfRegistering marks a provider whose OAuth client registers itself via
	// RFC 7591 dynamic client registration at connect time (no admin-configured
	// app). The credentials UI skips such providers: there is no client id/secret
	// to manage, so a "Set up OAuth app" action would be misleading.
	SelfRegistering bool     `json:"selfRegistering,omitempty"`
	AuthMethods     []string `json:"authMethods,omitempty"`
	// Strategies advertises the org-selectable OAuth scope strategies (design
	// D6); empty when the provider's scopes are fixed.
	Strategies []string `json:"strategies,omitempty"`
}

// ProviderSchema mirrors pkg/sdk/datasources.ProviderSchema. Properties includes
// both the non-secret config fields and the provider's write-only credential
// fields (marked writeOnly), so a form can be rendered from one schema.
type ProviderSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

// Integration mirrors pkg/sdk/datasources.Integration — the read/write shape of
// a data-source integration. It carries no credential material.
type Integration struct {
	ID                  string  `json:"id"`
	ProjectID           string  `json:"projectId"`
	Name                string  `json:"name"`
	Description         *string `json:"description,omitempty"`
	ProviderType        string  `json:"providerType"`
	SourceType          string  `json:"sourceType"`
	SyncMode            string  `json:"syncMode"`
	SyncIntervalMinutes *int    `json:"syncIntervalMinutes,omitempty"`
	// McpSlug is the immutable MCP tool-name segment for a source-backed MCP
	// server (Gmail). Empty for providers without source tools.
	McpSlug *string `json:"mcpSlug,omitempty"`
	// Config carries the non-secret config values (credential keys stripped) so
	// the connect/edit UI can round-trip a source's persisted settings without
	// exposing write-only credential fields. Credential material never appears
	// here (design D2).
	Config map[string]any `json:"config,omitempty"`
	// OAuthStrategy names the scope strategy this source effectively uses
	// (resolved project → org → provider default). It is filled on the
	// single-source read only, and only for OAuth providers; empty means the
	// provider default applies.
	OAuthStrategy string `json:"oauthStrategy,omitempty"`
	// OAuthGrantedStrategy names the scope strategy the stored token was
	// actually granted at connect, resolved from the token response's granted
	// scope. It differs from OAuthStrategy, which reports the currently
	// configured strategy: a source authorized under drive.file keeps its
	// granted strategy even after the org's configured strategy changes. Empty
	// for older sources connected before this was persisted, or when the token
	// carried only custom/unrecognised scopes.
	OAuthGrantedStrategy string     `json:"oauthGrantedStrategy,omitempty"`
	LastSyncedAt         *time.Time `json:"lastSyncedAt,omitempty"`
	NextSyncAt           *time.Time `json:"nextSyncAt,omitempty"`
	Status               string     `json:"status"`
	ErrorMessage         *string    `json:"errorMessage,omitempty"`
	ErrorCount           int        `json:"errorCount"`
	// CredentialID names the project-scoped saved credential backing this
	// source, when it uses one. Omitted for legacy sources whose credentials
	// were stored inline. It is a reference, not secret material.
	CredentialID *string `json:"credentialId,omitempty"`
	// OAuthAppID/OAuthAppName name the vendor-level OAuth app the source uses.
	// OAuthAppID is empty when the source resolves to the deployment default.
	OAuthAppID   *string   `json:"oauthAppId,omitempty"`
	OAuthAppName string    `json:"oauthAppName,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
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
	// SavedCredentialID references a project-scoped saved credential. When set,
	// the source references it instead of storing an inline credential copy.
	SavedCredentialID *string `json:"credentialId,omitempty"`
	// OAuthAppID selects the named OAuth app this source uses. Empty lets the
	// resolver pick the sole project/org app or the deployment default.
	OAuthAppID *string `json:"oauthAppId,omitempty"`
	// oauthCompletion marks a create issued by the OAuth callback. Only that
	// path resolved an OAuth client, so only it may pin the resolved app
	// implicitly; a credentials (PAT) create must not, even when the provider
	// also supports OAuth (#554). Never serialised, so a caller cannot claim it.
	oauthCompletion bool
	// AllowIncompleteConfig lets a caller (the OAuth callback) create a source
	// whose config is missing its sync target (folder_id/repo). The target is
	// selected after consent and applied via a later update, so config
	// validation is skipped for the create. Never serialised.
	AllowIncompleteConfig bool `json:"-"`
}

// UpdateIntegrationRequest mirrors pkg/sdk/datasources.UpdateIntegrationRequest.
type UpdateIntegrationRequest struct {
	Name                *string        `json:"name,omitempty"`
	Description         *string        `json:"description,omitempty"`
	Config              map[string]any `json:"config,omitempty"`
	SyncMode            *string        `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int           `json:"syncIntervalMinutes,omitempty"`
	Enabled             *bool          `json:"enabled,omitempty"`
	// SavedCredentialID references a project-scoped saved credential. An empty
	// string clears the reference and returns the source to its inline blob.
	SavedCredentialID *string `json:"credentialId,omitempty"`
	// OAuthAppID selects the named OAuth app. An empty string clears the explicit
	// selection and returns the source to automatic resolution.
	OAuthAppID *string `json:"oauthAppId,omitempty"`
}

// TestConfigRequest mirrors pkg/sdk/datasources.TestConfigRequest.
type TestConfigRequest struct {
	ProviderType      string         `json:"providerType"`
	Config            map[string]any `json:"config"`
	SavedCredentialID *string        `json:"credentialId,omitempty"`
}

// ProviderListRequest mirrors TestConfigRequest for a credential-scoped listing
// (repositories, refs) of an unsaved provider config. Nothing is persisted.
type ProviderListRequest struct {
	ProviderType      string         `json:"providerType"`
	Config            map[string]any `json:"config"`
	SavedCredentialID *string        `json:"credentialId,omitempty"`
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
