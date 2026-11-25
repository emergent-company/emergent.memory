package discoveryjobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

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

// Enqueue queues document ids for a discovery run. It is the sync worker's
// entry point: ids are coalesced into the project's queued request and drained
// by StartQueueRunner, so a concurrent run or crash cannot drop them.
func (s *Service) Enqueue(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) error {
	return s.repo.Enqueue(ctx, projectID, documentIDs)
}

// discoveryRequest is a queued batch of document ids awaiting a discovery run.
type discoveryRequest struct {
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
)

// Enqueue adds document ids to a project's discovery request queue, coalescing
// into the single queued row. Ids that fail to parse as UUIDs are ignored.
func (r *Repository) Enqueue(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) error {
	if len(documentIDs) == 0 {
		return nil
	}
	res, err := r.db.NewUpdate().
		Table("kb.discovery_requests").
		Set("document_ids = document_ids || ?", pqArray(documentIDs)).
		Set("updated_at = now()").
		Where("project_id = ?", projectID).
		Where("status = ?", requestQueued).
		Exec(ctx)
	if err != nil {
		r.log.Error("discovery queue: merge", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = r.db.NewInsert().
		Model(&discoveryRequest{ID: uuid.New(), ProjectID: projectID, DocumentIDs: documentIDs, Status: requestQueued}).
		On("CONFLICT (project_id) WHERE status IN ('queued','running') DO NOTHING").
		Exec(ctx)
	if err != nil {
		r.log.Error("discovery queue: insert", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// ListQueued returns the queued request rows oldest-first.
func (r *Repository) ListQueued(ctx context.Context) ([]discoveryRequest, error) {
	var rows []discoveryRequest
	err := r.db.NewSelect().
		Model(&rows).
		Where("status = ?", requestQueued).
		Order("updated_at ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("discovery queue: list", logger.Error(err))
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	return rows, nil
}

// Claim marks a request running and returns its document ids, so the runner can
// start a discovery run over them. It returns ok=false when another runner
// claimed it first.
func (r *Repository) Claim(ctx context.Context, requestID uuid.UUID) ([]uuid.UUID, bool, error) {
	var ids []uuid.UUID
	res, err := r.db.NewUpdate().
		Table("kb.discovery_requests").
		Set("status = ?", requestRunning).
		Set("attempts = attempts + 1").
		Set("updated_at = now()").
		Where("id = ?", requestID).
		Where("status = ?", requestQueued).
		Exec(ctx)
	if err != nil {
		r.log.Error("discovery queue: claim", logger.Error(err))
		return nil, false, apperror.ErrInternal.WithInternal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, false, nil
	}
	err = r.db.NewSelect().
		Table("kb.discovery_requests").
		Column("document_ids").
		Where("id = ?", requestID).
		Scan(ctx, &ids)
	if err != nil {
		r.log.Error("discovery queue: read ids", logger.Error(err))
		return nil, false, apperror.ErrInternal.WithInternal(err)
	}
	return ids, true, nil
}

// Finish removes a request after its run completed (or returns the remainder to
// the queue on error, capped at maxAttempts).
func (r *Repository) Finish(ctx context.Context, requestID uuid.UUID, keep []uuid.UUID, maxAttempts int) error {
	if len(keep) > 0 {
		_, err := r.db.NewUpdate().
			Table("kb.discovery_requests").
			Set("document_ids = ?", pqArray(keep)).
			Set("status = ?", requestQueued).
			Set("updated_at = now()").
			Where("id = ?", requestID).
			Exec(ctx)
		return err
	}
	_, err := r.db.NewDelete().
		Table("kb.discovery_requests").
		Where("id = ?", requestID).
		Exec(ctx)
	if err != nil {
		r.log.Error("discovery queue: finish", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
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

// drainQueue claims queued requests for projects with no active discovery run
// and starts a run over their document ids, capping to the per-run document
// cap and returning any remainder to the queue.
func (s *Service) drainQueue(ctx context.Context) {
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

		ids, ok, cerr := s.repo.Claim(ctx, req.ID)
		if cerr != nil || !ok {
			if cerr != nil {
				s.log.Warn("discovery queue: claim failed", logger.Error(cerr))
			}
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
			// A 409 (run appeared between our check and now) or any other
			// failure returns the ids to the queue for a later tick.
			s.log.Warn("discovery queue: start failed", logger.Error(serr))
			if ferr := s.repo.Finish(ctx, req.ID, ids, 0); ferr != nil {
				s.log.Warn("discovery queue: requeue failed", logger.Error(ferr))
			}
			continue
		}
		if ferr := s.repo.Finish(ctx, req.ID, keep, 0); ferr != nil {
			s.log.Warn("discovery queue: finish failed", logger.Error(ferr))
		}
	}
}
