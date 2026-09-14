package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

// --- documents ---

// Document is one ingested document from the memory service (GET
// /api/documents). Nullable/omitempty fields degrade to zero values.
type Document struct {
	ID                   string `json:"id"`
	Filename             string `json:"filename"`
	MimeType             string `json:"mimeType"`
	SourceType           string `json:"sourceType"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
	Chunks               int    `json:"chunks"`
	EmbeddedChunks       int    `json:"embeddedChunks"`
	TotalChars           int    `json:"totalChars"`
	ExtractionStatus     string `json:"extractionStatus"`
	ProcessingStatus     string `json:"processingStatus"`
	ObjectsCreated       int    `json:"objectsCreated"`
	RelationshipsCreated int    `json:"relationshipsCreated"`
}

// Chunk is one chunk of a document (GET /api/chunks).
type Chunk struct {
	ID            string `json:"id"`
	DocumentID    string `json:"documentId"`
	DocumentTitle string `json:"documentTitle"`
	Index         int    `json:"index"`
	Size          int    `json:"size"`
	HasEmbedding  bool   `json:"hasEmbedding"`
	Text          string `json:"text"`
	CreatedAt     string `json:"createdAt"`
}

// documentHeaders returns the per-request headers for the document/chunk REST
// routes, which scope by the X-Project-ID header (unlike agent-definition
// routes that embed the project id in the path).
func (m *MemoryClient) documentHeaders(ctx context.Context) map[string]string {
	return map[string]string{"X-Project-ID": m.projectIDFor(ctx)}
}

// ListDocuments lists the project's documents with cursor pagination, returning
// the documents and the next-page cursor (empty when there are no more).
func (m *MemoryClient) ListDocuments(ctx context.Context, cursor string) ([]Document, string, error) {
	path := "/api/documents?limit=100"
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	var out struct {
		Documents  []Document `json:"documents"`
		Total      int        `json:"total"`
		NextCursor string     `json:"next_cursor"`
	}
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, "", err
	}
	return out.Documents, out.NextCursor, nil
}

// GetDocument fetches a single document by id.
func (m *MemoryClient) GetDocument(ctx context.Context, id string) (*Document, error) {
	var out Document
	if err := m.doH(ctx, http.MethodGet, "/api/documents/"+id, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDocument deletes a document and all its related entities (chunks,
// extraction jobs, graph objects) via DELETE /api/documents/:id. The backend
// owns the cascade; a 404 surfaces as an error like every other memory 4xx.
func (m *MemoryClient) DeleteDocument(ctx context.Context, id string) error {
	return m.doH(ctx, http.MethodDelete, "/api/documents/"+id, nil, m.documentHeaders(ctx), nil)
}

// ListChunks lists the chunks of one document in index order.
func (m *MemoryClient) ListChunks(ctx context.Context, documentID string) ([]Chunk, error) {
	var out struct {
		Data       []Chunk `json:"data"`
		TotalCount int     `json:"totalCount"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/chunks?documentId="+url.QueryEscape(documentID), nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// UploadDocument uploads a file as a new document (multipart POST
// /api/documents/upload) and returns the new document id.
func (m *MemoryClient) UploadDocument(ctx context.Context, filename string, r io.Reader) (UploadResult, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return UploadResult{}, err
	}
	if _, err := io.Copy(part, r); err != nil {
		return UploadResult{}, err
	}
	if err := w.Close(); err != nil {
		return UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/documents/upload", &buf)
	if err != nil {
		return UploadResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	req.Header.Set("X-Project-ID", m.projectIDFor(ctx))
	req.Header.Set("Content-Type", w.FormDataContentType())
	for k, v := range sessionHeaders(ctx) {
		req.Header.Set(k, v)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return UploadResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return UploadResult{}, err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error apiError `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Code != "" {
			return UploadResult{}, fmt.Errorf("memory %d %s: %s", resp.StatusCode, e.Error.Code, e.Error.Message)
		}
		return UploadResult{}, fmt.Errorf("memory %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
		IsDuplicate        bool   `json:"isDuplicate"`
		ExistingDocumentID string `json:"existingDocumentId"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return UploadResult{}, err
	}
	if out.Document.ID == "" {
		return UploadResult{}, fmt.Errorf("memory upload returned no document id: %s", string(raw))
	}
	return UploadResult{
		DocumentID:         out.Document.ID,
		IsDuplicate:        out.IsDuplicate,
		ExistingDocumentID: out.ExistingDocumentID,
	}, nil
}

// CreateExtractionJob triggers object extraction on a document (POST
// /api/admin/extraction-jobs) and returns the created job id.
func (m *MemoryClient) CreateExtractionJob(ctx context.Context, documentID string) (string, error) {
	body := map[string]any{
		"project_id":        m.projectIDFor(ctx),
		"source_type":       "document",
		"source_id":         documentID,
		"extraction_config": map[string]any{},
	}
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := m.do(ctx, http.MethodPost, "/api/admin/extraction-jobs", body, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", fmt.Errorf("memory extraction job returned no id")
	}
	return out.Data.ID, nil
}

// ExtractionSummary is the result of a document's most recent completed
// extraction job (GET /api/documents/:id/extraction-summary).
type ExtractionSummary struct {
	JobID                string         `json:"jobId"`
	ObjectsCreated       int            `json:"objectsCreated"`
	RelationshipsCreated int            `json:"relationshipsCreated"`
	ObjectsByType        map[string]int `json:"objectsByType"`
	ObjectIDs            []string       `json:"objectIds"`
	CompletedAt          string         `json:"completedAt"`
}

// GetExtractionSummary fetches the most recent completed extraction summary
// for a document. Returns (nil, nil) when no completed extraction exists.
func (m *MemoryClient) GetExtractionSummary(ctx context.Context, documentID string) (*ExtractionSummary, error) {
	var out ExtractionSummary
	if err := m.doH(ctx, http.MethodGet, "/api/documents/"+documentID+"/extraction-summary", nil, m.documentHeaders(ctx), &out); err != nil {
		if isMemoryNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}
