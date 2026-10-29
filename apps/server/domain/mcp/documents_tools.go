package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

const maxUploadBytes = 10 * 1024 * 1024 // 10 MB

// ============================================================================
// Documents Tool Definitions
// ============================================================================

func documentsToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:          "document-list",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "documents:read",
			Description:   "List documents in the current project as metadata (id, filename, sourceType, mimeType, chunks, embeddedChunks, totalChars, fileSizeBytes, statuses, timestamps). Document bodies are NOT included by default — pass include_content=true only when you need the text, and prefer document-get or search-hybrid for a single document. Paginate with next_cursor/cursor.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"limit": {
						Type:        "integer",
						Description: "Maximum number of documents to return (default 50, max 500)",
					},
					"source_type": {
						Type:        "string",
						Description: "Filter by source type (e.g. 'upload', 'github', 'web', 'repo')",
					},
					"cursor": {
						Type:        "string",
						Description: "Opaque pagination cursor from a previous response's next_cursor. Omit for the first page.",
					},
					"include_content": {
						Type:        "boolean",
						Description: "When true, each document also carries its full content. Large corpora make this payload huge (megabytes) — leave it off unless you know the result set is small.",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:          "document-get",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "documents:read",
			Description:   "Get a single document by its ID. Returns full document metadata including conversion status, storage info, and chunk counts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"document_id": {
						Type:        "string",
						Description: "UUID of the document to retrieve",
					},
				},
				Required: []string{"document_id"},
			},
		},
		{
			Name:          "document-upload",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "documents:write",
			Description: "Upload a document to the current project by providing its content as a base64-encoded string. " +
				"The decoded content must not exceed 10 MB. Returns the created document id, title, and conversion status.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"filename": {
						Type:        "string",
						Description: "Original filename including extension (e.g. 'report.pdf', 'notes.md')",
					},
					"content_base64": {
						Type:        "string",
						Description: "Base64-encoded file content",
					},
					"mime_type": {
						Type:        "string",
						Description: "MIME type of the file (e.g. 'application/pdf', 'text/markdown'). Optional — inferred from filename if omitted.",
					},
				},
				Required: []string{"filename", "content_base64"},
			},
		},
		{
			Name:          "document-delete",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "documents:write",
			Description:   "Delete a document and all its associated chunks from the current project.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"document_id": {
						Type:        "string",
						Description: "UUID of the document to delete",
					},
				},
				Required: []string{"document_id"},
			},
		},
	}
}

// ============================================================================
// Documents Tool Handlers
// ============================================================================

func (s *Service) executeListDocuments(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	limit := 50
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	params := documents.ListParams{
		ProjectID: projectID,
		Limit:     limit,
	}
	if st, ok := args["source_type"].(string); ok && st != "" {
		params.SourceType = &st
	}
	// Without cursor support a 900-document project could only ever be paged by
	// re-reading the same first page, and without the source_type filter it was
	// impossible to see anything past the first page at all.
	if cur, ok := args["cursor"].(string); ok && cur != "" {
		parsed, err := documents.ParseCursor(cur)
		if err != nil {
			return nil, fmt.Errorf("list_documents: invalid cursor: %w", err)
		}
		params.Cursor = parsed
	}

	result, err := s.documentsSvc.List(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list_documents: %w", err)
	}

	includeContent, _ := args["include_content"].(bool)
	docs := make([]map[string]any, 0, len(result.Documents))
	for i := range result.Documents {
		docs = append(docs, documentSummary(&result.Documents[i], includeContent))
	}
	resp := map[string]any{
		"documents":   docs,
		"total":       result.Total,
		"next_cursor": result.NextCursor,
	}
	return s.wrapResult(resp)
}

// documentSummary projects a document onto the fields an agent needs to decide
// what to read next. Content is the one field that must stay opt-in: a full
// listing of a large corpus inlined every document body (measured: 1 MB for 50
// documents), which blew the caller's context and was then compressed away to a
// preview the caller could not expand.
func documentSummary(d *documents.Document, includeContent bool) map[string]any {
	sum := map[string]any{
		"id":             d.ID,
		"chunks":         d.Chunks,
		"embeddedChunks": d.EmbeddedChunks,
		"totalChars":     d.TotalChars,
	}
	if d.Filename != nil && *d.Filename != "" {
		sum["filename"] = *d.Filename
	}
	if d.SourceURL != nil && *d.SourceURL != "" {
		sum["sourceUrl"] = *d.SourceURL
	}
	if d.MimeType != nil && *d.MimeType != "" {
		sum["mimeType"] = *d.MimeType
	}
	if d.SourceType != nil && *d.SourceType != "" {
		sum["sourceType"] = *d.SourceType
	}
	if d.SourceExternalID != nil && *d.SourceExternalID != "" {
		sum["sourceExternalId"] = *d.SourceExternalID
	}
	if d.ConversionStatus != nil && *d.ConversionStatus != "" {
		sum["conversionStatus"] = *d.ConversionStatus
	}
	if d.ConversionError != nil && *d.ConversionError != "" {
		sum["conversionError"] = *d.ConversionError
	}
	if d.ProcessingStatus != nil && *d.ProcessingStatus != "" {
		sum["processingStatus"] = *d.ProcessingStatus
	}
	if d.SourceStatus != "" {
		sum["sourceStatus"] = d.SourceStatus
	}
	if d.FileSizeBytes != nil {
		sum["fileSizeBytes"] = *d.FileSizeBytes
	}
	if d.ParentDocumentID != nil && *d.ParentDocumentID != "" {
		sum["parentDocumentId"] = *d.ParentDocumentID
	}
	sum["canonicalId"] = d.CanonicalID
	sum["version"] = d.Version
	sum["createdAt"] = d.CreatedAt
	sum["updatedAt"] = d.UpdatedAt
	if includeContent && d.Content != nil {
		sum["content"] = *d.Content
	}
	return sum
}

func (s *Service) executeGetDocument(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	docID, _ := args["document_id"].(string)
	if docID == "" {
		return nil, fmt.Errorf("get_document: 'document_id' is required")
	}
	doc, err := s.documentsSvc.GetByID(ctx, projectID, docID)
	if err != nil {
		return nil, fmt.Errorf("get_document: %w", err)
	}
	return s.wrapResult(doc)
}

func (s *Service) executeUploadDocument(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return nil, fmt.Errorf("upload_document: 'filename' is required")
	}
	contentB64, _ := args["content_base64"].(string)
	if contentB64 == "" {
		return nil, fmt.Errorf("upload_document: 'content_base64' is required")
	}

	// Decode — try standard then URL-safe base64
	decoded, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(contentB64)
		if err != nil {
			return nil, fmt.Errorf("upload_document: invalid base64 content: %w", err)
		}
	}
	if len(decoded) > maxUploadBytes {
		return nil, fmt.Errorf("upload_document: decoded file size %d bytes exceeds 10 MB limit", len(decoded))
	}

	mimeType, _ := args["mime_type"].(string)
	if mimeType == "" {
		mimeType = inferMIMEType(filename)
	}

	// Compute file hash for deduplication
	hashSum := sha256.Sum256(decoded)
	fileHash := hex.EncodeToString(hashSum[:])

	// Resolve org ID from auth context
	orgID := auth.OrgIDFromContext(ctx)

	uploadParams := documents.UploadParams{
		ProjectID: projectID,
		OrgID:     orgID,
		Filename:  filename,
		MimeType:  mimeType,
		FileSize:  int64(len(decoded)),
		FileHash:  fileHash,
	}

	// Upload to storage. Fail loudly when storage is unavailable instead of
	// silently creating an unusable document record with no content.
	if s.storageSvc == nil || !s.storageSvc.Enabled() {
		return nil, fmt.Errorf("upload_document: object storage is not configured — document content cannot be stored")
	}
	uploadResult, err := s.storageSvc.UploadDocument(ctx, bytes.NewReader(decoded), int64(len(decoded)), storage.DocumentUploadOptions{
		OrgID:     orgID,
		ProjectID: projectID,
		Filename:  filename,
		UploadOptions: storage.UploadOptions{
			ContentType: mimeType,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upload_document: storage upload failed: %w", err)
	}
	uploadParams.StorageKey = uploadResult.Key
	uploadParams.StorageURL = uploadResult.StorageURL

	resp, err := s.documentsSvc.CreateFromUpload(ctx, uploadParams)
	if err != nil {
		return nil, fmt.Errorf("upload_document: %w", err)
	}
	return s.wrapResult(resp)
}

func (s *Service) executeDeleteDocument(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	docID, _ := args["document_id"].(string)
	if docID == "" {
		return nil, fmt.Errorf("delete_document: 'document_id' is required")
	}
	result, err := s.documentsSvc.Delete(ctx, projectID, docID)
	if err != nil {
		return nil, fmt.Errorf("delete_document: %w", err)
	}
	return s.wrapResult(result)
}

// inferMIMEType returns a best-guess MIME type based on filename extension.
func inferMIMEType(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(lower, ".md"), strings.HasSuffix(lower, ".markdown"):
		return "text/markdown"
	case strings.HasSuffix(lower, ".txt"):
		return "text/plain"
	case strings.HasSuffix(lower, ".html"), strings.HasSuffix(lower, ".htm"):
		return "text/html"
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".docx"):
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case strings.HasSuffix(lower, ".csv"):
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}
