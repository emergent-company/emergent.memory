package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
	"github.com/emergent-company/emergent/pkg/mathutil"
	"github.com/emergent-company/emergent/pkg/pgutils"
)

// Repository handles text search operations on kb.chunks
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new search repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("search.repo")),
	}
}

// beginTxWithIVFFlatProbes starts a transaction and sets ivfflat.probes for improved
// vector index recall. SET LOCAL scopes the setting to the current transaction only,
// preventing cross-request interference. With ~100 IVFFlat lists (typical), probes=10
// scans 10% of the index, improving recall from ~40% to ~90%+ with negligible latency cost.
func (r *Repository) beginTxWithIVFFlatProbes(ctx context.Context, probes int) (bun.Tx, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return tx, apperror.ErrDatabase.WithInternal(err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL ivfflat.probes = %d", probes)); err != nil {
		_ = tx.Rollback()
		return tx, apperror.ErrDatabase.WithInternal(err)
	}
	return tx, nil
}

// TextSearchMode defines the type of text search
type TextSearchMode string

const (
	TextSearchModeLexical TextSearchMode = "lexical"
	TextSearchModeVector  TextSearchMode = "vector"
	TextSearchModeHybrid  TextSearchMode = "hybrid"
)

// TextSearchParams contains parameters for text search
type TextSearchParams struct {
	ProjectID     uuid.UUID
	Query         string
	Vector        []float32 // Query embedding for vector/hybrid search
	Mode          TextSearchMode
	LexicalWeight float32
	VectorWeight  float32
	Limit         int
}

// TextSearchResultRow represents a single text search result from the database
type TextSearchResultRow struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	ChunkIndex int
	Text       string
	Score      float32
}

// TextSearchResponse contains the search results and metadata
type TextSearchResponse struct {
	Results         []*TextSearchResult
	Mode            TextSearchMode
	TotalCandidates int
}

// LexicalSearch performs full-text search on kb.chunks using tsv column
func (r *Repository) LexicalSearch(ctx context.Context, params TextSearchParams) (*TextSearchResponse, error) {
	limit := mathutil.ClampLimit(params.Limit, 20, 100)

	query := `
		SELECT c.id, c.document_id, c.chunk_index, c.text,
			   ts_rank(c.tsv, websearch_to_tsquery('simple', ?)) AS score
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.tsv @@ websearch_to_tsquery('simple', ?)
		  AND d.project_id = ?
		ORDER BY score DESC
		LIMIT ?
	`

	rows, err := r.db.QueryContext(ctx, query, params.Query, params.Query, params.ProjectID, limit)
	if err != nil {
		r.log.Error("lexical search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer rows.Close()

	var results []*TextSearchResult
	for rows.Next() {
		var row TextSearchResultRow
		if err := rows.Scan(&row.ID, &row.DocumentID, &row.ChunkIndex, &row.Text, &row.Score); err != nil {
			r.log.Error("lexical search row scan failed", logger.Error(err))
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		mode := string(TextSearchModeLexical)
		docID := row.DocumentID.String()
		results = append(results, &TextSearchResult{
			ID:         row.ID,
			DocumentID: row.DocumentID,
			ChunkIndex: row.ChunkIndex,
			Text:       row.Text,
			Score:      row.Score,
			Mode:       &mode,
			Source:     &docID,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &TextSearchResponse{
		Results:         results,
		Mode:            TextSearchModeLexical,
		TotalCandidates: len(results),
	}, nil
}

// VectorSearch performs vector similarity search on kb.chunks using embedding column
func (r *Repository) VectorSearch(ctx context.Context, params TextSearchParams) (*TextSearchResponse, error) {
	if len(params.Vector) == 0 {
		return nil, apperror.ErrBadRequest.WithMessage("vector required for vector search")
	}

	limit := mathutil.ClampLimit(params.Limit, 20, 100)
	vectorStr := pgutils.FormatVector(params.Vector)

	// Begin transaction with increased IVFFlat probes for better recall
	tx, err := r.beginTxWithIVFFlatProbes(ctx, 10)
	if err != nil {
		r.log.Error("vector search: failed to set ivfflat probes", logger.Error(err))
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Cosine distance: lower is better, convert to similarity score (1 - distance)
	query := `
		SELECT c.id, c.document_id, c.chunk_index, c.text,
			   (1 - (c.embedding <=> ?::vector)) AS score
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.embedding IS NOT NULL
		  AND d.project_id = ?
		ORDER BY c.embedding <=> ?::vector
		LIMIT ?
	`

	rows, err := tx.QueryContext(ctx, query, vectorStr, params.ProjectID, vectorStr, limit)
	if err != nil {
		r.log.Error("vector search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer rows.Close()

	var results []*TextSearchResult
	for rows.Next() {
		var row TextSearchResultRow
		if err := rows.Scan(&row.ID, &row.DocumentID, &row.ChunkIndex, &row.Text, &row.Score); err != nil {
			r.log.Error("vector search row scan failed", logger.Error(err))
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		mode := string(TextSearchModeVector)
		docID := row.DocumentID.String()
		results = append(results, &TextSearchResult{
			ID:         row.ID,
			DocumentID: row.DocumentID,
			ChunkIndex: row.ChunkIndex,
			Text:       row.Text,
			Score:      row.Score,
			Mode:       &mode,
			Source:     &docID,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Commit the read-only transaction
	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &TextSearchResponse{
		Results:         results,
		Mode:            TextSearchModeVector,
		TotalCandidates: len(results),
	}, nil
}

// HybridSearch combines lexical and vector search using z-score normalization and weighted fusion
func (r *Repository) HybridSearch(ctx context.Context, params TextSearchParams) (*TextSearchResponse, error) {
	if len(params.Vector) == 0 {
		// Fall back to lexical only
		return r.LexicalSearch(ctx, params)
	}

	limit := mathutil.ClampLimit(params.Limit, 20, 100)

	// Fetch 2x limit from each source for better fusion
	fetchLimit := limit * 2
	vectorStr := pgutils.FormatVector(params.Vector)

	// Set default weights
	lexicalWeight := params.LexicalWeight
	vectorWeight := params.VectorWeight
	if lexicalWeight <= 0 {
		lexicalWeight = 0.5
	}
	if vectorWeight <= 0 {
		vectorWeight = 0.5
	}

	// Execute lexical search
	lexicalQuery := `
		SELECT c.id, c.document_id, c.chunk_index, c.text,
			   ts_rank(c.tsv, websearch_to_tsquery('simple', ?)) AS score
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.tsv @@ websearch_to_tsquery('simple', ?)
		  AND d.project_id = ?
		ORDER BY score DESC
		LIMIT ?
	`
	lexicalRows, err := r.db.QueryContext(ctx, lexicalQuery, params.Query, params.Query, params.ProjectID, fetchLimit)
	if err != nil {
		r.log.Error("hybrid lexical search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	lexicalResults := make(map[uuid.UUID]*hybridCandidate)
	var lexicalScores []float32
	for lexicalRows.Next() {
		var row TextSearchResultRow
		if err := lexicalRows.Scan(&row.ID, &row.DocumentID, &row.ChunkIndex, &row.Text, &row.Score); err != nil {
			lexicalRows.Close()
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		lexicalResults[row.ID] = &hybridCandidate{
			ID:           row.ID,
			DocumentID:   row.DocumentID,
			ChunkIndex:   row.ChunkIndex,
			Text:         row.Text,
			LexicalScore: row.Score,
		}
		lexicalScores = append(lexicalScores, row.Score)
	}
	lexicalRows.Close()

	// Execute vector search with increased IVFFlat probes for better recall
	tx, err := r.beginTxWithIVFFlatProbes(ctx, 10)
	if err != nil {
		r.log.Error("hybrid search: failed to set ivfflat probes", logger.Error(err))
		return nil, err
	}

	vectorQuery := `
		SELECT c.id, c.document_id, c.chunk_index, c.text,
			   (1 - (c.embedding <=> ?::vector)) AS score
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.embedding IS NOT NULL
		  AND d.project_id = ?
		ORDER BY c.embedding <=> ?::vector
		LIMIT ?
	`
	vectorRows, err := tx.QueryContext(ctx, vectorQuery, vectorStr, params.ProjectID, vectorStr, fetchLimit)
	if err != nil {
		_ = tx.Rollback()
		r.log.Error("hybrid vector search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	var vectorScores []float32
	for vectorRows.Next() {
		var row TextSearchResultRow
		if err := vectorRows.Scan(&row.ID, &row.DocumentID, &row.ChunkIndex, &row.Text, &row.Score); err != nil {
			vectorRows.Close()
			_ = tx.Rollback()
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		if existing, ok := lexicalResults[row.ID]; ok {
			existing.VectorScore = row.Score
		} else {
			lexicalResults[row.ID] = &hybridCandidate{
				ID:          row.ID,
				DocumentID:  row.DocumentID,
				ChunkIndex:  row.ChunkIndex,
				Text:        row.Text,
				VectorScore: row.Score,
			}
		}
		vectorScores = append(vectorScores, row.Score)
	}
	vectorRows.Close()
	// Commit the read-only vector search transaction
	_ = tx.Commit()

	// Calculate z-score normalization parameters
	lexicalMean, lexicalStd := mathutil.CalcMeanStd(lexicalScores)
	vectorMean, vectorStd := mathutil.CalcMeanStd(vectorScores)

	// Normalize and fuse scores
	var candidates []*hybridCandidate
	for _, c := range lexicalResults {
		// Z-score normalize each score, then apply sigmoid to get [0,1]
		normalizedLexical := float32(0)
		if c.LexicalScore > 0 && lexicalStd > 0 {
			z := (c.LexicalScore - lexicalMean) / lexicalStd
			normalizedLexical = mathutil.Sigmoid(z)
		}

		normalizedVector := float32(0)
		if c.VectorScore > 0 && vectorStd > 0 {
			z := (c.VectorScore - vectorMean) / vectorStd
			normalizedVector = mathutil.Sigmoid(z)
		}

		// Weighted combination
		c.FusedScore = normalizedLexical*lexicalWeight + normalizedVector*vectorWeight
		candidates = append(candidates, c)
	}

	// Sort by fused score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].FusedScore > candidates[j].FusedScore
	})

	// Take top results
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	// Convert to results
	results := make([]*TextSearchResult, len(candidates))
	mode := string(TextSearchModeHybrid)
	for i, c := range candidates {
		docID := c.DocumentID.String()
		results[i] = &TextSearchResult{
			ID:         c.ID,
			DocumentID: c.DocumentID,
			ChunkIndex: c.ChunkIndex,
			Text:       c.Text,
			Score:      c.FusedScore,
			Mode:       &mode,
			Source:     &docID,
		}
	}

	return &TextSearchResponse{
		Results:         results,
		Mode:            TextSearchModeHybrid,
		TotalCandidates: len(lexicalResults),
	}, nil
}

// hybridCandidate holds intermediate fusion data
type hybridCandidate struct {
	ID           uuid.UUID
	DocumentID   uuid.UUID
	ChunkIndex   int
	Text         string
	LexicalScore float32
	VectorScore  float32
	FusedScore   float32
}

// RelationshipSearchParams contains parameters for relationship vector search
type RelationshipSearchParams struct {
	ProjectID uuid.UUID
	Vector    []float32 // Query embedding for semantic search
	Limit     int       // Result limit (default: 50, max: 100)
}

// RelationshipSearchResult represents a single relationship search result
type RelationshipSearchResult struct {
	ID          uuid.UUID
	SrcID       uuid.UUID
	DstID       uuid.UUID
	Type        string
	TripletText string
	Score       float32
	Properties  map[string]any
}

// RelationshipSearchResponse contains relationship search results
type RelationshipSearchResponse struct {
	Results         []*RelationshipSearchResult
	TotalCandidates int
}

// SearchRelationships performs vector similarity search on relationship embeddings.
// Finds semantically similar relationships using triplet text embeddings (e.g., "Elon Musk founded Tesla").
// Filters out relationships without embeddings (WHERE embedding IS NOT NULL).
// Uses the ivfflat index for efficient approximate nearest neighbor search.
func (r *Repository) SearchRelationships(ctx context.Context, params RelationshipSearchParams) (*RelationshipSearchResponse, error) {
	if len(params.Vector) == 0 {
		return nil, apperror.ErrBadRequest.WithMessage("vector required for relationship search")
	}

	limit := mathutil.ClampLimit(params.Limit, 50, 100)
	vectorStr := pgutils.FormatVector(params.Vector)

	// Begin transaction with increased IVFFlat probes for better recall
	tx, err := r.beginTxWithIVFFlatProbes(ctx, 10)
	if err != nil {
		r.log.Error("relationship search: failed to set ivfflat probes", logger.Error(err))
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Cosine distance: lower is better, convert to similarity score (1 - distance)
	// Joins with graph_objects to construct triplet text: "{source.name} {type} {target.name}"
	query := `
		SELECT 
			r.id,
			r.src_id,
			r.dst_id,
			r.type,
			r.properties,
			COALESCE(src.name, src.key, src.id::text) || ' ' || 
				LOWER(REPLACE(r.type, '_', ' ')) || ' ' || 
				COALESCE(dst.name, dst.key, dst.id::text) AS triplet_text,
			(1 - (r.embedding <=> ?::vector)) AS score
		FROM kb.graph_relationships r
		JOIN kb.graph_objects src ON src.id = r.src_id
		JOIN kb.graph_objects dst ON dst.id = r.dst_id
		WHERE r.embedding IS NOT NULL
		  AND r.deleted_at IS NULL
		  AND src.project_id = ?
		ORDER BY r.embedding <=> ?::vector
		LIMIT ?
	`

	rows, err := tx.QueryContext(ctx, query, vectorStr, params.ProjectID, vectorStr, limit)
	if err != nil {
		r.log.Error("relationship vector search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer rows.Close()

	var results []*RelationshipSearchResult
	for rows.Next() {
		var row struct {
			ID          uuid.UUID
			SrcID       uuid.UUID
			DstID       uuid.UUID
			Type        string
			Properties  []byte // JSONB
			TripletText string
			Score       float32
		}
		if err := rows.Scan(&row.ID, &row.SrcID, &row.DstID, &row.Type, &row.Properties, &row.TripletText, &row.Score); err != nil {
			r.log.Error("relationship search row scan failed", logger.Error(err))
			return nil, apperror.ErrDatabase.WithInternal(err)
		}

		// Parse JSONB properties
		var props map[string]any
		if len(row.Properties) > 0 {
			if err := json.Unmarshal(row.Properties, &props); err != nil {
				r.log.Warn("failed to parse relationship properties", logger.Error(err), slog.String("relationship_id", row.ID.String()))
			}
		}

		results = append(results, &RelationshipSearchResult{
			ID:          row.ID,
			SrcID:       row.SrcID,
			DstID:       row.DstID,
			Type:        row.Type,
			TripletText: row.TripletText,
			Score:       row.Score,
			Properties:  props,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Commit the read-only transaction
	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &RelationshipSearchResponse{
		Results:         results,
		TotalCandidates: len(results),
	}, nil
}
