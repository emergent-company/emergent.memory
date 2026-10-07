package sources

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/internal/gdrive"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/xberg"
)

// DriveClient is the Drive transport surface the sync worker needs. It is the
// subset of *gdrive.Client used for enumeration and content fetch, and it is
// satisfied by a fake in integration tests (no live Google calls).
type DriveClient interface {
	ListFolderItems(ctx context.Context, folderID string, mimeTypes []string, recursive bool, sharedDriveID string) ([]gdrive.Item, error)
	ExportText(ctx context.Context, fileID, mimeType string) ([]byte, error)
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
}

// DriveClientFactory builds a DriveClient from decrypted credentials. Injectable
// (constructor injection) so an integration test can substitute a fake.
type DriveClientFactory func(ctx context.Context, creds gdrive.Credentials) (DriveClient, error)

// DiscoveryStarter delegates schema discovery to domain/discoveryjobs after a
// sync ingests new documents. It is optional (nil-safe): a source with no new
// documents, or a container without the discovery module, skips delegation.
type DiscoveryStarter interface {
	StartDiscovery(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) error
}

// SyncWorker polls for pending sync jobs and drives them through
// pending → processing → completed|failed|cancelled, isolating per-item errors
// and persisting durable progress as it advances.
type SyncWorker struct {
	svc       *Service
	docs      *documents.Service
	newDrive  DriveClientFactory
	xberg     *xberg.Client
	discovery DiscoveryStarter
	log       *slog.Logger

	interval          time.Duration
	batchSize         int
	staleThresholdMin int

	stopCh    chan struct{}
	stoppedCh chan struct{}
	running   bool
	mu        sync.Mutex
	wg        sync.WaitGroup
}

// NewSyncWorker creates a sync worker. The Drive client factory, xberg client,
// and discovery starter are constructor-injected (not setters) so tests can
// substitute fakes. A nil discovery/xberg is handled by skipping those paths.
func NewSyncWorker(
	svc *Service,
	docs *documents.Service,
	newDrive DriveClientFactory,
	xberg *xberg.Client,
	discovery DiscoveryStarter,
	log *slog.Logger,
) *SyncWorker {
	return &SyncWorker{
		svc:               svc,
		docs:              docs,
		newDrive:          newDrive,
		xberg:             xberg,
		discovery:         discovery,
		log:               log.With(logger.Scope("sources.worker")),
		interval:          5 * time.Second,
		batchSize:         10,
		staleThresholdMin: 10,
	}
}

// Start begins the polling loop and recovers stale jobs on startup.
func (w *SyncWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.stoppedCh = make(chan struct{})
	w.mu.Unlock()

	go w.recoverStaleJobs(ctx)

	w.log.Info("sync worker starting",
		slog.Duration("poll_interval", w.interval),
		slog.Int("batch_size", w.batchSize))

	w.wg.Add(1)
	go w.run(ctx)
	return nil
}

// Stop gracefully stops the worker, waiting for the current batch to complete.
func (w *SyncWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	select {
	case <-w.stoppedCh:
		w.log.Info("sync worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("sync worker stop timeout, forcing shutdown")
	}
	return nil
}

func (w *SyncWorker) recoverStaleJobs(ctx context.Context) {
	recovered, err := w.svc.repo.RecoverStaleJobs(ctx, w.staleThresholdMin)
	if err != nil {
		w.log.Warn("failed to recover stale sync jobs", "error", err)
		return
	}
	if recovered > 0 {
		w.log.Info("recovered stale sync jobs", slog.Int("count", recovered))
	}
}

func (w *SyncWorker) run(ctx context.Context) {
	defer w.wg.Done()
	defer close(w.stoppedCh)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				w.log.Warn("sync batch failed", "error", err)
			}
		}
	}
}

// RunOnce dequeues and processes one batch of pending jobs. Exported so tests
// (and the scheduler's immediate-admission path) can drive a batch
// synchronously.
func (w *SyncWorker) RunOnce(ctx context.Context) error {
	ids, err := w.svc.repo.DequeueSyncJob(ctx, w.batchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		job, err := w.svc.repo.GetSyncJobByID(ctx, id)
		if err != nil {
			w.log.Warn("failed to load sync job", "job_id", id, "error", err)
			continue
		}
		if job == nil {
			continue
		}
		if err := w.ProcessJob(ctx, job); err != nil {
			w.log.Warn("sync job failed", "job_id", id, "error", err)
		}
	}
	return nil
}

// ProcessJob drives a single (already-processing) job to a terminal state.
func (w *SyncWorker) ProcessJob(ctx context.Context, job *SyncJob) error {
	jobLog := w.log.With("job_id", job.ID, "source_id", job.SourceID)

	src, err := w.svc.repo.GetByIDWithCredentials(ctx, job.SourceID, job.ProjectID)
	if err != nil {
		return w.failJob(ctx, job, err)
	}

	creds, err := w.svc.decryptCredentials(src.EncryptedCredentials)
	if err != nil {
		return w.failJob(ctx, job, fmt.Errorf("decrypt credentials: %w", err))
	}

	client, err := w.newDrive(ctx, creds)
	if err != nil {
		return w.failJob(ctx, job, fmt.Errorf("initialize drive client: %w", err))
	}

	folderID, mimeTypes, recursive, sharedDriveID, err := parseSyncConfig(src.Config)
	if err != nil {
		return w.failJob(ctx, job, err)
	}

	headRevs, err := w.svc.repo.ListHeadSourceRevisions(ctx, job.ProjectID, job.SourceID)
	if err != nil {
		return w.failJob(ctx, job, err)
	}
	deletedHeads, err := w.svc.repo.ListDeletedHeadSourceIDs(ctx, job.ProjectID, job.SourceID)
	if err != nil {
		return w.failJob(ctx, job, err)
	}

	enumerating := "enumerating"
	if err := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, SyncProgress{Phase: &enumerating}); err != nil {
		return w.failJob(ctx, job, err)
	}

	items, err := client.ListFolderItems(ctx, folderID, mimeTypes, recursive, sharedDriveID)
	if err != nil {
		return w.failJob(ctx, job, fmt.Errorf("enumerate drive folder: %w", err))
	}

	syncing := "syncing"
	if err := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, SyncProgress{TotalItems: len(items), Phase: &syncing}); err != nil {
		return w.failJob(ctx, job, err)
	}

	seen := make(map[string]bool, len(items))
	var createdDocIDs []string
	var itemErrors []map[string]any
	processed, successful, failed, skipped := 0, 0, 0, 0

	// progress builds a full durable-progress snapshot from the local counters so
	// each persisted update carries every counter (the store replaces them).
	progress := func(phase, message *string) SyncProgress {
		return SyncProgress{
			TotalItems:      len(items),
			ProcessedItems:  processed,
			SuccessfulItems: successful,
			FailedItems:     failed,
			SkippedItems:    skipped,
			Phase:           phase,
			Message:         message,
		}
	}

	for _, item := range items {
		// Cancellation check between items: the handler flips status to
		// 'cancelled' and the worker stops fetching further items.
		if w.isCancelled(ctx, job.ID) {
			_ = w.svc.repo.MarkSyncJobCancelled(ctx, job.ID, "cancelled")
			jobLog.Info("sync cancelled")
			return nil
		}

		seen[item.ID] = true
		processed++

		// Cheap pre-filter: skip when the HEAD revision already matches. A HEAD
		// marked deleted_from_source is never skipped — a revived item (e.g.
		// restored from Drive trash) may keep the same headRevisionId and still
		// needs ApplySourceItem to reactivate it.
		if rev, ok := headRevs[item.ID]; ok && rev == item.RevisionID && !deletedHeads[item.ID] {
			skipped++
			if err := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); err != nil {
				return w.failJob(ctx, job, err)
			}
			continue
		}

		content, mimeType, err := w.fetchItem(ctx, client, item)
		if err != nil {
			failed++
			itemErrors = append(itemErrors, map[string]any{"externalId": item.ID, "error": err.Error()})
			if uerr := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); uerr != nil {
				return w.failJob(ctx, job, uerr)
			}
			continue
		}

		result, err := w.docs.ApplySourceItem(ctx, documents.ApplySourceItemParams{
			ProjectID:  job.ProjectID.String(),
			SourceID:   job.SourceID.String(),
			ExternalID: item.ID,
			RevisionID: item.RevisionID,
			Filename:   item.Name,
			MimeType:   mimeType,
			Content:    content,
			SourceType: SourceTypeDrive,
		})
		if err != nil {
			failed++
			itemErrors = append(itemErrors, map[string]any{"externalId": item.ID, "error": err.Error()})
			if uerr := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); uerr != nil {
				return w.failJob(ctx, job, uerr)
			}
			continue
		}
		successful++
		if result.Created {
			createdDocIDs = append(createdDocIDs, result.DocumentID)
		}
		if err := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); err != nil {
			return w.failJob(ctx, job, err)
		}
	}

	// Removals: prior HEAD items absent from this enumeration are marked
	// deleted-from-source (retained, not destroyed).
	for extID := range headRevs {
		if seen[extID] {
			continue
		}
		if err := w.docs.MarkSourceItemDeleted(ctx, job.ProjectID.String(), job.SourceID.String(), extID); err != nil {
			jobLog.Warn("failed to mark source item deleted", "external_id", extID, "error", err)
		}
	}

	// Persist per-item error detail (structured) on a completed job.
	if len(itemErrors) > 0 {
		if err := w.svc.repo.RecordSyncJobErrorDetail(ctx, job.ID, map[string]any{"items": itemErrors}); err != nil {
			return w.failJob(ctx, job, err)
		}
	}

	// Advance the source cursor/last_synced_at only after a successful batch.
	now := time.Now().UTC()
	cursor := map[string]any{"total_items": len(items), "synced_at": now.Format(time.RFC3339)}
	if err := w.svc.repo.AdvanceSourceCursor(ctx, job.ProjectID, job.SourceID, cursor, now); err != nil {
		return w.failJob(ctx, job, fmt.Errorf("advance source cursor: %w", err))
	}

	message := "sync completed"
	if failed > 0 {
		message = fmt.Sprintf("sync completed with %d failed items", failed)
	}
	if err := w.svc.repo.MarkSyncJobCompleted(ctx, job.ID, message); err != nil {
		return err
	}

	// Delegate schema discovery with newly ingested documents (§4.10).
	if len(createdDocIDs) > 0 && w.discovery != nil {
		ids := make([]uuid.UUID, 0, len(createdDocIDs))
		for _, id := range createdDocIDs {
			if u, err := uuid.Parse(id); err == nil {
				ids = append(ids, u)
			}
		}
		if len(ids) > 0 {
			if err := w.discovery.StartDiscovery(ctx, job.ProjectID, ids); err != nil {
				jobLog.Warn("discovery delegation failed", "error", err)
			}
		}
	}

	jobLog.Info("sync completed",
		slog.Int("processed", processed),
		slog.Int("successful", successful),
		slog.Int("failed", failed),
		slog.Int("skipped", skipped))
	return nil
}

// isCancelled reports whether the job has been flipped to 'cancelled' by the
// cancel endpoint while the worker is mid-batch.
func (w *SyncWorker) isCancelled(ctx context.Context, jobID uuid.UUID) bool {
	job, err := w.svc.repo.GetSyncJobByID(ctx, jobID)
	if err != nil || job == nil {
		return false
	}
	return job.Status == SyncStatusCancelled
}

// fetchItem fetches and converts one item to parsed text. Google-native docs are
// exported to text (pass-through); other files are downloaded and, when
// xberg-routable, parsed through xberg; plain-text downloads pass through.
func (w *SyncWorker) fetchItem(ctx context.Context, client DriveClient, item gdrive.Item) (content, mimeType string, err error) {
	if isGoogleNative(item.MimeType) {
		data, err := client.ExportText(ctx, item.ID, item.MimeType)
		if err != nil {
			return "", "", err
		}
		return string(data), googleNativeExportMime(item.MimeType), nil
	}

	data, err := client.DownloadFile(ctx, item.ID)
	if err != nil {
		return "", "", err
	}

	if xberg.ShouldUseXberg(item.MimeType, item.Name) {
		if w.xberg == nil {
			return "", "", errors.New("xberg unavailable for binary conversion")
		}
		result, err := w.xberg.ExtractText(ctx, data, item.Name, item.MimeType, nil)
		if err != nil {
			return "", "", err
		}
		return result.Content, item.MimeType, nil
	}

	return string(data), item.MimeType, nil
}

// failJob transitions a job to terminal failed and records the error on the
// source (leaving last_synced_at intact so the scheduler re-admits it).
func (w *SyncWorker) failJob(ctx context.Context, job *SyncJob, err error) error {
	errMsg := err.Error()
	if mErr := w.svc.repo.MarkSyncJobFailed(ctx, job.ID, errMsg, map[string]any{"message": errMsg}); mErr != nil {
		w.log.Error("failed to mark sync job failed", "job_id", job.ID, "error", mErr)
	}
	if rErr := w.svc.repo.RecordSourceError(ctx, job.ProjectID, job.SourceID, errMsg); rErr != nil {
		w.log.Error("failed to record source error", "source_id", job.SourceID, "error", rErr)
	}
	return err
}

// parseSyncConfig extracts the enumeration scope from a source config.
func parseSyncConfig(cfg map[string]any) (folderID string, mimeTypes []string, recursive bool, sharedDriveID string, err error) {
	folderID, _ = cfg["folder_id"].(string)
	if strings.TrimSpace(folderID) == "" {
		return "", nil, false, "", apperror.NewValidation("config.folder_id is required")
	}
	sharedDriveID, _ = cfg["shared_drive_id"].(string)
	recursive, _ = cfg["recursive"].(bool)
	switch mt := cfg["mime_types"].(type) {
	case []string:
		mimeTypes = mt
	case []any:
		for _, v := range mt {
			if s, ok := v.(string); ok {
				mimeTypes = append(mimeTypes, s)
			}
		}
	}
	return folderID, mimeTypes, recursive, sharedDriveID, nil
}

// isGoogleNative reports whether a MIME type is a Google-native editable doc.
func isGoogleNative(mimeType string) bool {
	return strings.HasPrefix(mimeType, "application/vnd.google-apps.")
}

// googleNativeExportMime maps a Google-native MIME type to the text export
// target's MIME type (mirrors gdrive.exportMime).
func googleNativeExportMime(mimeType string) string {
	if mimeType == "application/vnd.google-apps.spreadsheet" {
		return "text/csv"
	}
	return "text/plain"
}
