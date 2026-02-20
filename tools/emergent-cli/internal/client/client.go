package client

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
)

// Client wraps the SDK client for CLI usage
type Client struct {
	SDK *sdk.Client
	cfg *config.Config
}

// New creates a new CLI client using the SDK
func New(cfg *config.Config) (*Client, error) {
	// Determine authentication mode
	var authConfig sdk.AuthConfig

	if cfg.APIKey != "" {
		// Standalone mode: Use API key
		authConfig = sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: cfg.APIKey,
		}
	} else {
		// Full mode: Use OAuth with credential file
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}

		authConfig = sdk.AuthConfig{
			Mode:      "oauth",
			ClientID:  "emergent-cli",
			CredsPath: filepath.Join(homeDir, ".emergent", "credentials.json"),
		}
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
		SDK: sdkClient,
		cfg: cfg,
	}, nil
}

// SetContext updates the organization and project context
func (c *Client) SetContext(orgID, projectID string) {
	c.SDK.SetContext(orgID, projectID)
	c.cfg.OrgID = orgID
	c.cfg.ProjectID = projectID
}
func (c *Client) ProjectID() string {
	return c.cfg.ProjectID
}
