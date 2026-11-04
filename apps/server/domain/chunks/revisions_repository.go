package chunks

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// chunkInProject reports whether the chunk exists and belongs to the project.
func (r *Repository) chunkInProject(ctx context.Context, projectID, chunkID uuid.UUID) (bool, error) {
	count, err := r.db.NewSelect().
		TableExpr("kb.chunks AS c").
		Join("INNER JOIN kb.documents AS d ON d.id = c.document_id").
		Where("c.id = ?", chunkID).
		Where("d.project_id = ?", projectID).
		Count(ctx)
	if err != nil {
		return false, apperror.NewInternal("failed to check chunk ownership", err)
	}
	return count > 0, nil
}

// maxChunkRevisionsReturned caps the number of revisions returned per chunk to
// bound memory and response size. The newest revisions are returned; the
// service reverses them back to ascending order for callers.
const maxChunkRevisionsReturned = 500

// ListRevisions returns a chunk's newest revisions ordered by descending seq.
func (r *Repository) ListRevisions(ctx context.Context, projectID, chunkID uuid.UUID) ([]*ChunkRevision, error) {
	ok, err := r.chunkInProject(ctx, projectID, chunkID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NewNotFound("chunk", chunkID.String())
	}

	var revs []*ChunkRevision
	err = r.db.NewSelect().
		Model(&revs).
		Where("chunk_id = ?", chunkID).
		Order("seq DESC").
		Limit(maxChunkRevisionsReturned).
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewInternal("failed to list chunk revisions", err)
	}
	return revs, nil
}

// GetRevision returns a single revision of a chunk within a project.
func (r *Repository) GetRevision(ctx context.Context, projectID, chunkID uuid.UUID, seq int) (*ChunkRevision, error) {
	var rev ChunkRevision
	err := r.db.NewSelect().
		TableExpr("kb.chunk_revisions AS cr").
		ColumnExpr("cr.*").
		Join("INNER JOIN kb.chunks AS c ON c.id = cr.chunk_id").
		Join("INNER JOIN kb.documents AS d ON d.id = c.document_id").
		Where("cr.chunk_id = ?", chunkID).
		Where("cr.seq = ?", seq).
		Where("d.project_id = ?", projectID).
		Scan(ctx, &rev)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.NewNotFound("chunk revision", chunkID.String())
		}
		return nil, apperror.NewInternal("failed to get chunk revision", err)
	}
	return &rev, nil
}

// ApplyChunkEdit atomically records a new revision with the given text, updates
// the chunk's text (marking it edited), advances its current revision, and
// enqueues re-embedding. It returns the new revision sequence number.
func (r *Repository) ApplyChunkEdit(ctx context.Context, projectID, chunkID uuid.UUID, text string, author *string) (int, error) {
	var next int
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row struct {
			CurrentRevision int `bun:"current_revision"`
		}
		err := tx.NewSelect().
			TableExpr("kb.chunks AS c").
			ColumnExpr("c.current_revision").
			Join("INNER JOIN kb.documents AS d ON d.id = c.document_id").
			Where("c.id = ?", chunkID).
			Where("d.project_id = ?", projectID).
			For("UPDATE OF c").
			Scan(ctx, &row)
		if err == sql.ErrNoRows {
			return apperror.NewNotFound("chunk", chunkID.String())
		}
		if err != nil {
			return apperror.NewInternal("failed to lock chunk for edit", err)
		}

		next = row.CurrentRevision + 1
		rev := &ChunkRevision{
			ID:        uuid.New(),
			ChunkID:   chunkID,
			Seq:       next,
			Text:      text,
			Author:    author,
			CreatedAt: time.Now().UTC(),
		}
		if _, err := tx.NewInsert().Model(rev).Exec(ctx); err != nil {
			return apperror.NewInternal("failed to insert chunk revision", err)
		}
		if _, err := tx.NewUpdate().
			TableExpr("kb.chunks").
			Set("text = ?", text).
			Set("edited = true").
			Set("current_revision = ?", next).
			Set("updated_at = now()").
			Where("id = ?", chunkID).
			Exec(ctx); err != nil {
			return apperror.NewInternal("failed to update chunk text", err)
		}

		// Re-embed the edited chunk. Best-effort: a queue failure must not roll
		// back the edit itself.
		if _, err := tx.NewRaw(
			"INSERT INTO kb.chunk_embedding_jobs (id, chunk_id, status, priority, created_at, updated_at) "+
				"VALUES (?, ?, 'pending', ?, now(), now()) "+
				"ON CONFLICT (chunk_id) WHERE status IN ('pending','processing') DO NOTHING",
			uuid.New(), chunkID, 1,
		).Exec(ctx); err != nil {
			r.log.Warn("failed to enqueue chunk embedding job after edit", "error", err, "chunkId", chunkID)
		}

		return nil
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}
