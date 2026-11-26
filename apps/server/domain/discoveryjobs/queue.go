package discoveryjobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

func pqArray(ids []uuid.UUID) pq.StringArray {
	out := make(pq.StringArray, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// discoveryRequest is a queued batch of document ids awaiting a discovery run.
type discoveryRequest struct {
	bun.BaseModel `bun:"table:kb.discovery_requests,alias:dr"`

	ID          uuid.UUID   `bun:"id,pk"`
	ProjectID   uuid.UUID   `bun:"project_id"`
	DocumentIDs []uuid.UUID `bun:"document_ids,array"`
	Status      string      `bun:"status"`
	Attempts    int         `bun:"attempts"`
	UpdatedAt   time.Time   `bun:"updated_at"`
}

const (
	requestQueued  = "queued"
	requestRunning = "running"
	// maxQueueAttempts is how many times a request may fail to start before its
	// ids are dropped rather than requeued forever.
	maxQueueAttempts = 5
	// staleRunningAfter is how long a request may sit in 'running' before a
	// crash between Claim and Finish is assumed and the row is re-queued.
	staleRunningAfter = 10 * time.Minute
)

// Enqueue adds document ids to a project's discovery request queue, coalescing
// into the single active row. Ids are never dropped: a running row is appended
// to without changing its status (a second instance cannot re-claim it), and a
// stale running row is recovered by RecoverStaleRunning, which keeps its ids.
func (r *Repository) Enqueue(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) error {
	if len(documentIDs) == 0 {
		return nil
	}
	_, err := r.db.NewRaw(`
		INSERT INTO kb.discovery_requests (id, project_id, document_ids, status)
		VALUES (gen_random_uuid(), ?, ?, 'queued')
		ON CONFLICT (project_id) WHERE status IN ('queued','running')
		DO UPDATE SET
			document_ids = kb.discovery_requests.document_ids || EXCLUDED.document_ids,
			updated_at = now()`,
		projectID, pqArray(documentIDs)).Exec(ctx)
	if err != nil {
		r.log.Error("discovery queue: enqueue", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// ListQueued returns the queued request rows oldest-first.
func (r *Repository) ListQueued(ctx context.Context) ([]discoveryRequest, error) {
	var rows []discoveryRequest
	err := r.db.NewSelect().
		Table("kb.discovery_requests").
		Column("id", "project_id", "document_ids", "status", "attempts", "updated_at").
		Where("status = ?", requestQueued).
		Order("updated_at ASC").
		Scan(ctx, &rows)
	if err != nil {
		r.log.Error("discovery queue: list", logger.Error(err))
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	return rows, nil
}

// RecoverStaleRunning re-queues running requests whose updated_at is older than
// the threshold, repairing rows a crash left between Claim and Finish.
func (r *Repository) RecoverStaleRunning(ctx context.Context) error {
	_, err := r.db.NewRaw(`
		UPDATE kb.discovery_requests
		SET status = 'queued', updated_at = now()
		WHERE status = 'running' AND updated_at < now() - (? * interval '1 second')`,
		staleRunningAfter.Seconds()).Exec(ctx)
	if err != nil {
		r.log.Error("discovery queue: recover stale", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// Claim atomically marks a queued request running and returns its document ids
// and attempt count. The ids stay in the row (running) so a crash between Claim
// and the discovery run's durable creation does not lose them: RecoverStaleRunning
// re-queues the row with its ids intact. ok=false when another runner claimed it
// first.
func (r *Repository) Claim(ctx context.Context, requestID uuid.UUID) ([]uuid.UUID, int, bool, error) {
	var (
		ids      []uuid.UUID
		attempts int
		ok       bool
	)
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var raw string
		err := tx.NewRaw(`
			SELECT array_to_json(document_ids)::text, attempts
			FROM kb.discovery_requests
			WHERE id = ? AND status = 'queued'
			FOR UPDATE`, requestID).Scan(ctx, &raw, &attempts)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if _, err := tx.NewRaw(`
			UPDATE kb.discovery_requests
			SET status = 'running', attempts = attempts + 1, updated_at = now()
			WHERE id = ?`, requestID).Exec(ctx); err != nil {
			return err
		}
		ids = parseUUIDArray(raw)
		attempts++ // the UPDATE above incremented the stored counter
		ok = true
		return nil
	})
	if err != nil {
		r.log.Error("discovery queue: claim", logger.Error(err))
		return nil, 0, false, apperror.ErrInternal.WithInternal(err)
	}
	return ids, attempts, ok, nil
}

// parseUUIDArray decodes a Postgres array_to_json array of UUIDs ("[..]") into
// uuid values; unparseable entries are skipped.
func parseUUIDArray(raw string) []uuid.UUID {
	if raw == "" || raw == "null" {
		return nil
	}
	var strs []string
	if err := json.Unmarshal([]byte(raw), &strs); err != nil {
		return nil
	}
	out := make([]uuid.UUID, 0, len(strs))
	for _, s := range strs {
		if u, err := uuid.Parse(s); err == nil {
			out = append(out, u)
		}
	}
	return out
}

// Finish records that a run over `claimed` ids completed: `keep` are the claimed
// ids that still need a run (e.g. the portion beyond a per-run cap, or the full
// set after a failed start). It removes the ids that were actually started
// (claimed - keep) from the row and re-queues the rest, including any ids a
// concurrent Enqueue appended during the run. The row is dropped only when
// nothing remains.
func (r *Repository) Finish(ctx context.Context, requestID uuid.UUID, claimed, keep []uuid.UUID) error {
	run := subtract(claimed, keep)
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var raw string
		err := tx.NewRaw(`SELECT array_to_json(document_ids)::text FROM kb.discovery_requests WHERE id = ? FOR UPDATE`, requestID).Scan(ctx, &raw)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		final := subtract(parseUUIDArray(raw), run)
		if len(final) == 0 {
			if _, err := tx.NewDelete().Table("kb.discovery_requests").Where("id = ?", requestID).Exec(ctx); err != nil {
				return err
			}
			return nil
		}
		_, err = tx.NewRaw(`UPDATE kb.discovery_requests SET document_ids = ?, status = 'queued', updated_at = now() WHERE id = ?`, pqArray(final), requestID).Exec(ctx)
		return err
	})
}

// subtract returns the ids in a that are not in b (order preserved).
func subtract(a, b []uuid.UUID) []uuid.UUID {
	remove := make(map[uuid.UUID]bool, len(b))
	for _, id := range b {
		remove[id] = true
	}
	out := make([]uuid.UUID, 0, len(a))
	for _, id := range a {
		if !remove[id] {
			out = append(out, id)
		}
	}
	return out
}

// Enqueue queues document ids for a discovery run. It is the sync worker's
// entry point: ids are coalesced into the project's queued request and drained
// by StartQueueRunner, so a concurrent run or crash cannot drop them.
func (s *Service) Enqueue(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) error {
	return s.repo.Enqueue(ctx, projectID, documentIDs)
}

// StartQueueRunner begins draining the queue until ctx is cancelled.
func (s *Service) StartQueueRunner(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.drainQueue(ctx)
		}
	}
}

// drainQueue recovers stale running rows, then claims queued requests for
// projects with no active discovery run and starts a run over their document
// ids, capping to the per-run document cap and returning the remainder (or a
// failed start's full set) to the queue.
func (s *Service) drainQueue(ctx context.Context) {
	if err := s.repo.RecoverStaleRunning(ctx); err != nil {
		s.log.Warn("discovery queue: recover failed", logger.Error(err))
	}

	reqs, err := s.repo.ListQueued(ctx)
	if err != nil {
		s.log.Warn("discovery queue: list failed", logger.Error(err))
		return
	}
	for _, req := range reqs {
		active, aerr := s.repo.ActiveJobForProject(ctx, req.ProjectID)
		if aerr != nil {
			s.log.Warn("discovery queue: active lookup failed", logger.Error(aerr))
			continue
		}
		if active != nil {
			continue
		}

		ids, attempts, ok, cerr := s.repo.Claim(ctx, req.ID)
		if cerr != nil {
			s.log.Warn("discovery queue: claim failed", logger.Error(cerr))
			continue
		}
		if !ok {
			continue
		}

		run := ids
		var keep []uuid.UUID
		if cap := s.documentCap(); len(ids) > cap {
			run = ids[:cap]
			keep = ids[cap:]
		}

		pctx := auth.ContextWithProjectID(ctx, req.ProjectID.String())
		_, serr := s.StartDiscovery(pctx, req.ProjectID, &StartDiscoveryRequest{
			Scope:       DiscoveryScopeDocuments,
			DocumentIDs: run,
			Trigger:     DiscoveryTriggerSourceSync,
		})
		if serr != nil {
			s.log.Warn("discovery queue: start failed", logger.Error(serr))
			if attempts >= maxQueueAttempts {
				s.log.Error("discovery queue: dropping request after repeated failures",
					slog.String("project_id", req.ProjectID.String()),
					slog.Int("attempts", attempts))
				if ferr := s.repo.Finish(ctx, req.ID, ids, nil); ferr != nil {
					s.log.Warn("discovery queue: drop failed", logger.Error(ferr))
				}
				continue
			}
			// Nothing started: re-queue the full claimed set.
			if ferr := s.repo.Finish(ctx, req.ID, ids, ids); ferr != nil {
				s.log.Warn("discovery queue: requeue failed", logger.Error(ferr))
			}
			continue
		}
		if ferr := s.repo.Finish(ctx, req.ID, ids, keep); ferr != nil {
			s.log.Warn("discovery queue: finish failed", logger.Error(ferr))
		}
	}
}
