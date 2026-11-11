package documents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDocumentCreator is a DocumentCreator double that records the params
// the ingestor hands to the document row.
type stubDocumentCreator struct {
	got  []UploadParams
	resp *UploadDocumentResponse
	err  error
}

func (f *stubDocumentCreator) CreateFromUpload(_ context.Context, params UploadParams) (*UploadDocumentResponse, error) {
	f.got = append(f.got, params)
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &UploadDocumentResponse{Document: &DocumentSummary{ID: "doc-1"}}, nil
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newIngestor(doc *stubDocumentCreator, stor uploadStorage, jobs *fakeParsingJobs, auto *fakeAutoExtractReader) *UploadIngestor {
	var reader ProjectAutoExtractReader
	if auto != nil {
		reader = auto
	}
	return NewUploadIngestor(doc, stor, jobs, reader, discardLog())
}

// TestUploadIngestor_QueuesExactlyOneParsingJob is the regression test for
// #586: an upload that reaches the ingestor must enqueue exactly one parsing
// job, or the stored document stays empty forever.
func TestUploadIngestor_QueuesExactlyOneParsingJob(t *testing.T) {
	doc := &stubDocumentCreator{}
	stor := &fakeStorage{enabled: true, uploadKey: "uploads/notes.md"}
	jobs := &fakeParsingJobs{}
	ing := newIngestor(doc, stor, jobs, nil)

	userID := "user-1"
	resp, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID:       "proj-1",
		OrgID:           "org-1",
		CreatedByUserID: &userID,
		Filename:        "notes.md",
		MimeType:        "text/markdown",
		Data:            []byte("# hello"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, doc.got, 1)
	assert.Equal(t, "proj-1", doc.got[0].ProjectID)
	assert.Equal(t, "org-1", doc.got[0].OrgID)
	assert.Equal(t, userID, *doc.got[0].CreatedByUserID)
	assert.Equal(t, "uploads/notes.md", doc.got[0].StorageKey)
	assert.Equal(t, int64(len("# hello")), doc.got[0].FileSize)
	assert.NotEmpty(t, doc.got[0].FileHash, "the ingestor must hash the bytes it stored")

	require.Len(t, jobs.got, 1, "an upload must enqueue exactly one parsing job")
	job := jobs.got[0]
	assert.Equal(t, "doc-1", job.DocumentID)
	assert.Equal(t, "proj-1", job.ProjectID)
	assert.Equal(t, "org-1", job.OrganizationID)
	assert.Equal(t, "file_upload", job.SourceType)
	assert.Equal(t, "notes.md", *job.SourceFilename)
	assert.Equal(t, "text/markdown", *job.MimeType)
	assert.Equal(t, int64(len("# hello")), *job.FileSizeBytes)
	assert.Equal(t, "uploads/notes.md", *job.StorageKey)
}

// TestUploadIngestor_AutoExtractFollowsProjectDefault proves the MCP path gets
// the same auto-extract behaviour as a REST upload with no explicit parameter.
func TestUploadIngestor_AutoExtractFollowsProjectDefault(t *testing.T) {
	t.Run("project default true", func(t *testing.T) {
		doc := &stubDocumentCreator{}
		jobs := &fakeParsingJobs{}
		ing := newIngestor(doc, &fakeStorage{enabled: true, uploadKey: "k"}, jobs, &fakeAutoExtractReader{enabled: true})

		_, err := ing.Ingest(context.Background(), IngestUploadParams{
			ProjectID: "proj-1",
			Filename:  "notes.md",
			MimeType:  "text/markdown",
			Data:      []byte("body"),
		})
		require.NoError(t, err)
		require.Len(t, jobs.got, 1)
		assert.True(t, jobs.got[0].AutoExtract)
	})

	t.Run("explicit false overrides project default", func(t *testing.T) {
		doc := &stubDocumentCreator{}
		jobs := &fakeParsingJobs{}
		ing := newIngestor(doc, &fakeStorage{enabled: true, uploadKey: "k"}, jobs, &fakeAutoExtractReader{enabled: true})

		no := false
		_, err := ing.Ingest(context.Background(), IngestUploadParams{
			ProjectID:            "proj-1",
			Filename:             "notes.md",
			MimeType:             "text/markdown",
			Data:                 []byte("body"),
			RequestedAutoExtract: &no,
		})
		require.NoError(t, err)
		require.Len(t, jobs.got, 1)
		assert.False(t, jobs.got[0].AutoExtract)
	})
}

func TestUploadIngestor_RejectsUnreadableContentBeforeStoring(t *testing.T) {
	doc := &stubDocumentCreator{}
	stor := &fakeStorage{enabled: true, uploadKey: "k"}
	jobs := &fakeParsingJobs{}
	ing := newIngestor(doc, stor, jobs, nil)

	_, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID: "proj-1",
		Filename:  "empty.md",
		MimeType:  "text/markdown",
		Data:      nil,
	})
	require.Error(t, err)
	assert.Empty(t, doc.got, "nothing must be stored or created for an empty upload")
	assert.Len(t, jobs.got, 0)
}

func TestUploadIngestor_StorageUnavailable(t *testing.T) {
	doc := &stubDocumentCreator{}
	jobs := &fakeParsingJobs{}
	ing := newIngestor(doc, &fakeStorage{enabled: false}, jobs, nil)

	_, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID: "proj-1",
		Filename:  "notes.md",
		MimeType:  "text/markdown",
		Data:      []byte("body"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
	assert.Len(t, jobs.got, 0)
}

func TestUploadIngestor_DocumentFailureRollsBackObjectAndQueuesNothing(t *testing.T) {
	stor := &fakeStorage{enabled: true, uploadKey: "uploads/orphan.md"}
	jobs := &fakeParsingJobs{}
	doc := &stubDocumentCreator{err: errors.New("insert failed")}
	ing := newIngestor(doc, stor, jobs, nil)

	_, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID: "proj-1",
		Filename:  "notes.md",
		MimeType:  "text/markdown",
		Data:      []byte("body"),
	})
	require.Error(t, err)
	assert.Equal(t, []string{"uploads/orphan.md"}, stor.deletedKeys,
		"a failed document create must delete the stored object")
	assert.Len(t, jobs.got, 0, "no parsing job may be queued for a document that was never created")
}

func TestUploadIngestor_JobEnqueueFailureDoesNotFailTheUpload(t *testing.T) {
	doc := &stubDocumentCreator{}
	jobs := &fakeParsingJobs{err: fmt.Errorf("queue full")}
	ing := newIngestor(doc, &fakeStorage{enabled: true, uploadKey: "k"}, jobs, nil)

	resp, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID: "proj-1",
		Filename:  "notes.md",
		MimeType:  "text/markdown",
		Data:      []byte("body"),
	})
	require.NoError(t, err, "a failed enqueue must not fail an upload that already succeeded")
	require.NotNil(t, resp)
	require.Len(t, jobs.got, 1)
}

// TestUploadIngestor_NilDependenciesNoPanic keeps the nil-safe contract for
// deployments without a configured parsing queue or document service.
func TestUploadIngestor_NilDependenciesNoPanic(t *testing.T) {
	t.Run("nil parsing jobs", func(t *testing.T) {
		doc := &stubDocumentCreator{}
		ing := NewUploadIngestor(doc, &fakeStorage{enabled: true, uploadKey: "k"}, nil, nil, discardLog())
		_, err := ing.Ingest(context.Background(), IngestUploadParams{
			ProjectID: "proj-1", Filename: "notes.md", MimeType: "text/markdown", Data: []byte("body"),
		})
		require.NoError(t, err)
	})

	t.Run("nil document creator", func(t *testing.T) {
		ing := NewUploadIngestor(nil, &fakeStorage{enabled: true, uploadKey: "k"}, &fakeParsingJobs{}, nil, discardLog())
		_, err := ing.Ingest(context.Background(), IngestUploadParams{
			ProjectID: "proj-1", Filename: "notes.md", MimeType: "text/markdown", Data: []byte("body"),
		})
		require.Error(t, err)
	})
}

// The production port implementation is the adapter over the unexported
// *Service.createFromUpload; *Service itself is deliberately not a
// DocumentCreator, so nothing outside this package can insert an upload row
// without going through UploadIngestor.
var _ DocumentCreator = serviceDocumentCreator{}

// ...and the same adapter is the ingestor's optional dedupe capability.
var _ ExistingUploadFinder = serviceDocumentCreator{}

// dedupeStubDocumentCreator is a DocumentCreator that also answers the dedupe
// lookup, recording every hash it was asked about.
type dedupeStubDocumentCreator struct {
	stubDocumentCreator
	existing *UploadDocumentResponse
	findErr  error
	lookedUp []string
}

func (f *dedupeStubDocumentCreator) FindExistingUploadByHash(_ context.Context, _, fileHash string) (*UploadDocumentResponse, error) {
	f.lookedUp = append(f.lookedUp, fileHash)
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.existing, nil
}

// TestUploadIngestor_DedupeReturnsExistingDocumentWithoutStoringOrQueueing is
// the regression test for a retried upload paying twice: when the same bytes
// already produced a document in this project, the ingest must not store them
// again, must not create a second row, and above all must not enqueue a second
// parse.
func TestUploadIngestor_DedupeReturnsExistingDocumentWithoutStoringOrQueueing(t *testing.T) {
	existingID := "doc-existing"
	doc := &dedupeStubDocumentCreator{existing: &UploadDocumentResponse{
		Document:           &DocumentSummary{ID: existingID},
		IsDuplicate:        true,
		ExistingDocumentID: &existingID,
	}}
	stor := &fakeStorage{enabled: true, uploadKey: "uploads/retry.md"}
	jobs := &fakeParsingJobs{}
	ing := NewUploadIngestor(doc, stor, jobs, nil, discardLog())

	resp, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID:        "proj-1",
		OrgID:            "org-1",
		Filename:         "notes.md",
		MimeType:         "text/markdown",
		Data:             []byte("# hello"),
		DedupeByFileHash: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.IsDuplicate)
	require.NotNil(t, resp.ExistingDocumentID)
	assert.Equal(t, existingID, *resp.ExistingDocumentID)

	assert.Equal(t, []string{computeFileHash([]byte("# hello"))}, doc.lookedUp,
		"the lookup must use the digest of the bytes being uploaded")
	assert.Empty(t, doc.got, "a duplicate must not create a second document row")
	assert.Zero(t, stor.uploadCalls, "a duplicate must not store the bytes again")
	assert.Empty(t, jobs.got, "a duplicate must not enqueue a second parsing job")
}

// TestUploadIngestor_DedupeMissUploadsNormally keeps the happy path: bytes that
// were never seen still become a document and a parsing job.
func TestUploadIngestor_DedupeMissUploadsNormally(t *testing.T) {
	doc := &dedupeStubDocumentCreator{}
	stor := &fakeStorage{enabled: true, uploadKey: "uploads/new.md"}
	jobs := &fakeParsingJobs{}
	ing := NewUploadIngestor(doc, stor, jobs, nil, discardLog())

	resp, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID:        "proj-1",
		Filename:         "notes.md",
		MimeType:         "text/markdown",
		Data:             []byte("# hello"),
		DedupeByFileHash: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsDuplicate)
	assert.Len(t, doc.lookedUp, 1)
	assert.Len(t, doc.got, 1)
	assert.Equal(t, 1, stor.uploadCalls)
	assert.Len(t, jobs.got, 1)
}

// TestUploadIngestor_DedupeIsOptIn protects the HTTP upload contract: a second
// upload of the same file is a second document unless the caller asked
// otherwise, which is why dedupe is a per-call flag rather than a property of
// the bytes.
func TestUploadIngestor_DedupeIsOptIn(t *testing.T) {
	doc := &dedupeStubDocumentCreator{existing: &UploadDocumentResponse{
		Document: &DocumentSummary{ID: "doc-existing"},
	}}
	stor := &fakeStorage{enabled: true, uploadKey: "uploads/again.md"}
	jobs := &fakeParsingJobs{}
	ing := NewUploadIngestor(doc, stor, jobs, nil, discardLog())

	resp, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID: "proj-1",
		Filename:  "notes.md",
		MimeType:  "text/markdown",
		Data:      []byte("# hello"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsDuplicate, "without the flag the upload is a new document")
	assert.Empty(t, doc.lookedUp, "no lookup may run for an upload that did not ask for dedupe")
	assert.Len(t, doc.got, 1)
	assert.Len(t, jobs.got, 1)
}

// TestUploadIngestor_DedupeLookupErrorStillUploads: dedupe is a cost
// optimisation, never a reason to fail an upload.
func TestUploadIngestor_DedupeLookupErrorStillUploads(t *testing.T) {
	doc := &dedupeStubDocumentCreator{findErr: errors.New("lookup timed out")}
	stor := &fakeStorage{enabled: true, uploadKey: "uploads/fallback.md"}
	jobs := &fakeParsingJobs{}
	ing := NewUploadIngestor(doc, stor, jobs, nil, discardLog())

	resp, err := ing.Ingest(context.Background(), IngestUploadParams{
		ProjectID:        "proj-1",
		Filename:         "notes.md",
		MimeType:         "text/markdown",
		Data:             []byte("# hello"),
		DedupeByFileHash: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, doc.got, 1)
	assert.Len(t, jobs.got, 1)
}
