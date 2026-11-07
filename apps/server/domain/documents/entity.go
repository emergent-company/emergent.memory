package documents

import (
	"fmt"
	"strings"
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

	// Uploader (#2260). Set from the authenticated caller on the create paths;
	// NULL for a document ingested from a data source (no single uploader) and
	// for rows written before the column existed. The display name is resolved by
	// the listing's join — a per-row lookup would be an N+1 the page does not
	// need.
	CreatedByUserID      *string `bun:"created_by_user_id" json:"createdByUserId,omitempty"`
	CreatedByDisplayName *string `bun:"created_by_display_name,scanonly" json:"createdByDisplayName,omitempty"`

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

// sourceNoneFilter is the sourceId value that selects documents with no
// connected data source (direct uploads). A real source id is a UUID, so the
// sentinel cannot collide with one.
const sourceNoneFilter = "none"

// Document status filter buckets. The derived processing status has six states;
// clients filter on four, each a group of those states. The groups are disjoint
// because they are defined as a partition of the CASE that produces the status
// (see processingStatusExpr), not as independent predicates.
const (
	StatusBucketProcessing = "processing"
	StatusBucketReady      = "ready"
	StatusBucketExtracted  = "extracted"
	StatusBucketFailed     = "failed"
)

// statusBucketStates maps each filter bucket to the derived states it covers.
// Every state belongs to exactly one bucket.
var statusBucketStates = map[string][]string{
	StatusBucketProcessing: {"converting", "extracting"},
	StatusBucketReady:      {"ready_for_extraction"},
	StatusBucketExtracted:  {"completed"},
	StatusBucketFailed:     {"conversion_failed", "extraction_failed"},
}

// Document list sort keys and directions.
const (
	SortCreated = "created"
	SortUpdated = "updated"

	OrderAsc  = "asc"
	OrderDesc = "desc"
)

// NormalizeStatusBucket validates a status filter value. An empty value means
// "no status filter" and is returned as-is; anything outside the bucket
// vocabulary is an error so a typo cannot silently look like a working filter.
func NormalizeStatusBucket(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", nil
	}
	if _, ok := statusBucketStates[s]; !ok {
		return "", fmt.Errorf("status must be one of %s, %s, %s, %s", StatusBucketProcessing, StatusBucketReady, StatusBucketExtracted, StatusBucketFailed)
	}
	return s, nil
}

// NormalizeSort validates a sort key, defaulting to created.
func NormalizeSort(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "":
		return SortCreated, nil
	case SortCreated, SortUpdated:
		return s, nil
	default:
		return "", fmt.Errorf("sort must be one of %s, %s", SortCreated, SortUpdated)
	}
}

// NormalizeOrder validates a sort direction, defaulting to descending.
func NormalizeOrder(raw string) (string, error) {
	o := strings.ToLower(strings.TrimSpace(raw))
	switch o {
	case "":
		return OrderDesc, nil
	case OrderAsc, OrderDesc:
		return o, nil
	default:
		return "", fmt.Errorf("order must be one of %s, %s", OrderAsc, OrderDesc)
	}
}

// ListParams contains parameters for listing documents
type ListParams struct {
	ProjectID string
	Limit     int
	Cursor    *Cursor
	// SourceType filters on the source-kind category stamped on the document
	// ("upload", "drive", "repo", ...). Deprecated for UI filtering in favour of
	// SourceID; kept because it is part of the public listing API.
	SourceType *string
	// SourceID filters on one connected data source. SourceUnassigned selects
	// documents with no data source (direct uploads) and is mutually exclusive
	// with SourceID.
	SourceID         *string
	SourceUnassigned bool
	RootOnly         bool
	ParentDocumentID *string
	// Status filters on the derived processing status, using a bucket from
	// statusBucketStates. Empty means no status filter.
	Status string
	// Query is a case-insensitive substring match over the document's filename
	// and source URL. Empty means no search.
	Query string
	// Sort is SortCreated or SortUpdated; Order is OrderAsc or OrderDesc.
	// Empty values are defaulted by normalized().
	Sort  string
	Order string
	// IncludeDeletedFromSource, when true, includes documents whose
	// source_status is 'deleted_from_source'. Default listings exclude them.
	IncludeDeletedFromSource bool
	// SkipStatusCounts lets the service layer fetch the (expensive) breakdown
	// separately, so it can be cached while the page itself stays live.
	SkipStatusCounts bool
}

// normalized applies the default sort and order. It does not validate: callers
// that accept user input validate first (NormalizeSort/NormalizeOrder) so the
// invalid value becomes a 400 rather than a silent default.
func (p ListParams) normalized() ListParams {
	if p.Sort == "" {
		p.Sort = SortCreated
	}
	if p.Order == "" {
		p.Order = OrderDesc
	}
	return p
}

// StatusCounts is the per-status breakdown returned with every listing. All is
// the total ignoring the status filter; each bucket counts the documents the
// other filters (search, source, visibility) matched.
type StatusCounts struct {
	All        int `json:"all"`
	Processing int `json:"processing"`
	Ready      int `json:"ready"`
	Extracted  int `json:"extracted"`
	Failed     int `json:"failed"`
}

func (c *StatusCounts) add(status string, n int) {
	c.All += n
	switch bucketForState(status) {
	case StatusBucketProcessing:
		c.Processing += n
	case StatusBucketReady:
		c.Ready += n
	case StatusBucketExtracted:
		c.Extracted += n
	case StatusBucketFailed:
		c.Failed += n
	}
}

// bucketForState resolves a derived processing status to its filter bucket, or
// "" when the state is unknown (such a row counts toward All only). The buckets
// are a partition, so the first match is the only match.
func bucketForState(status string) string {
	for bucket, states := range statusBucketStates {
		for _, state := range states {
			if state == status {
				return bucket
			}
		}
	}
	return ""
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
	// Version is the version number of the row DocumentID names (1 for a first
	// ingest, HEAD+1 for an append, the existing HEAD for a skip).
	Version    int
	Created    bool // first ingest created the HEAD version
	NewVersion bool // a content change appended a new version
	Skipped    bool // identical parsed text; no new version
}

// Cursor represents pagination cursor. It is a keyset over (sort value, id):
// Sort names the key it was minted for, Value is that key's value at the last
// returned row, and ID breaks ties.
type Cursor struct {
	Sort  string    `json:"sort,omitempty"`
	Value time.Time `json:"value,omitempty"`
	ID    string    `json:"id"`
	// CreatedAt is the pre-sort-key cursor field. Cursors minted before the
	// sort discriminator existed carry only this; ParseCursor normalizes them
	// to Sort=created, Value=CreatedAt. It is a pointer so new cursors omit it
	// from their JSON instead of emitting a zero timestamp.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// ListResult contains the result of listing documents
type ListResult struct {
	Documents []Document `json:"documents"`
	// Total is the number of documents matching every filter, including the
	// status filter.
	Total int `json:"total"`
	// StatusCounts breaks the result down by derived status. It ignores the
	// status filter (so a client can render stable filter counts) but honours
	// the search and source filters.
	StatusCounts StatusCounts `json:"statusCounts"`
	NextCursor   *string      `json:"next_cursor,omitempty"`
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
	Branches           int `json:"branches"`
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
	ProjectID string
	OrgID     string
	// CreatedByUserID is the uploader, recorded on the created row (#2260).
	CreatedByUserID *string
	Filename        string
	MimeType        string
	FileSize        int64
	FileHash        string
	StorageKey      string
	StorageURL      string
	AutoExtract     bool
	Metadata        map[string]any
	// SkipNotification suppresses the per-document upload announcement. The
	// batch endpoint sets it and emits one digest for the whole request instead.
	SkipNotification bool
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
	Branches           int `json:"branches"`
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

// SourceCount is one provenance bucket for the documents source filter: the
// connected data source (empty for documents with no source) and the source
// kind, with the number of HEAD documents behind it.
type SourceCount struct {
	SourceID   *string `bun:"source_id" json:"sourceId,omitempty"`
	SourceType *string `bun:"source_type" json:"sourceType,omitempty"`
	Count      int     `bun:"count" json:"count"`
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
