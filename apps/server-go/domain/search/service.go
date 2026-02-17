package search

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent/domain/graph"
	"github.com/emergent-company/emergent/pkg/embeddings"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Service handles unified search combining graph and text results
type Service struct {
	repo         *Repository
	graphService *graph.Service
	embeddings   *embeddings.Service
	log          *slog.Logger
}

// NewService creates a new search service
func NewService(
	repo *Repository,
	graphService *graph.Service,
	embeddingsSvc *embeddings.Service,
	log *slog.Logger,
) *Service {
	return &Service{
		repo:         repo,
		graphService: graphService,
		embeddings:   embeddingsSvc,
		log:          log.With(logger.Scope("search.svc")),
	}
}

// Search executes unified search combining graph and text results
func (s *Service) Search(ctx context.Context, projectID uuid.UUID, req *UnifiedSearchRequest, searchCtx *SearchContext) (*UnifiedSearchResponse, error) {
	startTime := time.Now()

	// Apply defaults
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	resultTypes := req.ResultTypes
	if resultTypes == "" {
		resultTypes = ResultTypeBoth
	}

	fusionStrategy := req.FusionStrategy
	if fusionStrategy == "" {
		fusionStrategy = FusionStrategyWeighted
	}

	// Pre-compute query embedding once for all search goroutines (D1: embedding deduplication)
	var queryVector []float32
	if s.embeddings != nil {
		vec, err := s.embeddings.EmbedQuery(ctx, req.Query)
		if err != nil {
			s.log.Warn("failed to generate query embedding, falling back to lexical-only search", logger.Error(err))
		} else {
			queryVector = vec
		}
	}

	// Execute graph and text searches in parallel
	type graphResult struct {
		results  []*UnifiedSearchGraphResult
		elapsed  time.Duration
		rawDebug any
		err      error
	}
	type textResult struct {
		results  []*TextSearchResult
		mode     string
		elapsed  time.Duration
		rawDebug any
		err      error
	}
	type relationshipResult struct {
		results  []*RelationshipSearchResult
		elapsed  time.Duration
		rawDebug any
		err      error
	}

	graphCh := make(chan graphResult, 1)
	textCh := make(chan textResult, 1)
	relationshipCh := make(chan relationshipResult, 1)

	// Execute graph search
	go func() {
		if resultTypes == ResultTypeText {
			graphCh <- graphResult{results: nil, elapsed: 0}
			return
		}
		start := time.Now()
		results, rawDebug, err := s.executeGraphSearch(ctx, projectID, req, searchCtx, queryVector)
		graphCh <- graphResult{results: results, elapsed: time.Since(start), rawDebug: rawDebug, err: err}
	}()

	// Execute text search
	go func() {
		if resultTypes == ResultTypeGraph {
			textCh <- textResult{results: nil, elapsed: 0}
			return
		}
		start := time.Now()
		results, mode, rawDebug, err := s.executeTextSearch(ctx, projectID, req, queryVector)
		textCh <- textResult{results: results, mode: mode, elapsed: time.Since(start), rawDebug: rawDebug, err: err}
	}()

	// Execute relationship search
	go func() {
		if resultTypes == ResultTypeText {
			relationshipCh <- relationshipResult{results: nil, elapsed: 0}
			return
		}
		start := time.Now()
		results, rawDebug, err := s.executeRelationshipSearch(ctx, projectID, req, queryVector)
		relationshipCh <- relationshipResult{results: results, elapsed: time.Since(start), rawDebug: rawDebug, err: err}
	}()

	// Wait for all searches
	graphRes := <-graphCh
	textRes := <-textCh
	relationshipRes := <-relationshipCh

	if graphRes.err != nil {
		return nil, graphRes.err
	}
	if textRes.err != nil {
		return nil, textRes.err
	}
	if relationshipRes.err != nil {
		return nil, relationshipRes.err
	}

	// Expand relationships for graph results if enabled
	graphResults := graphRes.results
	var relationshipElapsed time.Duration
	if req.RelationshipOptions != nil && req.RelationshipOptions.Enabled && len(graphResults) > 0 {
		start := time.Now()
		graphResults = s.expandRelationships(ctx, projectID, graphResults, req.RelationshipOptions)
		relationshipElapsed = time.Since(start)
	}

	// Fuse results
	fusionStart := time.Now()
	fusedResults := s.fuseResults(graphResults, textRes.results, relationshipRes.results, fusionStrategy, req.Weights, limit)
	fusionElapsed := time.Since(fusionStart)

	// Count result types
	graphCount := 0
	textCount := 0
	relationshipCount := 0
	for _, r := range fusedResults {
		if r.Type == ItemTypeGraph {
			graphCount++
		} else if r.Type == ItemTypeText {
			textCount++
		} else if r.Type == ItemTypeRelationship {
			relationshipCount++
		}
	}

	// Build metadata
	metadata := UnifiedSearchMetadata{
		TotalResults:            len(fusedResults),
		GraphResultCount:        graphCount,
		TextResultCount:         textCount,
		RelationshipResultCount: relationshipCount,
		FusionStrategy:          fusionStrategy,
		ExecutionTime: UnifiedSearchExecutionTime{
			FusionMs: int(fusionElapsed.Milliseconds()),
			TotalMs:  int(time.Since(startTime).Milliseconds()),
		},
	}

	if resultTypes != ResultTypeText {
		graphMs := int(graphRes.elapsed.Milliseconds())
		metadata.ExecutionTime.GraphSearchMs = &graphMs

		relationshipMs := int(relationshipRes.elapsed.Milliseconds())
		metadata.ExecutionTime.RelationshipSearchMs = &relationshipMs
	}
	if resultTypes != ResultTypeGraph {
		textMs := int(textRes.elapsed.Milliseconds())
		metadata.ExecutionTime.TextSearchMs = &textMs
	}
	if relationshipElapsed > 0 {
		relMs := int(relationshipElapsed.Milliseconds())
		metadata.ExecutionTime.RelationshipExpansionMs = &relMs
	}

	// Build debug info if requested
	var debug *UnifiedSearchDebug
	if req.IncludeDebug {
		debug = s.buildDebugInfo(graphRes.rawDebug, textRes.rawDebug, relationshipRes.rawDebug, graphResults, textRes.results, relationshipRes.results, fusionStrategy, req.Weights, len(fusedResults))
	}

	return &UnifiedSearchResponse{
		Results:  fusedResults,
		Metadata: metadata,
		Debug:    debug,
	}, nil
}

// executeGraphSearch runs the graph search using the graph service.
// If queryVector is non-nil, it is used directly; otherwise falls back to embedding the query.
func (s *Service) executeGraphSearch(ctx context.Context, projectID uuid.UUID, req *UnifiedSearchRequest, searchCtx *SearchContext, queryVector []float32) ([]*UnifiedSearchGraphResult, any, error) {
	// Use pre-computed vector if available, otherwise embed independently (standalone call path)
	vector := queryVector
	if len(vector) == 0 && s.embeddings != nil {
		vec, err := s.embeddings.EmbedQuery(ctx, req.Query)
		if err != nil {
			s.log.Warn("failed to generate query embedding for graph search", logger.Error(err))
			// Continue with lexical-only search
		} else {
			vector = vec
		}
	}

	// Build hybrid search request
	hybridReq := &graph.HybridSearchRequest{
		Query:  req.Query,
		Vector: vector,
		Limit:  req.Limit,
	}

	// Execute search (pass nil opts since unified search has its own debug handling)
	searchResp, err := s.graphService.HybridSearch(ctx, projectID, hybridReq, nil)
	if err != nil {
		return nil, nil, err
	}

	// Convert to unified search results
	results := make([]*UnifiedSearchGraphResult, len(searchResp.Data))
	graphObjectIDs := make([]uuid.UUID, 0, len(searchResp.Data))

	for i, item := range searchResp.Data {
		key := ""
		if item.Object.Key != nil {
			key = *item.Object.Key
		}
		results[i] = &UnifiedSearchGraphResult{
			ObjectID:      item.Object.ID.String(),
			CanonicalID:   item.Object.CanonicalID.String(),
			ObjectType:    item.Object.Type,
			Key:           key,
			Fields:        item.Object.Properties,
			Score:         item.Score,
			Rank:          i + 1,
			LexicalScore:  item.LexicalScore,
			VectorScore:   item.VectorScore,
			Relationships: []UnifiedSearchRelationship{},
		}
		graphObjectIDs = append(graphObjectIDs, item.Object.ID)
	}

	// Track access asynchronously (don't block response)
	if len(graphObjectIDs) > 0 {
		go func() {
			bgCtx := context.Background()
			if err := s.graphService.UpdateAccessTimestamps(bgCtx, graphObjectIDs); err != nil {
				s.log.Warn("failed to update access timestamps", logger.Error(err))
			}
		}()
	}

	// Return raw debug items if debug is enabled
	var rawDebug any
	if req.IncludeDebug {
		rawDebug = searchResp.Data
	}

	return results, rawDebug, nil
}

// executeTextSearch runs text search on document chunks.
// If queryVector is non-nil, it is used directly; otherwise falls back to embedding the query.
func (s *Service) executeTextSearch(ctx context.Context, projectID uuid.UUID, req *UnifiedSearchRequest, queryVector []float32) ([]*TextSearchResult, string, any, error) {
	// Use pre-computed vector if available, otherwise embed independently (standalone call path)
	vector := queryVector
	if len(vector) == 0 && s.embeddings != nil {
		vec, err := s.embeddings.EmbedQuery(ctx, req.Query)
		if err != nil {
			s.log.Warn("failed to generate query embedding for text search", logger.Error(err))
			// Continue with lexical-only search
		} else {
			vector = vec
		}
	}

	// Execute hybrid search (or lexical if no embedding)
	params := TextSearchParams{
		ProjectID:     projectID,
		Query:         req.Query,
		Vector:        vector,
		Mode:          TextSearchModeHybrid,
		LexicalWeight: 0.5,
		VectorWeight:  0.5,
		Limit:         req.Limit,
	}

	var resp *TextSearchResponse
	var err error

	if len(vector) > 0 {
		resp, err = s.repo.HybridSearch(ctx, params)
	} else {
		resp, err = s.repo.LexicalSearch(ctx, params)
	}

	if err != nil {
		return nil, "", nil, err
	}

	// Return raw debug info
	var rawDebug any
	if req.IncludeDebug {
		rawDebug = resp.Results
	}

	return resp.Results, string(resp.Mode), rawDebug, nil
}

// executeRelationshipSearch runs relationship vector search.
// If queryVector is non-nil, it is used directly; otherwise falls back to embedding the query.
func (s *Service) executeRelationshipSearch(ctx context.Context, projectID uuid.UUID, req *UnifiedSearchRequest, queryVector []float32) ([]*RelationshipSearchResult, any, error) {
	// Use pre-computed vector if available, otherwise embed independently (standalone call path)
	vector := queryVector
	if len(vector) == 0 && s.embeddings != nil {
		vec, err := s.embeddings.EmbedQuery(ctx, req.Query)
		if err != nil {
			s.log.Warn("failed to generate query embedding for relationship search", logger.Error(err))
			return nil, nil, nil
		}
		vector = vec
	}

	if len(vector) == 0 {
		return nil, nil, nil
	}

	params := RelationshipSearchParams{
		ProjectID: projectID,
		Vector:    vector,
		Limit:     req.Limit,
	}

	resp, err := s.repo.SearchRelationships(ctx, params)
	if err != nil {
		return nil, nil, err
	}

	var rawDebug any
	if req.IncludeDebug {
		rawDebug = resp.Results
	}

	return resp.Results, rawDebug, nil
}

// expandRelationships fetches relationships for graph results
func (s *Service) expandRelationships(ctx context.Context, projectID uuid.UUID, results []*UnifiedSearchGraphResult, options *UnifiedSearchRelationshipOptions) []*UnifiedSearchGraphResult {
	if options == nil || !options.Enabled || options.MaxDepth == 0 {
		return results
	}

	// Collect object IDs
	objectIDs := make([]uuid.UUID, len(results))
	for i, r := range results {
		objectIDs[i] = uuid.MustParse(r.ObjectID)
	}

	// Build relationship map for each object
	relationshipMap := make(map[string][]UnifiedSearchRelationship)

	for _, objID := range objectIDs {
		edgesResp, err := s.graphService.GetEdges(ctx, projectID, objID, graph.GetEdgesParams{})
		if err != nil {
			s.log.Warn("failed to expand relationships", logger.Error(err), slog.String("object_id", objID.String()))
			continue
		}

		var rels []UnifiedSearchRelationship

		// Add incoming relationships
		if options.Direction == "" || options.Direction == "in" || options.Direction == "both" {
			for _, rel := range edgesResp.Incoming {
				rels = append(rels, UnifiedSearchRelationship{
					ObjectID:   rel.SrcID.String(),
					Type:       rel.Type,
					Direction:  "in",
					Properties: rel.Properties,
				})
			}
		}

		// Add outgoing relationships
		if options.Direction == "" || options.Direction == "out" || options.Direction == "both" {
			for _, rel := range edgesResp.Outgoing {
				rels = append(rels, UnifiedSearchRelationship{
					ObjectID:   rel.DstID.String(),
					Type:       rel.Type,
					Direction:  "out",
					Properties: rel.Properties,
				})
			}
		}

		// Apply maxNeighbors limit
		if options.MaxNeighbors > 0 && len(rels) > options.MaxNeighbors {
			rels = rels[:options.MaxNeighbors]
		}

		relationshipMap[objID.String()] = rels
	}

	// Attach relationships to results
	for _, r := range results {
		if rels, ok := relationshipMap[r.ObjectID]; ok {
			r.Relationships = rels
		}
	}

	return results
}

// fuseResults combines graph and text results using the specified strategy
func (s *Service) fuseResults(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, relationshipResults []*RelationshipSearchResult, strategy UnifiedSearchFusionStrategy, weights *UnifiedSearchWeights, limit int) []UnifiedSearchResultItem {
	switch strategy {
	case FusionStrategyWeighted:
		return s.fuseWeighted(graphResults, textResults, relationshipResults, weights, limit)
	case FusionStrategyRRF:
		return s.fuseRRF(graphResults, textResults, relationshipResults, limit)
	case FusionStrategyInterleave:
		return s.fuseInterleave(graphResults, textResults, relationshipResults, limit)
	case FusionStrategyGraphFirst:
		return s.fuseGraphFirst(graphResults, textResults, relationshipResults, limit)
	case FusionStrategyTextFirst:
		return s.fuseTextFirst(graphResults, textResults, relationshipResults, limit)
	default:
		return s.fuseWeighted(graphResults, textResults, relationshipResults, weights, limit)
	}
}

// fuseWeighted combines results using weighted scores
func (s *Service) fuseWeighted(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, relationshipResults []*RelationshipSearchResult, weights *UnifiedSearchWeights, limit int) []UnifiedSearchResultItem {
	graphWeight := float32(0.5)
	textWeight := float32(0.5)
	relationshipWeight := float32(0)

	if weights != nil {
		if weights.GraphWeight > 0 {
			graphWeight = weights.GraphWeight
		}
		if weights.TextWeight > 0 {
			textWeight = weights.TextWeight
		}
		if weights.RelationshipWeight > 0 {
			relationshipWeight = weights.RelationshipWeight
		}
	}

	// Determine normalization mode based on whether RelationshipWeight is explicitly set
	if relationshipWeight > 0 {
		// Three-way normalize: all three weights participate in normalization
		totalWeight := graphWeight + textWeight + relationshipWeight
		if totalWeight > 0 {
			graphWeight /= totalWeight
			textWeight /= totalWeight
			relationshipWeight /= totalWeight
		}
	} else {
		// Backward-compatible: two-way normalize graph+text only
		// Relationships will use the post-normalization graphWeight
		totalWeight := graphWeight + textWeight
		if totalWeight > 0 {
			graphWeight /= totalWeight
			textWeight /= totalWeight
		}
		relationshipWeight = graphWeight // same weight as graph results
	}

	type scoredItem struct {
		item       UnifiedSearchResultItem
		fusedScore float32
	}

	var combined []scoredItem

	for _, g := range graphResults {
		item := s.graphResultToItem(g)
		combined = append(combined, scoredItem{
			item:       item,
			fusedScore: g.Score * graphWeight,
		})
	}

	for _, t := range textResults {
		item := s.textResultToItem(t)
		combined = append(combined, scoredItem{
			item:       item,
			fusedScore: t.Score * textWeight,
		})
	}

	for _, r := range relationshipResults {
		item := s.relationshipResultToItem(r)
		combined = append(combined, scoredItem{
			item:       item,
			fusedScore: r.Score * relationshipWeight,
		})
	}

	sort.Slice(combined, func(i, j int) bool {
		return combined[i].fusedScore > combined[j].fusedScore
	})

	if len(combined) > limit {
		combined = combined[:limit]
	}

	results := make([]UnifiedSearchResultItem, len(combined))
	for i, c := range combined {
		c.item.Score = c.fusedScore
		results[i] = c.item
	}

	return results
}

// fuseRRF combines results using Reciprocal Rank Fusion
func (s *Service) fuseRRF(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, relationshipResults []*RelationshipSearchResult, limit int) []UnifiedSearchResultItem {
	const k = 60

	graphSet := rrfResultSet{results: make([]rrfResult, len(graphResults))}
	for i, g := range graphResults {
		graphSet.results[i] = rrfResult{id: g.ObjectID, rank: i}
	}

	textSet := rrfResultSet{results: make([]rrfResult, len(textResults))}
	for i, t := range textResults {
		textSet.results[i] = rrfResult{id: t.ID.String(), rank: i}
	}

	relationshipSet := rrfResultSet{results: make([]rrfResult, len(relationshipResults))}
	for i, r := range relationshipResults {
		relationshipSet.results[i] = rrfResult{id: r.ID.String(), rank: i}
	}

	merged := reciprocalRankFusion([]rrfResultSet{graphSet, textSet, relationshipSet}, k)

	if len(merged) > limit {
		merged = merged[:limit]
	}

	idToGraph := make(map[string]*UnifiedSearchGraphResult)
	for _, g := range graphResults {
		idToGraph[g.ObjectID] = g
	}

	idToText := make(map[string]*TextSearchResult)
	for _, t := range textResults {
		idToText[t.ID.String()] = t
	}

	idToRelationship := make(map[string]*RelationshipSearchResult)
	for _, r := range relationshipResults {
		idToRelationship[r.ID.String()] = r
	}

	results := make([]UnifiedSearchResultItem, len(merged))
	for i, scoredID := range merged {
		if g, ok := idToGraph[scoredID.id]; ok {
			item := s.graphResultToItem(g)
			item.Score = scoredID.score
			results[i] = item
		} else if t, ok := idToText[scoredID.id]; ok {
			item := s.textResultToItem(t)
			item.Score = scoredID.score
			results[i] = item
		} else if r, ok := idToRelationship[scoredID.id]; ok {
			item := s.relationshipResultToItem(r)
			item.Score = scoredID.score
			results[i] = item
		}
	}

	return results
}

// fuseInterleave alternates between graph, text, and relationship results (three-way round-robin).
// When one source is exhausted, continues alternating between the remaining sources.
func (s *Service) fuseInterleave(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, relationshipResults []*RelationshipSearchResult, limit int) []UnifiedSearchResultItem {
	var results []UnifiedSearchResultItem

	graphIdx := 0
	textIdx := 0
	relIdx := 0

	for len(results) < limit {
		added := false

		// Add graph result
		if graphIdx < len(graphResults) {
			results = append(results, s.graphResultToItem(graphResults[graphIdx]))
			graphIdx++
			added = true
			if len(results) >= limit {
				break
			}
		}

		// Add text result
		if textIdx < len(textResults) {
			results = append(results, s.textResultToItem(textResults[textIdx]))
			textIdx++
			added = true
			if len(results) >= limit {
				break
			}
		}

		// Add relationship result
		if relIdx < len(relationshipResults) {
			results = append(results, s.relationshipResultToItem(relationshipResults[relIdx]))
			relIdx++
			added = true
			if len(results) >= limit {
				break
			}
		}

		// Stop if all sources exhausted
		if !added {
			break
		}
	}

	return results
}

// fuseGraphFirst shows all graph results, then relationship results, then text results
func (s *Service) fuseGraphFirst(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, relationshipResults []*RelationshipSearchResult, limit int) []UnifiedSearchResultItem {
	var results []UnifiedSearchResultItem

	for _, g := range graphResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.graphResultToItem(g))
	}

	for _, r := range relationshipResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.relationshipResultToItem(r))
	}

	for _, t := range textResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.textResultToItem(t))
	}

	return results
}

// fuseTextFirst shows all text results, then relationship results, then graph results
func (s *Service) fuseTextFirst(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, relationshipResults []*RelationshipSearchResult, limit int) []UnifiedSearchResultItem {
	var results []UnifiedSearchResultItem

	for _, t := range textResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.textResultToItem(t))
	}

	for _, r := range relationshipResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.relationshipResultToItem(r))
	}

	for _, g := range graphResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.graphResultToItem(g))
	}

	return results
}

// graphResultToItem converts a graph result to a unified search result item
func (s *Service) graphResultToItem(g *UnifiedSearchGraphResult) UnifiedSearchResultItem {
	return UnifiedSearchResultItem{
		Type:          ItemTypeGraph,
		ID:            g.ObjectID,
		Score:         g.Score,
		ObjectID:      g.ObjectID,
		CanonicalID:   g.CanonicalID,
		Rank:          g.Rank,
		ObjectType:    g.ObjectType,
		Key:           g.Key,
		Fields:        g.Fields,
		Relationships: g.Relationships,
		Explanation:   g.Explanation,
	}
}

// textResultToItem converts a text result to a unified search result item
func (s *Service) textResultToItem(t *TextSearchResult) UnifiedSearchResultItem {
	docID := t.DocumentID.String()
	return UnifiedSearchResultItem{
		Type:       ItemTypeText,
		ID:         t.ID.String(),
		Score:      t.Score,
		Snippet:    t.Text,
		Source:     t.Source,
		Mode:       t.Mode,
		DocumentID: &docID,
	}
}

// relationshipResultToItem converts a relationship result to a unified search result item
func (s *Service) relationshipResultToItem(r *RelationshipSearchResult) UnifiedSearchResultItem {
	return UnifiedSearchResultItem{
		Type:             ItemTypeRelationship,
		ID:               r.ID.String(),
		Score:            r.Score,
		RelationshipType: r.Type,
		TripletText:      r.TripletText,
		SourceID:         r.SrcID.String(),
		TargetID:         r.DstID.String(),
		Properties:       r.Properties,
	}
}

// buildDebugInfo creates debug information for the search response
func (s *Service) buildDebugInfo(graphDebug, textDebug, relationshipDebug any, graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, relationshipResults []*RelationshipSearchResult, strategy UnifiedSearchFusionStrategy, weights *UnifiedSearchWeights, postFusionCount int) *UnifiedSearchDebug {
	var scoreDistribution *UnifiedSearchScoreDistribution

	if len(graphResults) > 0 || len(textResults) > 0 || len(relationshipResults) > 0 {
		scoreDistribution = &UnifiedSearchScoreDistribution{}

		if len(graphResults) > 0 {
			graphScores := make([]float32, len(graphResults))
			for i, g := range graphResults {
				graphScores[i] = g.Score
			}
			min, max, mean := calcScoreStats(graphScores)
			scoreDistribution.Graph = &ScoreStats{Min: min, Max: max, Mean: mean}
		}

		if len(textResults) > 0 {
			textScores := make([]float32, len(textResults))
			for i, t := range textResults {
				textScores[i] = t.Score
			}
			min, max, mean := calcScoreStats(textScores)
			scoreDistribution.Text = &ScoreStats{Min: min, Max: max, Mean: mean}
		}

		if len(relationshipResults) > 0 {
			relationshipScores := make([]float32, len(relationshipResults))
			for i, r := range relationshipResults {
				relationshipScores[i] = r.Score
			}
			min, max, mean := calcScoreStats(relationshipScores)
			scoreDistribution.Relationship = &ScoreStats{Min: min, Max: max, Mean: mean}
		}
	}

	fusionDetails := &UnifiedSearchFusionDetails{
		Strategy:        strategy,
		Weights:         weights,
		PostFusionCount: postFusionCount,
		PreFusionCounts: &PreFusionCounts{
			Graph:        len(graphResults),
			Text:         len(textResults),
			Relationship: len(relationshipResults),
		},
	}

	return &UnifiedSearchDebug{
		GraphSearch:       graphDebug,
		TextSearch:        textDebug,
		ScoreDistribution: scoreDistribution,
		FusionDetails:     fusionDetails,
	}
}

// calcScoreStats calculates min, max, mean for a slice of scores
func calcScoreStats(scores []float32) (min, max, mean float32) {
	if len(scores) == 0 {
		return 0, 0, 0
	}

	min = scores[0]
	max = scores[0]
	var sum float32

	for _, s := range scores {
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
		sum += s
	}

	mean = sum / float32(len(scores))
	return min, max, mean
}

// rrfResultSet represents a ranked result set for RRF merging.
// Each result is identified by a unique ID and has an original rank (0-based).
type rrfResultSet struct {
	results []rrfResult
}

type rrfResult struct {
	id   string
	rank int
}

// reciprocalRankFusion merges 2 or more ranked result sets using RRF algorithm.
// Returns merged IDs with their RRF scores in descending score order.
// k=60 is the standard RRF constant (balances rank importance).
func reciprocalRankFusion(resultSets []rrfResultSet, k int) []rrfScoredID {
	if k <= 0 {
		k = 60
	}

	scoreMap := make(map[string]float32)

	for _, set := range resultSets {
		for _, result := range set.results {
			rrfScore := float32(1.0) / float32(k+result.rank+1)
			scoreMap[result.id] += rrfScore
		}
	}

	type entry struct {
		id    string
		score float32
	}
	var entries []entry
	for id, score := range scoreMap {
		entries = append(entries, entry{id: id, score: score})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	merged := make([]rrfScoredID, len(entries))
	for i, e := range entries {
		merged[i] = rrfScoredID{id: e.id, score: e.score}
	}

	return merged
}

type rrfScoredID struct {
	id    string
	score float32
}
