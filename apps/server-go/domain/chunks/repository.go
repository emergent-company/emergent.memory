package chunks

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Repository handles database operations for chunks
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new chunks repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("chunks.repo")),
	}
}

// ListByProject returns all chunks for a project, optionally filtered by document ID
func (r *Repository) ListByProject(ctx context.Context, projectID uuid.UUID, documentID *uuid.UUID) ([]*ChunkWithDocInfo, error) {
	var chunks []*ChunkWithDocInfo

	// Build query with join to documents for document info and RLS
	// Use a subquery for aggregate stats to avoid cartesian product
	query := r.db.NewSelect().
		TableExpr("kb.chunks AS c").
		ColumnExpr("c.*").
		ColumnExpr("d.filename AS document_filename").
		ColumnExpr("d.source_url AS document_source_url").
		ColumnExpr("stats.total_chars").
		ColumnExpr("stats.chunk_count").
		ColumnExpr("stats.embedded_chunks").
		Join("INNER JOIN kb.documents AS d ON d.id = c.document_id").
		Join(`LEFT JOIN LATERAL (
			SELECT 
				SUM(LENGTH(text)) AS total_chars,
				COUNT(*) AS chunk_count,
				COUNT(embedding) FILTER (WHERE embedding IS NOT NULL) AS embedded_chunks
			FROM kb.chunks 
			WHERE document_id = c.document_id
		) AS stats ON true`).
		Where("d.project_id = ?", projectID).
		Order("c.document_id", "c.chunk_index")

	if documentID != nil {
		query = query.Where("c.document_id = ?", *documentID)
	}

	err := query.Scan(ctx, &chunks)
	if err != nil {
		r.log.Error("failed to list chunks", "error", err, "projectId", projectID)
		return nil, apperror.NewInternal("failed to list chunks", err)
	}

	return chunks, nil
}

// GetByID retrieves a single chunk by ID within a project
func (r *Repository) GetByID(ctx context.Context, projectID, chunkID uuid.UUID) (*ChunkWithDocInfo, error) {
	var chunk ChunkWithDocInfo

	err := r.db.NewSelect().
		TableExpr("kb.chunks AS c").
		ColumnExpr("c.*").
		ColumnExpr("d.filename AS document_filename").
		ColumnExpr("d.source_url AS document_source_url").
		Join("INNER JOIN kb.documents AS d ON d.id = c.document_id").
		Where("c.id = ?", chunkID).
		Where("d.project_id = ?", projectID).
		Scan(ctx, &chunk)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.NewNotFound("chunk", chunkID.String())
		}
		r.log.Error("failed to get chunk", "error", err, "chunkId", chunkID)
		return nil, apperror.NewInternal("failed to get chunk", err)
	}

	return &chunk, nil
}

// Create creates a new chunk
func (r *Repository) Create(ctx context.Context, chunk *Chunk) error {
	_, err := r.db.NewInsert().
		Model(chunk).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to create chunk", "error", err, "documentId", chunk.DocumentID)
		return apperror.NewInternal("failed to create chunk", err)
	}

	return nil
}

// CreateBatch creates multiple chunks in a single transaction
func (r *Repository) CreateBatch(ctx context.Context, chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	_, err := r.db.NewInsert().
		Model(&chunks).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to create chunks batch", "error", err, "count", len(chunks))
		return apperror.NewInternal("failed to create chunks", err)
	}

	return nil
}

// Delete deletes a chunk by ID within a project (RLS enforced via document join)
func (r *Repository) Delete(ctx context.Context, projectID, chunkID uuid.UUID) error {
	// First verify the chunk exists and belongs to the project
	var count int
	count, err := r.db.NewSelect().
		TableExpr("kb.chunks AS c").
		Join("INNER JOIN kb.documents AS d ON d.id = c.document_id").
		Where("c.id = ?", chunkID).
		Where("d.project_id = ?", projectID).
		Count(ctx)

	if err != nil {
		r.log.Error("failed to check chunk existence", "error", err, "chunkId", chunkID)
		return apperror.NewInternal("failed to delete chunk", err)
	}

	if count == 0 {
		return apperror.NewNotFound("chunk", chunkID.String())
	}

	// Delete the chunk
	_, err = r.db.NewDelete().
		Model((*Chunk)(nil)).
		Where("id = ?", chunkID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete chunk", "error", err, "chunkId", chunkID)
		return apperror.NewInternal("failed to delete chunk", err)
	}

	return nil
}

// BulkDelete deletes multiple chunks by IDs within a project
func (r *Repository) BulkDelete(ctx context.Context, projectID uuid.UUID, chunkIDs []uuid.UUID) (*BulkDeletionSummary, error) {
	results := make([]*DeletionResult, 0, len(chunkIDs))
	totalDeleted := 0
	totalFailed := 0

	for _, chunkID := range chunkIDs {
		err := r.Delete(ctx, projectID, chunkID)
		if err != nil {
			results = append(results, &DeletionResult{
				ID:      chunkID.String(),
				Success: false,
				Error:   err.Error(),
			})
			totalFailed++
		} else {
			results = append(results, &DeletionResult{
				ID:      chunkID.String(),
				Success: true,
			})
			totalDeleted++
		}
	}

	return &BulkDeletionSummary{
		TotalRequested: len(chunkIDs),
		TotalDeleted:   totalDeleted,
		TotalFailed:    totalFailed,
		Results:        results,
	}, nil
}

// DeleteByDocument deletes all chunks for a document within a project
func (r *Repository) DeleteByDocument(ctx context.Context, projectID, documentID uuid.UUID) (*DocumentChunksDeletionResult, error) {
	// First verify document belongs to project and count chunks
	var count int
	count, err := r.db.NewSelect().
		TableExpr("kb.chunks AS c").
		Join("INNER JOIN kb.documents AS d ON d.id = c.document_id").
		Where("c.document_id = ?", documentID).
		Where("d.project_id = ?", projectID).
		Count(ctx)

	if err != nil {
		r.log.Error("failed to count document chunks", "error", err, "documentId", documentID)
		return &DocumentChunksDeletionResult{
			DocumentID:    documentID.String(),
			ChunksDeleted: 0,
			Success:       false,
			Error:         "failed to count chunks",
		}, nil
	}

	if count == 0 {
		// No chunks found - could be document doesn't exist or has no chunks
		return &DocumentChunksDeletionResult{
			DocumentID:    documentID.String(),
			ChunksDeleted: 0,
			Success:       true,
		}, nil
	}

	// Delete all chunks for this document
	result, err := r.db.NewDelete().
		Model((*Chunk)(nil)).
		Where("document_id = ?", documentID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete document chunks", "error", err, "documentId", documentID)
		return &DocumentChunksDeletionResult{
			DocumentID:    documentID.String(),
			ChunksDeleted: 0,
			Success:       false,
			Error:         "failed to delete chunks",
		}, nil
	}

	rowsAffected, _ := result.RowsAffected()

	return &DocumentChunksDeletionResult{
		DocumentID:    documentID.String(),
		ChunksDeleted: int(rowsAffected),
		Success:       true,
	}, nil
}

// BulkDeleteByDocuments deletes all chunks for multiple documents within a project
func (r *Repository) BulkDeleteByDocuments(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) (*BulkDocumentChunksDeletionSummary, error) {
	results := make([]*DocumentChunksDeletionResult, 0, len(documentIDs))
	totalChunks := 0

	for _, docID := range documentIDs {
		result, err := r.DeleteByDocument(ctx, projectID, docID)
		if err != nil {
			results = append(results, &DocumentChunksDeletionResult{
				DocumentID:    docID.String(),
				ChunksDeleted: 0,
				Success:       false,
				Error:         err.Error(),
			})
		} else {
			results = append(results, result)
			totalChunks += result.ChunksDeleted
		}
	}

	return &BulkDocumentChunksDeletionSummary{
		TotalDocuments: len(documentIDs),
		TotalChunks:    totalChunks,
		Results:        results,
	}, nil
}

// UpdateEmbedding updates the embedding vector for a chunk
func (r *Repository) UpdateEmbedding(ctx context.Context, chunkID uuid.UUID, embedding []float32) error {
	// Convert float32 slice to PostgreSQL vector literal format
	vecLiteral := floatsToVectorLiteral(embedding)

	_, err := r.db.NewRaw(
		"UPDATE kb.chunks SET embedding = ?::vector, updated_at = now() WHERE id = ?",
		vecLiteral, chunkID,
	).Exec(ctx)

	if err != nil {
		r.log.Error("failed to update chunk embedding", "error", err, "chunkId", chunkID)
		return apperror.NewInternal("failed to update embedding", err)
	}

	return nil
}

// floatsToVectorLiteral converts a slice of float32 to PostgreSQL vector literal format
func floatsToVectorLiteral(vec []float32) string {
	if len(vec) == 0 {
		return "[]"
	}

	result := "["
	for i, v := range vec {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%g", v)
	}
	result += "]"
	return result
}

// CountByDocument returns the number of chunks for a document
func (r *Repository) CountByDocument(ctx context.Context, documentID uuid.UUID) (int, error) {
	count, err := r.db.NewSelect().
		Model((*Chunk)(nil)).
		Where("document_id = ?", documentID).
		Count(ctx)

	if err != nil {
		r.log.Error("failed to count chunks", "error", err, "documentId", documentID)
		return 0, apperror.NewInternal("failed to count chunks", err)
	}

	return count, nil
}
