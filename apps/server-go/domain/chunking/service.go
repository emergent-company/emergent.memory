package chunking

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
	"github.com/emergent/emergent-core/pkg/textsplitter"
)

type Service struct {
	db  bun.IDB
	log *slog.Logger
}

func NewService(db bun.IDB, log *slog.Logger) *Service {
	return &Service{
		db:  db,
		log: log.With(logger.Scope("chunking.svc")),
	}
}

type RecreateChunksResponse struct {
	Status  string                `json:"status"`
	Summary RecreateChunksSummary `json:"summary"`
}

type RecreateChunksSummary struct {
	OldChunks int            `json:"oldChunks"`
	NewChunks int            `json:"newChunks"`
	Strategy  string         `json:"strategy"`
	Config    map[string]any `json:"config,omitempty"`
}

func (s *Service) RecreateChunks(ctx context.Context, projectID, documentID string) (*RecreateChunksResponse, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid project ID format")
	}

	if _, err := uuid.Parse(documentID); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid document ID format")
	}

	var content sql.NullString
	err := s.db.NewSelect().
		TableExpr("kb.documents").
		Column("content").
		Where("id = ?", documentID).
		Where("project_id = ?", projectID).
		Scan(ctx, &content)

	if err == sql.ErrNoRows {
		return nil, apperror.ErrNotFound.WithMessage("Document not found")
	}
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	if !content.Valid || content.String == "" {
		return nil, apperror.ErrBadRequest.WithMessage("Document has no content to chunk")
	}

	var oldCount int
	err = s.db.NewSelect().
		TableExpr("kb.chunks").
		ColumnExpr("COUNT(*)").
		Where("document_id = ?", documentID).
		Scan(ctx, &oldCount)
	if err != nil {
		s.log.Warn("failed to count existing chunks", slog.String("error", err.Error()))
		oldCount = 0
	}

	if oldCount > 0 {
		_, err = s.db.NewDelete().
			TableExpr("kb.chunks").
			Where("document_id = ?", documentID).
			Exec(ctx)
		if err != nil {
			return nil, apperror.ErrInternal.WithMessage("Failed to delete existing chunks").WithInternal(err)
		}
	}

	cfg := textsplitter.DefaultConfig()
	textChunks := textsplitter.Split(content.String, cfg)

	if len(textChunks) == 0 {
		return &RecreateChunksResponse{
			Status: "success",
			Summary: RecreateChunksSummary{
				OldChunks: oldCount,
				NewChunks: 0,
				Strategy:  "recursive_character",
				Config: map[string]any{
					"chunkSize":    cfg.ChunkSize,
					"chunkOverlap": cfg.ChunkOverlap,
				},
			},
		}, nil
	}

	now := time.Now().UTC()
	var chunkIDs []string
	for i, text := range textChunks {
		chunkID := uuid.New().String()
		chunkIDs = append(chunkIDs, chunkID)

		_, err = s.db.NewRaw(`
			INSERT INTO kb.chunks (id, document_id, chunk_index, text, metadata, created_at, updated_at)
			VALUES (?, ?, ?, ?, '{"strategy": "recursive_character"}'::jsonb, ?, ?)
		`, chunkID, documentID, i, text, now, now).Exec(ctx)
		if err != nil {
			s.log.Error("failed to insert chunk",
				slog.Int("index", i),
				slog.String("error", err.Error()))
			return nil, apperror.ErrInternal.WithMessage("Failed to create chunks").WithInternal(err)
		}
	}

	if len(chunkIDs) > 0 {
		for _, chunkID := range chunkIDs {
			jobID := uuid.New().String()
			_, err = s.db.NewRaw(`
				INSERT INTO kb.chunk_embedding_jobs (id, chunk_id, status, priority, created_at, updated_at)
				VALUES (?, ?, 'pending', 1, ?, ?)
			`, jobID, chunkID, now, now).Exec(ctx)
			if err != nil {
				s.log.Warn("failed to enqueue embedding job",
					slog.String("chunkId", chunkID),
					slog.String("error", err.Error()))
			}
		}
		s.log.Info("enqueued chunk embedding jobs",
			slog.Int("count", len(chunkIDs)),
			slog.String("documentId", documentID))
	}

	s.log.Info("recreated chunks",
		slog.String("documentId", documentID),
		slog.Int("oldChunks", oldCount),
		slog.Int("newChunks", len(textChunks)))

	return &RecreateChunksResponse{
		Status: "success",
		Summary: RecreateChunksSummary{
			OldChunks: oldCount,
			NewChunks: len(textChunks),
			Strategy:  "recursive_character",
			Config: map[string]any{
				"chunkSize":    cfg.ChunkSize,
				"chunkOverlap": cfg.ChunkOverlap,
			},
		},
	}, nil
}
