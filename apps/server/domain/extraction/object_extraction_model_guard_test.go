package extraction_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func guardTestLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// The generative-model gate now lives at the job-creation chokepoint
// (ObjectExtractionJobsService.CreateJob) rather than the admin handler, so
// every creation path is covered. These DB-backed tests exercise the gate
// directly (service + MCP adapter + auto-extract), independent of the HTTP
// surface.

// TestCreateJobRejectsWithoutGenerativeModel is the core chokepoint guard: a
// project with no resolvable generative model must be rejected before a job row
// is inserted, instead of enqueueing a job that dies at run time (#1555).
func TestCreateJobRejectsWithoutGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "chokepoint_no_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	log := guardTestLog()
	svc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), log, extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{})

	src := "document"
	_, err := svc.CreateJob(ctx, extraction.CreateObjectExtractionJobOptions{ProjectID: projectID, SourceType: &src})

	require.ErrorIs(t, err, extraction.ErrNoGenerativeModel,
		"CreateJob with no generative model must return the sentinel, got: %v", err)
	require.Equal(t, 0, countExtractionJobs(t, ctx, testDB, projectID),
		"no job row may be inserted when the gate rejects")
}

// TestCreateJobSucceedsWithGenerativeModel proves the gate does not over-block:
// with a resolvable model the job is created as before.
func TestCreateJobSucceedsWithGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "chokepoint_with_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	svc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), guardTestLog(), extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{model: "deepseek/deepseek-v4-flash"})

	src := "document"
	job, err := svc.CreateJob(ctx, extraction.CreateObjectExtractionJobOptions{ProjectID: projectID, SourceType: &src})

	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, 1, countExtractionJobs(t, ctx, testDB, projectID))
}

// TestCreateJobFailsOpenOnResolverError preserves pre-existing behavior on a
// transient resolution error: extraction is not blocked (matching the gateway's
// extractionModelMissing fail-open convention).
func TestCreateJobFailsOpenOnResolverError(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "chokepoint_resolver_err")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	svc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), guardTestLog(), extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{err: errors.New("model config store unavailable")})

	src := "document"
	_, err := svc.CreateJob(ctx, extraction.CreateObjectExtractionJobOptions{ProjectID: projectID, SourceType: &src})

	require.NoError(t, err, "resolution error must fail open")
	require.Equal(t, 1, countExtractionJobs(t, ctx, testDB, projectID))
}

// TestCreateJobFailsOpenWithoutResolver is the mutation counterpoint: a service
// built without a resolver has no gate at all, proving the empty-model gate is
// what blocks creation (and keeping 3-arg constructor callers/tests working).
func TestCreateJobFailsOpenWithoutResolver(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "chokepoint_no_resolver")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	svc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), guardTestLog(), extraction.DefaultObjectExtractionConfig())

	src := "document"
	_, err := svc.CreateJob(ctx, extraction.CreateObjectExtractionJobOptions{ProjectID: projectID, SourceType: &src})

	require.NoError(t, err, "no resolver => gate disabled => fail open")
	require.Equal(t, 1, countExtractionJobs(t, ctx, testDB, projectID))
}

// TestReextractionQueuerRejectsWithoutGenerativeModel covers the MCP
// queue-reextraction path (previously unguarded): it must return a clear error,
// not a panic/500, and insert no row.
func TestReextractionQueuerRejectsWithoutGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "chokepoint_mcp_no_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	svc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), guardTestLog(), extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{})
	queuer := extraction.NewReextractionQueuerMCPAdapter(svc)

	_, err := queuer.QueueReextraction(ctx, projectID, uuid.New().String(), uuid.New().String())

	require.Error(t, err, "MCP reextraction must be rejected when no model resolves")
	require.ErrorIs(t, err, extraction.ErrNoGenerativeModel)
	require.Contains(t, err.Error(), "generative model",
		"error must be a clear, actionable reason, got: %v", err)
	require.Equal(t, 0, countExtractionJobs(t, ctx, testDB, projectID))
}

// TestReextractionQueuerSucceedsWithGenerativeModel proves the MCP path still
// creates a reextraction job when a model resolves.
func TestReextractionQueuerSucceedsWithGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "chokepoint_mcp_with_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	documentID := createGuardTestDocument(t, ctx, testDB, projectID)
	svc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), guardTestLog(), extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{model: "deepseek/deepseek-v4-flash"})
	queuer := extraction.NewReextractionQueuerMCPAdapter(svc)

	jobID, err := queuer.QueueReextraction(ctx, projectID, documentID, uuid.New().String())

	require.NoError(t, err)
	require.NotEmpty(t, jobID)
	require.Equal(t, 1, countExtractionJobs(t, ctx, testDB, projectID))
}

// TestTriggerForDocumentSkipsWithoutGenerativeModel covers the document
// auto-extract path (raw-content create and post-parse): it must surface the
// sentinel so the caller can log and skip, and insert no row.
func TestTriggerForDocumentSkipsWithoutGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "chokepoint_autoextract_no_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	documentID := createGuardTestDocument(t, ctx, testDB, projectID)
	svc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), guardTestLog(), extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{})

	err := svc.TriggerForDocument(ctx, projectID, documentID)

	require.ErrorIs(t, err, extraction.ErrNoGenerativeModel)
	require.Equal(t, 0, countExtractionJobs(t, ctx, testDB, projectID))
}

// TestTriggerForDocumentCreatesWithGenerativeModel proves auto-extract still
// creates a job when a model resolves.
func TestTriggerForDocumentCreatesWithGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "chokepoint_autoextract_with_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	documentID := createGuardTestDocument(t, ctx, testDB, projectID)
	svc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), guardTestLog(), extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{model: "deepseek/deepseek-v4-flash"})

	err := svc.TriggerForDocument(ctx, projectID, documentID)

	require.NoError(t, err)
	require.Equal(t, 1, countExtractionJobs(t, ctx, testDB, projectID))
}

// createGuardTestDocument inserts a real kb.documents row so paths that set
// document_id satisfy the FK constraint.
func createGuardTestDocument(t *testing.T, ctx context.Context, testDB *testutil.TestDB, projectID string) string {
	t.Helper()
	documentID := uuid.New().String()
	filename := "guard-test.txt"
	content := "guard test document content"
	require.NoError(t, testutil.CreateTestDocument(ctx, testDB.GetDB(), testutil.TestDocument{
		ID:        documentID,
		ProjectID: projectID,
		Filename:  &filename,
		Content:   &content,
	}))
	return documentID
}
