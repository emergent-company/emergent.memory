package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Service handles document business logic
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new documents service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("documents.svc")),
	}
}

// List retrieves documents with pagination and filtering
func (s *Service) List(ctx context.Context, params ListParams) (*ListResult, error) {
	return s.repo.List(ctx, params)
}

// GetByID retrieves a single document by ID
func (s *Service) GetByID(ctx context.Context, projectID, documentID string) (*Document, error) {
	doc, err := s.repo.GetByID(ctx, projectID, documentID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, apperror.ErrNotFound.WithMessage("Document not found")
	}
	return doc, nil
}

// GetSourceTypes returns all distinct source types with document counts
func (s *Service) GetSourceTypes(ctx context.Context) ([]SourceTypeWithCount, error) {
	return s.repo.GetDistinctSourceTypes(ctx)
}

// CreateParams contains parameters for creating a document
type CreateParams struct {
	ProjectID     string
	Filename      *string
	Content       *string
	StorageKey    *string
	SourceType    *string
	MimeType      *string
	FileSizeBytes *int64
}

// Create creates a new document with content deduplication
// If a document with the same content hash exists, returns the existing document
func (s *Service) Create(ctx context.Context, params CreateParams) (*Document, bool, error) {
	filename := "unnamed.txt"
	if params.Filename != nil {
		trimmed := strings.TrimSpace(*params.Filename)
		if trimmed != "" {
			filename = trimmed
		}
	}

	content := ""
	if params.Content != nil {
		content = *params.Content
	}

	contentHash := computeContentHash(content)

	existingDoc, err := s.repo.GetByContentHash(ctx, params.ProjectID, contentHash)
	if err != nil {
		return nil, false, err
	}
	if existingDoc != nil {
		s.log.Info("document deduplicated",
			slog.String("projectId", params.ProjectID),
			slog.String("existingId", existingDoc.ID),
			slog.String("contentHash", contentHash))
		return existingDoc, false, nil
	}

	now := time.Now().UTC()
	doc := &Document{
		ID:          uuid.New().String(),
		ProjectID:   params.ProjectID,
		Filename:    &filename,
		Content:     &content,
		ContentHash: &contentHash,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if params.StorageKey != nil {
		doc.StorageKey = params.StorageKey
	}
	if params.SourceType != nil {
		doc.SourceType = params.SourceType
	}
	if params.MimeType != nil {
		doc.MimeType = params.MimeType
	}
	if params.FileSizeBytes != nil {
		doc.FileSizeBytes = params.FileSizeBytes
	}

	err = s.repo.Create(ctx, doc)
	if err != nil {
		if appErr, ok := err.(*apperror.Error); ok && appErr.Code == "duplicate" {
			existingDoc, getErr := s.repo.GetByContentHash(ctx, params.ProjectID, contentHash)
			if getErr == nil && existingDoc != nil {
				return existingDoc, false, nil
			}
		}
		return nil, false, err
	}

	s.log.Info("document created",
		slog.String("id", doc.ID),
		slog.String("projectId", params.ProjectID),
		slog.String("filename", filename))

	return doc, true, nil
}

// Delete deletes a document and all related entities
func (s *Service) Delete(ctx context.Context, projectID, documentID string) (*DeleteResponse, error) {
	// Validate UUID format
	if _, err := uuid.Parse(documentID); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid document ID format")
	}

	summary, err := s.repo.DeleteWithCascade(ctx, projectID, documentID)
	if err != nil {
		return nil, err
	}

	s.log.Info("document deleted",
		slog.String("id", documentID),
		slog.String("projectId", projectID),
		slog.Int("chunks", summary.Chunks),
		slog.Int("extractionJobs", summary.ExtractionJobs))

	return &DeleteResponse{
		Status:  "deleted",
		Summary: summary,
	}, nil
}

// BulkDelete deletes multiple documents and all related entities
func (s *Service) BulkDelete(ctx context.Context, projectID string, documentIDs []string) (*DeleteResponse, error) {
	// Validate all UUIDs
	for _, id := range documentIDs {
		if _, err := uuid.Parse(id); err != nil {
			return nil, apperror.ErrBadRequest.WithMessage("Invalid document ID format: " + id)
		}
	}

	summary, notFound, err := s.repo.BulkDeleteWithCascade(ctx, projectID, documentIDs)
	if err != nil {
		return nil, err
	}

	deleted := len(documentIDs) - len(notFound)
	status := "deleted"
	if len(notFound) > 0 {
		status = "partial"
	}

	s.log.Info("documents bulk deleted",
		slog.String("projectId", projectID),
		slog.Int("requested", len(documentIDs)),
		slog.Int("deleted", deleted),
		slog.Int("notFound", len(notFound)))

	response := &DeleteResponse{
		Status:  status,
		Deleted: deleted,
		Summary: summary,
	}

	if len(notFound) > 0 {
		response.NotFound = notFound
	}

	return response, nil
}

// CreateFromUpload creates a document record from a file upload
// If a document with the same file hash exists, returns the existing document
func (s *Service) CreateFromUpload(ctx context.Context, params UploadParams) (*UploadDocumentResponse, error) {
	// Apply defaults
	filename := strings.TrimSpace(params.Filename)
	if filename == "" {
		filename = "unnamed"
	}

	// Check for existing document with same file hash (deduplication)
	existingDoc, err := s.repo.GetByFileHash(ctx, params.ProjectID, params.FileHash)
	if err != nil {
		return nil, err
	}
	if existingDoc != nil {
		s.log.Info("document upload deduplicated",
			slog.String("projectId", params.ProjectID),
			slog.String("existingId", existingDoc.ID),
			slog.String("fileHash", params.FileHash))

		return &UploadDocumentResponse{
			Document:           toDocumentSummary(existingDoc),
			IsDuplicate:        true,
			ExistingDocumentID: &existingDoc.ID,
		}, nil
	}

	// Determine conversion status based on mime type and filename
	conversionStatus := determineConversionStatus(params.MimeType, params.Filename)

	// Create new document
	now := time.Now().UTC()
	doc := &Document{
		ID:               uuid.New().String(),
		ProjectID:        params.ProjectID,
		Filename:         &filename,
		MimeType:         &params.MimeType,
		FileHash:         &params.FileHash,
		FileSizeBytes:    &params.FileSize,
		StorageKey:       &params.StorageKey,
		StorageURL:       &params.StorageURL,
		ConversionStatus: &conversionStatus,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// Set source type to "upload"
	sourceType := "upload"
	doc.SourceType = &sourceType

	err = s.repo.Create(ctx, doc)
	if err != nil {
		// Handle race condition: another request may have created the same document
		if appErr, ok := err.(*apperror.Error); ok && appErr.Code == "duplicate" {
			existingDoc, getErr := s.repo.GetByFileHash(ctx, params.ProjectID, params.FileHash)
			if getErr == nil && existingDoc != nil {
				return &UploadDocumentResponse{
					Document:           toDocumentSummary(existingDoc),
					IsDuplicate:        true,
					ExistingDocumentID: &existingDoc.ID,
				}, nil
			}
		}
		return nil, err
	}

	s.log.Info("document uploaded",
		slog.String("id", doc.ID),
		slog.String("projectId", params.ProjectID),
		slog.String("filename", filename),
		slog.Int64("size", params.FileSize),
		slog.String("mimeType", params.MimeType))

	return &UploadDocumentResponse{
		Document:    toDocumentSummary(doc),
		IsDuplicate: false,
	}, nil
}

// toDocumentSummary converts a Document to a DocumentSummary
func toDocumentSummary(doc *Document) *DocumentSummary {
	name := ""
	if doc.Filename != nil {
		name = *doc.Filename
	}

	status := "not_required"
	if doc.ConversionStatus != nil {
		status = *doc.ConversionStatus
	}

	return &DocumentSummary{
		ID:               doc.ID,
		Name:             name,
		MimeType:         doc.MimeType,
		FileSizeBytes:    doc.FileSizeBytes,
		ConversionStatus: status,
		ConversionError:  doc.ConversionError,
		StorageKey:       doc.StorageKey,
		CreatedAt:        doc.CreatedAt.Format(time.RFC3339),
	}
}

// determineConversionStatus determines if a file needs conversion based on mime type and filename
func determineConversionStatus(mimeType, filename string) string {
	// Audio files need Whisper transcription
	if strings.HasPrefix(mimeType, "audio/") {
		return "pending"
	}
	ext := strings.ToLower(filepath.Ext(filename))
	audioExts := map[string]bool{
		".mp3": true, ".wav": true, ".m4a": true, ".ogg": true,
		".flac": true, ".aac": true, ".mp4": true, ".webm": true,
		".weba": true, ".opus": true,
	}
	if audioExts[ext] {
		return "pending"
	}

	// Plain text and markdown don't need conversion
	if strings.HasPrefix(mimeType, "text/") {
		return "not_required"
	}

	// Common document types that need conversion
	needsConversion := []string{
		"application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}

	for _, ct := range needsConversion {
		if mimeType == ct {
			return "pending"
		}
	}

	// Default to not required for unknown types
	return "not_required"
}

// computeContentHash computes SHA-256 hash of content
func computeContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// GetContent retrieves just the content of a document
func (s *Service) GetContent(ctx context.Context, projectID, documentID string) (*string, error) {
	content, err := s.repo.GetContent(ctx, projectID, documentID)
	if err != nil {
		return nil, err
	}
	// Note: content being nil (document found but no content) is valid
	// We return a pointer to distinguish from "not found"
	return content, nil
}

// GetStorageInfo retrieves storage-related info for a document (for downloads)
func (s *Service) GetStorageInfo(ctx context.Context, projectID, documentID string) (*StorageInfo, error) {
	info, err := s.repo.GetStorageInfo(ctx, projectID, documentID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, apperror.ErrNotFound.WithMessage("Document not found")
	}
	return info, nil
}

// GetDeletionImpact returns the impact of deleting a document
func (s *Service) GetDeletionImpact(ctx context.Context, projectID, documentID string) (*DeletionImpact, error) {
	// Validate UUID format
	if _, err := uuid.Parse(documentID); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid document ID format")
	}

	impact, err := s.repo.GetDeletionImpact(ctx, projectID, documentID)
	if err != nil {
		return nil, err
	}
	if impact == nil {
		return nil, apperror.ErrNotFound.WithMessage("Document not found")
	}
	return impact, nil
}

// GetBulkDeletionImpact returns the impact of deleting multiple documents
func (s *Service) GetBulkDeletionImpact(ctx context.Context, projectID string, documentIDs []string) (*BulkDeletionImpact, error) {
	// Validate all UUIDs
	for _, id := range documentIDs {
		if _, err := uuid.Parse(id); err != nil {
			return nil, apperror.ErrBadRequest.WithMessage("Invalid document ID format: " + id)
		}
	}

	impact, err := s.repo.GetBulkDeletionImpact(ctx, projectID, documentIDs)
	if err != nil {
		return nil, err
	}
	return impact, nil
}

// ResetConversionStatus resets a document's conversion status to pending (for retry)
func (s *Service) ResetConversionStatus(ctx context.Context, documentID string) error {
	return s.repo.UpdateConversionStatus(ctx, documentID, "pending", nil)
}

// MarkConversionFailed marks a document's conversion as failed
func (s *Service) MarkConversionFailed(ctx context.Context, documentID, errorMsg string) error {
	return s.repo.UpdateConversionStatus(ctx, documentID, "failed", &errorMsg)
}

// MarkConversionNotRequired marks a document as not requiring conversion
func (s *Service) MarkConversionNotRequired(ctx context.Context, documentID string) error {
	return s.repo.UpdateConversionStatus(ctx, documentID, "not_required", nil)
}
