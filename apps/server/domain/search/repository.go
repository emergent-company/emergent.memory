package search

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sort"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/branchsql"
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
	// Namespace, when non-nil and non-empty, restricts chunk results to chunks
	// extracted into at least one graph object whose live HEAD carries that
	// namespace. Chunks have no namespace of their own (kb.chunks is
	// project-scoped); the object→chunk link (kb.object_chunks) is what makes a
	// chunk belong to a namespace. Without this the text leg would leak chunks
	// from every namespace into a namespace-restricted search.
	Namespace *string
	// NamespaceIsNull, when true (and Namespace unset), scopes chunk results to
	// chunks extracted into a graph object whose live HEAD has namespace IS NULL.
	// The MCP default search treats an omitted namespace as the null namespace
	// (matching its schema-type allow/block lists), so its chunk leg must carry
	// the same scoping or it leaks chunks extracted into non-null-namespace
	// objects (#1910).
	NamespaceIsNull bool
}

// chunkNamespacePredicate returns the extra WHERE fragment (and its bind args)
// that scopes chunk results to a namespace, or ("", nil) when no namespace
// restriction applies. It assumes the enclosing query aliases kb.chunks as `c`
// and kb.documents as `d`.
//
// A chunk belongs to the namespace of the object it was extracted from. Because
// graph deletes are versioned — SoftDelete inserts a tombstone HEAD and
// supersedes the prior HEAD, leaving the prior row's own deleted_at NULL — the
// predicate resolves the version linked by kb.object_chunks.object_id to the
// object's live HEAD (the supersedes_id IS NULL AND deleted_at IS NULL row of
// the same canonical_id and branch) and tests that HEAD. So a soft-deleted
// object's chunks are excluded, while a chunk linked to a superseded but still
// live version still counts: namespace is inherited across versions within a
// branch, and a branch-forked copy (which carries no namespace of its own)
// cannot admit a chunk under a namespace it was never extracted into. The
// predicate is fail-closed: a chunk with no live extracted object in the
// requested namespace is excluded.
//
// When namespace is nil/empty and nullNamespace is true, the predicate instead
// matches objects whose live HEAD has namespace IS NULL (the MCP default
// search). Both unset means no restriction.
func chunkNamespacePredicate(namespace *string, nullNamespace bool) (string, []any) {
	namespaceCond := ""
	var args []any
	switch {
	case namespace != nil && *namespace != "":
		namespaceCond = "o.namespace = ?"
		args = []any{*namespace}
	case nullNamespace:
		namespaceCond = "o.namespace IS NULL"
	default:
		return "", nil
	}
	return `
		  AND EXISTS (
			SELECT 1 FROM kb.object_chunks oc
			JOIN kb.graph_objects ov ON ov.id = oc.object_id
			JOIN kb.graph_objects o
			  ON o.canonical_id = ov.canonical_id
			 AND o.project_id = ov.project_id
			 AND o.branch_id IS NOT DISTINCT FROM ov.branch_id
			WHERE oc.chunk_id = c.id
			  AND o.project_id = d.project_id
			  AND o.supersedes_id IS NULL
			  AND o.deleted_at IS NULL
			  AND ` + namespaceCond + `
		  )`, args
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

	results, err := r.lexicalSearch(ctx, params, limit)
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
// retries once with numeric terms removed (ftsquery.Relax) and then once more
// with the terms OR-joined (ftsquery.Disjoin). A hyphenated identifier in the
// query is rewritten by websearch_to_tsquery into a phrase that cannot match
// this index, which zeroes the whole AND clause — see ftsquery.Relax. A natural
// multi-term query over chunks where no single chunk holds every term is
// zeroed by AND semantics but well represented under OR — see ftsquery.Disjoin.
func (r *Repository) lexicalSearch(ctx context.Context, params TextSearchParams, limit int) ([]*TextSearchResult, error) {
	results, err := r.runChunkLexical(ctx, params, params.Query, limit, false)
	if err != nil || len(results) > 0 {
		return results, err
	}

	relaxed, ok := ftsquery.Relax(params.Query)
	if ok {
		relaxedResults, err := r.runChunkLexical(ctx, params, relaxed, limit, false)
		if err != nil {
			return nil, err
		}
		if len(relaxedResults) > 0 {
			return relaxedResults, nil
		}
	}

	disjoined, ok := ftsquery.Disjoin(params.Query)
	if !ok {
		return results, nil
	}
	return r.runChunkLexical(ctx, params, disjoined, limit, true)
}

// runChunkLexical executes the chunks lexical query for queryText. When
// disjoin is true, queryText is a `|`-joined term disjunction matched with
// to_tsquery (OR semantics) instead of websearch_to_tsquery (AND semantics).
func (r *Repository) runChunkLexical(ctx context.Context, params TextSearchParams, queryText string, limit int, disjoin bool) ([]*TextSearchResult, error) {
	tsqueryFn := "websearch_to_tsquery"
	if disjoin {
		tsqueryFn = "to_tsquery"
	}

	nsPredicate, nsArgs := chunkNamespacePredicate(params.Namespace, params.NamespaceIsNull)

	query := `
		SELECT c.id, c.document_id, c.chunk_index, c.text,
			   ts_rank_cd(c.tsv, ` + tsqueryFn + `('simple', ?), 32) AS score
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.tsv @@ ` + tsqueryFn + `('simple', ?)
		  AND d.project_id = ?
		  AND COALESCE((c.metadata->>'isParent')::boolean, false) = false` + nsPredicate + `
		ORDER BY score DESC, c.id ASC
		LIMIT ?
	`

	args := []any{queryText, queryText, params.ProjectID}
	args = append(args, nsArgs...)
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
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

	nsPredicate, nsArgs := chunkNamespacePredicate(params.Namespace, params.NamespaceIsNull)

	// Cosine distance: lower is better, convert to similarity score (1 - distance)
	query := `
		SELECT c.id, c.document_id, c.chunk_index, c.text,
			   (1 - (c.embedding <=> ?::vector)) AS score
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.embedding IS NOT NULL
		  AND d.project_id = ?
		  AND COALESCE((c.metadata->>'isParent')::boolean, false) = false` + nsPredicate + `
		ORDER BY c.embedding <=> ?::vector
		LIMIT ?
	`

	args := []any{vectorStr, params.ProjectID}
	args = append(args, nsArgs...)
	args = append(args, vectorStr, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
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
	lexicalHits, err := r.lexicalSearch(ctx, params, fetchLimit)
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

	nsPredicate, nsArgs := chunkNamespacePredicate(params.Namespace, params.NamespaceIsNull)

	vectorQuery := `
		SELECT c.id, c.document_id, c.chunk_index, c.text,
			   (1 - (c.embedding <=> ?::vector)) AS score
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.embedding IS NOT NULL
		  AND d.project_id = ?
		  AND COALESCE((c.metadata->>'isParent')::boolean, false) = false` + nsPredicate + `
		ORDER BY c.embedding <=> ?::vector
		LIMIT ?
	`
	vectorArgs := []any{vectorStr, params.ProjectID}
	vectorArgs = append(vectorArgs, nsArgs...)
	vectorArgs = append(vectorArgs, vectorStr, fetchLimit)

	vectorRows, err := r.db.QueryContext(ctx, vectorQuery, vectorArgs...)
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

	// Don't penalise the surviving channel when the other returned no scores.
	lexicalWeight, vectorWeight = mathutil.EffectiveFusionWeights(lexicalWeight, vectorWeight, len(lexicalScores) > 0, len(vectorScores) > 0)

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
		if candidates[i].FusedScore != candidates[j].FusedScore {
			return candidates[i].FusedScore > candidates[j].FusedScore
		}
		return slices.Compare(candidates[i].ID[:], candidates[j].ID[:]) < 0
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

// BranchTarget is one unmerged branch a scoped search may read (#265).
type BranchTarget struct {
	ID          uuid.UUID `bun:"id"`
	Name        string    `bun:"name"`
	Origin      string    `bun:"origin"`
	OriginLabel *string   `bun:"origin_label"`
}

// branchReadCandidateLimit bounds how many unmerged branches one scoped search
// considers before the scope's own maxBranches cap applies. It is a defensive
// ceiling for a project that accumulated thousands of open branches, not a
// policy: a branch beyond it is reported as omitted like any other.
const branchReadCandidateLimit = 500

// ListUnmergedBranches returns a project's open branches, newest first, minus
// any extraction branch whose source document no longer exists. Origins, when
// non-empty, restricts the query to those origins; name globs are applied by the
// caller because they are a scope rule, not a SQL predicate.
func (r *Repository) ListUnmergedBranches(ctx context.Context, projectID uuid.UUID, origins []string) ([]*BranchTarget, error) {
	var out []*BranchTarget
	q := r.db.NewSelect().
		TableExpr("kb.branches AS b").
		ColumnExpr("b.id").
		ColumnExpr("b.name").
		ColumnExpr("b.origin").
		ColumnExpr("b.origin_label").
		Where("b.project_id = ?", projectID).
		Where("b.merged_at IS NULL").
		Where(branchsql.DeletedExtractionSourceExclusion).
		Order("b.created_at DESC").
		Limit(branchReadCandidateLimit)
	if len(origins) > 0 {
		q = q.Where("b.origin IN (?)", bun.In(origins))
	}
	if err := q.Scan(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
