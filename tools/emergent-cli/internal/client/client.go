package client

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/auth"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
)

// Client wraps http.Client with automatic authentication
type Client struct {
	httpClient *http.Client
	cfg        *config.Config
}

// New creates a new authenticated HTTP client
func New(cfg *config.Config) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cfg: cfg,
	}
}

// NewRequest creates an authenticated HTTP request
func (c *Client) NewRequest(method, path string, body io.Reader) (*http.Request, error) {
	url := c.cfg.ServerURL + path

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// If API key is configured, use X-API-Key header (standalone mode)
	if c.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", c.cfg.APIKey)
		return req, nil
	}

	// Otherwise, use OAuth Bearer token (full mode)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	credsPath := filepath.Join(homeDir, ".emergent", "credentials.json")
	creds, err := auth.Load(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not authenticated. Run 'emergent-cli login' first")
		}
		return nil, fmt.Errorf("failed to load credentials: %w", err)
	}

	// Check if token is expired and refresh if needed
	if creds.IsExpired() {
		if creds.RefreshToken == "" {
			return nil, fmt.Errorf("token expired and no refresh token available. Run 'emergent-cli login'")
		}

		// Discover OIDC config
		oidcConfig, err := auth.DiscoverOIDC(c.cfg.ServerURL)
		if err != nil {
			return nil, fmt.Errorf("failed to discover OIDC configuration: %w", err)
		}

		// Refresh the token
		tokenResp, err := auth.RefreshToken(oidcConfig, creds.RefreshToken, "emergent-cli")
		if err != nil {
			return nil, fmt.Errorf("failed to refresh token: %w", err)
		}

		// Update credentials
		creds.AccessToken = tokenResp.AccessToken
		if tokenResp.RefreshToken != "" {
			creds.RefreshToken = tokenResp.RefreshToken
		}
		creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

		// Save updated credentials
		if err := auth.Save(creds, credsPath); err != nil {
			return nil, fmt.Errorf("failed to save refreshed credentials: %w", err)
		}
	}

	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	return req, nil
}

// Do executes an authenticated HTTP request
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.httpClient.Do(req)
}

// Get performs a GET request with authentication
func (c *Client) Get(path string) (*http.Response, error) {
	req, err := c.NewRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

// Post performs a POST request with authentication
func (c *Client) Post(path string, contentType string, body io.Reader) (*http.Response, error) {
	req, err := c.NewRequest("POST", path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return c.Do(req)
}

// Delete performs a DELETE request with authentication
func (c *Client) Delete(path string) (*http.Response, error) {
	req, err := c.NewRequest("DELETE", path, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}
