// Package chunks provides the Chunks service client for the Emergent API SDK.
package chunks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Chunks API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// =============================================================================
// SDK Types
// =============================================================================

// ChunkMetadata contains metadata about how the chunk was created.
type ChunkMetadata struct {
	Strategy     string `json:"strategy,omitempty"`
	StartOffset  int    `json:"startOffset,omitempty"`
	EndOffset    int    `json:"endOffset,omitempty"`
	BoundaryType string `json:"boundaryType,omitempty"`
}

// Chunk represents a document chunk returned by the API.
type Chunk struct {
	ID             string         `json:"id"`
	DocumentID     string         `json:"documentId"`
	DocumentTitle  string         `json:"documentTitle,omitempty"`
	Index          int            `json:"index"`
	Size           int            `json:"size"`
	HasEmbedding   bool           `json:"hasEmbedding"`
	Text           string         `json:"text"`
	CreatedAt      string         `json:"createdAt,omitempty"`
	Metadata       *ChunkMetadata `json:"metadata,omitempty"`
	TotalChars     *int           `json:"totalChars,omitempty"`
	ChunkCount     *int           `json:"chunkCount,omitempty"`
	EmbeddedChunks *int           `json:"embeddedChunks,omitempty"`
}

// ListOptions holds query parameters for listing chunks.
type ListOptions struct {
	DocumentID string
}

// ListResponse is the response from listing chunks.
type ListResponse struct {
	Data       []*Chunk `json:"data"`
	TotalCount int      `json:"totalCount"`
}

// DeletionResult represents the result of a single chunk deletion.
type DeletionResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// BulkDeletionSummary is the response for bulk chunk deletion.
type BulkDeletionSummary struct {
	TotalRequested int               `json:"totalRequested"`
	TotalDeleted   int               `json:"totalDeleted"`
	TotalFailed    int               `json:"totalFailed"`
	Results        []*DeletionResult `json:"results"`
}

// DocumentChunksDeletionResult is the result of deleting all chunks for a document.
type DocumentChunksDeletionResult struct {
	DocumentID    string `json:"documentId"`
	ChunksDeleted int    `json:"chunksDeleted"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
}

// BulkDocumentChunksDeletionSummary is the response for bulk document chunks deletion.
type BulkDocumentChunksDeletionSummary struct {
	TotalDocuments int                             `json:"totalDocuments"`
	TotalChunks    int                             `json:"totalChunks"`
	Results        []*DocumentChunksDeletionResult `json:"results"`
}

// =============================================================================
// Constructor / context
// =============================================================================

// NewClient creates a new Chunks service client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider, orgID, projectID string) *Client {
	return &Client{
		http:      httpClient,
		base:      baseURL,
		auth:      authProvider,
		orgID:     orgID,
		projectID: projectID,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.orgID = orgID
	c.projectID = projectID
}

// =============================================================================
// Internal helpers
// =============================================================================

func (c *Client) prepareRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}

	return req, nil
}

func (c *Client) doJSON(req *http.Request, result any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	return nil
}

// =============================================================================
// Operations
// =============================================================================

// List retrieves chunks, optionally filtered by document ID.
// Server: GET /api/chunks?documentId=...
func (c *Client) List(ctx context.Context, opts *ListOptions) (*ListResponse, error) {
	req, err := c.prepareRequest(ctx, "GET", c.base+"/api/chunks", nil)
	if err != nil {
		return nil, err
	}

	if opts != nil {
		q := req.URL.Query()
		if opts.DocumentID != "" {
			q.Set("documentId", opts.DocumentID)
		}
		req.URL.RawQuery = q.Encode()
	}

	var result ListResponse
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Delete removes a single chunk by ID.
// Server: DELETE /api/chunks/:id → 204
func (c *Client) Delete(ctx context.Context, id string) error {
	req, err := c.prepareRequest(ctx, "DELETE", c.base+"/api/chunks/"+id, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// BulkDelete deletes multiple chunks by their IDs.
// Server: DELETE /api/chunks (body: {ids: [...]}) → BulkDeletionSummary
func (c *Client) BulkDelete(ctx context.Context, ids []string) (*BulkDeletionSummary, error) {
	body, err := json.Marshal(map[string][]string{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, "DELETE", c.base+"/api/chunks", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var result BulkDeletionSummary
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteByDocument deletes all chunks for a specific document.
// Server: DELETE /api/chunks/by-document/:documentId → DocumentChunksDeletionResult
func (c *Client) DeleteByDocument(ctx context.Context, documentID string) (*DocumentChunksDeletionResult, error) {
	req, err := c.prepareRequest(ctx, "DELETE", c.base+"/api/chunks/by-document/"+documentID, nil)
	if err != nil {
		return nil, err
	}

	var result DocumentChunksDeletionResult
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// BulkDeleteByDocuments deletes all chunks for multiple documents.
// Server: DELETE /api/chunks/by-documents (body: {documentIds: [...]}) → BulkDocumentChunksDeletionSummary
func (c *Client) BulkDeleteByDocuments(ctx context.Context, documentIDs []string) (*BulkDocumentChunksDeletionSummary, error) {
	body, err := json.Marshal(map[string][]string{"documentIds": documentIDs})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, "DELETE", c.base+"/api/chunks/by-documents", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var result BulkDocumentChunksDeletionSummary
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
