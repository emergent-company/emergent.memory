package integration

// SourceSyncSuite exercises the document-source sync lifecycle: the sync worker
// (with a fake Drive client), the
// single-active-job admission rule, stale recovery, scheduled admission, and the
// sync REST surface. It runs against the in-process test server backed by a real
// Postgres test database (testutil.BaseSuite); the suite skips via
// testdb.SkipOrFatal when Postgres is unreachable.

import (
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/sources"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/gdrive"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func TestSourceSyncSuite(t *testing.T) {
	suite.Run(t, new(SourceSyncSuite))
}

type SourceSyncSuite struct {
	testutil.BaseSuite
	testKey []byte
	log     *slog.Logger
}

func (s *SourceSyncSuite) SetupSuite() {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	s.testKey = key
	// Set before the server/config is built so the sources service derives the
	// same config-sourced credential-encryption key as production.
	s.T().Setenv("LLM_ENCRYPTION_KEY", hex.EncodeToString(key))
	s.SetDBSuffix("sourcesync")
	s.BaseSuite.SetupSuite()
	s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newServiceWith builds a sources.Service on the test transaction with a real
// key and the given provider registry.
func (s *SourceSyncSuite) newServiceWith(registry *sources.Registry) *sources.Service {
	cfg := &config.Config{}
	cfg.LLMProvider.EncryptionKey = hex.EncodeToString(s.testKey)
	return sources.NewService(sources.NewRepository(s.DB(), s.log), cfg, s.log, registry)
}

// newService builds a sources.Service on the test transaction with a real key.
func (s *SourceSyncSuite) newService() *sources.Service {
	return s.newServiceWith(sources.NewDefaultRegistry())
}

func (s *SourceSyncSuite) newDocsService() *documents.Service {
	return documents.NewService(documents.NewRepository(s.DB(), s.log), s.log)
}

func (s *SourceSyncSuite) projectUUID() uuid.UUID {
	return uuid.MustParse(s.ProjectID)
}

func (s *SourceSyncSuite) createSource(svc *sources.Service, interval *int) string {
	s.T().Helper()
	created, err := svc.Create(s.Ctx, s.projectUUID(), &sources.CreateIntegrationRequest{
		Name:         "Drive",
		ProviderType: sources.ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder-1",
			"client_id":     "cid",
			"client_secret": "cs",
			"refresh_token": "rt",
		},
		SyncIntervalMinutes: interval,
	})
	s.Require().NoError(err)
	return created.ID
}

func (s *SourceSyncSuite) trigger(svc *sources.Service, sourceID string) *sources.TriggerSyncResponse {
	s.T().Helper()
	resp, err := svc.TriggerSync(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID))
	s.Require().NoError(err)
	return resp
}

// ---------------------------------------------------------------------------
// Fake Drive client
// ---------------------------------------------------------------------------

type fakeDrive struct {
	mu            sync.Mutex
	items         []gdrive.Item
	exportText    map[string]string
	downloadBytes map[string]string
	fetchErr      map[string]error
	listErr       error
	exportCalls   []string
	downloadCalls []string
}

func (f *fakeDrive) ListFolderItems(_ context.Context, _ string, _ []string, _ bool, _ string) ([]gdrive.Item, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.items, nil
}

func (f *fakeDrive) ExportText(_ context.Context, fileID, _ string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exportCalls = append(f.exportCalls, fileID)
	if err := f.fetchErr[fileID]; err != nil {
		return nil, err
	}
	return []byte(f.exportText[fileID]), nil
}

func (f *fakeDrive) DownloadFile(_ context.Context, fileID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloadCalls = append(f.downloadCalls, fileID)
	if err := f.fetchErr[fileID]; err != nil {
		return nil, err
	}
	return []byte(f.downloadBytes[fileID]), nil
}

func (f *fakeDrive) exported() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.exportCalls...)
}

func (f *fakeDrive) downloaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.downloadCalls...)
}

func (f *fakeDrive) fetched() []string {
	return append(f.exported(), f.downloaded()...)
}

func (f *fakeDrive) TestConnection(_ context.Context) error { return nil }

func (f *fakeDrive) ListFolders(_ context.Context, _, _ string) ([]gdrive.Item, bool, error) {
	return nil, false, nil
}

func (f *fakeDrive) GetFolder(_ context.Context, folderID string) (gdrive.Folder, error) {
	return gdrive.Folder{ID: folderID}, nil
}

func (f *fakeDrive) AccessToken(_ context.Context) (string, time.Time, error) {
	return "integration-token", time.Now().Add(time.Hour), nil
}

func (f *fakeDrive) AccountEmail(_ context.Context) (string, error) { return "", nil }

func (s *SourceSyncSuite) newWorker(fake *fakeDrive) *sources.SyncWorker {
	provider := sources.NewGoogleDriveProvider(func(_ context.Context, _ gdrive.Credentials) (sources.DriveTransport, error) {
		return fake, nil
	})
	svc := s.newServiceWith(sources.NewRegistry(provider))
	return sources.NewSyncWorker(svc, s.newDocsService(), nil, nil, s.log)
}

// ---------------------------------------------------------------------------
// Worker lifecycle
// ---------------------------------------------------------------------------

func (s *SourceSyncSuite) TestSyncLifecycle() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	resp := s.trigger(svc, sourceID)
	s.Require().True(resp.Success, "first trigger must succeed")
	s.Require().NotNil(resp.JobID)
	jobID := uuid.MustParse(*resp.JobID)

	// Pending before processing.
	dto, err := svc.GetSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), jobID)
	s.Require().NoError(err)
	s.Require().Equal(sources.SyncStatusPending, dto.Status)

	fake := &fakeDrive{items: []gdrive.Item{}}
	worker := s.newWorker(fake)
	s.Require().NoError(worker.RunOnce(s.Ctx))

	dto, err = svc.GetSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), jobID)
	s.Require().NoError(err)
	s.Require().Equal(sources.SyncStatusCompleted, dto.Status, "job must complete")
	s.Require().NotNil(dto.StartedAt, "started_at must be recorded")
	s.Require().NotNil(dto.CompletedAt, "completed_at must be recorded")
	s.Require().Equal(sources.TriggerTypeManual, dto.TriggerType)
}

func (s *SourceSyncSuite) TestSyncSkipsUnchangedRevision() {
	// Pre-ingest an item at rev-1.
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	docsSvc := s.newDocsService()
	_, err := docsSvc.ApplySourceItem(s.Ctx, documents.ApplySourceItemParams{
		ProjectID:  s.ProjectID,
		SourceID:   sourceID,
		ExternalID: "file-1",
		RevisionID: "rev-1",
		Filename:   "notes.txt",
		MimeType:   "text/plain",
		Content:    "original parsed text body for the unchanged-revision sync test",
	})
	s.Require().NoError(err)

	resp := s.trigger(svc, sourceID)
	s.Require().True(resp.Success)
	jobID := uuid.MustParse(*resp.JobID)

	fake := &fakeDrive{
		items:         []gdrive.Item{{ID: "file-1", Name: "notes.txt", MimeType: "text/plain", RevisionID: "rev-1"}},
		downloadBytes: map[string]string{"file-1": "original parsed text body for the unchanged-revision sync test"},
	}
	s.Require().NoError(s.newWorker(fake).RunOnce(s.Ctx))

	s.Require().Empty(fake.fetched(), "unchanged revision must not be fetched")

	dto, err := svc.GetSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), jobID)
	s.Require().NoError(err)
	s.Require().Equal(1, dto.TotalItems)
	s.Require().Equal(1, dto.ProcessedItems)
	s.Require().Equal(1, dto.SkippedItems)
	s.Require().Equal(0, dto.SuccessfulItems)
}

func (s *SourceSyncSuite) TestSyncIngestsChangedItem() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	docsSvc := s.newDocsService()
	r1, err := docsSvc.ApplySourceItem(s.Ctx, documents.ApplySourceItemParams{
		ProjectID:  s.ProjectID,
		SourceID:   sourceID,
		ExternalID: "file-1",
		RevisionID: "rev-1",
		Filename:   "notes.txt",
		MimeType:   "text/plain",
		Content:    "original content",
	})
	s.Require().NoError(err)
	s.Require().True(r1.Created)

	resp := s.trigger(svc, sourceID)
	s.Require().True(resp.Success)
	jobID := uuid.MustParse(*resp.JobID)

	fake := &fakeDrive{
		items:         []gdrive.Item{{ID: "file-1", Name: "notes.txt", MimeType: "text/plain", RevisionID: "rev-2"}},
		downloadBytes: map[string]string{"file-1": "changed parsed text that differs from the original content enough to change the hash"},
	}
	s.Require().NoError(s.newWorker(fake).RunOnce(s.Ctx))

	s.Require().Equal([]string{"file-1"}, fake.downloaded(), "changed item must be fetched")

	versions, err := docsSvc.GetVersionHistory(s.Ctx, s.ProjectID, r1.DocumentID)
	s.Require().NoError(err)
	s.Require().Len(versions, 2, "changed item must append a version")

	dto, err := svc.GetSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), jobID)
	s.Require().NoError(err)
	s.Require().Equal(1, dto.SuccessfulItems)
}

// TestSyncEmptyRevisionIsNotSkipped is the regression guard for Google-native
// Docs: Drive reports no headRevisionId for them, so both the stored revision
// and the reported revision were empty. The pre-filter treated that as
// "unchanged" and never re-fetched an edited document, so no next version was
// ever created. No revision signal must mean "fetch and let the parsed-text
// hash decide".
func (s *SourceSyncSuite) TestSyncEmptyRevisionIsNotSkipped() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	docsSvc := s.newDocsService()
	r1, err := docsSvc.ApplySourceItem(s.Ctx, documents.ApplySourceItemParams{
		ProjectID:  s.ProjectID,
		SourceID:   sourceID,
		ExternalID: "doc-1",
		RevisionID: "",
		Filename:   "Spec",
		MimeType:   "application/vnd.google-apps.document",
		Content:    "first export of the google doc",
	})
	s.Require().NoError(err)
	s.Require().True(r1.Created)

	resp := s.trigger(svc, sourceID)
	s.Require().True(resp.Success)
	jobID := uuid.MustParse(*resp.JobID)

	fake := &fakeDrive{
		items:      []gdrive.Item{{ID: "doc-1", Name: "Spec", MimeType: "application/vnd.google-apps.document", RevisionID: ""}},
		exportText: map[string]string{"doc-1": "second export of the google doc, edited"},
	}
	s.Require().NoError(s.newWorker(fake).RunOnce(s.Ctx))

	s.Require().Equal([]string{"doc-1"}, fake.exported(), "an item with no revision must still be fetched")

	versions, err := docsSvc.GetVersionHistory(s.Ctx, s.ProjectID, r1.DocumentID)
	s.Require().NoError(err)
	s.Require().Len(versions, 2, "edited content must append a version")

	dto, err := svc.GetSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), jobID)
	s.Require().NoError(err)
	s.Require().Equal(0, dto.SkippedItems)
	s.Require().Equal(1, dto.SuccessfulItems)
}

func (s *SourceSyncSuite) TestSyncPerItemFailureDoesNotAbort() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	resp := s.trigger(svc, sourceID)
	s.Require().True(resp.Success)
	jobID := uuid.MustParse(*resp.JobID)

	fake := &fakeDrive{
		items: []gdrive.Item{
			{ID: "bad", Name: "bad.txt", MimeType: "text/plain", RevisionID: "rev-1"},
			{ID: "good", Name: "good.txt", MimeType: "text/plain", RevisionID: "rev-1"},
		},
		downloadBytes: map[string]string{"good": "some parsed content for the good item"},
		fetchErr:      map[string]error{"bad": io.ErrUnexpectedEOF},
	}
	s.Require().NoError(s.newWorker(fake).RunOnce(s.Ctx))

	dto, err := svc.GetSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), jobID)
	s.Require().NoError(err)
	s.Require().Equal(sources.SyncStatusCompleted, dto.Status, "one item failing must not abort the sync")
	s.Require().Equal(2, dto.ProcessedItems)
	s.Require().Equal(1, dto.SuccessfulItems)
	s.Require().Equal(1, dto.FailedItems)
}

func (s *SourceSyncSuite) TestSyncCancelStopsFetching() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	resp := s.trigger(svc, sourceID)
	s.Require().True(resp.Success)
	jobID := uuid.MustParse(*resp.JobID)

	// Claim the job (pending → processing) as the worker would.
	repo := sources.NewRepository(s.DB(), s.log)
	ids, err := repo.DequeueSyncJob(s.Ctx, 1)
	s.Require().NoError(err)
	s.Require().Len(ids, 1)

	// Cancel the running job.
	cancelResp, err := svc.CancelSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), jobID)
	s.Require().NoError(err)
	s.Require().True(cancelResp.Success)

	// Processing the job now observes the cancellation flag and fetches nothing.
	fake := &fakeDrive{
		items:         []gdrive.Item{{ID: "file-1", Name: "a.txt", MimeType: "text/plain", RevisionID: "rev-1"}},
		downloadBytes: map[string]string{"file-1": "content"},
	}
	job, err := repo.GetSyncJobByID(s.Ctx, jobID)
	s.Require().NoError(err)
	s.Require().NoError(s.newWorker(fake).ProcessJob(s.Ctx, job))

	s.Require().Empty(fake.fetched(), "cancelled job must not fetch items")

	dto, err := svc.GetSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), jobID)
	s.Require().NoError(err)
	s.Require().Equal(sources.SyncStatusCancelled, dto.Status)
}

func (s *SourceSyncSuite) TestSyncRemovedItemMarkedDeleted() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	docsSvc := s.newDocsService()
	_, err := docsSvc.ApplySourceItem(s.Ctx, documents.ApplySourceItemParams{
		ProjectID:  s.ProjectID,
		SourceID:   sourceID,
		ExternalID: "file-1",
		RevisionID: "rev-1",
		Filename:   "notes.txt",
		MimeType:   "text/plain",
		Content:    "content that will be removed from the source",
	})
	s.Require().NoError(err)

	resp := s.trigger(svc, sourceID)
	s.Require().True(resp.Success)

	// Enumerate an empty folder: the previously ingested item is now absent.
	fake := &fakeDrive{items: []gdrive.Item{}}
	s.Require().NoError(s.newWorker(fake).RunOnce(s.Ctx))

	var status string
	err = s.DB().NewRaw(`SELECT source_status FROM kb.documents WHERE source_external_id = ? AND supersedes_id IS NULL`, "file-1").Scan(s.Ctx, &status)
	s.Require().NoError(err)
	s.Require().Equal(documents.SourceStatusDeletedFromSource, status, "removed item must be marked deleted-from-source")
}

// TestSyncRestoredItemReactivatesWithUnchangedRevision covers the trash-restore
// edge: restoring an item from Drive trash does not bump headRevisionId, so the
// revision pre-filter must not skip a deleted HEAD — it must route the item
// through ApplySourceItem to reactivate it.
func (s *SourceSyncSuite) TestSyncRestoredItemReactivatesWithUnchangedRevision() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	docsSvc := s.newDocsService()

	const content = "content that is removed then restored unchanged"
	_, err := docsSvc.ApplySourceItem(s.Ctx, documents.ApplySourceItemParams{
		ProjectID:  s.ProjectID,
		SourceID:   sourceID,
		ExternalID: "file-restore",
		RevisionID: "rev-1",
		Filename:   "restore.txt",
		MimeType:   "text/plain",
		Content:    content,
	})
	s.Require().NoError(err)

	headStatus := func() string {
		s.T().Helper()
		var status string
		err := s.DB().NewRaw(`SELECT source_status FROM kb.documents WHERE source_external_id = ? AND supersedes_id IS NULL`, "file-restore").Scan(s.Ctx, &status)
		s.Require().NoError(err)
		return status
	}

	// First sync: the item is absent -> marked deleted-from-source.
	resp := s.trigger(svc, sourceID)
	s.Require().True(resp.Success)
	s.Require().NoError(s.newWorker(&fakeDrive{items: []gdrive.Item{}}).RunOnce(s.Ctx))
	s.Require().Equal(documents.SourceStatusDeletedFromSource, headStatus())

	// Second sync: the item re-appears with the SAME revision and content.
	resp = s.trigger(svc, sourceID)
	s.Require().True(resp.Success, "re-trigger after completion must succeed")
	fake := &fakeDrive{
		items:         []gdrive.Item{{ID: "file-restore", Name: "restore.txt", MimeType: "text/plain", RevisionID: "rev-1"}},
		downloadBytes: map[string]string{"file-restore": content},
	}
	s.Require().NoError(s.newWorker(fake).RunOnce(s.Ctx))
	s.Require().NotEmpty(fake.fetched(), "a deleted HEAD must be re-fetched even when its revision is unchanged")
	s.Require().Equal(documents.SourceStatusActive, headStatus(), "restored item must be reactivated")
}

// ---------------------------------------------------------------------------
// Admission + stale recovery
// ---------------------------------------------------------------------------

func (s *SourceSyncSuite) TestAdmissionRejectsOverlapping() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)

	first := s.trigger(svc, sourceID)
	s.Require().True(first.Success)
	s.Require().NotNil(first.JobID)

	second := s.trigger(svc, sourceID)
	s.Require().False(second.Success, "second trigger while pending must be rejected")
	s.Require().Equal(*first.JobID, *second.JobID, "rejected trigger must return the existing job")

	// Only one active job exists.
	jobs, err := svc.ListSyncJobs(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), nil)
	s.Require().NoError(err)
	active := 0
	for _, j := range jobs.Items {
		if j.Status == sources.SyncStatusPending || j.Status == sources.SyncStatusProcessing {
			active++
		}
	}
	s.Require().Equal(1, active, "exactly one active job must exist")
}

func (s *SourceSyncSuite) TestStaleRecoveryKeepsSingleActiveJob() {
	svc := s.newService()
	sourceID := s.createSource(svc, nil)
	repo := sources.NewRepository(s.DB(), s.log)

	// Insert a stale processing job directly (simulate a crash mid-sync).
	staleID := uuid.New()
	now := time.Now().UTC()
	stale := &sources.SyncJob{
		ID:          staleID,
		Status:      sources.SyncStatusProcessing,
		ScheduledAt: now,
		ProjectID:   s.projectUUID(),
		SourceID:    uuid.MustParse(sourceID),
		TriggerType: sources.TriggerTypeManual,
		StartedAt:   ptrTime(now.Add(-time.Hour)),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := repo.CreateSyncJob(s.Ctx, stale)
	s.Require().NoError(err)

	recovered, err := repo.RecoverStaleJobs(s.Ctx, 1)
	s.Require().NoError(err)
	s.Require().Equal(1, recovered)

	// The recovered job is pending and still the single active job; a trigger
	// now returns it rather than admitting a second.
	resp := s.trigger(svc, sourceID)
	s.Require().False(resp.Success)
	s.Require().Equal(staleID.String(), *resp.JobID)
}

func (s *SourceSyncSuite) TestAdmitDueSources() {
	svc := s.newService()
	repo := sources.NewRepository(s.DB(), s.log)

	// A: due (interval set, last synced long ago).
	dueID := s.createSource(svc, ptrInt(5))
	// B: not due (interval set, just synced).
	notDueID := s.createSource(svc, ptrInt(60))
	// C: no interval (never scheduled).
	noIntervalID := s.createSource(svc, nil)

	now := time.Now().UTC()
	s.Require().NoError(repo.AdvanceSourceCursor(s.Ctx, s.projectUUID(), uuid.MustParse(notDueID), map[string]any{"t": "x"}, now))
	s.Require().NoError(repo.AdvanceSourceCursor(s.Ctx, s.projectUUID(), uuid.MustParse(dueID), map[string]any{"t": "x"}, now.Add(-10*time.Minute)))

	s.Require().NoError(svc.AdmitDueSources(s.Ctx))

	// A admitted (scheduled), B and C not.
	s.requireActiveJobCount(sourceIDUUID(dueID), 1)
	s.requireActiveJobCount(sourceIDUUID(notDueID), 0)
	s.requireActiveJobCount(sourceIDUUID(noIntervalID), 0)

	jobs, err := svc.ListSyncJobs(s.Ctx, s.projectUUID(), uuid.MustParse(dueID), nil)
	s.Require().NoError(err)
	s.Require().Len(jobs.Items, 1)
	s.Require().Equal(sources.TriggerTypeScheduled, jobs.Items[0].TriggerType)

	// Second sweep: active job suppresses a duplicate.
	s.Require().NoError(svc.AdmitDueSources(s.Ctx))
	s.requireActiveJobCount(sourceIDUUID(dueID), 1)
}

func (s *SourceSyncSuite) requireActiveJobCount(sourceID uuid.UUID, want int) {
	s.T().Helper()
	repo := sources.NewRepository(s.DB(), s.log)
	jobs, err := repo.ListSyncJobs(s.Ctx, s.projectUUID(), sourceID, nil)
	s.Require().NoError(err)
	active := 0
	for _, j := range jobs {
		if j.Status == sources.SyncStatusPending || j.Status == sources.SyncStatusProcessing {
			active++
		}
	}
	s.Require().Equal(want, active)
}

func sourceIDUUID(id string) uuid.UUID { return uuid.MustParse(id) }

func ptrInt(v int) *int              { return &v }
func ptrTime(v time.Time) *time.Time { return &v }

// ---------------------------------------------------------------------------
// Handler / REST surface
// ---------------------------------------------------------------------------

func (s *SourceSyncSuite) createSourceHTTP() string {
	s.T().Helper()
	resp := s.Client.POST("/api/data-source-integrations",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "Drive",
			"providerType": "google_drive",
			"config": map[string]any{
				"folder_id":     "folder-1",
				"client_id":     "cid",
				"client_secret": "cs",
				"refresh_token": "rt",
			},
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode, resp.String())
	var created map[string]any
	s.Require().NoError(resp.JSON(&created))
	id, _ := created["id"].(string)
	s.Require().NotEmpty(id)
	return id
}

func (s *SourceSyncSuite) TestSyncTriggerHandlerSuccessAndConflict() {
	id := s.createSourceHTTP()

	first := s.Client.POST("/api/data-source-integrations/"+id+"/sync",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, first.StatusCode, first.String())
	var f map[string]any
	s.Require().NoError(first.JSON(&f))
	s.Require().Equal(true, f["success"])
	jobID, _ := f["jobId"].(string)
	s.Require().NotEmpty(jobID)

	second := s.Client.POST("/api/data-source-integrations/"+id+"/sync",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, second.StatusCode, second.String())
	var g map[string]any
	s.Require().NoError(second.JSON(&g))
	s.Require().Equal(false, g["success"])
	s.Require().Equal(jobID, g["jobId"])
}

func (s *SourceSyncSuite) TestSyncJobEndpoints() {
	id := s.createSourceHTTP()

	trigger := s.Client.POST("/api/data-source-integrations/"+id+"/sync",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, trigger.StatusCode)
	var tr map[string]any
	s.Require().NoError(trigger.JSON(&tr))
	jobID, _ := tr["jobId"].(string)
	s.Require().NotEmpty(jobID)

	// List.
	list := s.Client.GET("/api/data-source-integrations/"+id+"/sync-jobs",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, list.StatusCode, list.String())
	var lr sources.ListSyncJobsResponse
	s.Require().NoError(list.JSON(&lr))
	s.Require().Len(lr.Items, 1)
	s.Require().Equal(sources.SyncStatusPending, lr.Items[0].Status)
	s.Require().Equal(sources.TriggerTypeManual, lr.Items[0].TriggerType)

	// Latest.
	latest := s.Client.GET("/api/data-source-integrations/"+id+"/sync-jobs/latest",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, latest.StatusCode, latest.String())
	var latestJob sources.SyncJobDTO
	s.Require().NoError(latest.JSON(&latestJob))
	s.Require().Equal(jobID, latestJob.ID)

	// Detail.
	detail := s.Client.GET("/api/data-source-integrations/"+id+"/sync-jobs/"+jobID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, detail.StatusCode, detail.String())
	var job sources.SyncJobDTO
	s.Require().NoError(detail.JSON(&job))
	s.Require().Equal(jobID, job.ID)
	s.Require().Equal(id, job.IntegrationID)

	// Cancel.
	cancel := s.Client.POST("/api/data-source-integrations/"+id+"/sync-jobs/"+jobID+"/cancel",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, cancel.StatusCode, cancel.String())
	var cr sources.CancelSyncJobResponse
	s.Require().NoError(cancel.JSON(&cr))
	s.Require().True(cr.Success)

	// After cancel: status cancelled.
	detail = s.Client.GET("/api/data-source-integrations/"+id+"/sync-jobs/"+jobID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, detail.StatusCode)
	s.Require().NoError(detail.JSON(&job))
	s.Require().Equal(sources.SyncStatusCancelled, job.Status)
}

func (s *SourceSyncSuite) TestSyncJobEndpointsProjectScope404() {
	id := s.createSourceHTTP()
	trigger := s.Client.POST("/api/data-source-integrations/"+id+"/sync",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, trigger.StatusCode)
	var tr map[string]any
	s.Require().NoError(trigger.JSON(&tr))
	jobID, _ := tr["jobId"].(string)

	otherProject := uuid.New().String()
	resp := s.Client.GET("/api/data-source-integrations/"+id+"/sync-jobs/"+jobID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(otherProject))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode, "cross-project job lookup must be 404: %s", resp.String())
}

func (s *SourceSyncSuite) TestSyncJobEndpointsUnauth401() {
	id := uuid.New().String()
	resp := s.Client.GET("/api/data-source-integrations/" + id + "/sync-jobs")
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode, "unauthenticated sync-jobs must be 401")
}
