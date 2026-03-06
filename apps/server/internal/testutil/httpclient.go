package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"
)

// HTTPClient is an HTTP-only test client that can hit either:
// - An in-process test server (via httptest)
// - An external server (via real HTTP)
//
// This allows the same tests to run against both Go and NestJS implementations.
type HTTPClient struct {
	// For in-process testing
	inProcessHandler http.Handler

	// For external server testing
	baseURL    string
	httpClient *http.Client
}

// HTTPResponse wraps both httptest.ResponseRecorder and http.Response
// to provide a unified interface for tests.
type HTTPResponse struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// NewHTTPClient creates a new HTTP client.
// If TEST_SERVER_URL env var is set, it uses that for external server testing.
// Otherwise, it requires an in-process handler.
func NewHTTPClient(handler http.Handler) *HTTPClient {
	baseURL := os.Getenv("TEST_SERVER_URL")

	client := &HTTPClient{
		inProcessHandler: handler,
		baseURL:          baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	return client
}

// NewExternalHTTPClient creates a client for external server testing only.
// baseURL should be like "http://localhost:3002" or "http://localhost:3000"
func NewExternalHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// IsExternal returns true if this client hits an external server
func (c *HTTPClient) IsExternal() bool {
	return c.baseURL != ""
}

// BaseURL returns the base URL for external servers, or empty for in-process
func (c *HTTPClient) BaseURL() string {
	return c.baseURL
}

// Request performs an HTTP request
func (c *HTTPClient) Request(method, path string, opts ...RequestOption) *HTTPResponse {
	if c.IsExternal() {
		return c.externalRequest(method, path, opts...)
	}
	return c.inProcessRequest(method, path, opts...)
}

// inProcessRequest handles requests to in-process test server
func (c *HTTPClient) inProcessRequest(method, path string, opts ...RequestOption) *HTTPResponse {
	req := httptest.NewRequest(method, path, nil)

	// Apply options
	for _, opt := range opts {
		opt(req)
	}

	rec := httptest.NewRecorder()
	c.inProcessHandler.ServeHTTP(rec, req)

	return &HTTPResponse{
		StatusCode: rec.Code,
		Body:       rec.Body.Bytes(),
		Headers:    rec.Header(),
	}
}

// externalRequest handles requests to external server
func (c *HTTPClient) externalRequest(method, path string, opts ...RequestOption) *HTTPResponse {
	// Build full URL
	url := c.baseURL + path

	// Create a temporary request to collect options
	tempReq := httptest.NewRequest(method, path, nil)
	for _, opt := range opts {
		opt(tempReq)
	}

	// Create the real request
	var body io.Reader
	if tempReq.Body != nil {
		bodyBytes, _ := io.ReadAll(tempReq.Body)
		body = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return &HTTPResponse{StatusCode: 0, Body: []byte(err.Error())}
	}

	// Copy headers from temp request
	for k, v := range tempReq.Header {
		req.Header[k] = v
	}

	// Perform request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &HTTPResponse{StatusCode: 0, Body: []byte(err.Error())}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	return &HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Headers:    resp.Header,
	}
}

// GET performs a GET request
func (c *HTTPClient) GET(path string, opts ...RequestOption) *HTTPResponse {
	return c.Request(http.MethodGet, path, opts...)
}

// POST performs a POST request
func (c *HTTPClient) POST(path string, opts ...RequestOption) *HTTPResponse {
	return c.Request(http.MethodPost, path, opts...)
}

// PUT performs a PUT request
func (c *HTTPClient) PUT(path string, opts ...RequestOption) *HTTPResponse {
	return c.Request(http.MethodPut, path, opts...)
}

// DELETE performs a DELETE request
func (c *HTTPClient) DELETE(path string, opts ...RequestOption) *HTTPResponse {
	return c.Request(http.MethodDelete, path, opts...)
}

// PATCH performs a PATCH request
func (c *HTTPClient) PATCH(path string, opts ...RequestOption) *HTTPResponse {
	return c.Request(http.MethodPatch, path, opts...)
}

// JSON unmarshals the response body into v
func (r *HTTPResponse) JSON(v any) error {
	return json.Unmarshal(r.Body, v)
}

// String returns the response body as a string
func (r *HTTPResponse) String() string {
	return string(r.Body)
}

// ============================================================================
// API Helper Methods for Creating Test Fixtures via HTTP
// ============================================================================

// CreateOrg creates an organization via API and returns its ID
func (c *HTTPClient) CreateOrg(name string, authToken string) (string, error) {
	resp := c.POST("/api/orgs",
		WithAuth(authToken),
		WithJSONBody(map[string]any{"name": name}),
	)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to create org: status %d, body: %s", resp.StatusCode, resp.String())
	}

	var result map[string]any
	if err := resp.JSON(&result); err != nil {
		return "", fmt.Errorf("failed to parse org response: %w", err)
	}

	id, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("org response missing id field")
	}

	return id, nil
}

// DeleteOrg deletes an organization via API
func (c *HTTPClient) DeleteOrg(orgID, authToken string) error {
	resp := c.DELETE(fmt.Sprintf("/api/orgs/%s", orgID),
		WithAuth(authToken),
	)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete org: status %d, body: %s", resp.StatusCode, resp.String())
	}

	return nil
}

// CreateProject creates a project via API and returns its ID
func (c *HTTPClient) CreateProject(name, orgID, authToken string) (string, error) {
	resp := c.POST("/api/projects",
		WithAuth(authToken),
		WithOrgID(orgID),
		WithJSONBody(map[string]any{
			"name":  name,
			"orgId": orgID,
		}),
	)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to create project: status %d, body: %s", resp.StatusCode, resp.String())
	}

	var result map[string]any
	if err := resp.JSON(&result); err != nil {
		return "", fmt.Errorf("failed to parse project response: %w", err)
	}

	id, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("project response missing id field")
	}

	return id, nil
}

// CreateDocument creates a document via API and returns its ID
func (c *HTTPClient) CreateDocument(projectID, orgID, authToken, filename, content string) (string, error) {
	resp := c.POST("/api/documents",
		WithAuth(authToken),
		WithProjectID(projectID),
		WithOrgID(orgID),
		WithJSONBody(map[string]any{
			"filename": filename,
			"content":  content,
		}),
	)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to create document: status %d, body: %s", resp.StatusCode, resp.String())
	}

	var result map[string]any
	if err := resp.JSON(&result); err != nil {
		return "", fmt.Errorf("failed to parse document response: %w", err)
	}

	id, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("document response missing id field")
	}

	return id, nil
}

// ListDocuments lists documents and returns the response
func (c *HTTPClient) ListDocuments(projectID, orgID, authToken string) (*DocumentsListResponse, error) {
	resp := c.GET("/api/documents",
		WithAuth(authToken),
		WithProjectID(projectID),
		WithOrgID(orgID),
	)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list documents: status %d, body: %s", resp.StatusCode, resp.String())
	}

	var result DocumentsListResponse
	if err := resp.JSON(&result); err != nil {
		return nil, fmt.Errorf("failed to parse documents response: %w", err)
	}

	return &result, nil
}

// GetDocument gets a document by ID
func (c *HTTPClient) GetDocument(docID, projectID, orgID, authToken string) (*HTTPResponse, error) {
	resp := c.GET(fmt.Sprintf("/api/documents/%s", docID),
		WithAuth(authToken),
		WithProjectID(projectID),
		WithOrgID(orgID),
	)
	return resp, nil
}

// DeleteDocument deletes a document by ID
func (c *HTTPClient) DeleteDocument(docID, projectID, orgID, authToken string) (*HTTPResponse, error) {
	resp := c.DELETE(fmt.Sprintf("/api/documents/%s", docID),
		WithAuth(authToken),
		WithProjectID(projectID),
		WithOrgID(orgID),
	)
	return resp, nil
}

// ListChunks lists chunks and returns the response
func (c *HTTPClient) ListChunks(projectID, orgID, authToken string, queryParams map[string]string) (*ChunksListResponse, error) {
	path := "/api/chunks"
	if len(queryParams) > 0 {
		params := make([]string, 0, len(queryParams))
		for k, v := range queryParams {
			params = append(params, fmt.Sprintf("%s=%s", k, v))
		}
		path += "?" + strings.Join(params, "&")
	}

	resp := c.GET(path,
		WithAuth(authToken),
		WithProjectID(projectID),
		WithOrgID(orgID),
	)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list chunks: status %d, body: %s", resp.StatusCode, resp.String())
	}

	var result ChunksListResponse
	if err := resp.JSON(&result); err != nil {
		return nil, fmt.Errorf("failed to parse chunks response: %w", err)
	}

	return &result, nil
}

// ============================================================================
// Response Types
// ============================================================================

// DocumentsListResponse represents the response from listing documents
type DocumentsListResponse struct {
	Documents  []map[string]any `json:"documents"`
	Total      int              `json:"total"`
	NextCursor *string          `json:"next_cursor,omitempty"`
}

// ChunksListResponse represents the response from listing chunks
type ChunksListResponse struct {
	Data       []map[string]any `json:"data"`
	TotalCount int              `json:"totalCount"`
}
