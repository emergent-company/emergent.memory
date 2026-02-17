package githubapp

import (
	"time"

	"github.com/uptrace/bun"
)

// GitHubAppConfig stores GitHub App credentials for repository access.
// Table: core.github_app_config
// At most one row per Emergent instance (singleton per owner_id).
type GitHubAppConfig struct {
	bun.BaseModel `bun:"table:core.github_app_config,alias:gac"`

	ID                     string    `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID                  int64     `bun:"app_id,notnull" json:"app_id"`
	AppSlug                string    `bun:"app_slug,notnull,default:''" json:"app_slug"`
	PrivateKeyEncrypted    []byte    `bun:"private_key_encrypted,type:bytea,notnull" json:"-"`
	WebhookSecretEncrypted []byte    `bun:"webhook_secret_encrypted,type:bytea" json:"-"`
	ClientID               string    `bun:"client_id,notnull,default:''" json:"client_id"`
	ClientSecretEncrypted  []byte    `bun:"client_secret_encrypted,type:bytea" json:"-"`
	InstallationID         *int64    `bun:"installation_id" json:"installation_id,omitempty"`
	InstallationOrg        *string   `bun:"installation_org" json:"installation_org,omitempty"`
	OwnerID                string    `bun:"owner_id,notnull,default:''" json:"owner_id"`
	CreatedAt              time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt              time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// IsInstalled returns true if the GitHub App has been installed on an org/account.
func (c *GitHubAppConfig) IsInstalled() bool {
	return c.InstallationID != nil && *c.InstallationID > 0
}

// --- DTOs ---

// StatusResponse is the response DTO for GitHub App connection status.
type StatusResponse struct {
	Connected       bool    `json:"connected"`
	AppID           int64   `json:"app_id,omitempty"`
	AppSlug         string  `json:"app_slug,omitempty"`
	InstallationID  *int64  `json:"installation_id,omitempty"`
	InstallationOrg *string `json:"installation_org,omitempty"`
	ConnectedBy     string  `json:"connected_by,omitempty"`
	ConnectedAt     string  `json:"connected_at,omitempty"`
}

// ConnectRequest is the request DTO for initiating the GitHub App manifest flow.
type ConnectRequest struct {
	RedirectURL string `json:"redirect_url,omitempty"` // Override default redirect URL
}

// ConnectResponse is the response DTO for the manifest flow initiation.
type ConnectResponse struct {
	ManifestURL string `json:"manifest_url"`
}

// CallbackRequest is the request DTO for the GitHub App manifest callback.
type CallbackRequest struct {
	Code string `json:"code" query:"code" validate:"required"`
}

// CLISetupRequest is the request DTO for CLI-based credential setup.
type CLISetupRequest struct {
	AppID          int64  `json:"app_id" validate:"required"`
	PrivateKeyPEM  string `json:"private_key_pem" validate:"required"`
	InstallationID int64  `json:"installation_id" validate:"required"`
}

// WebhookEvent represents a minimal GitHub webhook payload.
type WebhookEvent struct {
	Action       string               `json:"action"`
	Installation *WebhookInstallation `json:"installation,omitempty"`
}

// WebhookInstallation represents the installation object in a webhook payload.
type WebhookInstallation struct {
	ID      int64           `json:"id"`
	AppID   int64           `json:"app_id"`
	Account *WebhookAccount `json:"account,omitempty"`
}

// WebhookAccount represents the account (org or user) in an installation webhook.
type WebhookAccount struct {
	Login string `json:"login"`
	Type  string `json:"type"` // "Organization" or "User"
}

// ManifestConversionResponse represents GitHub's response from the manifest code exchange.
type ManifestConversionResponse struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	PEM           string `json:"pem"`
	WebhookSecret string `json:"webhook_secret"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
}

// InstallationTokenResponse represents GitHub's response for installation access tokens.
type InstallationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
