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
	"strconv"
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
	// Version is the document's version number: 1 for an original, N for the
	// Nth revision of a source document.
	Version int `json:"version"`

	// Uploader (#2260): the display name of the person who uploaded the document,
	// resolved server-side. Absent for a document synced from a data source and
	// for rows written before the column existed.
	CreatedByUserID      string `json:"createdByUserId,omitempty"`
	CreatedByDisplayName string `json:"createdByDisplayName,omitempty"`

	// Source provenance (document-versioning / document-api): present on
	// documents ingested from a data source. SourceStatus is "active" or
	// "deleted_from_source"; the rest are absent on uploaded documents.
	SourceID         string `json:"sourceId,omitempty"`
	SourceExternalID string `json:"sourceExternalId,omitempty"`
	SourceRevisionID string `json:"sourceRevisionId,omitempty"`
	SourceStatus     string `json:"sourceStatus,omitempty"`
	SourceSyncedAt   string `json:"sourceSyncedAt,omitempty"`
	SourceDeletedAt  string `json:"sourceDeletedAt,omitempty"`
}

// Chunk is one chunk of a document (GET /api/chunks).
type Chunk struct {
	ID              string  `json:"id"`
	DocumentID      string  `json:"documentId"`
	DocumentTitle   string  `json:"documentTitle"`
	Index           int     `json:"index"`
	Size            int     `json:"size"`
	HasEmbedding    bool    `json:"hasEmbedding"`
	Text            string  `json:"text"`
	CreatedAt       string  `json:"createdAt"`
	ParentChunkID   *string `json:"parentChunkId,omitempty"`
	Edited          bool    `json:"edited"`
	CurrentRevision int     `json:"currentRevision"`
}

// documentHeaders returns the per-request headers for the document/chunk REST
// routes, which scope by the X-Project-ID header (unlike agent-definition
// routes that embed the project id in the path).
func (m *MemoryClient) documentHeaders(ctx context.Context) map[string]string {
	return map[string]string{"X-Project-ID": m.projectIDFor(ctx)}
}

// DocumentListParams is one request for a page of the documents listing. The
// zero value is the default view: newest-first, no filters or search. Values are
// validated server-side; the gateway normalizes them before sending so an
// out-of-vocabulary query parameter never reaches the API.
type DocumentListParams struct {
	Query      string // case-insensitive match over filename and source URL
	Status     string // processing|ready|extracted|failed, "" for all
	SourceType string // "upload", a data-source type, or "" for all
	Sort       string // created|updated
	Order      string // asc|desc
	Cursor     string
	Limit      int
}

// DocumentStatusCounts is the API's per-status breakdown. All ignores the status
// filter; the buckets honour the search and source filters, so the tabs stay
// stable while one is selected.
type DocumentStatusCounts struct {
	All        int `json:"all"`
	Processing int `json:"processing"`
	Ready      int `json:"ready"`
	Extracted  int `json:"extracted"`
	Failed     int `json:"failed"`
}

// DocumentPage is one page of the documents listing.
type DocumentPage struct {
	Documents    []Document
	Total        int
	StatusCounts DocumentStatusCounts
	NextCursor   string
}

// ListDocuments lists the project's documents with filtering, search, sorting
// and cursor pagination.
func (m *MemoryClient) ListDocuments(ctx context.Context, params DocumentListParams) (*DocumentPage, error) {
	if params.Limit <= 0 {
		params.Limit = documentPageSize
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(params.Limit))
	if params.Query != "" {
		q.Set("q", params.Query)
	}
	if params.Status != "" {
		q.Set("status", params.Status)
	}
	if params.SourceType != "" {
		q.Set("sourceType", params.SourceType)
	}
	if params.Sort != "" {
		q.Set("sort", params.Sort)
	}
	if params.Order != "" {
		q.Set("order", params.Order)
	}
	if params.Cursor != "" {
		q.Set("cursor", params.Cursor)
	}
	var out struct {
		Documents    []Document           `json:"documents"`
		Total        int                  `json:"total"`
		StatusCounts DocumentStatusCounts `json:"statusCounts"`
		NextCursor   string               `json:"next_cursor"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/documents?"+q.Encode(), nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &DocumentPage{
		Documents:    out.Documents,
		Total:        out.Total,
		StatusCounts: out.StatusCounts,
		NextCursor:   out.NextCursor,
	}, nil
}

// DocumentSourceType is one provenance value present in the project, with the
// number of documents behind it. It backs the source filter's options.
type DocumentSourceType struct {
	SourceType string `json:"sourceType"`
	Count      int    `json:"count"`
}

// ListDocumentSourceTypes lists the document source types present in the
// project with their counts (GET /api/documents/source-types).
func (m *MemoryClient) ListDocumentSourceTypes(ctx context.Context) ([]DocumentSourceType, error) {
	var out struct {
		SourceTypes []DocumentSourceType `json:"sourceTypes"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/documents/source-types", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.SourceTypes, nil
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

// --- chunk curation ---

// ChunkRevision is one version of a chunk's text (GET /api/chunks/:id/revisions).
type ChunkRevision struct {
	ID        string `json:"id"`
	ChunkID   string `json:"chunkId"`
	Seq       int    `json:"seq"`
	Text      string `json:"text"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

// ChunkEditResult reports the revision created by an edit or rollback.
type ChunkEditResult struct {
	ChunkID  string `json:"chunkId"`
	Revision int    `json:"revision"`
}

// ChunkDiffLine is one line of a diff between two chunk revisions.
type ChunkDiffLine struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ChunkDiff is the diff between two revisions of a chunk.
type ChunkDiff struct {
	From  int             `json:"from"`
	To    int             `json:"to"`
	Lines []ChunkDiffLine `json:"lines"`
}

// EditChunk replaces a chunk's text (PATCH /api/chunks/:id), recording a
// revision and scheduling re-embedding.
func (m *MemoryClient) EditChunk(ctx context.Context, chunkID, text string) (ChunkEditResult, error) {
	var out ChunkEditResult
	err := m.doH(ctx, http.MethodPatch, "/api/chunks/"+url.PathEscape(chunkID), map[string]any{"text": text}, m.documentHeaders(ctx), &out)
	return out, err
}

// ListChunkRevisions returns a chunk's revisions in ascending seq order.
func (m *MemoryClient) ListChunkRevisions(ctx context.Context, chunkID string) ([]ChunkRevision, error) {
	var out struct {
		Data []ChunkRevision `json:"data"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/chunks/"+url.PathEscape(chunkID)+"/revisions", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// DiffChunkRevisions returns the line diff between two revisions of a chunk.
func (m *MemoryClient) DiffChunkRevisions(ctx context.Context, chunkID string, from, to int) (ChunkDiff, error) {
	var out ChunkDiff
	path := fmt.Sprintf("/api/chunks/%s/revisions/diff?from=%d&to=%d", url.PathEscape(chunkID), from, to)
	err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out)
	return out, err
}

// RollbackChunk restores a previous revision's text as a new revision.
func (m *MemoryClient) RollbackChunk(ctx context.Context, chunkID string, seq int) (ChunkEditResult, error) {
	var out ChunkEditResult
	err := m.doH(ctx, http.MethodPost, "/api/chunks/"+url.PathEscape(chunkID)+"/rollback", map[string]any{"seq": seq}, m.documentHeaders(ctx), &out)
	return out, err
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

// ExtractionJob is one extraction job for a document (GET
// /api/admin/extraction-jobs). Fields are snake_case per the admin DTO; the
// timestamp fields are RFC3339 strings (empty when the server omitted a
// nullable time).
type ExtractionJob struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	Status     string `json:"status"`
	// DeadLettered is true when the job exhausted its retry budget. Status is
	// "failed" for both intermediate failures and dead letters.
	DeadLettered     bool     `json:"dead_lettered"`
	TotalItems       int      `json:"total_items"`
	ProcessedItems   int      `json:"processed_items"`
	SuccessfulItems  int      `json:"successful_items"`
	FailedItems      int      `json:"failed_items"`
	DiscoveredTypes  []string `json:"discovered_types"`
	CreatedObjects   []string `json:"created_objects"`
	ObjectsCreated   int      `json:"objects_created"`
	CreatedObjectIDs []string `json:"created_object_ids"`
	StagingBranchID  string   `json:"staging_branch_id"`
	// RunID is the agent run that records the job's pipeline transcript, when the
	// server recorded one. The chat rail opens it as a chat-shaped transcript;
	// legacy jobs (no run) link to the extraction history page instead.
	RunID        string  `json:"run_id,omitempty"`
	ErrorMessage *string `json:"error_message"`
	StartedAt    string  `json:"started_at"`
	CompletedAt  string  `json:"completed_at"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`

	// Trace fields, present only when the server recorded an OpenTelemetry
	// trace for the run. Absent on legacy jobs and when tracing is disabled.
	TraceID      string         `json:"trace_id,omitempty"`
	Spans        []TraceSpan    `json:"spans,omitempty"`
	TraceCostUSD *float64       `json:"trace_cost_usd,omitempty"`
	ErrorDetails map[string]any `json:"error_details,omitempty"`
	DebugInfo    map[string]any `json:"debug_info,omitempty"`
	RetryCount   int            `json:"retry_count"`
}

// ObjectIDs returns the object ids this job created, preferring the modern
// created_object_ids column and falling back to the legacy created_objects
// jsonb column for pre-migration jobs (which the worker never wrote).
func (j ExtractionJob) ObjectIDs() []string {
	if len(j.CreatedObjectIDs) > 0 {
		return j.CreatedObjectIDs
	}
	return j.CreatedObjects
}

// ObjectCount returns the number of objects this job created: the id-backed
// columns when present, else the reported objects_created count. Used by the
// extraction-history row badge.
func (j ExtractionJob) ObjectCount() int {
	if n := len(j.ObjectIDs()); n > 0 {
		return n
	}
	return j.ObjectsCreated
}

// ListExtractionJobs lists the extraction jobs for one document, newest first
// (GET /api/admin/extraction-jobs/projects/{projectID}?source_id=...&source_type=document).
// The response envelope is {success,data:{jobs:[...],total,page,limit,total_pages}}.
func (m *MemoryClient) ListExtractionJobs(ctx context.Context, documentID string) ([]ExtractionJob, error) {
	path := "/api/admin/extraction-jobs/projects/" + url.PathEscape(m.projectIDFor(ctx)) +
		"?source_id=" + url.QueryEscape(documentID) +
		"&source_type=document&limit=100"
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Jobs []ExtractionJob `json:"jobs"`
		} `json:"data"`
	}
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Data.Jobs, nil
}

// ListProjectExtractionJobs lists the project's extraction jobs, newest first,
// with no source filter (GET /api/admin/extraction-jobs/projects/{projectID}).
// It backs the chat rail's extraction origin, which shows a project's recent
// extraction runs rather than one document's history.
func (m *MemoryClient) ListProjectExtractionJobs(ctx context.Context, limit int) ([]ExtractionJob, error) {
	if limit <= 0 {
		limit = 50
	}
	path := "/api/admin/extraction-jobs/projects/" + url.PathEscape(m.projectIDFor(ctx)) +
		"?limit=" + strconv.Itoa(limit)
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Jobs []ExtractionJob `json:"jobs"`
		} `json:"data"`
	}
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Data.Jobs, nil
}

// GetExtractionJob fetches one extraction job by id (GET
// /api/admin/extraction-jobs/{jobId}), unwrapping the {success,data} envelope
// like ListExtractionJobs. The data payload carries the admin job DTO plus the
// trace fields (trace_id, spans, trace_cost_usd) when the server recorded an
// OpenTelemetry trace for the run.
func (m *MemoryClient) GetExtractionJob(ctx context.Context, jobID string) (*ExtractionJob, error) {
	var out struct {
		Success bool          `json:"success"`
		Data    ExtractionJob `json:"data"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/admin/extraction-jobs/"+url.PathEscape(jobID), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// DownloadDocument fetches a document's original file bytes (GET
// /api/documents/{id}/download). The raw body and its Content-Type are returned
// unparsed — the caller streams them to the response and owns the returned
// ReadCloser. The endpoint 307-redirects to a signed URL and the default client
// follows it. A non-2xx response is an error.
func (m *MemoryClient) DownloadDocument(ctx context.Context, id string) (io.ReadCloser, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+"/api/documents/"+id+"/download", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	for k, v := range m.documentHeaders(ctx) {
		req.Header.Set(k, v)
	}
	for k, v := range sessionHeaders(ctx) {
		req.Header.Set(k, v)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if rerr != nil {
			return nil, "", rerr
		}
		return nil, "", parseMemoryError(resp.StatusCode, raw)
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
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

// GetDocumentContent fetches a document's stored text content (GET
// /api/documents/{id}/content). The response is {"content": *string}; a
// document with no stored content (null/absent) yields "".
func (m *MemoryClient) GetDocumentContent(ctx context.Context, documentID string) (string, error) {
	var out struct {
		Content *string `json:"content"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/documents/"+documentID+"/content", nil, m.documentHeaders(ctx), &out); err != nil {
		return "", err
	}
	if out.Content == nil {
		return "", nil
	}
	return *out.Content, nil
}
