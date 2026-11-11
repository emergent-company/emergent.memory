package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// --- Upload doubles ---------------------------------------------------------

type uploadTestStorage struct {
	enabled bool
	key     string
}

func (s *uploadTestStorage) Enabled() bool { return s.enabled }

func (s *uploadTestStorage) UploadDocument(_ context.Context, _ io.Reader, _ int64, _ storage.DocumentUploadOptions) (*storage.UploadResult, error) {
	return &storage.UploadResult{Key: s.key, StorageURL: "http://fake/" + s.key}, nil
}

func (s *uploadTestStorage) Delete(context.Context, string) error { return nil }

type uploadTestDocCreator struct {
	got []documents.UploadParams
	// existing is what an identical upload already produced, if any; findCalls
	// counts the dedupe lookups.
	existing  *documents.UploadDocumentResponse
	findCalls int
	err       error
}

func (c *uploadTestDocCreator) CreateFromUpload(_ context.Context, params documents.UploadParams) (*documents.UploadDocumentResponse, error) {
	c.got = append(c.got, params)
	if c.err != nil {
		return nil, c.err
	}
	return &documents.UploadDocumentResponse{Document: &documents.DocumentSummary{ID: "doc-mcp-1"}}, nil
}

func (c *uploadTestDocCreator) FindExistingUploadByHash(_ context.Context, _, _ string) (*documents.UploadDocumentResponse, error) {
	c.findCalls++
	return c.existing, nil
}

type uploadTestParsingJobs struct {
	got          []documents.ParsingJobOptions
	autoExtracts []bool
}

func (j *uploadTestParsingJobs) CreateJob(_ context.Context, opts documents.ParsingJobOptions) error {
	j.got = append(j.got, opts)
	j.autoExtracts = append(j.autoExtracts, opts.AutoExtract)
	return nil
}

type uploadTestAutoExtract struct {
	enabled bool
}

func (r *uploadTestAutoExtract) AutoExtractObjectsForProject(context.Context, string) (bool, error) {
	return r.enabled, nil
}

func newUploadTestService(t *testing.T, auto *uploadTestAutoExtract) (*Service, *uploadTestDocCreator, *uploadTestParsingJobs) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	creator := &uploadTestDocCreator{}
	jobs := &uploadTestParsingJobs{}
	var reader documents.ProjectAutoExtractReader
	if auto != nil {
		reader = auto
	}
	ing := documents.NewUploadIngestor(creator, &uploadTestStorage{enabled: true, key: "uploads/mcp.md"}, jobs, reader, log)
	return &Service{uploadIngestor: ing, log: log}, creator, jobs
}

// TestExecuteUploadDocument_QueuesExactlyOneParsingJob is the regression test
// for #586: an MCP upload must enqueue exactly one parsing job, like the HTTP
// upload paths, or the document is stored with no text and extraction later
// fails with "document has no content".
func TestExecuteUploadDocument_QueuesExactlyOneParsingJob(t *testing.T) {
	svc, creator, jobs := newUploadTestService(t, nil)

	ctx := auth.ContextWithOrgID(auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "user-1"}), "org-1")
	content := []byte("# from MCP\n")

	res, err := svc.executeUploadDocument(ctx, "proj-1", map[string]any{
		"filename":       "notes.md",
		"content_base64": base64.StdEncoding.EncodeToString(content),
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	require.Len(t, creator.got, 1, "exactly one document row per upload")
	require.Len(t, jobs.got, 1, "an MCP upload must enqueue exactly one parsing job")

	job := jobs.got[0]
	assert.Equal(t, "doc-mcp-1", job.DocumentID)
	assert.Equal(t, "proj-1", job.ProjectID)
	assert.Equal(t, "org-1", job.OrganizationID)
	assert.Equal(t, "file_upload", job.SourceType)
	assert.Equal(t, "notes.md", *job.SourceFilename)
	assert.Equal(t, "uploads/mcp.md", *job.StorageKey)
	assert.Equal(t, int64(len(content)), *job.FileSizeBytes)

	assert.Equal(t, "org-1", creator.got[0].OrgID)
	require.NotNil(t, creator.got[0].CreatedByUserID)
	assert.Equal(t, "user-1", *creator.got[0].CreatedByUserID)
}

// TestExecuteUploadDocument_FollowsProjectAutoExtract keeps the MCP path
// behaving like a REST upload with no explicit autoExtract parameter.
func TestExecuteUploadDocument_FollowsProjectAutoExtract(t *testing.T) {
	t.Run("project default on queues extraction", func(t *testing.T) {
		svc, _, jobs := newUploadTestService(t, &uploadTestAutoExtract{enabled: true})

		_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
			"filename":       "notes.md",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("body")),
		})
		require.NoError(t, err)
		require.Len(t, jobs.got, 1)
		assert.True(t, jobs.got[0].AutoExtract)
	})

	t.Run("no reader leaves extraction off", func(t *testing.T) {
		svc, _, jobs := newUploadTestService(t, nil)

		_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
			"filename":       "notes.md",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("body")),
		})
		require.NoError(t, err)
		require.Len(t, jobs.got, 1)
		assert.False(t, jobs.got[0].AutoExtract)
	})
}

// TestExecuteUploadDocument_RejectsEmptyContent checks the shared content guard
// still runs on the MCP path: nothing is created and no job is queued.
func TestExecuteUploadDocument_RejectsEmptyContent(t *testing.T) {
	svc, creator, jobs := newUploadTestService(t, nil)

	_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
		"filename":       "empty.md",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("   ")),
	})
	require.NoError(t, err, "whitespace-only text is valid content; the guard only rejects empty bytes")

	_, err = svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
		"filename":       "empty.md",
		"content_base64": "",
	})
	require.Error(t, err)
	assert.Len(t, creator.got, 1)
	assert.Len(t, jobs.got, 1)
}

// TestExecuteUploadDocument_FailsClosedWithoutIngestor: a deployment that never
// wires the upload pipeline must not create an unparsed document row.
func TestExecuteUploadDocument_FailsClosedWithoutIngestor(t *testing.T) {
	svc := &Service{}

	_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
		"filename":       "notes.md",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("body")),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

// TestExecuteUploadDocument_AutoExtractOverride is the regression test for the
// per-call extraction override: an agent must be able to store a file without
// spending extraction tokens even in a project that extracts by default, and to
// request extraction in a project that does not.
func TestExecuteUploadDocument_AutoExtractOverride(t *testing.T) {
	t.Run("explicit false overrides a project default of on", func(t *testing.T) {
		svc, _, jobs := newUploadTestService(t, &uploadTestAutoExtract{enabled: true})

		_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
			"filename":       "notes.md",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("body")),
			"auto_extract":   false,
		})
		require.NoError(t, err)
		require.Len(t, jobs.got, 1)
		assert.False(t, jobs.got[0].AutoExtract,
			"an explicit auto_extract=false must win over the project default")
	})

	t.Run("explicit true overrides a project default of off", func(t *testing.T) {
		svc, _, jobs := newUploadTestService(t, &uploadTestAutoExtract{enabled: false})

		_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
			"filename":       "notes.md",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("body")),
			"auto_extract":   true,
		})
		require.NoError(t, err)
		require.Len(t, jobs.got, 1)
		assert.True(t, jobs.got[0].AutoExtract)
	})

	t.Run("absent still follows the project default", func(t *testing.T) {
		svc, _, jobs := newUploadTestService(t, &uploadTestAutoExtract{enabled: true})

		_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
			"filename":       "notes.md",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("body")),
		})
		require.NoError(t, err)
		require.Len(t, jobs.got, 1)
		assert.True(t, jobs.got[0].AutoExtract)
	})

	t.Run("a non-boolean value is refused, not coerced", func(t *testing.T) {
		svc, creator, jobs := newUploadTestService(t, nil)

		_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
			"filename":       "notes.md",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("body")),
			"auto_extract":   "false",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "boolean")
		assert.Empty(t, creator.got, "nothing may be stored for a rejected argument")
		assert.Empty(t, jobs.got)
	})
}

// TestExecuteUploadDocument_RetryDoesNotReparse is the regression test for the
// retry cost: an MCP client that resends the same bytes (timeout, reconnect,
// unsure the call landed) must get the document the first attempt created, not a
// second row and a second parse.
func TestExecuteUploadDocument_RetryDoesNotReparse(t *testing.T) {
	svc, creator, jobs := newUploadTestService(t, &uploadTestAutoExtract{enabled: true})
	args := map[string]any{
		"filename":       "notes.md",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("# once")),
	}

	first, err := svc.executeUploadDocument(context.Background(), "proj-1", args)
	require.NoError(t, err)
	require.Len(t, creator.got, 1)
	require.Len(t, jobs.got, 1)
	require.Equal(t, 1, creator.findCalls, "the MCP path must ask whether these bytes already exist")
	_ = first

	// The first upload is now a document in the project; the retry must find it.
	existingID := "doc-mcp-1"
	creator.existing = &documents.UploadDocumentResponse{
		Document:           &documents.DocumentSummary{ID: existingID},
		IsDuplicate:        true,
		ExistingDocumentID: &existingID,
	}

	res, err := svc.executeUploadDocument(context.Background(), "proj-1", args)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, creator.got, 1, "the retry must not create a second document")
	require.Len(t, jobs.got, 1, "the retry must not enqueue a second parse")

	require.NotEmpty(t, res.Content)
	var payload documents.UploadDocumentResponse
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].Text), &payload))
	assert.True(t, payload.IsDuplicate, "a retry must be reported as a duplicate, never silently")
	require.NotNil(t, payload.ExistingDocumentID)
	assert.Equal(t, existingID, *payload.ExistingDocumentID)
}

// TestExecuteUploadDocument_DoesNotLeakInternalDetail: the tool result is
// client-visible, so a failed ingest must report the stable message and keep the
// wrapped cause (SQL text, upstream bodies) in the log.
func TestExecuteUploadDocument_DoesNotLeakInternalDetail(t *testing.T) {
	svc, creator, _ := newUploadTestService(t, nil)
	creator.err = apperror.ErrInternal.WithInternal(errors.New(`pq: relation "kb.documents" does not exist`))

	_, err := svc.executeUploadDocument(context.Background(), "proj-1", map[string]any{
		"filename":       "notes.md",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("body")),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload_document:")
	assert.Contains(t, err.Error(), apperror.ErrInternal.Message)
	assert.NotContains(t, err.Error(), `pq: relation`)
	assert.NotContains(t, err.Error(), "internal_error:")
}
