package search

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/emergent/emergent-core/domain/graph"
	"github.com/emergent/emergent-core/pkg/embeddings"
	"github.com/emergent/emergent-core/pkg/logger"
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

	graphCh := make(chan graphResult, 1)
	textCh := make(chan textResult, 1)

	// Execute graph search
	go func() {
		if resultTypes == ResultTypeText {
			graphCh <- graphResult{results: nil, elapsed: 0}
			return
		}
		start := time.Now()
		results, rawDebug, err := s.executeGraphSearch(ctx, projectID, req, searchCtx)
		graphCh <- graphResult{results: results, elapsed: time.Since(start), rawDebug: rawDebug, err: err}
	}()

	// Execute text search
	go func() {
		if resultTypes == ResultTypeGraph {
			textCh <- textResult{results: nil, elapsed: 0}
			return
		}
		start := time.Now()
		results, mode, rawDebug, err := s.executeTextSearch(ctx, projectID, req)
		textCh <- textResult{results: results, mode: mode, elapsed: time.Since(start), rawDebug: rawDebug, err: err}
	}()

	// Wait for both searches
	graphRes := <-graphCh
	textRes := <-textCh

	if graphRes.err != nil {
		return nil, graphRes.err
	}
	if textRes.err != nil {
		return nil, textRes.err
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
	fusedResults := s.fuseResults(graphResults, textRes.results, fusionStrategy, req.Weights, limit)
	fusionElapsed := time.Since(fusionStart)

	// Count result types
	graphCount := 0
	textCount := 0
	for _, r := range fusedResults {
		if r.Type == ItemTypeGraph {
			graphCount++
		} else {
			textCount++
		}
	}

	// Build metadata
	metadata := UnifiedSearchMetadata{
		TotalResults:     len(fusedResults),
		GraphResultCount: graphCount,
		TextResultCount:  textCount,
		FusionStrategy:   fusionStrategy,
		ExecutionTime: UnifiedSearchExecutionTime{
			FusionMs: int(fusionElapsed.Milliseconds()),
			TotalMs:  int(time.Since(startTime).Milliseconds()),
		},
	}

	if resultTypes != ResultTypeText {
		graphMs := int(graphRes.elapsed.Milliseconds())
		metadata.ExecutionTime.GraphSearchMs = &graphMs
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
		debug = s.buildDebugInfo(graphRes.rawDebug, textRes.rawDebug, graphResults, textRes.results, fusionStrategy, req.Weights, len(fusedResults))
	}

	return &UnifiedSearchResponse{
		Results:  fusedResults,
		Metadata: metadata,
		Debug:    debug,
	}, nil
}

// executeGraphSearch runs the graph search using the graph service
func (s *Service) executeGraphSearch(ctx context.Context, projectID uuid.UUID, req *UnifiedSearchRequest, searchCtx *SearchContext) ([]*UnifiedSearchGraphResult, any, error) {
	// Get query embedding for hybrid search
	var vector []float32
	if s.embeddings != nil {
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

// executeTextSearch runs text search on document chunks
func (s *Service) executeTextSearch(ctx context.Context, projectID uuid.UUID, req *UnifiedSearchRequest) ([]*TextSearchResult, string, any, error) {
	// Get query embedding for hybrid search
	var vector []float32
	if s.embeddings != nil {
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
		edgesResp, err := s.graphService.GetEdges(ctx, projectID, objID)
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
func (s *Service) fuseResults(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, strategy UnifiedSearchFusionStrategy, weights *UnifiedSearchWeights, limit int) []UnifiedSearchResultItem {
	switch strategy {
	case FusionStrategyWeighted:
		return s.fuseWeighted(graphResults, textResults, weights, limit)
	case FusionStrategyRRF:
		return s.fuseRRF(graphResults, textResults, limit)
	case FusionStrategyInterleave:
		return s.fuseInterleave(graphResults, textResults, limit)
	case FusionStrategyGraphFirst:
		return s.fuseGraphFirst(graphResults, textResults, limit)
	case FusionStrategyTextFirst:
		return s.fuseTextFirst(graphResults, textResults, limit)
	default:
		return s.fuseWeighted(graphResults, textResults, weights, limit)
	}
}

// fuseWeighted combines results using weighted scores
func (s *Service) fuseWeighted(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, weights *UnifiedSearchWeights, limit int) []UnifiedSearchResultItem {
	graphWeight := float32(0.5)
	textWeight := float32(0.5)
	if weights != nil {
		if weights.GraphWeight > 0 {
			graphWeight = weights.GraphWeight
		}
		if weights.TextWeight > 0 {
			textWeight = weights.TextWeight
		}
	}

	// Normalize weights
	totalWeight := graphWeight + textWeight
	if totalWeight > 0 {
		graphWeight /= totalWeight
		textWeight /= totalWeight
	}

	// Create scored items
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

	// Sort by fused score descending
	sort.Slice(combined, func(i, j int) bool {
		return combined[i].fusedScore > combined[j].fusedScore
	})

	// Apply limit
	if len(combined) > limit {
		combined = combined[:limit]
	}

	// Extract items with updated scores
	results := make([]UnifiedSearchResultItem, len(combined))
	for i, c := range combined {
		c.item.Score = c.fusedScore
		results[i] = c.item
	}

	return results
}

// fuseRRF combines results using Reciprocal Rank Fusion
func (s *Service) fuseRRF(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, limit int) []UnifiedSearchResultItem {
	const k = 60 // Standard RRF constant

	scoreMap := make(map[string]struct {
		item  UnifiedSearchResultItem
		score float32
	})

	// Add graph results with RRF score
	for i, g := range graphResults {
		rrfScore := float32(1.0) / float32(k+i+1)
		item := s.graphResultToItem(g)
		scoreMap[g.ObjectID] = struct {
			item  UnifiedSearchResultItem
			score float32
		}{item: item, score: rrfScore}
	}

	// Add text results with RRF score (combine if overlapping)
	for i, t := range textResults {
		rrfScore := float32(1.0) / float32(k+i+1)
		id := t.ID.String()

		if existing, ok := scoreMap[id]; ok {
			// Item appears in both - boost score
			existing.score += rrfScore
			scoreMap[id] = existing
		} else {
			item := s.textResultToItem(t)
			scoreMap[id] = struct {
				item  UnifiedSearchResultItem
				score float32
			}{item: item, score: rrfScore}
		}
	}

	// Convert to slice and sort
	type entry struct {
		id    string
		item  UnifiedSearchResultItem
		score float32
	}
	var entries []entry
	for id, v := range scoreMap {
		entries = append(entries, entry{id: id, item: v.item, score: v.score})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	// Apply limit and update scores
	if len(entries) > limit {
		entries = entries[:limit]
	}

	results := make([]UnifiedSearchResultItem, len(entries))
	for i, e := range entries {
		e.item.Score = e.score
		results[i] = e.item
	}

	return results
}

// fuseInterleave alternates between graph and text results
func (s *Service) fuseInterleave(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, limit int) []UnifiedSearchResultItem {
	var results []UnifiedSearchResultItem

	graphIdx := 0
	textIdx := 0

	for len(results) < limit {
		// Add graph result
		if graphIdx < len(graphResults) {
			results = append(results, s.graphResultToItem(graphResults[graphIdx]))
			graphIdx++
		}
		if len(results) >= limit {
			break
		}

		// Add text result
		if textIdx < len(textResults) {
			results = append(results, s.textResultToItem(textResults[textIdx]))
			textIdx++
		}
		if len(results) >= limit {
			break
		}

		// Stop if both exhausted
		if graphIdx >= len(graphResults) && textIdx >= len(textResults) {
			break
		}
	}

	return results
}

// fuseGraphFirst shows all graph results, then text results
func (s *Service) fuseGraphFirst(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, limit int) []UnifiedSearchResultItem {
	var results []UnifiedSearchResultItem

	for _, g := range graphResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.graphResultToItem(g))
	}

	for _, t := range textResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.textResultToItem(t))
	}

	return results
}

// fuseTextFirst shows all text results, then graph results
func (s *Service) fuseTextFirst(graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, limit int) []UnifiedSearchResultItem {
	var results []UnifiedSearchResultItem

	for _, t := range textResults {
		if len(results) >= limit {
			break
		}
		results = append(results, s.textResultToItem(t))
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

// buildDebugInfo creates debug information for the search response
func (s *Service) buildDebugInfo(graphDebug, textDebug any, graphResults []*UnifiedSearchGraphResult, textResults []*TextSearchResult, strategy UnifiedSearchFusionStrategy, weights *UnifiedSearchWeights, postFusionCount int) *UnifiedSearchDebug {
	// Calculate score distribution
	var scoreDistribution *UnifiedSearchScoreDistribution

	if len(graphResults) > 0 || len(textResults) > 0 {
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
	}

	// Build fusion details
	fusionDetails := &UnifiedSearchFusionDetails{
		Strategy:        strategy,
		Weights:         weights,
		PostFusionCount: postFusionCount,
		PreFusionCounts: &PreFusionCounts{
			Graph: len(graphResults),
			Text:  len(textResults),
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
