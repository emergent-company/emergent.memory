package documents

import (
	"time"

	"github.com/uptrace/bun"
)

// Document represents a document entity from kb.documents table
type Document struct {
	bun.BaseModel `bun:"table:kb.documents"`

	ID        string `bun:"id,pk" json:"id"`
	ProjectID string `bun:"project_id" json:"projectId"`

	// Basic metadata
	Filename  *string `bun:"filename" json:"filename,omitempty"`
	SourceURL *string `bun:"source_url" json:"sourceUrl,omitempty"`
	MimeType  *string `bun:"mime_type" json:"mimeType,omitempty"`

	// Content
	Content     *string `bun:"content" json:"content,omitempty"`
	ContentHash *string `bun:"content_hash" json:"contentHash,omitempty"`
	FileHash    *string `bun:"file_hash" json:"fileHash,omitempty"`

	// Timestamps
	CreatedAt time.Time `bun:"created_at" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at" json:"updatedAt"`

	// Hierarchy
	ParentDocumentID *string `bun:"parent_document_id" json:"parentDocumentId,omitempty"`

	// Conversion status
	ConversionStatus      *string    `bun:"conversion_status" json:"conversionStatus,omitempty"`
	ConversionError       *string    `bun:"conversion_error" json:"conversionError,omitempty"`
	ConversionCompletedAt *time.Time `bun:"conversion_completed_at" json:"conversionCompletedAt,omitempty"`

	// Storage
	StorageKey    *string `bun:"storage_key" json:"storageKey,omitempty"`
	FileSizeBytes *int64  `bun:"file_size_bytes" json:"fileSizeBytes,omitempty"`
	StorageURL    *string `bun:"storage_url" json:"storageUrl,omitempty"`

	// Data source
	SourceType              *string `bun:"source_type" json:"sourceType,omitempty"`
	DataSourceIntegrationID *string `bun:"data_source_integration_id" json:"dataSourceIntegrationId,omitempty"`
	ExternalSourceID        *string `bun:"external_source_id" json:"externalSourceId,omitempty"`
	SyncVersion             *int    `bun:"sync_version" json:"syncVersion,omitempty"`

	// Metadata
	IntegrationMetadata map[string]any `bun:"integration_metadata,type:jsonb" json:"integrationMetadata,omitempty"`
	Metadata            map[string]any `bun:"metadata,type:jsonb" json:"metadata,omitempty"`

	// Computed fields (populated via JOIN/subquery, not stored in documents table)
	Chunks           int     `bun:"chunks,scanonly" json:"chunks"`
	EmbeddedChunks   int     `bun:"embedded_chunks,scanonly" json:"embeddedChunks"`
	TotalChars       int     `bun:"total_chars,scanonly" json:"totalChars"`
	ExtractionStatus *string `bun:"extraction_status,scanonly" json:"extractionStatus,omitempty"`
}

// ListParams contains parameters for listing documents
type ListParams struct {
	ProjectID        string
	Limit            int
	Cursor           *Cursor
	SourceType       *string
	IntegrationID    *string
	RootOnly         bool
	ParentDocumentID *string
}

// Cursor represents pagination cursor
type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

// ListResult contains the result of listing documents
type ListResult struct {
	Documents  []Document `json:"documents"`
	Total      int        `json:"total"`
	NextCursor *string    `json:"next_cursor,omitempty"`
}

// CreateDocumentRequest is the request body for creating a document
type CreateDocumentRequest struct {
	Filename string `json:"filename" validate:"omitempty,max=512"`
	Content  string `json:"content" validate:"omitempty"`
}

// BulkDeleteRequest is the request body for bulk deleting documents
type BulkDeleteRequest struct {
	IDs []string `json:"ids" validate:"required,min=1,dive,uuid"`
}

// DeleteResponse is the response for delete operations
type DeleteResponse struct {
	Status   string         `json:"status"`            // "deleted" or "partial"
	Deleted  int            `json:"deleted,omitempty"` // Count of deleted documents (for bulk)
	NotFound []string       `json:"notFound,omitempty"`
	Summary  *DeleteSummary `json:"summary,omitempty"`
}

// DeleteSummary contains counts of related entities deleted
type DeleteSummary struct {
	Chunks             int `json:"chunks"`
	ExtractionJobs     int `json:"extractionJobs"`
	GraphObjects       int `json:"graphObjects"`
	GraphRelationships int `json:"graphRelationships"`
	Notifications      int `json:"notifications"`
}

// UploadDocumentResponse is the response for file upload
type UploadDocumentResponse struct {
	Document           *DocumentSummary `json:"document"`
	IsDuplicate        bool             `json:"isDuplicate"`
	ExistingDocumentID *string          `json:"existingDocumentId,omitempty"`
}

// DocumentSummary is a simplified document representation for upload responses
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

// UploadParams contains parameters for uploading a document
type UploadParams struct {
	ProjectID   string
	OrgID       string
	Filename    string
	MimeType    string
	FileSize    int64
	FileHash    string
	StorageKey  string
	StorageURL  string
	AutoExtract bool
}

// BatchUploadResult is the response for batch file upload
type BatchUploadResult struct {
	Summary BatchUploadSummary      `json:"summary"`
	Results []BatchUploadFileResult `json:"results"`
}

// BatchUploadSummary contains counts for batch upload results
type BatchUploadSummary struct {
	Total      int `json:"total"`
	Successful int `json:"successful"`
	Duplicates int `json:"duplicates"`
	Failed     int `json:"failed"`
}

// BatchUploadFileResult represents the result for a single file in a batch upload
type BatchUploadFileResult struct {
	Filename   string  `json:"filename"`
	Status     string  `json:"status"` // "success", "duplicate", "failed"
	DocumentID *string `json:"documentId,omitempty"`
	Chunks     *int    `json:"chunks,omitempty"`
	Error      *string `json:"error,omitempty"`
}

// ContentResponse is the response for GET /:id/content
type ContentResponse struct {
	Content *string `json:"content"`
}

// DeletionImpact represents the impact of deleting a document
type DeletionImpact struct {
	Document DocumentInfo  `json:"document"`
	Impact   ImpactSummary `json:"impact"`
}

// DocumentInfo is a simplified document representation for deletion impact
type DocumentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

// ImpactSummary contains counts of entities that will be deleted
type ImpactSummary struct {
	Chunks             int `json:"chunks"`
	ExtractionJobs     int `json:"extractionJobs"`
	GraphObjects       int `json:"graphObjects"`
	GraphRelationships int `json:"graphRelationships"`
	Notifications      int `json:"notifications"`
}

// BulkDeletionImpact represents the impact of bulk deleting documents
type BulkDeletionImpact struct {
	TotalDocuments int              `json:"totalDocuments"`
	Impact         ImpactSummary    `json:"impact"`
	Documents      []DeletionImpact `json:"documents,omitempty"`
}

// BulkDeletionImpactRequest is the request body for bulk deletion impact
type BulkDeletionImpactRequest struct {
	IDs []string `json:"ids" validate:"required,min=1,dive,uuid"`
}

// StorageInfo contains storage-related document info for downloads
type StorageInfo struct {
	ID               string
	Filename         *string
	StorageKey       *string
	MimeType         *string
	FileSizeBytes    *int64
	ProjectID        string
	ConversionStatus *string
}

// RecreateChunksResponse is the response for POST /:id/recreate-chunks
type RecreateChunksResponse struct {
	Status  string                `json:"status"`
	Summary RecreateChunksSummary `json:"summary"`
}

// RecreateChunksSummary contains details about chunk recreation
type RecreateChunksSummary struct {
	OldChunks int            `json:"oldChunks"`
	NewChunks int            `json:"newChunks"`
	Strategy  string         `json:"strategy"`
	Config    map[string]any `json:"config,omitempty"`
}

// RetryConversionResponse is the response for POST /:id/retry-conversion
type RetryConversionResponse struct {
	Success bool   `json:"success"`
	JobID   string `json:"jobId,omitempty"`
	Message string `json:"message"`
}

// CancelConversionResponse is the response for POST /:id/cancel-conversion
type CancelConversionResponse struct {
	Success       bool   `json:"success"`
	CancelledJobs int    `json:"cancelledJobs"`
	Message       string `json:"message"`
}

type SourceTypeWithCount struct {
	SourceType string `bun:"source_type" json:"sourceType"`
	Count      int    `bun:"count" json:"count"`
}
