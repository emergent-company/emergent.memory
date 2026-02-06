package search

import (
	"context"
	"log/slog"
	"sort"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
	"github.com/emergent/emergent-core/pkg/mathutil"
	"github.com/emergent/emergent-core/pkg/pgutils"
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

	rows, err := r.db.QueryContext(ctx, query, vectorStr, params.ProjectID, vectorStr, limit)
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

	// Execute vector search
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
	vectorRows, err := r.db.QueryContext(ctx, vectorQuery, vectorStr, params.ProjectID, vectorStr, fetchLimit)
	if err != nil {
		r.log.Error("hybrid vector search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	var vectorScores []float32
	for vectorRows.Next() {
		var row TextSearchResultRow
		if err := vectorRows.Scan(&row.ID, &row.DocumentID, &row.ChunkIndex, &row.Text, &row.Score); err != nil {
			vectorRows.Close()
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
