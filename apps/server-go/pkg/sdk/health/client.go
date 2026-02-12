// Package health provides the Health service client for the Emergent API SDK.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Health API.
type Client struct {
	http *http.Client
	base string
}

// NewClient creates a new health client.
func NewClient(httpClient *http.Client, baseURL string) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string           `json:"status"`
	Timestamp string           `json:"timestamp"`
	Uptime    string           `json:"uptime"`
	Version   string           `json:"version"`
	Checks    map[string]Check `json:"checks"`
}

// Check represents an individual health check result
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Health returns the overall service health.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request (no auth required for health endpoints)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors (503 is acceptable for unhealthy state)
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusServiceUnavailable {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Parse response
	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &health, nil
}

// Ready returns readiness status (for k8s readiness probes).
func (c *Client) Ready(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/ready", nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request (no auth required)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 200 = ready, 503 = not ready, anything else is an error
	if resp.StatusCode == http.StatusOK {
		return true, nil
	} else if resp.StatusCode == http.StatusServiceUnavailable {
		return false, nil
	}

	return false, sdkerrors.ParseErrorResponse(resp)
}

// Healthz returns a simple health check (for k8s liveness probe).
func (c *Client) Healthz(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request (no auth required)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz check failed with status %d", resp.StatusCode)
	}

	return nil
}
