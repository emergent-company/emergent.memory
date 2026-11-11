package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// DocumentCreator is the document-row half of an upload: the port the ingestor
// uses to insert the row. The real implementation is *Service's unexported
// createFromUpload, wrapped by serviceDocumentCreator below — the service
// method is deliberately not exported so that no package outside documents can
// create a document from an upload without going through UploadIngestor (which
// also queues the parsing job, see #586). The interface exists so the ingestor
// can be exercised without a database.
type DocumentCreator interface {
	CreateFromUpload(ctx context.Context, params UploadParams) (*UploadDocumentResponse, error)
}

// serviceDocumentCreator adapts *Service to DocumentCreator. This type is the
// only implementation wired in production; it is what makes "a new entry point
// cannot forget the parsing job" mechanically true rather than a convention.
type serviceDocumentCreator struct {
	svc *Service
}

func (c serviceDocumentCreator) CreateFromUpload(ctx context.Context, params UploadParams) (*UploadDocumentResponse, error) {
	return c.svc.createFromUpload(ctx, params)
}

// provideDocumentCreator is the fx provider for the port.
func provideDocumentCreator(s *Service) DocumentCreator {
	return documentCreatorFor(s)
}

// documentCreatorFor adapts a service to the port, returning nil for a nil
// service so the ingestor fails closed with 503 instead of panicking on a nil
// receiver.
func documentCreatorFor(s *Service) DocumentCreator {
	if s == nil {
		return nil
	}
	return serviceDocumentCreator{svc: s}
}

// UploadIngestor is the one place that turns uploaded bytes into a document the
// parsing pipeline will pick up.
//
// Every upload entry point used to repeat the same three steps by hand — store
// the object, create the document row, enqueue the parsing job — and the MCP
// document-upload tool omitted the third, so MCP uploads were stored but never
// parsed (#586). The sequence lives here, and *Service.createFromUpload (the
// only way to insert the row) is unexported, so another package cannot create a
// document from an upload without also queueing its parse.
type UploadIngestor struct {
	documents          DocumentCreator
	storage            uploadStorage
	parsingJobs        ParsingJobCreator
	projectAutoExtract ProjectAutoExtractReader
	log                *slog.Logger
}

// NewUploadIngestor wires the ingestor. storage and parsingJobs may be nil in
// deployments (or tests) that do not configure them: a missing storage service
// fails the call with 503, a missing parsing-job queue leaves the document
// unqueued, and neither panics.
func NewUploadIngestor(
	documents DocumentCreator,
	storage uploadStorage,
	parsingJobs ParsingJobCreator,
	projectAutoExtract ProjectAutoExtractReader,
	log *slog.Logger,
) *UploadIngestor {
	if log == nil {
		log = slog.Default()
	}
	return &UploadIngestor{
		documents:          documents,
		storage:            storage,
		parsingJobs:        parsingJobs,
		projectAutoExtract: projectAutoExtract,
		log:                log,
	}
}

// IngestUploadParams describes one upload requested by any entry point.
type IngestUploadParams struct {
	ProjectID       string
	OrgID           string
	CreatedByUserID *string
	Filename        string
	MimeType        string
	// Data is the decoded file content.
	Data []byte
	// RequestedAutoExtract is the caller's tri-state override: nil means "use
	// the project default", matching the HTTP upload endpoints.
	RequestedAutoExtract *bool
	Metadata             map[string]any
	SkipNotification     bool
}

// Ingest stores the bytes, creates the document row, and queues exactly one
// parsing job for it. A failed enqueue is logged but not returned: the document
// already exists and the caller has a successful upload, exactly as on the HTTP
// paths.
func (i *UploadIngestor) Ingest(ctx context.Context, p IngestUploadParams) (*UploadDocumentResponse, error) {
	if i == nil || i.documents == nil {
		return nil, apperror.New(http.StatusServiceUnavailable, "documents_unavailable",
			"document service is not configured")
	}
	if i.storage == nil || !i.storage.Enabled() {
		return nil, apperror.New(http.StatusServiceUnavailable, "storage_unavailable",
			"Storage service is not configured")
	}

	size := int64(len(p.Data))
	if err := ValidateUploadContent(p.MimeType, p.Filename, size); err != nil {
		return nil, err
	}

	autoExtract := resolveAutoExtract(ctx, i.log, i.projectAutoExtract, p.ProjectID, p.RequestedAutoExtract)

	fileHash := computeFileHash(p.Data)

	uploadResult, err := i.storage.UploadDocument(ctx, bytes.NewReader(p.Data), size, storage.DocumentUploadOptions{
		OrgID:     p.OrgID,
		ProjectID: p.ProjectID,
		Filename:  p.Filename,
		UploadOptions: storage.UploadOptions{
			ContentType: p.MimeType,
		},
	})
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	resp, err := i.documents.CreateFromUpload(ctx, UploadParams{
		ProjectID:        p.ProjectID,
		OrgID:            p.OrgID,
		CreatedByUserID:  p.CreatedByUserID,
		Filename:         p.Filename,
		MimeType:         p.MimeType,
		FileSize:         size,
		FileHash:         fileHash,
		StorageKey:       uploadResult.Key,
		StorageURL:       uploadResult.StorageURL,
		Metadata:         p.Metadata,
		SkipNotification: p.SkipNotification,
	})
	if err != nil {
		i.deleteStorageObject(ctx, uploadResult.Key)
		return nil, err
	}

	i.createParsingJob(ctx, ParsingJobOptions{
		OrganizationID: p.OrgID,
		ProjectID:      p.ProjectID,
		DocumentID:     resp.Document.ID,
		SourceType:     "file_upload",
		SourceFilename: &p.Filename,
		MimeType:       &p.MimeType,
		FileSizeBytes:  &size,
		StorageKey:     &uploadResult.Key,
		AutoExtract:    autoExtract,
	})

	return resp, nil
}

// computeFileHash computes SHA-256 hash of file bytes.
func computeFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// createParsingJob queues the document parsing job and logs (but does not fail
// on) errors. A failed enqueue means the document stays unparsed — the upload
// itself already succeeded.
func (i *UploadIngestor) createParsingJob(ctx context.Context, opts ParsingJobOptions) {
	if i.parsingJobs == nil {
		return
	}
	if err := i.parsingJobs.CreateJob(ctx, opts); err != nil {
		i.log.Error("failed to create parsing job",
			slog.String("document_id", opts.DocumentID),
			logger.Error(err),
		)
	}
}

// deleteStorageObject deletes a storage object and logs any error.
// Errors are non-fatal: the upload already succeeded; orphaned objects are a
// storage-hygiene concern, not a user-visible failure.
func (i *UploadIngestor) deleteStorageObject(ctx context.Context, key string) {
	if i.storage == nil {
		return
	}
	if err := i.storage.Delete(ctx, key); err != nil {
		i.log.Error("failed to delete storage object",
			slog.String("key", key),
			logger.Error(err),
		)
	}
}
