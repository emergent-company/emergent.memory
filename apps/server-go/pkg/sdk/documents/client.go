// Package documents provides the Documents service client for the Emergent API SDK.
package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Documents API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// Document represents a document in the Emergent API.
type Document struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`

	// Basic metadata
	Filename  *string `json:"filename,omitempty"`
	SourceURL *string `json:"sourceUrl,omitempty"`
	MimeType  *string `json:"mimeType,omitempty"`

	// Content
	Content     *string `json:"content,omitempty"`
	ContentHash *string `json:"contentHash,omitempty"`
	FileHash    *string `json:"fileHash,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Hierarchy
	ParentDocumentID *string `json:"parentDocumentId,omitempty"`

	// Conversion status
	ConversionStatus      *string    `json:"conversionStatus,omitempty"`
	ConversionError       *string    `json:"conversionError,omitempty"`
	ConversionCompletedAt *time.Time `json:"conversionCompletedAt,omitempty"`

	// Storage
	StorageKey    *string `json:"storageKey,omitempty"`
	FileSizeBytes *int64  `json:"fileSizeBytes,omitempty"`
	StorageURL    *string `json:"storageUrl,omitempty"`

	// Data source
	SourceType              *string `json:"sourceType,omitempty"`
	DataSourceIntegrationID *string `json:"dataSourceIntegrationId,omitempty"`
	ExternalSourceID        *string `json:"externalSourceId,omitempty"`
	SyncVersion             *int    `json:"syncVersion,omitempty"`

	// Metadata
	IntegrationMetadata map[string]any `json:"integrationMetadata,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`

	// Computed fields
	Chunks           int     `json:"chunks"`
	EmbeddedChunks   int     `json:"embeddedChunks"`
	TotalChars       int     `json:"totalChars"`
	ExtractionStatus *string `json:"extractionStatus,omitempty"`
}

// ListOptions holds options for listing documents.
type ListOptions struct {
	Limit            int
	Cursor           string
	SourceType       string
	IntegrationID    string
	RootOnly         bool
	ParentDocumentID string
}

// ListResult represents the response from listing documents.
type ListResult struct {
	Documents  []Document `json:"documents"`
	Total      int        `json:"total"`
	NextCursor *string    `json:"next_cursor,omitempty"`
}

// CreateRequest is the request body for creating a document.
type CreateRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// DeleteResponse is the response for delete operations.
type DeleteResponse struct {
	Status   string         `json:"status"`
	Deleted  int            `json:"deleted,omitempty"`
	NotFound []string       `json:"notFound,omitempty"`
	Summary  *DeleteSummary `json:"summary,omitempty"`
}

// DeleteSummary contains counts of related entities deleted.
type DeleteSummary struct {
	Chunks             int `json:"chunks"`
	ExtractionJobs     int `json:"extractionJobs"`
	GraphObjects       int `json:"graphObjects"`
	GraphRelationships int `json:"graphRelationships"`
	Notifications      int `json:"notifications"`
}

// BulkDeleteRequest is the request body for bulk deleting documents.
type BulkDeleteRequest struct {
	IDs []string `json:"ids"`
}

// ContentResponse is the response for getting document content.
type ContentResponse struct {
	Content *string `json:"content"`
}

// DeletionImpact represents the impact of deleting a document.
type DeletionImpact struct {
	Document DocumentInfo  `json:"document"`
	Impact   ImpactSummary `json:"impact"`
}

// DocumentInfo is a simplified document representation for deletion impact.
type DocumentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

// ImpactSummary contains counts of entities that will be deleted.
type ImpactSummary struct {
	Chunks             int `json:"chunks"`
	ExtractionJobs     int `json:"extractionJobs"`
	GraphObjects       int `json:"graphObjects"`
	GraphRelationships int `json:"graphRelationships"`
	Notifications      int `json:"notifications"`
}

// BulkDeletionImpact represents the impact of bulk deleting documents.
type BulkDeletionImpact struct {
	TotalDocuments int              `json:"totalDocuments"`
	Impact         ImpactSummary    `json:"impact"`
	Documents      []DeletionImpact `json:"documents,omitempty"`
}

// BulkDeletionImpactRequest is the request body for bulk deletion impact.
type BulkDeletionImpactRequest struct {
	IDs []string `json:"ids"`
}

// UploadResponse is the response for single file upload.
type UploadResponse struct {
	Document           *DocumentSummary `json:"document"`
	IsDuplicate        bool             `json:"isDuplicate"`
	ExistingDocumentID *string          `json:"existingDocumentId,omitempty"`
}

// DocumentSummary is a simplified document for upload responses.
type DocumentSummary struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	MimeType         *string `json:"mimeType,omitempty"`
	FileSizeBytes    *int64  `json:"fileSizeBytes,omitempty"`
	ConversionStatus string  `json:"conversionStatus"`
	ConversionError  *string `json:"conversionError,omitempty"`
	StorageKey       *string `json:"storageKey,omitempty"`
	CreatedAt        string  `json:"createdAt"`
}

// BatchUploadResult is the response for batch file upload.
type BatchUploadResult struct {
	Summary BatchUploadSummary      `json:"summary"`
	Results []BatchUploadFileResult `json:"results"`
}

// BatchUploadSummary contains counts for batch upload results.
type BatchUploadSummary struct {
	Total      int `json:"total"`
	Successful int `json:"successful"`
	Duplicates int `json:"duplicates"`
	Failed     int `json:"failed"`
}

// BatchUploadFileResult represents the result for a single file in a batch upload.
type BatchUploadFileResult struct {
	Filename   string  `json:"filename"`
	Status     string  `json:"status"` // "success", "duplicate", "failed"
	DocumentID *string `json:"documentId,omitempty"`
	Chunks     *int    `json:"chunks,omitempty"`
	Error      *string `json:"error,omitempty"`
}

// SourceTypeWithCount represents a source type with its document count.
type SourceTypeWithCount struct {
	SourceType string `json:"sourceType"`
	Count      int    `json:"count"`
}

// UploadFileInput represents a file to upload.
type UploadFileInput struct {
	// Filename is the name of the file.
	Filename string
	// Reader provides the file content.
	Reader io.Reader
	// ContentType is the MIME type of the file (optional, auto-detected if empty).
	ContentType string
}

// NewClient creates a new Documents service client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider, orgID, projectID string) *Client {
	return &Client{
		http:      httpClient,
		base:      baseURL,
		auth:      authProvider,
		orgID:     orgID,
		projectID: projectID,
	}
}

// SetContext sets the organization and project context for all subsequent API calls.
func (c *Client) SetContext(orgID, projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgID = orgID
	c.projectID = projectID
}

// setHeaders adds auth and context headers to a request.
func (c *Client) setHeaders(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	return nil
}

// List retrieves a paginated list of documents.
//
// Example:
//
//	result, err := client.Documents.List(ctx, &documents.ListOptions{Limit: 50})
//	for _, doc := range result.Documents {
//	    fmt.Printf("%s: %s\n", doc.ID, *doc.Filename)
//	}
func (c *Client) List(ctx context.Context, opts *ListOptions) (*ListResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/documents", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	if opts != nil {
		q := req.URL.Query()
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Cursor != "" {
			q.Set("cursor", opts.Cursor)
		}
		if opts.SourceType != "" {
			q.Set("sourceType", opts.SourceType)
		}
		if opts.IntegrationID != "" {
			q.Set("integrationId", opts.IntegrationID)
		}
		if opts.RootOnly {
			q.Set("rootOnly", "true")
		}
		if opts.ParentDocumentID != "" {
			q.Set("parentDocumentId", opts.ParentDocumentID)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ListResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Get retrieves a single document by its ID.
//
// Example:
//
//	doc, err := client.Documents.Get(ctx, "doc-uuid")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Document: %s\n", *doc.Filename)
func (c *Client) Get(ctx context.Context, id string) (*Document, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/documents/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var doc Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &doc, nil
}

// Create creates a new document with inline content.
// Returns the created document. If an identical document exists (by content hash),
// the existing document is returned instead (deduplication).
//
// Example:
//
//	doc, err := client.Documents.Create(ctx, &documents.CreateRequest{
//	    Filename: "notes.txt",
//	    Content:  "Hello world",
//	})
func (c *Client) Create(ctx context.Context, createReq *CreateRequest) (*Document, error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/documents", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var doc Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &doc, nil
}

// Delete deletes a single document and all related entities.
//
// Example:
//
//	result, err := client.Documents.Delete(ctx, "doc-uuid")
func (c *Client) Delete(ctx context.Context, id string) (*DeleteResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/documents/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result DeleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// BulkDelete deletes multiple documents by their IDs.
//
// Example:
//
//	result, err := client.Documents.BulkDelete(ctx, []string{"id1", "id2", "id3"})
func (c *Client) BulkDelete(ctx context.Context, ids []string) (*DeleteResponse, error) {
	body, err := json.Marshal(BulkDeleteRequest{IDs: ids})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/documents", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result DeleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetContent retrieves the text content of a document.
//
// Example:
//
//	content, err := client.Documents.GetContent(ctx, "doc-uuid")
func (c *Client) GetContent(ctx context.Context, id string) (*string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/documents/"+id+"/content", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Content, nil
}

// Download returns a signed URL for downloading the original document file.
// The server returns a 307 redirect to the signed URL. This method follows
// the redirect and returns the final URL.
// Set CheckRedirect on the http.Client to http.ErrUseLastResponse to get the URL without following it.
//
// Example:
//
//	downloadURL, err := client.Documents.Download(ctx, "doc-uuid")
func (c *Client) Download(ctx context.Context, id string) (string, error) {
	// Use a client that does NOT follow redirects so we can capture the URL
	noRedirectClient := *c.http
	noRedirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/documents/"+id+"/download", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return "", err
	}

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return "", sdkerrors.ParseErrorResponse(resp)
	}

	if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		if location == "" {
			return "", fmt.Errorf("redirect response missing Location header")
		}
		return location, nil
	}

	return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

// GetSourceTypes returns a list of all available document source types with counts.
// This endpoint does NOT require a project ID.
//
// Example:
//
//	types, err := client.Documents.GetSourceTypes(ctx)
func (c *Client) GetSourceTypes(ctx context.Context) ([]SourceTypeWithCount, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/documents/source-types", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result struct {
		SourceTypes []SourceTypeWithCount `json:"sourceTypes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.SourceTypes, nil
}

// GetDeletionImpact previews the impact of deleting a document.
// Returns counts of related entities that would be deleted.
//
// Example:
//
//	impact, err := client.Documents.GetDeletionImpact(ctx, "doc-uuid")
func (c *Client) GetDeletionImpact(ctx context.Context, id string) (*DeletionImpact, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/documents/"+id+"/deletion-impact", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result DeletionImpact
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// BulkDeletionImpact previews the impact of bulk deleting multiple documents.
//
// Example:
//
//	impact, err := client.Documents.BulkDeletionImpact(ctx, []string{"id1", "id2"})
func (c *Client) BulkDeletionImpact(ctx context.Context, ids []string) (*BulkDeletionImpact, error) {
	body, err := json.Marshal(BulkDeletionImpactRequest{IDs: ids})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/documents/deletion-impact", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result BulkDeletionImpact
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Upload uploads a single file and creates a document record.
// Supports deduplication by file content hash.
//
// Example:
//
//	f, _ := os.Open("document.pdf")
//	defer f.Close()
//	result, err := client.Documents.Upload(ctx, &documents.UploadFileInput{
//	    Filename: "document.pdf",
//	    Reader:   f,
//	})
func (c *Client) Upload(ctx context.Context, input *UploadFileInput) (*UploadResponse, error) {
	return c.UploadWithOptions(ctx, input, false)
}

// UploadWithOptions uploads a single file with additional options.
// If autoExtract is true, triggers automatic extraction after upload.
func (c *Client) UploadWithOptions(ctx context.Context, input *UploadFileInput, autoExtract bool) (*UploadResponse, error) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", input.Filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, input.Reader); err != nil {
		return nil, fmt.Errorf("failed to write file content: %w", err)
	}

	if autoExtract {
		if err := writer.WriteField("autoExtract", "true"); err != nil {
			return nil, fmt.Errorf("failed to write autoExtract field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/documents/upload", body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// UploadBatch uploads multiple files in a single request.
// Max 100 files per batch, each max 10MB.
//
// Example:
//
//	results, err := client.Documents.UploadBatch(ctx, []documents.UploadFileInput{
//	    {Filename: "file1.txt", Reader: reader1},
//	    {Filename: "file2.txt", Reader: reader2},
//	})
func (c *Client) UploadBatch(ctx context.Context, files []UploadFileInput) (*BatchUploadResult, error) {
	return c.UploadBatchWithOptions(ctx, files, false)
}

// UploadBatchWithOptions uploads multiple files with additional options.
func (c *Client) UploadBatchWithOptions(ctx context.Context, files []UploadFileInput, autoExtract bool) (*BatchUploadResult, error) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	for _, f := range files {
		part, err := writer.CreateFormFile("files", f.Filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create form file for %s: %w", f.Filename, err)
		}
		if _, err := io.Copy(part, f.Reader); err != nil {
			return nil, fmt.Errorf("failed to write file content for %s: %w", f.Filename, err)
		}
	}

	if autoExtract {
		if err := writer.WriteField("autoExtract", "true"); err != nil {
			return nil, fmt.Errorf("failed to write autoExtract field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/documents/upload/batch", body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result BatchUploadResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
