// Package modelconfig provides the ModelConfig service client for the Emergent API SDK.
// It covers project-level default generative and embedding model selection.
package modelconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the ModelConfig API.
type Client struct {
	http *http.Client
	base string
	auth auth.Provider
}

// NewClient creates a new modelconfig client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// ModelConfigResponse is the stored model config for a project.
type ModelConfigResponse struct {
	GenerativeModel string    `json:"generativeModel"`
	EmbeddingModel  string    `json:"embeddingModel"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// EffectiveModelConfig is the resolved model config with source info.
type EffectiveModelConfig struct {
	GenerativeModel       string `json:"generativeModel"`
	GenerativeModelSource string `json:"generativeModelSource"`
	EmbeddingModel        string `json:"embeddingModel"`
	EmbeddingModelSource  string `json:"embeddingModelSource"`
}

// UpsertRequest sets the generative and/or embedding model for a project.
type UpsertRequest struct {
	GenerativeModel string `json:"generativeModel"`
	EmbeddingModel  string `json:"embeddingModel"`
}

// Get returns the stored model config for a project.
func (c *Client) Get(ctx context.Context, projectID string) (*ModelConfigResponse, error) {
	var result ModelConfigResponse
	if err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/api/v1/projects/%s/model-config", url.PathEscape(projectID)),
		nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Upsert sets the generative and/or embedding model for a project.
func (c *Client) Upsert(ctx context.Context, projectID string, req *UpsertRequest) (*ModelConfigResponse, error) {
	var result ModelConfigResponse
	if err := c.doJSON(ctx, "PUT",
		fmt.Sprintf("/api/v1/projects/%s/model-config", url.PathEscape(projectID)),
		req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Delete removes the model config for a project.
func (c *Client) Delete(ctx context.Context, projectID string) error {
	return c.doJSON(ctx, "DELETE",
		fmt.Sprintf("/api/v1/projects/%s/model-config", url.PathEscape(projectID)),
		nil, nil)
}

// GetEffective returns the resolved effective model config for a project.
func (c *Client) GetEffective(ctx context.Context, projectID string) (*EffectiveModelConfig, error) {
	var result EffectiveModelConfig
	if err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/api/v1/projects/%s/model-config/effective", url.PathEscape(projectID)),
		nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("apply auth: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if out != nil {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}
		if len(respBody) > 0 {
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("unmarshal response: %w", err)
			}
		}
	}
	return nil
}
