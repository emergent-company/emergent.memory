package chunking

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/chunks"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/textsplitter"
)

type Service struct {
	db  bun.IDB
	log *slog.Logger

	// defaultChunkSize / defaultChunkOverlap are the env-backed defaults
	// (CHUNK_SIZE / CHUNK_OVERLAP, falling back to textsplitter defaults
	// 1000/200). Per-document chunking_config overrides them per document.
	defaultChunkSize    int
	defaultChunkOverlap int
}

func NewService(db bun.IDB, cfg *config.Config, log *slog.Logger) *Service {
	def := textsplitter.DefaultConfig()
	chunkSize, chunkOverlap := def.ChunkSize, def.ChunkOverlap
	if cfg != nil {
		if cfg.Chunking.ChunkSize > 0 {
			chunkSize = cfg.Chunking.ChunkSize
		}
		if cfg.Chunking.ChunkOverlap >= 0 {
			chunkOverlap = cfg.Chunking.ChunkOverlap
		}
	}
	return &Service{
		db:                  db,
		log:                 log.With(logger.Scope("chunking.svc")),
		defaultChunkSize:    chunkSize,
		defaultChunkOverlap: chunkOverlap,
	}
}

type RecreateChunksResponse struct {
	Status  string                `json:"status"`
	Summary RecreateChunksSummary `json:"summary"`
}

// RecreateChunksRequest is the optional body for recreate-chunks. force
// discards manually edited chunks instead of preserving them.
type RecreateChunksRequest struct {
	Force bool `json:"force"`
}

type RecreateChunksSummary struct {
	OldChunks       int            `json:"oldChunks"`
	NewChunks       int            `json:"newChunks"`
	Strategy        string         `json:"strategy"`
	ParentChild     bool           `json:"parentChild,omitempty"`
	AutoProfile     bool           `json:"autoProfile,omitempty"`
	Shape           string         `json:"shape,omitempty"`
	PreservedEdited int            `json:"preservedEdited,omitempty"`
	DiscardedEdited int            `json:"discardedEdited,omitempty"`
	Force           bool           `json:"force,omitempty"`
	Config          map[string]any `json:"config,omitempty"`
}

// RecreateChunks regenerates a document's chunks using the project's chunking
// configuration. Manually edited chunks are preserved (flagged, with a warning
// reported in the summary) unless force is set, in which case they are
// discarded with a warning.
func (s *Service) RecreateChunks(ctx context.Context, projectID, documentID string, force bool) (*RecreateChunksResponse, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid project ID format")
	}

	docUUID, err := uuid.Parse(documentID)
	if err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid document ID format")
	}

	var content sql.NullString
	err = s.db.NewSelect().
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

	cfg := textsplitter.Config{
		ChunkSize:    s.defaultChunkSize,
		ChunkOverlap: s.defaultChunkOverlap,
	}
	projCfg := s.loadProjectChunkingConfig(ctx, projectID)
	strategy := textsplitter.NormalizeStrategy(derefString(projCfg.Strategy))
	parentChild := projCfg.ParentChild != nil && *projCfg.ParentChild
	autoProfile := projCfg.AutoProfile != nil && *projCfg.AutoProfile
	var shape textsplitter.Shape

	if autoProfile {
		// Auto-profile replaces the project parameters instead of layering
		// on them: the point is per-document sizing, and a project-wide
		// strategy is exactly what it exists to avoid. Explicit project
		// settings still apply when autoProfile is off, so existing projects
		// behave byte-for-byte as before.
		detected, signals := textsplitter.DetectShape(content.String)
		shape = detected
		profile := textsplitter.ProfileFor(detected)
		cfg = profile.Config
		strategy = profile.Options.Strategy
		parentChild = profile.Options.ParentChild
		s.log.Debug("detected document shape",
			slog.String("documentId", documentID),
			slog.String("shape", string(detected)),
			slog.Int("lines", signals.Lines),
			slog.Int("headings", signals.Headings),
			slog.Int("fences", signals.Fences),
			slog.Int("tableRows", signals.TableRows),
			slog.Int("speakerLines", signals.SpeakerLines))
	} else {
		if projCfg.ChunkSize != nil && *projCfg.ChunkSize > 0 {
			cfg.ChunkSize = *projCfg.ChunkSize
		}
		if projCfg.ChunkOverlap != nil && *projCfg.ChunkOverlap >= 0 {
			cfg.ChunkOverlap = *projCfg.ChunkOverlap
		}
	}
	pieces, err := textsplitter.SplitDocument(content.String, cfg, textsplitter.Options{
		Strategy:    strategy,
		ParentChild: parentChild,
	})
	if err != nil {
		return nil, apperror.ErrBadRequest.WithMessage(err.Error())
	}

	now := time.Now().UTC()

	// Preserve manually edited chunks unless the caller forces a full rebuild.
	var preserved []*chunks.Chunk
	discardedEdited := 0
	if force {
		discardedEdited, _ = s.countEditedChunks(ctx, documentID)
	} else {
		preserved, err = s.loadEditedChunks(ctx, documentID)
		if err != nil {
			return nil, apperror.NewInternal("failed to load edited chunks", err)
		}
	}

	chunkRows := make([]*chunks.Chunk, 0, len(pieces))
	pieceIDs := make([]uuid.UUID, len(pieces))
	for i := range pieces {
		pieceIDs[i] = uuid.New()
	}
	for i, piece := range pieces {
		meta := &chunks.ChunkMetadata{
			Strategy: string(strategy),
			IsParent: piece.IsParent,
		}
		if autoProfile {
			meta.AutoProfile = true
			meta.Shape = string(shape)
		}
		if piece.Oversized {
			meta.Oversized = true
		}
		if piece.HeaderCarry != "" {
			meta.HeaderCarry = piece.HeaderCarry
		}
		var parentID *uuid.UUID
		if piece.ParentIndex >= 0 && piece.ParentIndex < len(pieceIDs) {
			pid := pieceIDs[piece.ParentIndex]
			parentID = &pid
		}
		chunkRows = append(chunkRows, &chunks.Chunk{
			ID:            pieceIDs[i],
			DocumentID:    docUUID,
			Text:          piece.Text,
			Metadata:      meta,
			ParentChunkID: parentID,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}

	type embeddingJob struct {
		bun.BaseModel `bun:"table:kb.chunk_embedding_jobs"`
		ID            uuid.UUID `bun:"id,pk,type:uuid"`
		ChunkID       uuid.UUID `bun:"chunk_id,type:uuid,notnull"`
		Status        string    `bun:"status,notnull"`
		Priority      int       `bun:"priority,notnull"`
		CreatedAt     time.Time `bun:"created_at,notnull"`
		UpdatedAt     time.Time `bun:"updated_at,notnull"`
	}

	jobRows := make([]*embeddingJob, 0, len(chunkRows))
	for _, c := range chunkRows {
		// Parent chunks are structural context only: they are not embedded.
		if c.Metadata != nil && c.Metadata.IsParent {
			continue
		}
		jobRows = append(jobRows, &embeddingJob{
			ID:        uuid.New(),
			ChunkID:   c.ID,
			Status:    "pending",
			Priority:  1,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	// Original revision text per preserved chunk, for stale detection.
	originalTexts := map[uuid.UUID]string{}
	if len(preserved) > 0 {
		originalTexts, err = s.loadOriginalRevisionTexts(ctx, preserved)
		if err != nil {
			s.log.Warn("failed to load original revision texts", slog.String("error", err.Error()))
		}
	}

	surviving := map[uuid.UUID]bool{}
	for _, c := range preserved {
		surviving[c.ID] = true
	}
	for _, c := range chunkRows {
		surviving[c.ID] = true
	}

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Lock the document row to serialize concurrent re-chunks.
		var lockedID string
		if err := tx.NewRaw("SELECT id FROM kb.documents WHERE id = ? FOR UPDATE", documentID).Scan(ctx, &lockedID); err != nil {
			return err
		}

		// Compute the start index under the lock so concurrent re-chunks do
		// not race to the same chunk_index. Preserved edited chunks keep their
		// original positions; new derived chunks are placed after them.
		startIndex := 0
		if !force {
			startIndex = s.nextChunkIndexTx(ctx, tx, documentID)
		}
		for i := range chunkRows {
			chunkRows[i].ChunkIndex = startIndex + i
		}

		if force {
			if _, err := tx.NewDelete().TableExpr("kb.chunks").Where("document_id = ?", documentID).Exec(ctx); err != nil {
				return err
			}
		} else if oldCount > 0 {
			if _, err := tx.NewDelete().
				TableExpr("kb.chunks").
				Where("document_id = ?", documentID).
				Where("edited = false").
				Exec(ctx); err != nil {
				return err
			}
		}

		if len(chunkRows) > 0 {
			if _, err := tx.NewInsert().Model(&chunkRows).Exec(ctx); err != nil {
				return err
			}
		}

		if len(jobRows) > 0 {
			if _, err := tx.NewInsert().Model(&jobRows).Exec(ctx); err != nil {
				// Embedding jobs are best-effort: log but don't abort the transaction.
				s.log.Warn("failed to enqueue chunk embedding jobs",
					slog.String("documentId", documentID),
					slog.String("error", err.Error()))
			}
		}

		// Reconcile preserved edited chunks: flag stale when their original
		// span is gone, and null a parent link that no longer resolves.
		for _, c := range preserved {
			meta := c.Metadata
			if meta == nil {
				meta = &chunks.ChunkMetadata{}
			}
			changed := false

			if orig, ok := originalTexts[c.ID]; ok && !sourceTextStillPresent(orig, content.String) {
				meta.Stale = true
				changed = true
			}

			var parentID *uuid.UUID
			if c.ParentChunkID != nil {
				if surviving[*c.ParentChunkID] {
					parentID = c.ParentChunkID
				} else {
					changed = true // parent removed by re-chunk → drop the link
				}
			}

			if changed {
				metaJSON, _ := json.Marshal(meta)
				if _, err := tx.NewRaw(
					"UPDATE kb.chunks SET metadata = ?::jsonb, parent_chunk_id = ?, updated_at = now() WHERE id = ?",
					string(metaJSON), parentID, c.ID,
				).Exec(ctx); err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, apperror.ErrInternal.WithMessage("Failed to create chunks").WithInternal(err)
	}

	if len(preserved) > 0 {
		s.log.Warn("re-chunk preserved manually edited chunks",
			slog.String("documentId", documentID),
			slog.Int("preserved", len(preserved)))
	}
	if force && discardedEdited > 0 {
		s.log.Warn("re-chunk discarded manually edited chunks (force)",
			slog.String("documentId", documentID),
			slog.Int("discarded", discardedEdited))
	}

	s.log.Info("recreated chunks",
		slog.String("documentId", documentID),
		slog.Int("oldChunks", oldCount),
		slog.Int("newChunks", len(chunkRows)),
		slog.Int("preservedEdited", len(preserved)),
		slog.Int("discardedEdited", discardedEdited),
		slog.Int("embeddingJobs", len(jobRows)),
		slog.Bool("autoProfile", autoProfile),
		slog.String("shape", string(shape)))

	return &RecreateChunksResponse{
		Status: "success",
		Summary: RecreateChunksSummary{
			OldChunks:       oldCount,
			NewChunks:       len(chunkRows),
			Strategy:        string(strategy),
			ParentChild:     parentChild,
			AutoProfile:     autoProfile,
			Shape:           string(shape),
			PreservedEdited: len(preserved),
			DiscardedEdited: discardedEdited,
			Force:           force,
			Config: map[string]any{
				"chunkSize":    cfg.ChunkSize,
				"chunkOverlap": cfg.ChunkOverlap,
			},
		},
	}, nil
}

// sourceTextStillPresent reports whether the preserved chunk's original text
// still appears in the regenerated content. An empty original is treated as
// still present (nothing to invalidate).
func sourceTextStillPresent(originalText, newContent string) bool {
	if originalText == "" {
		return true
	}
	return strings.Contains(newContent, originalText)
}

func (s *Service) loadEditedChunks(ctx context.Context, documentID string) ([]*chunks.Chunk, error) {
	var rows []*chunks.Chunk
	err := s.db.NewSelect().
		Model(&rows).
		Where("document_id = ?", documentID).
		Where("edited = true").
		Order("chunk_index ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Service) countEditedChunks(ctx context.Context, documentID string) (int, error) {
	count, err := s.db.NewSelect().
		TableExpr("kb.chunks").
		Where("document_id = ?", documentID).
		Where("edited = true").
		Count(ctx)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// nextChunkIndexTx returns one past the maximum existing chunk_index for a
// document, so newly derived chunks do not collide with preserved ones. It
// must run on the locked transaction so concurrent re-chunks do not race.
func (s *Service) nextChunkIndexTx(ctx context.Context, tx bun.Tx, documentID string) int {
	var maxIdx sql.NullInt64
	err := tx.NewSelect().
		TableExpr("kb.chunks").
		ColumnExpr("MAX(chunk_index)").
		Where("document_id = ?", documentID).
		Scan(ctx, &maxIdx)
	if err != nil || !maxIdx.Valid {
		return 0
	}
	return int(maxIdx.Int64) + 1
}

// loadOriginalRevisionTexts returns seq-1 text for each given chunk.
func (s *Service) loadOriginalRevisionTexts(ctx context.Context, preserved []*chunks.Chunk) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, 0, len(preserved))
	for _, c := range preserved {
		ids = append(ids, c.ID)
	}
	rows := []struct {
		ChunkID uuid.UUID `bun:"chunk_id"`
		Text    string    `bun:"text"`
	}{}
	err := s.db.NewSelect().
		TableExpr("kb.chunk_revisions").
		Column("chunk_id", "text").
		Where("chunk_id IN (?)", bun.In(ids)).
		Where("seq = 1").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		out[r.ChunkID] = r.Text
	}
	return out, nil
}

// projectChunkingConfig carries the per-project chunking overrides read from
// kb.projects.chunking_config. Pointers distinguish "absent" from an explicit
// value. Missing column, missing row, or malformed JSON yield nils (callers
// fall back to env/default).
type projectChunkingConfig struct {
	ChunkSize    *int
	ChunkOverlap *int
	Strategy     *string
	ParentChild  *bool
	AutoProfile  *bool
}

// loadProjectChunkingConfig reads the project-level kb.projects.chunking_config
// JSONB. The JSON carries maxChunkSize (chunk size), overlap (chunk overlap),
// strategy (chunking strategy name), parentChild, and autoProfile (per-document
// shape detection replaces those values).
func (s *Service) loadProjectChunkingConfig(ctx context.Context, projectID string) projectChunkingConfig {
	var raw []byte
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		Column("chunking_config").
		Where("id = ?", projectID).
		Scan(ctx, &raw)
	if err != nil || len(raw) == 0 {
		return projectChunkingConfig{}
	}
	var projCfg struct {
		MaxChunkSize *int    `json:"maxChunkSize"`
		Overlap      *int    `json:"overlap"`
		Strategy     *string `json:"strategy"`
		ParentChild  *bool   `json:"parentChild"`
		AutoProfile  *bool   `json:"autoProfile"`
	}
	if jsonErr := json.Unmarshal(raw, &projCfg); jsonErr != nil {
		return projectChunkingConfig{}
	}
	return projectChunkingConfig{
		ChunkSize:    projCfg.MaxChunkSize,
		ChunkOverlap: projCfg.Overlap,
		Strategy:     projCfg.Strategy,
		ParentChild:  projCfg.ParentChild,
		AutoProfile:  projCfg.AutoProfile,
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
