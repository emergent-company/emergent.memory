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
