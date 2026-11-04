package search

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// RetrievalTrace is a persisted record of a unified search execution, stored in
// kb.retrieval_traces. JSONB columns hold the raw payloads as JSON bytes.
type RetrievalTrace struct {
	bun.BaseModel `bun:"table:kb.retrieval_traces"`

	ID          uuid.UUID `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ProjectID   uuid.UUID `bun:"project_id,type:uuid,notnull"`
	TraceID     uuid.UUID `bun:"trace_id,type:uuid,notnull"`
	Query       string    `bun:"query,notnull"`
	Filters     []byte    `bun:"filters,type:jsonb"`
	Candidates  []byte    `bun:"candidates,type:jsonb"`
	SelectedIDs []byte    `bun:"selected_ids,type:jsonb"`
	Scores      []byte    `bun:"scores,type:jsonb"`
	CreatedAt   time.Time `bun:"created_at,notnull,default:now()"`
}

// TraceStore persists and reads retrieval traces.
type TraceStore struct {
	db  bun.IDB
	log *slog.Logger
}

// NewTraceStore creates a new retrieval trace store.
func NewTraceStore(db bun.IDB, log *slog.Logger) *TraceStore {
	return &TraceStore{
		db:  db,
		log: log.With(logger.Scope("search.trace")),
	}
}

// Insert stores a retrieval trace row.
func (s *TraceStore) Insert(ctx context.Context, t *RetrievalTrace) error {
	if _, err := s.db.NewInsert().Model(t).Exec(ctx); err != nil {
		s.log.Error("failed to insert retrieval trace", logger.Error(err), slog.String("trace_id", t.TraceID.String()))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// GetByTraceID returns the most recent trace for the given trace ID, or
// (nil, nil) when no matching trace exists.
func (s *TraceStore) GetByTraceID(ctx context.Context, traceID uuid.UUID) (*RetrievalTrace, error) {
	trace := &RetrievalTrace{}
	if err := s.db.NewSelect().
		Model(trace).
		Where("trace_id = ?", traceID).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		s.log.Error("failed to fetch retrieval trace", logger.Error(err), slog.String("trace_id", traceID.String()))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return trace, nil
}

// DeleteExpired removes traces created before olderThan and returns the number
// of deleted rows.
func (s *TraceStore) DeleteExpired(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.NewDelete().
		TableExpr("kb.retrieval_traces").
		Where("created_at < ?", olderThan).
		Exec(ctx)
	if err != nil {
		s.log.Error("failed to delete expired retrieval traces", logger.Error(err))
		return 0, apperror.ErrDatabase.WithInternal(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
