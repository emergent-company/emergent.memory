package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/xberg"
)

// pendingSourceRelationship is a relationship to create after every record in
// the batch has an object id.
type pendingSourceRelationship struct {
	srcID uuid.UUID
	rel   ObjectRelationship
}

// DiscoveryStarter delegates schema discovery to domain/discoveryjobs after a
// sync ingests new documents. It is optional (nil-safe): a source with no new
// documents, or a container without the discovery module, skips delegation.
type DiscoveryStarter interface {
	StartDiscovery(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) error
	// AutoDiscoveryEnabled reports the project's auto-discovery setting. When
	// it returns false, a sync must not start discovery; an error means the
	// setting could not be read and delegation is skipped.
	AutoDiscoveryEnabled(ctx context.Context, projectID uuid.UUID) (bool, error)
}

// SyncWorker polls for pending sync jobs and drives them through
// pending → processing → completed|failed|cancelled, isolating per-item errors
// and persisting durable progress as it advances. It is provider-agnostic: it
// resolves the source's provider from the registry and drives its Connector.
type SyncWorker struct {
	svc       *Service
	docs      *documents.Service
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

// NewSyncWorker creates a sync worker. The xberg client and discovery starter
// are constructor-injected (not setters) so tests can substitute fakes. A nil
// discovery/xberg is handled by skipping those paths.
func NewSyncWorker(
	svc *Service,
	docs *documents.Service,
	xbergClient *xberg.Client,
	discovery DiscoveryStarter,
	log *slog.Logger,
) *SyncWorker {
	return &SyncWorker{
		svc:               svc,
		docs:              docs,
		xberg:             xbergClient,
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

// Worker-owned cursor keys. Provider-owned keys carry a provider prefix
// (gmail_) so the two sets can share the same jsonb row without colliding.
const (
	// cursorItemFailureAttemptsKey stores the per-item retry budget: external
	// item id → the number of consecutive syncs in which that item failed to
	// fetch or apply.
	cursorItemFailureAttemptsKey = "item_failure_attempts"
)

const (
	// maxItemFailureAttempts is how many consecutive syncs one item may fail
	// before the worker abandons it and lets the connector's resume point move
	// past it. Three syncs is about three hours at Gmail's default 60-minute
	// cadence (its minimum is 15 minutes): long enough to ride out a throttled
	// or 5xx stretch across separate syncs, short enough that one broken
	// message cannot freeze a multi-year backfill. The connector already
	// retries a throttled call in place (#2219), so a failure that reaches this
	// budget has survived its own retries; a transient one is expected to clear
	// in the next sync or two.
	maxItemFailureAttempts = 3

	// maxTrackedItemFailures bounds how many item ids the budget may hold. A
	// batch with more failures than this is a source-wide failure (quota
	// exhaustion, an outage) rather than a set of individually broken items, so
	// no per-item counts are recorded and the cursor is held. This keeps one
	// pathological batch — a capped sync may enumerate tens of thousands of
	// items — from ballooning the cursor row.
	maxTrackedItemFailures = 1000
)

// itemFailureAttempts is the persisted per-item retry budget: external item id →
// number of consecutive syncs the item has failed to fetch or apply. Keying by
// item id (rather than a boolean or a global counter) is what distinguishes the
// same item failing every sync from N different items each failing once.
type itemFailureAttempts map[string]int

// parseItemFailureAttempts reads the budget out of a stored cursor. Values come
// back as float64 from a jsonb decode and as int from a cursor built in this
// process, so both are accepted; anything else is ignored rather than failing
// the sync. Counts are clamped to the budget so a hand-edited or legacy row
// cannot stall the cursor forever.
func parseItemFailureAttempts(cursor map[string]any) itemFailureAttempts {
	raw, ok := cursor[cursorItemFailureAttemptsKey]
	if !ok {
		return nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		direct, ok := raw.(map[string]int)
		if !ok {
			return nil
		}
		values = make(map[string]any, len(direct))
		for id, n := range direct {
			values[id] = n
		}
	}
	attempts := make(itemFailureAttempts, len(values))
	for id, v := range values {
		if id == "" {
			continue
		}
		var n int
		switch count := v.(type) {
		case int:
			n = count
		case int64:
			n = int(count)
		case float64:
			n = int(count)
		default:
			continue
		}
		if n <= 0 {
			continue
		}
		if n > maxItemFailureAttempts {
			n = maxItemFailureAttempts
		}
		attempts[id] = n
	}
	return attempts
}

// hasRetryable reports whether any tracked item is still within its budget.
// When it is false, every failure in the batch has been abandoned.
func (a itemFailureAttempts) hasRetryable() bool {
	for _, n := range a {
		if n < maxItemFailureAttempts {
			return true
		}
	}
	return false
}

// nextItemFailureAttempts folds this batch's failures into the stored budget.
// Only ids that failed in this batch are carried forward: an item that
// succeeded, was skipped, or has left the enumeration drops out, so a recovery
// clears its history and the map is bounded by one batch. Each count is capped
// at the budget; ids that reach the cap are reported as abandoned.
func nextItemFailureAttempts(stored itemFailureAttempts, failedIDs []string) (itemFailureAttempts, []string) {
	attempts := make(itemFailureAttempts, len(failedIDs))
	var abandoned []string
	for _, id := range failedIDs {
		if id == "" {
			continue
		}
		if _, duplicate := attempts[id]; duplicate {
			continue
		}
		n := stored[id] + 1
		if n >= maxItemFailureAttempts {
			n = maxItemFailureAttempts
			abandoned = append(abandoned, id)
		}
		attempts[id] = n
	}
	sort.Strings(abandoned)
	return attempts, abandoned
}

// cursorDecision is the outcome of composing one batch's source cursor.
type cursorDecision struct {
	// cursor is the map to persist on the source row.
	cursor map[string]any
	// held reports that the connector's resume point was kept instead of
	// advanced, so the next sync re-lists the same window.
	held bool
	// attempts is the per-item budget after folding in this batch's failures.
	attempts itemFailureAttempts
	// abandoned lists the ids skipped by the advance on this batch. It is empty
	// while the cursor is held: an item that exhausted its budget is still
	// re-listed if another failure keeps the window in place, so it is only
	// reported once the watermark actually moves past it.
	abandoned []string
}

// buildSourceCursor composes the row's cursor map: the worker's own counters plus
// the connector's resume point.
//
// Connector state is merged, never substituted, because both live in the same
// jsonb row. The resume point advances only when every item in the batch is
// accounted for — applied, skipped, or failed for maxItemFailureAttempts
// consecutive syncs. A failure still within budget holds the stored point, so
// the window is re-listed and the item retried; that is the resumable-enumeration
// contract (#2219) and it is what keeps a transient failure from losing an item.
//
// Tracking failures per item is what makes the escape hatch safe: the same item
// failing every sync counts up and is eventually abandoned, while N different
// items each failing once never exhaust a budget. Without it one permanently
// broken item freezes the coverage watermark forever (#2220).
func buildSourceCursor(prev, next map[string]any, total int, failedIDs []string, now time.Time) cursorDecision {
	stored := parseItemFailureAttempts(prev)
	attempts, abandoned := nextItemFailureAttempts(stored, failedIDs)
	// A batch this large cannot be tracked item by item: treat it as a
	// source-wide failure, hold the cursor and preserve the counts already
	// stored instead of writing an unbounded id map.
	wholesale := len(failedIDs) > maxTrackedItemFailures
	if wholesale {
		attempts, abandoned = stored, nil
	}
	held := wholesale || attempts.hasRetryable()
	if held {
		// Nothing is dropped while the window stays put: a budget-exhausted item
		// is still re-listed (and reported as abandoned only once the advance
		// leaves it behind).
		abandoned = nil
	}

	source := next
	if held {
		source = prev
	}
	cursor := make(map[string]any, len(source)+3)
	for k, v := range source {
		cursor[k] = v
	}
	// Never inherit a stale budget from next or prev: the key is rewritten
	// below only while the cursor is held.
	delete(cursor, cursorItemFailureAttemptsKey)
	if held && len(attempts) > 0 {
		cursor[cursorItemFailureAttemptsKey] = map[string]int(attempts)
	}
	cursor["total_items"] = total
	cursor["synced_at"] = now.Format(time.RFC3339)
	return cursorDecision{cursor: cursor, held: held, attempts: attempts, abandoned: abandoned}
}

// abandonmentMessage is the source-row trace left when a batch advances past
// items that exhausted the retry budget. The id list is bounded so a large
// batch cannot write an unbounded error string; the job's structured error
// detail still carries every item.
func abandonmentMessage(ids []string) string {
	const shown = 5
	if len(ids) <= shown {
		return fmt.Sprintf("%d item(s) abandoned after %d consecutive failed syncs: %s",
			len(ids), maxItemFailureAttempts, strings.Join(ids, ", "))
	}
	return fmt.Sprintf("%d item(s) abandoned after %d consecutive failed syncs: %s and %d more",
		len(ids), maxItemFailureAttempts, strings.Join(ids[:shown], ", "), len(ids)-shown)
}

// ProcessJob drives a single (already-processing) job to a terminal state.
func (w *SyncWorker) ProcessJob(ctx context.Context, job *SyncJob) error {
	jobLog := w.log.With("job_id", job.ID, "source_id", job.SourceID)

	src, err := w.svc.repo.GetByIDWithCredentials(ctx, job.SourceID, job.ProjectID)
	if err != nil {
		return w.failJob(ctx, job, src, err)
	}

	provider, ok := w.svc.providers.Get(src.Provider)
	if !ok {
		return w.failJob(ctx, job, src, fmt.Errorf("unknown provider %q", src.Provider))
	}

	creds, err := w.svc.credentialsForSource(ctx, src)
	if err != nil {
		return w.failJob(ctx, job, src, fmt.Errorf("decrypt credentials: %w", err))
	}

	// Rotated OAuth tokens are written back so a rotating authorization server
	// (Trale) does not need a manual reconnect at every expiry. The connector
	// swallows a write failure — the token it just obtained is still valid, so the
	// sync continues — and credentialDeps logs it (#2091).
	deps := w.svc.credentialDeps(job.ProjectID, src.ID)
	deps.Extract = w.extractText
	connector, err := provider.NewConnector(ctx, creds, deps)
	if err != nil {
		return w.failJob(ctx, job, src, fmt.Errorf("initialize connector: %w", err))
	}
	// Connectors that hold a remote session (Trale's MCP client) release it when
	// the job ends; connectors without a Close method are unaffected.
	if c, ok := connector.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}

	headRevs, err := w.svc.repo.ListHeadSourceRevisions(ctx, job.ProjectID, job.SourceID)
	if err != nil {
		return w.failJob(ctx, job, src, err)
	}
	deletedHeads, err := w.svc.repo.ListDeletedHeadSourceIDs(ctx, job.ProjectID, job.SourceID)
	if err != nil {
		return w.failJob(ctx, job, src, err)
	}

	enumerating := "enumerating"
	if err := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, SyncProgress{Phase: &enumerating}); err != nil {
		return w.failJob(ctx, job, src, err)
	}

	// A connector that walks its source in chunks returns the cursor to persist
	// after this batch applies; one that enumerates in a single pass (every other
	// provider today) keeps the worker's own cursor shape. Passing the stored
	// cursor is what makes a capped or interrupted enumeration resume instead of
	// restarting at the newest item.
	var nextCursor map[string]any
	var items []SourceItem
	// Only a chunked connector has a resume point to hold; for a single-pass
	// connector the per-item budget would neither retry nor skip anything, so
	// item failures stay reported in the job's error detail as before.
	var cursorConnector bool
	if cc, ok := connector.(CursorConnector); ok {
		cursorConnector = true
		items, nextCursor, err = cc.EnumerateWithCursor(ctx, src.Config, src.Cursor)
	} else {
		items, err = connector.Enumerate(ctx, src.Config)
	}
	if err != nil {
		return w.failJob(ctx, job, src, fmt.Errorf("enumerate source: %w", err))
	}

	syncing := "syncing"
	if err := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, SyncProgress{TotalItems: len(items), Phase: &syncing}); err != nil {
		return w.failJob(ctx, job, src, err)
	}

	seen := make(map[string]bool, len(items))
	var createdDocIDs []string
	var itemErrors []map[string]any
	failedItems := make(map[string]bool)
	processed, successful, failed, skipped := 0, 0, 0, 0

	// Typed-object batch state: object ids by (type,key) and relationships that
	// can only be created after every item has been processed.
	objectIDs := make(map[string]uuid.UUID)
	seenRecords := make(map[string]bool)
	var pendingRels []pendingSourceRelationship

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

		// Cheap pre-filter: skip when the HEAD revision already matches. It only
		// applies when the provider reports a revision: a provider that reports
		// none would otherwise compare "" against a stored "" and skip the item
		// forever, so no revision means "fetch and let the text hash decide". A
		// HEAD marked deleted_from_source is never skipped either — a revived
		// item (e.g. restored from trash) may keep the same head revision and
		// still needs ApplySourceItem to reactivate it.
		if item.RevisionID != "" {
			if rev, ok := headRevs[item.ID]; ok && rev == item.RevisionID && !deletedHeads[item.ID] {
				ids, rels, recErr := w.applyItemRecords(ctx, job, src, item, "")
				for k, v := range ids {
					objectIDs[k] = v
				}
				pendingRels = append(pendingRels, rels...)
				if recErr == nil {
					markSeenRecords(seenRecords, item)
				}
				if recErr != nil {
					failed++
					failedItems[item.ID] = true
					itemErrors = append(itemErrors, map[string]any{"externalId": item.ID, "error": recErr.Error()})
				} else {
					skipped++
				}
				if err := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); err != nil {
					return w.failJob(ctx, job, src, err)
				}
				continue
			}
		}

		var content, mimeType string
		if rf, ok := connector.(RecordFetchConnector); ok {
			var records []ObjectRecord
			content, mimeType, records, err = rf.FetchWithRecords(ctx, item)
			item.Records = records
		} else {
			content, mimeType, err = connector.Fetch(ctx, item)
		}
		if err != nil {
			failed++
			failedItems[item.ID] = true
			itemErrors = append(itemErrors, map[string]any{"externalId": item.ID, "error": err.Error()})
			// Metadata records do not depend on the body fetch; keep the graph
			// metadata current even when text retrieval fails.
			ids, rels, _ := w.applyItemRecords(ctx, job, src, item, "")
			for k, v := range ids {
				objectIDs[k] = v
			}
			pendingRels = append(pendingRels, rels...)
			markSeenRecords(seenRecords, item)
			if uerr := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); uerr != nil {
				return w.failJob(ctx, job, src, uerr)
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
			SourceType: provider.Catalog().SourceType,
		})
		docID := ""
		if err == nil {
			docID = result.DocumentID
		}

		ids, rels, recErr := w.applyItemRecords(ctx, job, src, item, docID)
		for k, v := range ids {
			objectIDs[k] = v
		}
		pendingRels = append(pendingRels, rels...)
		if recErr == nil {
			markSeenRecords(seenRecords, item)
		}

		if err != nil {
			failed++
			failedItems[item.ID] = true
			itemErrors = append(itemErrors, map[string]any{"externalId": item.ID, "error": err.Error()})
			if uerr := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); uerr != nil {
				return w.failJob(ctx, job, src, uerr)
			}
			continue
		}
		if recErr != nil {
			failed++
			failedItems[item.ID] = true
			itemErrors = append(itemErrors, map[string]any{"externalId": item.ID, "error": recErr.Error()})
			if uerr := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); uerr != nil {
				return w.failJob(ctx, job, src, uerr)
			}
			continue
		}
		successful++
		if result.Created {
			createdDocIDs = append(createdDocIDs, result.DocumentID)
		}
		if err := w.svc.repo.UpdateSyncJobProgress(ctx, job.ID, progress(nil, nil)); err != nil {
			return w.failJob(ctx, job, src, err)
		}
	}

	// Relationship creation runs after every record has an object id so a
	// target defined by a later item resolves correctly.
	for _, pr := range pendingRels {
		dst, ok := objectIDs[recordKey(pr.rel.TargetType, pr.rel.TargetKey)]
		if !ok {
			jobLog.Debug("source relationship target not found",
				slog.String("type", pr.rel.Type),
				slog.String("target_type", pr.rel.TargetType),
				slog.String("target_key", pr.rel.TargetKey))
			continue
		}
		if _, _, err := w.svc.graph.UpsertRelationship(ctx, job.ProjectID, &graph.CreateGraphRelationshipRequest{
			Type:   pr.rel.Type,
			SrcID:  pr.srcID,
			DstID:  dst,
			Upsert: true,
		}, nil); err != nil {
			jobLog.Warn("failed to upsert source relationship",
				slog.String("type", pr.rel.Type), "error", err)
		}
	}

	// Removals: prior HEAD items absent from this enumeration are marked
	// deleted-from-source (retained, not destroyed) — but only when the
	// enumeration is authoritative. A partial crawl (a page/depth/byte/deadline
	// cap, or a page error) has not seen every prior item, so deleting an absent
	// id would destroy a page the crawl simply did not reach. Tombstones (a
	// canonical URL the crawl confirmed 404/410) are the only deletion evidence
	// allowed from a partial crawl: a definitive 404 proves the page is gone,
	// not that the crawl ran out of budget.
	headIDs := make([]string, 0, len(headRevs))
	for extID := range headRevs {
		headIDs = append(headIDs, extID)
	}
	sort.Strings(headIDs)

	complete := true
	if c, ok := connector.(EnumerationCompleteness); ok {
		complete = c.EnumerationComplete()
	}
	var tombstones []string
	if c, ok := connector.(SourceTombstones); ok {
		tombstones = c.Tombstones()
	}

	// Typed-object removals use the same completeness rule as documents: an
	// incomplete enumeration cannot prove a record is gone.
	if complete && w.svc.graph != nil {
		w.reconcileSourceObjects(ctx, job, seenRecords, jobLog)
	}

	deleted, skippedRemovals := planRemovals(headIDs, seen, complete, tombstones)
	for _, extID := range deleted {
		if err := w.docs.MarkSourceItemDeleted(ctx, job.ProjectID.String(), job.SourceID.String(), extID); err != nil {
			jobLog.Warn("failed to mark source item deleted", "external_id", extID, "error", err)
		}
	}
	if len(skippedRemovals) > 0 {
		jobLog.Info("removals skipped: incomplete enumeration cannot prove absence",
			slog.Int("count", len(skippedRemovals)))
	}

	// Compose the cursor before recording item detail: the per-item budget
	// decides both whether the connector's resume point moves and which
	// failures have exhausted their retries, and the job detail should carry
	// that verdict.
	var failedIDs []string
	if cursorConnector {
		failedIDs = make([]string, 0, len(failedItems))
		for id := range failedItems {
			failedIDs = append(failedIDs, id)
		}
		sort.Strings(failedIDs)
	}
	now := time.Now().UTC()
	decision := buildSourceCursor(src.Cursor, nextCursor, len(items), failedIDs, now)

	abandoned := make(map[string]bool, len(decision.abandoned))
	for _, id := range decision.abandoned {
		abandoned[id] = true
	}
	for _, detail := range itemErrors {
		id, _ := detail["externalId"].(string)
		if attempts, ok := decision.attempts[id]; ok {
			detail["attempts"] = attempts
		}
		if abandoned[id] {
			detail["abandoned"] = true
		}
	}

	// Persist per-item error detail (structured) on a completed job.
	if len(itemErrors) > 0 {
		if err := w.svc.repo.RecordSyncJobErrorDetail(ctx, job.ID, map[string]any{"items": itemErrors}); err != nil {
			return w.failJob(ctx, job, src, err)
		}
	}

	// Advance the source cursor/last_synced_at only after a successful batch.
	if err := w.svc.repo.AdvanceSourceCursor(ctx, job.ProjectID, job.SourceID, decision.cursor, now); err != nil {
		return w.failJob(ctx, job, src, fmt.Errorf("advance source cursor: %w", err))
	}

	// Leaving an abandonment only in the job's history would keep it invisible
	// on the source row, which is how this stall went unnoticed (#2220). Record
	// it after the advance, because AdvanceSourceCursor clears last_error.
	if len(decision.abandoned) > 0 {
		if rErr := w.svc.repo.RecordSourceError(ctx, job.ProjectID, job.SourceID, abandonmentMessage(decision.abandoned)); rErr != nil {
			jobLog.Error("failed to record abandoned items on the source", "error", rErr)
		}
	}

	message := "sync completed"
	switch {
	case len(decision.abandoned) > 0:
		message = fmt.Sprintf("sync completed with %d failed items (%d abandoned after %d consecutive failed syncs)",
			failed, len(decision.abandoned), maxItemFailureAttempts)
	case failed > 0:
		message = fmt.Sprintf("sync completed with %d failed items", failed)
	}
	if err := w.svc.repo.MarkSyncJobCompleted(ctx, job.ID, message); err != nil {
		return err
	}

	// A successful sync is proof the source is no longer parked, so resolve
	// defensively: the repair path should have cleared the item already, but a
	// sync that starts succeeding after an out-of-band credential fix must not
	// leave a stale actionable item behind (#2414).
	w.svc.resolveSourceNotification(ctx, job.ProjectID, job.SourceID)

	// Delegate schema discovery with newly ingested documents, unless the
	// project has turned automatic discovery off or the provider declares its
	// own typed schema pack.
	skipDiscovery := false
	if typed, ok := provider.(TypedObjectProvider); ok {
		if pack := typed.ObjectTypes(); pack != nil && pack.SkipDiscovery {
			skipDiscovery = true
		}
	}
	if !skipDiscovery {
		w.delegateDiscovery(ctx, job.ProjectID, createdDocIDs, jobLog)
	}

	jobLog.Info("sync completed",
		slog.Int("processed", processed),
		slog.Int("successful", successful),
		slog.Int("failed", failed),
		slog.Int("skipped", skipped),
		slog.Int("abandoned", len(decision.abandoned)),
		slog.Bool("cursor_held", decision.held))
	return nil
}

// delegateDiscovery starts schema discovery for the documents a sync ingested,
// unless the project has turned automatic discovery off.
//
// It never fails the sync: ingestion is the sync's job, discovery is a
// follow-on whose failures are visible on the Schema page and in the
// Needs-attention panel. The setting is read per sync (not cached) so a change
// applies to the next one, and a failed lookup skips delegation — fail closed —
// rather than spending model calls on a guess.
func (w *SyncWorker) delegateDiscovery(ctx context.Context, projectID uuid.UUID, createdDocIDs []string, log *slog.Logger) {
	if len(createdDocIDs) == 0 || w.discovery == nil {
		return
	}

	enabled, err := w.discovery.AutoDiscoveryEnabled(ctx, projectID)
	switch {
	case err != nil:
		log.Warn("auto-discovery setting lookup failed; skipping discovery delegation", "error", err)
		return
	case !enabled:
		log.Info("auto-discovery disabled for project; skipping discovery delegation",
			"project_id", projectID.String(), "documents", len(createdDocIDs))
		return
	}

	ids := make([]uuid.UUID, 0, len(createdDocIDs))
	for _, id := range createdDocIDs {
		if u, parseErr := uuid.Parse(id); parseErr == nil {
			ids = append(ids, u)
		}
	}
	if len(ids) == 0 {
		return
	}
	if startErr := w.discovery.StartDiscovery(ctx, projectID, ids); startErr != nil {
		// Includes the 409 a run in flight returns. Those documents are not
		// analysed by that run, and a later sync only delegates its own new
		// documents, so nothing picks them up automatically: the manual run on
		// the Schema page (or a retry) is the way to cover them. This line is
		// the operator trace.
		log.Warn("discovery delegation failed", "error", startErr)
	}
}

// planRemovals decides which prior HEAD items to mark deleted after an
// enumeration. delete is the set of prior external ids to soft-delete; skipped
// is the set left in place because a partial enumeration cannot prove their
// absence. A definitive tombstone (404/410) deletes even on a partial crawl.
func planRemovals(headIDs []string, seen map[string]bool, complete bool, tombstones []string) (delete, skipped []string) {
	tombSet := make(map[string]bool, len(tombstones))
	for _, t := range tombstones {
		tombSet[t] = true
	}
	for _, id := range headIDs {
		if seen[id] {
			continue
		}
		if complete || tombSet[id] {
			delete = append(delete, id)
		} else {
			skipped = append(skipped, id)
		}
	}
	return delete, skipped
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

// credentialEpoch returns the grant epoch of the source a run read, or nil when
// the row could not be loaded — the caller then parks unconditionally, as it did
// before the epoch existed.
func credentialEpoch(src *Source) *time.Time {
	if src == nil {
		return nil
	}
	epoch := src.CredentialsUpdatedAt
	return &epoch
}

// sourceObjectNamespace isolates one source's typed objects from every other
// source in the project, so identical external ids never collide.
func sourceObjectNamespace(sourceID uuid.UUID) string { return "src:" + sourceID.String() }

func recordKey(objectType, key string) string { return objectType + "\x00" + key }

func recordSeenKey(externalID, objectType, key string) string {
	return externalID + "\x00" + objectType + "\x00" + key
}

func markSeenRecords(seen map[string]bool, item SourceItem) {
	for _, rec := range item.Records {
		if rec.Type == "" || rec.Key == "" {
			continue
		}
		seen[recordSeenKey(item.ID, rec.Type, rec.Key)] = true
	}
}

// applyItemRecords upserts an item's typed records into the graph and records
// their mapping rows. It returns object ids keyed by (type,key) and any
// relationships that need a second pass.
func (w *SyncWorker) applyItemRecords(ctx context.Context, job *SyncJob, src *Source, item SourceItem, documentID string) (map[string]uuid.UUID, []pendingSourceRelationship, error) {
	out := make(map[string]uuid.UUID)
	if w.svc.graph == nil || len(item.Records) == 0 {
		return out, nil, nil
	}

	ctx = auth.WithActor(ctx, graph.ActorSystem, nil)
	namespace := sourceObjectNamespace(job.SourceID)
	now := time.Now().UTC()

	var docUUID *uuid.UUID
	if documentID != "" {
		if id, err := uuid.Parse(documentID); err == nil {
			docUUID = &id
		}
	}

	var rels []pendingSourceRelationship
	var firstErr error

	for start := 0; start < len(item.Records); start += 100 {
		end := start + 100
		if end > len(item.Records) {
			end = len(item.Records)
		}
		chunk := item.Records[start:end]

		reqs := make([]graph.CreateGraphObjectRequest, 0, len(chunk))
		for _, rec := range chunk {
			props := make(map[string]any, len(rec.Properties)+4)
			for k, v := range rec.Properties {
				props[k] = v
			}
			props["_source_id"] = job.SourceID.String()
			props["_source_provider"] = src.Provider
			props["_source_external_id"] = item.ID
			if documentID != "" {
				props["_document_id"] = documentID
			}
			ns := namespace
			reqs = append(reqs, graph.CreateGraphObjectRequest{
				Type:       rec.Type,
				Key:        stringPtr(rec.Key),
				Properties: props,
				Labels:     rec.Labels,
				Namespace:  &ns,
			})
		}

		resp, err := w.svc.graph.BulkUpsertObjects(ctx, job.ProjectID, &graph.BulkUpsertObjectsRequest{Items: reqs}, nil)
		if err != nil {
			return out, rels, fmt.Errorf("upsert typed objects: %w", err)
		}

		mappings := make([]*SourceObject, 0, len(chunk))
		for i, result := range resp.Results {
			rec := chunk[i]
			if !result.Success || result.Object == nil {
				if firstErr == nil {
					msg := "typed object upsert failed"
					if result.Error != nil {
						msg = *result.Error
					}
					firstErr = fmt.Errorf("%s/%s: %s", rec.Type, rec.Key, msg)
				}
				continue
			}
			obj := result.Object
			out[recordKey(rec.Type, rec.Key)] = obj.ID
			revision := item.RevisionID
			mappings = append(mappings, &SourceObject{
				ID:                uuid.New(),
				ProjectID:         job.ProjectID,
				SourceID:          job.SourceID,
				ExternalID:        item.ID,
				ObjectType:        rec.Type,
				ObjectKey:         rec.Key,
				ObjectID:          obj.ID,
				ObjectCanonicalID: obj.CanonicalID,
				DocumentID:        docUUID,
				RevisionID:        &revision,
				Status:            SourceObjectStatusActive,
				SyncedAt:          now,
				CreatedAt:         now,
				UpdatedAt:         now,
			})
			for _, rel := range rec.Relationships {
				rels = append(rels, pendingSourceRelationship{srcID: obj.ID, rel: rel})
			}
		}
		if err := w.svc.repo.UpsertSourceObjects(ctx, mappings); err != nil {
			return out, rels, err
		}
	}

	return out, rels, firstErr
}

// reconcileSourceObjects marks mappings and graph objects absent from a
// complete enumeration as deleted-from-source.
func (w *SyncWorker) reconcileSourceObjects(ctx context.Context, job *SyncJob, seen map[string]bool, log *slog.Logger) {
	active, err := w.svc.repo.ListActiveSourceObjects(ctx, job.ProjectID, job.SourceID)
	if err != nil {
		log.Warn("failed to list source objects for reconciliation", "error", err)
		return
	}
	var mappingIDs []uuid.UUID
	var objectIDs []uuid.UUID
	for _, obj := range active {
		if seen[recordSeenKey(obj.ExternalID, obj.ObjectType, obj.ObjectKey)] {
			continue
		}
		mappingIDs = append(mappingIDs, obj.ID)
		objectIDs = append(objectIDs, obj.ObjectID)
	}
	if len(mappingIDs) == 0 {
		return
	}
	if err := w.svc.repo.MarkSourceObjectsDeleted(ctx, mappingIDs); err != nil {
		log.Warn("failed to mark source object mappings deleted", "error", err)
	}
	ctx = auth.WithActor(ctx, graph.ActorSystem, nil)
	reason := "deleted from source"
	for _, id := range objectIDs {
		if err := w.svc.graph.Delete(ctx, job.ProjectID, id, nil, nil, &reason); err != nil {
			log.Debug("failed to soft-delete source object", "object_id", id, "error", err)
		}
	}
}

func stringPtr(v string) *string { return &v }

// extractText adapts the xberg client to the connector's generic text-extraction
// dependency. A nil client reports extraction as unavailable, which the provider
// surfaces as a per-item error for formats it cannot decode itself.
func (w *SyncWorker) extractText(ctx context.Context, content []byte, filename, mimeType string) (string, error) {
	if w.xberg == nil {
		return "", errors.New("text extraction unavailable")
	}
	result, err := w.xberg.ExtractText(ctx, content, filename, mimeType, nil)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// failJob transitions a job to terminal failed and records the error on the
// source. A failure a retry can fix leaves last_synced_at intact so the scheduler
// re-admits the source; a failure only a fresh consent can fix parks it, so the
// sweep stops producing a doomed job on every pass. The park is skipped when the
// credential this run read has been replaced since (#2425): the failure is still
// recorded, but the source stays admitted, because parking it would undo the
// reconnect that replaced the grant.
func (w *SyncWorker) failJob(ctx context.Context, job *SyncJob, src *Source, err error) error {
	errMsg := err.Error()
	if mErr := w.svc.repo.MarkSyncJobFailed(ctx, job.ID, errMsg, map[string]any{"message": errMsg}); mErr != nil {
		w.log.Error("failed to mark sync job failed", "job_id", job.ID, "error", mErr)
	}
	if IsReconnectRequired(err) {
		// src is the row this run read: its grant epoch decides whether the
		// failure is about the credential the source reads *now* (#2425).
		applied, rErr := w.svc.repo.MarkSourceNeedsReconnect(ctx, job.ProjectID, job.SourceID, errMsg, credentialEpoch(src))
		if rErr != nil {
			w.log.Error("failed to park source", "source_id", job.SourceID, "error", rErr)
		} else if applied {
			// The park committed, so the member has to be told: every project
			// member is left with one unresolved item naming the source and the
			// connector's own remediation. Best-effort — a delivery failure is
			// logged and never changes the sync outcome (#2414).
			w.svc.publishSourceNeedsReconnect(ctx, src, errMsg)
		} else {
			// The grant was replaced while this run was in flight — a reconnect of
			// this source, or of a saved credential it reads — or the row was deleted
			// (both are 0 rows affected). The failure belongs to a grant nobody uses
			// any more, and parking the source would undo the repair the user just
			// made, sending them round the reconnect loop a second time. Keep the
			// source admitted and record the failure so it stays visible in the sync
			// history; on a deleted row both writes are no-ops.
			w.log.Info("park not applied: credential replaced during the run, or the row is gone", "source_id", job.SourceID)
			if recErr := w.svc.repo.RecordSourceError(ctx, job.ProjectID, job.SourceID, errMsg); recErr != nil {
				w.log.Error("failed to record source error", "source_id", job.SourceID, "error", recErr)
			}
		}
		return err
	}
	if rErr := w.svc.repo.RecordSourceError(ctx, job.ProjectID, job.SourceID, errMsg); rErr != nil {
		w.log.Error("failed to record source error", "source_id", job.SourceID, "error", rErr)
	}
	return err
}
