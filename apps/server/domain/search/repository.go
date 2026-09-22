package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/ftsquery"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/mathutil"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
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
// preventing cross-request interference.
//
// This is retained for the relationship search leg, whose index on
// kb.graph_relationships.embedding is still IVFFlat. The chunks index
// (idx_chunks_embedding_hnsw, migration 00170) and the graph-objects index
// (00164) are HNSW, so for those queries the SET LOCAL is a harmless no-op: it
// neither errors nor changes the plan, latency, or recall.
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

// configuredIVFFlatProbes returns the ivfflat.probes value to use for vector
// searches, read from the SEARCH_IVFFLAT_PROBES env var. Defaults to 10 and is
// clamped to >= 1 so an invalid/empty config never disables index scans.
//
// After migrations 00164 (graph objects) and 00170 (chunks), both consumers of
// this value use HNSW indexes, so the setting no longer affects their plan or
// recall and is effectively vestigial — it is still applied per transaction to
// avoid a behaviour change and to keep a single knob should an IVFFlat index be
// reintroduced. Relationship search has its own knob
// (configuredRelationshipIVFFlatProbes).
func configuredIVFFlatProbes() int {
	probes := 10
	if v := os.Getenv("SEARCH_IVFFLAT_PROBES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			probes = n
		}
	}
	return probes
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

	results, err := r.lexicalSearch(ctx, params.ProjectID, params.Query, limit)
	if err != nil {
		return nil, err
	}

	return &TextSearchResponse{
		Results:         results,
		Mode:            TextSearchModeLexical,
		TotalCandidates: len(results),
	}, nil
}

// lexicalSearch runs the chunks lexical query and, when it matches nothing,
// retries once with numeric terms removed. A hyphenated identifier in the query
// is rewritten by websearch_to_tsquery into a phrase that cannot match this
// index, which zeroes the whole AND clause — see ftsquery.Relax.
func (r *Repository) lexicalSearch(ctx context.Context, projectID uuid.UUID, queryText string, limit int) ([]*TextSearchResult, error) {
	results, err := r.runChunkLexical(ctx, projectID, queryText, limit)
	if err != nil || len(results) > 0 {
		return results, err
	}

	relaxed, ok := ftsquery.Relax(queryText)
	if !ok {
		return results, nil
	}
	return r.runChunkLexical(ctx, projectID, relaxed, limit)
}

// runChunkLexical executes the chunks lexical query for queryText.
func (r *Repository) runChunkLexical(ctx context.Context, projectID uuid.UUID, queryText string, limit int) ([]*TextSearchResult, error) {
	query := `
		SELECT c.id, c.document_id, c.chunk_index, c.text,
			   ts_rank_cd(c.tsv, websearch_to_tsquery('simple', ?), 32) AS score
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.tsv @@ websearch_to_tsquery('simple', ?)
		  AND d.project_id = ?
		ORDER BY score DESC
		LIMIT ?
	`

	rows, err := r.db.QueryContext(ctx, query, queryText, queryText, projectID, limit)
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

	return results, nil
}

// VectorSearch performs vector similarity search on kb.chunks using embedding column
func (r *Repository) VectorSearch(ctx context.Context, params TextSearchParams) (*TextSearchResponse, error) {
	if len(params.Vector) == 0 {
		return nil, apperror.ErrBadRequest.WithMessage("vector required for vector search")
	}

	limit := mathutil.ClampLimit(params.Limit, 20, 100)
	vectorStr := pgutils.FormatVector(params.Vector)

	// Begin transaction and apply ivfflat.probes for the legacy IVFFlat leg.
	// The chunks index is HNSW (idx_chunks_embedding_hnsw, migration 00170), so
	// the SET LOCAL is a no-op for this query; it is kept to avoid a behaviour
	// change and because the helper is shared with relationship search.
	tx, err := r.beginTxWithIVFFlatProbes(ctx, configuredIVFFlatProbes())
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
	lexicalHits, err := r.lexicalSearch(ctx, params.ProjectID, params.Query, fetchLimit)
	if err != nil {
		r.log.Error("hybrid lexical search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	lexicalResults := make(map[uuid.UUID]*hybridCandidate)
	var lexicalScores []float32
	for _, hit := range lexicalHits {
		lexicalResults[hit.ID] = &hybridCandidate{
			ID:           hit.ID,
			DocumentID:   hit.DocumentID,
			ChunkIndex:   hit.ChunkIndex,
			Text:         hit.Text,
			LexicalScore: hit.Score,
		}
		lexicalScores = append(lexicalScores, hit.Score)
	}

	// Execute vector search. As in VectorSearch, ivfflat.probes is applied only
	// for the shared helper's benefit and is a no-op for the HNSW chunks index.
	tx, err := r.beginTxWithIVFFlatProbes(ctx, configuredIVFFlatProbes())
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
	Namespace *string   // optional: filter by relationship namespace (denormalised from the src object)
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
	// Source node metadata
	SrcKey        string
	SrcType       string
	SrcProperties map[string]any
	// Target node metadata
	DstKey        string
	DstType       string
	DstProperties map[string]any
}

// RelationshipSearchResponse contains relationship search results
type RelationshipSearchResponse struct {
	Results         []*RelationshipSearchResult
	TotalCandidates int
}

// SearchRelationships performs vector similarity search on relationship embeddings.
// Finds semantically similar relationships using triplet text embeddings (e.g., "Elon Musk founded Tesla").
// Filters out relationships without embeddings (WHERE embedding IS NOT NULL).
//
// kb.graph_relationships.embedding is served by an HNSW index (migration 00171,
// m=16 / ef_construction=64). Unlike the previous ivfflat index, HNSW has no
// `probes` knob and needs no training/list step, so this path deliberately does
// NOT set `ivfflat.probes`: the setting would be a no-op for the index and the
// former SEARCH_RELATIONSHIP_IVFFLAT_PROBES stopgap has been removed. This also
// removes the planner cost-model cliff where a higher probes value made the
// planner abandon the ivfflat index for an optimistic parallel sequential scan
// (tens of seconds to minutes on ~82k embedded relationships).
//
// The project filter is primarily applied to kb.graph_relationships.project_id (the
// row that owns the embedding): the r-side predicate keeps scoping attached to the
// table being scanned instead of depending on the join, and is evaluated without
// touching the joined row. The src/dst predicates below are retained as
// defense-in-depth — kb.graph_relationships does not enforce at the database that its
// endpoints share its project_id (that invariant is application-enforced), so a
// malformed or legacy cross-project row would otherwise surface another project's
// object metadata through the joins.
//
// Likewise, namespace is denormalised onto kb.graph_relationships (from the src
// object) so the namespace predicate stays on the table that owns the embedding
// instead of the joined kb.graph_objects row.
func (r *Repository) SearchRelationships(ctx context.Context, params RelationshipSearchParams) (*RelationshipSearchResponse, error) {
	if len(params.Vector) == 0 {
		return nil, apperror.ErrBadRequest.WithMessage("vector required for relationship search")
	}

	limit := mathutil.ClampLimit(params.Limit, 50, 100)
	vectorStr := pgutils.FormatVector(params.Vector)

	// Cosine distance: lower is better, convert to similarity score (1 - distance)
	// Joins with graph_objects to construct triplet text: "{source.name} {type} {target.name}"
	query, queryArgs := buildRelationshipSearchQuery(vectorStr, params.ProjectID, params.Namespace, limit)

	rows, err := r.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		r.log.Error("relationship vector search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer rows.Close()

	var results []*RelationshipSearchResult
	for rows.Next() {
		var row struct {
			ID            uuid.UUID
			SrcID         uuid.UUID
			DstID         uuid.UUID
			Type          string
			Properties    []byte // JSONB
			TripletText   string
			Score         float32
			SrcKey        string
			SrcType       string
			SrcProperties []byte // JSONB
			DstKey        string
			DstType       string
			DstProperties []byte // JSONB
		}
		if err := rows.Scan(
			&row.ID, &row.SrcID, &row.DstID, &row.Type, &row.Properties, &row.TripletText, &row.Score,
			&row.SrcKey, &row.SrcType, &row.SrcProperties,
			&row.DstKey, &row.DstType, &row.DstProperties,
		); err != nil {
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

		var srcProps map[string]any
		if len(row.SrcProperties) > 0 {
			if err := json.Unmarshal(row.SrcProperties, &srcProps); err != nil {
				r.log.Warn("failed to parse src properties", logger.Error(err))
			}
		}

		var dstProps map[string]any
		if len(row.DstProperties) > 0 {
			if err := json.Unmarshal(row.DstProperties, &dstProps); err != nil {
				r.log.Warn("failed to parse dst properties", logger.Error(err))
			}
		}

		results = append(results, &RelationshipSearchResult{
			ID:            row.ID,
			SrcID:         row.SrcID,
			DstID:         row.DstID,
			Type:          row.Type,
			TripletText:   row.TripletText,
			Score:         row.Score,
			Properties:    props,
			SrcKey:        row.SrcKey,
			SrcType:       row.SrcType,
			SrcProperties: srcProps,
			DstKey:        row.DstKey,
			DstType:       row.DstType,
			DstProperties: dstProps,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &RelationshipSearchResponse{
		Results:         results,
		TotalCandidates: len(results),
	}, nil
}

// buildRelationshipSearchQuery builds the ANN SQL for relationship search.
// When namespace is non-nil the predicate is applied to
// kb.graph_relationships.namespace (denormalised from the src object), NOT to the
// joined kb.graph_objects row: a predicate on a joined relation prevents pgvector
// from satisfying it with the embedding index and lets the planner fall back to a
// sequential scan over every embedded relationship.
func buildRelationshipSearchQuery(vectorStr string, projectID uuid.UUID, namespace *string, limit int) (string, []any) {
	baseQuery := `
		SELECT 
			r.id,
			r.src_id,
			r.dst_id,
			r.type,
			r.properties,
			COALESCE(src.key, src.id::text) || ' ' || 
				LOWER(REPLACE(r.type, '_', ' ')) || ' ' || 
				COALESCE(dst.key, dst.id::text) AS triplet_text,
			(1 - (r.embedding <=> ?::vector)) AS score,
			COALESCE(src.key, '') AS src_key,
			COALESCE(src.type, '') AS src_type,
			src.properties AS src_properties,
			COALESCE(dst.key, '') AS dst_key,
			COALESCE(dst.type, '') AS dst_type,
			dst.properties AS dst_properties
		FROM kb.graph_relationships r
		JOIN kb.graph_objects src ON src.id = r.src_id
		JOIN kb.graph_objects dst ON dst.id = r.dst_id
		WHERE r.embedding IS NOT NULL
		  AND r.deleted_at IS NULL
		  AND r.project_id = ?
		  AND src.project_id = ?
		  AND dst.project_id = ?`
	queryArgs := []any{vectorStr, projectID, projectID, projectID}
	if namespace != nil {
		baseQuery += "\n\t\t  AND r.namespace = ?"
		queryArgs = append(queryArgs, *namespace)
	}
	query := baseQuery + "\n\t\tORDER BY r.embedding <=> ?::vector\n\t\tLIMIT ?"
	queryArgs = append(queryArgs, vectorStr, limit)
	return query, queryArgs
}
