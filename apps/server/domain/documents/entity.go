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
	SourceType *string `bun:"source_type" json:"sourceType,omitempty"`

	// Versioning (supersedes chain). HEAD is the row whose supersedes_id is
	// NULL; a new version appends a row sharing canonical_id and increments
	// version. canonical_id/version/source_status carry DB defaults, so the
	// zero value is omitted from INSERT and the column default applies.
	CanonicalID  string  `bun:"canonical_id,notnull,default:uuid_generate_v4()" json:"canonicalId"`
	SupersedesID *string `bun:"supersedes_id" json:"supersedesId,omitempty"`
	Version      int     `bun:"version,notnull,default:1" json:"version"`

	// Source attribution
	SourceID         *string    `bun:"source_id" json:"sourceId,omitempty"`
	SourceExternalID *string    `bun:"source_external_id" json:"sourceExternalId,omitempty"`
	SourceRevisionID *string    `bun:"source_revision_id" json:"sourceRevisionId,omitempty"`
	SourceTextHash   *string    `bun:"source_text_hash" json:"sourceTextHash,omitempty"`
	SourceSyncedAt   *time.Time `bun:"source_synced_at" json:"sourceSyncedAt,omitempty"`
	SourceStatus     string     `bun:"source_status,notnull,default:'active'" json:"sourceStatus"`
	SourceDeletedAt  *time.Time `bun:"source_deleted_at" json:"sourceDeletedAt,omitempty"`

	// Domain classification (Phase 3)
	DomainName            *string        `bun:"domain_name" json:"domainName,omitempty"`
	DomainConfidence      *float32       `bun:"domain_confidence" json:"domainConfidence,omitempty"`
	ClassificationSignals map[string]any `bun:"classification_signals,type:jsonb" json:"classificationSignals,omitempty"`

	// Metadata
	Metadata map[string]any `bun:"metadata,type:jsonb" json:"metadata,omitempty"`

	// Computed fields (populated via JOIN/subquery, not stored in documents table)
	Chunks           int     `bun:"chunks,scanonly" json:"chunks"`
	EmbeddedChunks   int     `bun:"embedded_chunks,scanonly" json:"embeddedChunks"`
	TotalChars       int     `bun:"total_chars,scanonly" json:"totalChars"`
	ExtractionStatus *string `bun:"extraction_status,scanonly" json:"extractionStatus,omitempty"`

	// Unified processing status (derived from conversion + extraction state)
	ProcessingStatus *string `bun:"processing_status,scanonly" json:"processingStatus,omitempty"`

	// Compact extraction summary (from most recent completed extraction job)
	LastExtractionAt    *time.Time `bun:"last_extraction_at,scanonly" json:"lastExtractionAt,omitempty"`
	ExtractionObjects   *int       `bun:"extraction_objects_created,scanonly" json:"objectsCreated,omitempty"`
	ExtractionRelations *int       `bun:"extraction_relationships_created,scanonly" json:"relationshipsCreated,omitempty"`

	// Job IDs of all extraction jobs run on this document
	JobIDs []string `bun:"-" json:"jobIds,omitempty"`
}

// SourceStatus values for kb.documents.source_status.
const (
	SourceStatusActive            = "active"
	SourceStatusDeletedFromSource = "deleted_from_source"
)

// ListParams contains parameters for listing documents
type ListParams struct {
	ProjectID        string
	Limit            int
	Cursor           *Cursor
	SourceType       *string
	RootOnly         bool
	ParentDocumentID *string
	// IncludeDeletedFromSource, when true, includes documents whose
	// source_status is 'deleted_from_source'. Default listings exclude them.
	IncludeDeletedFromSource bool
}

// DocumentVersion is a summary of a single document version for the history
// endpoint. ListVersionsByCanonical returns []Document; this type is the
// trimmed view the service layer can project for history reads.
type DocumentVersion struct {
	ID               string     `json:"id"`
	CanonicalID      string     `json:"canonicalId"`
	Version          int        `json:"version"`
	SupersedesID     *string    `json:"supersedesId,omitempty"`
	SourceStatus     string     `json:"sourceStatus,omitempty"`
	SourceRevisionID *string    `json:"sourceRevisionId,omitempty"`
	SourceDeletedAt  *time.Time `json:"sourceDeletedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`

	// Extraction metadata attributed to this specific version row (from
	// kb.object_extraction_jobs where document_id = this version's id).
	ExtractionJobs   int        `json:"extractionJobs"`
	ObjectsCreated   int        `json:"objectsCreated"`
	LastExtractionAt *time.Time `json:"lastExtractionAt,omitempty"`
}

// ApplySourceItemParams is the input to the provider-agnostic ingestion seam.
// The source worker performs fetching/parsing and passes the already-parsed text
// as Content.
type ApplySourceItemParams struct {
	ProjectID  string
	SourceID   string
	ExternalID string
	RevisionID string
	Filename   string
	MimeType   string
	Content    string
	// SourceType is the source-type category stamped on the document (e.g.
	// "drive") so source-type listings/counts attribute it correctly. Optional;
	// empty leaves source_type NULL (including all pre-existing calls).
	SourceType string
}

// ApplySourceItemResult is the outcome of applying a source item to the
// document store. Exactly one of Created / NewVersion / Skipped is true.
type ApplySourceItemResult struct {
	DocumentID string
	Created    bool // first ingest created the HEAD version
	NewVersion bool // a content change appended a new version
	Skipped    bool // identical parsed text; no new version
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

// UploadDocumentResponse is the response for file upload.
// Uploads are no longer deduplicated (issue #381); IsDuplicate and
// ExistingDocumentID are retained for backward compatibility and are always
// false/nil.
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
	Metadata    map[string]any
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
	Status     string  `json:"status"` // "success", "failed"
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

// ExtractionSummary is the response for GET /:id/extraction-summary
type ExtractionSummary struct {
	JobID                string         `json:"jobId"`
	CompletedAt          time.Time      `json:"completedAt"`
	ObjectsCreated       int            `json:"objectsCreated"`
	RelationshipsCreated int            `json:"relationshipsCreated"`
	ObjectsByType        map[string]int `json:"objectsByType"`
	ObjectIDs            []string       `json:"objectIds,omitempty"`
	ChunksProcessed      int            `json:"chunksProcessed"`
	TotalChunks          int            `json:"totalChunks"`
	HasErrors            bool           `json:"hasErrors"`
	ErrorSummary         *string        `json:"errorSummary"`
}
