package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// DocumentParsingSweepTask reconciles uploaded documents that have no parsing
// job.
//
// An upload creates its document row and then enqueues the parsing job as two
// separate operations, not one transaction. A crash, a client disconnect that
// cancels the request context, or a transient queue error in that window leaves
// a document that exists with no content and no job: the upload looked
// successful, parsing never happens, nothing retries, and nothing surfaces it.
// The sweep is the reconciliation — it enqueues a job for any uploaded document
// old enough that a normal upload would have enqueued one already.
//
// It heals exactly the state nothing else owns: no parsing job row at all. A
// document whose job failed stays out of scope (conversion_status='failed'), so
// a permanently unparseable file is not resurrected, and neither is a document
// whose terminal job was purged on the retention policy. The inserted job
// carries no auto_extract flag: the sweep restores parsing, never spends the
// project's extraction budget on its own.
type DocumentParsingSweepTask struct {
	db        *bun.DB
	log       *slog.Logger
	minAge    time.Duration
	batchSize int
}

// Defaults for the sweep. minAge leaves room for an upload that is still in
// flight (row committed, job not yet enqueued); batchSize bounds the burst a
// first run over a large backlog creates.
const (
	defaultDocumentParsingSweepMinAge    = 15 * time.Minute
	defaultDocumentParsingSweepBatchSize = 100
)

// NewDocumentParsingSweepTask creates the sweep. A non-positive minAge or
// batchSize falls back to the defaults.
func NewDocumentParsingSweepTask(db *bun.DB, log *slog.Logger, minAge time.Duration, batchSize int) *DocumentParsingSweepTask {
	if minAge <= 0 {
		minAge = defaultDocumentParsingSweepMinAge
	}
	if batchSize <= 0 {
		batchSize = defaultDocumentParsingSweepBatchSize
	}
	return &DocumentParsingSweepTask{
		db:        db,
		log:       log.With(logger.Scope("scheduler.document_parsing_sweep")),
		minAge:    minAge,
		batchSize: batchSize,
	}
}

// documentParsingSweepQuery enqueues one parsing job per orphaned upload, in one
// statement so two sweepers cannot both act on the same row set: the NOT EXISTS
// makes an already-enqueued document ineligible, and the advisory lock in Run
// serialises the remaining race between concurrent sweepers.
//
// The document set is deliberately narrow. content IS NULL selects a document
// whose text was never written; conversion_status 'failed' is excluded because
// that document's parse already reached a terminal failure (its job row is
// either still there or was purged on retention) and re-running it would only
// fail again; source_type 'upload' excludes rows a data source owns, whose
// re-parse is that source's sync's job.
//
// The JOIN on kb.projects is an inner join, not a LEFT JOIN: a document with no
// project_id has no organization to enqueue under, and
// kb.document_parsing_jobs.project_id is NOT NULL. A LEFT JOIN would select a
// NULL project_id for that row, and one such row would fail the whole
// multi-row INSERT — so the sweep would error on every run and heal nothing,
// which is the exact failure it exists to prevent. Such a row is out of scope:
// nothing else can be enqueued for it either.
const documentParsingSweepQuery = `INSERT INTO kb.document_parsing_jobs
	(organization_id, project_id, status, source_type, source_filename, mime_type,
	 file_size_bytes, storage_key, document_id, metadata)
SELECT p.organization_id, d.project_id, 'pending', 'file_upload', d.filename, d.mime_type,
       d.file_size_bytes, d.storage_key, d.id, jsonb_build_object('swept', true)
FROM kb.documents d
JOIN kb.projects p ON p.id = d.project_id
WHERE d.content IS NULL
  AND d.project_id IS NOT NULL
  AND d.source_type = 'upload'
  AND d.conversion_status IS DISTINCT FROM 'failed'
  AND d.source_status <> 'deleted_from_source'
  AND d.created_at < now() - make_interval(mins => ?)
  AND NOT EXISTS (
      SELECT 1 FROM kb.document_parsing_jobs j WHERE j.document_id = d.id
  )
ORDER BY d.created_at ASC
LIMIT ?`

// Run enqueues parsing jobs for orphaned uploads. It is best-effort: a failure
// is logged and returned so the scheduler records it, and the next run retries
// the same document set.
func (t *DocumentParsingSweepTask) Run(ctx context.Context) error {
	start := time.Now()

	var enqueued int64
	err := t.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Serialise concurrent sweepers (several server replicas share one
		// database). Transaction-scoped: released when the tx ends.
		if _, lockErr := tx.NewRaw(
			"SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "kb.document_parsing_sweep",
		).Exec(ctx); lockErr != nil {
			return fmt.Errorf("acquire document parsing sweep lock: %w", lockErr)
		}
		res, execErr := tx.NewRaw(documentParsingSweepQuery, int(t.minAge.Minutes()), t.batchSize).Exec(ctx)
		if execErr != nil {
			return fmt.Errorf("enqueue parsing jobs for orphaned uploads: %w", execErr)
		}
		n, _ := res.RowsAffected()
		enqueued = n
		return nil
	})
	if err != nil {
		t.log.Error("document parsing sweep failed", logger.Error(err))
		return err
	}

	if enqueued > 0 {
		t.log.Warn("document parsing sweep: enqueued parsing jobs for uploads that had none",
			slog.Int64("count", enqueued),
			slog.Duration("min_age", t.minAge))
	} else {
		t.log.Debug("document parsing sweep: no orphaned uploads")
	}
	t.log.Debug("document parsing sweep: completed",
		slog.Int64("enqueued", enqueued),
		slog.Duration("duration", time.Since(start)))
	return nil
}
