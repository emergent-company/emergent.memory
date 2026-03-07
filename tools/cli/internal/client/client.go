package client

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	cliauth "github.com/emergent-company/emergent.memory/tools/cli/internal/auth"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
)

// oauthClientID is the client ID for the memory-cli OAuth app.
const oauthClientID = "362800068257972227"

// Client wraps the SDK client for CLI usage
type Client struct {
	SDK       *sdk.Client
	cfg       *config.Config
	authToken string // effective bearer token, set during New()
}

// New creates a new CLI client using the SDK
func New(cfg *config.Config) (*Client, error) {
	// Determine authentication mode
	var authConfig sdk.AuthConfig
	var effectiveToken string

	if cfg.ProjectToken != "" {
		// Project Token mode: Use as API key
		authConfig = sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: cfg.ProjectToken,
		}
		effectiveToken = cfg.ProjectToken
	} else if cfg.APIKey != "" {
		// Standalone mode: Use API key
		authConfig = sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: cfg.APIKey,
		}
		effectiveToken = cfg.APIKey
	} else {
		// Full mode: load token from credentials.json (written by `memory login`
		// or `memory auth set-token`) and use it as a Bearer token. This avoids
		// the live OIDC discovery that sdk.NewWithDeviceFlow() requires.
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}

		credsPath := filepath.Join(homeDir, ".memory", "credentials.json")
		creds, err := cliauth.Load(credsPath)
		if err != nil {
			return nil, fmt.Errorf("not authenticated: %w\nRun 'memory login' or 'memory auth set-token <token>'", err)
		}
		if creds.IsExpired() {
			if creds.RefreshToken == "" || creds.IssuerURL == "" {
				return nil, fmt.Errorf("credentials expired — run 'memory login' or 'memory auth set-token <token>'")
			}
			oidcConfig, err := cliauth.DiscoverOIDC(creds.IssuerURL)
			if err != nil {
				return nil, fmt.Errorf("credentials expired and could not refresh — run 'memory login'")
			}
			tokenResp, err := cliauth.RefreshToken(oidcConfig, creds.RefreshToken, oauthClientID)
			if err != nil {
				return nil, fmt.Errorf("credentials expired and refresh failed — run 'memory login'")
			}
			creds.AccessToken = tokenResp.AccessToken
			creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
			if tokenResp.RefreshToken != "" {
				creds.RefreshToken = tokenResp.RefreshToken
			}
			_ = cliauth.Save(creds, credsPath) // persist silently; ignore error
		}
		authConfig = sdk.AuthConfig{
			Mode:   "apitoken",
			APIKey: creds.AccessToken,
		}
		effectiveToken = creds.AccessToken
	}

	// Create SDK client
	sdkConfig := sdk.Config{
		ServerURL: cfg.ServerURL,
		Auth:      authConfig,
	}

	// Set context if provided
	if cfg.OrgID != "" {
		sdkConfig.OrgID = cfg.OrgID
	}
	if cfg.ProjectID != "" {
		sdkConfig.ProjectID = cfg.ProjectID
	}

	sdkClient, err := sdk.New(sdkConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create SDK client: %w", err)
	}

	return &Client{
		SDK:       sdkClient,
		cfg:       cfg,
		authToken: effectiveToken,
	}, nil
}

// SetContext updates the organization and project context
func (c *Client) SetContext(orgID, projectID string) {
	c.SDK.SetContext(orgID, projectID)
	c.cfg.OrgID = orgID
	c.cfg.ProjectID = projectID
}

// ProjectID returns the current project ID
func (c *Client) ProjectID() string {
	return c.cfg.ProjectID
}

// BaseURL returns the server URL
func (c *Client) BaseURL() string {
	return c.cfg.ServerURL
}

// APIKey returns the API key if configured
func (c *Client) APIKey() string {
	return c.cfg.APIKey
}

// AuthorizationHeader returns the value to use for the Authorization header,
// derived from whichever credential source was configured: project token,
// standalone API key, or the access token loaded from credentials.json.
// Returns an empty string if no credential is available.
func (c *Client) AuthorizationHeader() string {
	if c.authToken != "" {
		return "Bearer " + c.authToken
	}
	return ""
}

// HasProjectToken reports whether the client was configured with a project-scoped
// token (MEMORY_PROJECT_TOKEN). When true the token already identifies a single
// project, so interactive project selection can be skipped.
func (c *Client) HasProjectToken() bool {
	return c.cfg.ProjectToken != ""
}

// HasProjectScope reports whether the client has any project scope set — either
// via a project token (MEMORY_PROJECT_TOKEN) or a pre-resolved project ID
// (MEMORY_PROJECT_ID / MEMORY_PROJECT name resolution). When true, interactive
// project selection can be skipped.
func (c *Client) HasProjectScope() bool {
	return c.cfg.ProjectToken != "" || c.cfg.ProjectID != ""
}
