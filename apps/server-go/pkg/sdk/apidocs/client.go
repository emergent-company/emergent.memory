// Package apidocs provides the API Docs service client for the Emergent API SDK.
// This client accesses the built-in documentation endpoints (not OpenAPI/Swagger).
package apidocs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the API Docs endpoints.
type Client struct {
	http *http.Client
	base string
	auth auth.Provider
}

// NewClient creates a new API docs client.
// This is a non-context client (no org/project required).
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// --- Types ---

// DocumentMeta represents a document's metadata (without content).
type DocumentMeta struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	Path        string   `json:"path"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	LastUpdated string   `json:"lastUpdated"`
	ReadTime    int      `json:"readTime"`
	Related     []string `json:"related"`
}

// Document represents a full document with content.
type Document struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Category    string    `json:"category"`
	Path        string    `json:"path"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	LastUpdated string    `json:"lastUpdated"`
	ReadTime    int       `json:"readTime"`
	Related     []string  `json:"related"`
	Content     string    `json:"content"`
	ParsedAt    time.Time `json:"parsedAt"`
}

// ListDocumentsResponse is the response from listing documents.
type ListDocumentsResponse struct {
	Documents []DocumentMeta `json:"documents"`
	Total     int            `json:"total"`
}

// CategoryInfo represents a documentation category.
type CategoryInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}

// CategoriesResponse is the response from listing categories.
type CategoriesResponse struct {
	Categories []CategoryInfo `json:"categories"`
	Total      int            `json:"total"`
}

// --- Methods ---

// ListDocuments lists all documentation documents.
// GET /api/docs
func (c *Client) ListDocuments(ctx context.Context) (*ListDocumentsResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/docs", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ListDocumentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetDocument gets a specific documentation document by slug.
// GET /api/docs/:slug
func (c *Client) GetDocument(ctx context.Context, slug string) (*Document, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/docs/"+slug, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result Document
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetCategories gets all documentation categories.
// GET /api/docs/categories
func (c *Client) GetCategories(ctx context.Context) (*CategoriesResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/docs/categories", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result CategoriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
