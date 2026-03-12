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
			Name:        "document-list",
			Description: "List documents in the current project. Returns an array of document objects with id, filename, mimeType, conversionStatus, fileSizeBytes, and timestamps. Supports optional pagination.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"limit": {
						Type:        "integer",
						Description: "Maximum number of documents to return (default 50)",
					},
					"source_type": {
						Type:        "string",
						Description: "Filter by source type (e.g. 'upload', 'github', 'web')",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "document-get",
			Description: "Get a single document by its ID. Returns full document metadata including conversion status, storage info, and chunk counts.",
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
			Name: "document-upload",
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
			Name:        "document-delete",
			Description: "Delete a document and all its associated chunks from the current project.",
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
	result, err := s.documentsSvc.List(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list_documents: %w", err)
	}
	resp := map[string]any{
		"documents":   result.Documents,
		"total":       result.Total,
		"next_cursor": result.NextCursor,
	}
	return s.wrapResult(resp)
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

	// Upload to storage if available, otherwise store with empty keys (local/dev)
	if s.storageSvc != nil && s.storageSvc.Enabled() {
		key := storage.GenerateDocumentKey(projectID, orgID, filename)
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
		_ = key
		uploadParams.StorageKey = uploadResult.Key
		uploadParams.StorageURL = uploadResult.StorageURL
	}

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
