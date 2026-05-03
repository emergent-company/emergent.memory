package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/search"
)

// getTypesForNamespace returns the set of type names allowed by the namespace filter.
// Returns nil when namespace is "all" (no filtering needed).
// When namespace is "" (default), returns types where namespace IS NULL.
// When namespace is a specific value, returns types with that namespace.
func (s *Service) getTypesForNamespace(ctx context.Context, projectID string, namespaceFilter string) (map[string]bool, error) {
	if namespaceFilter == "all" {
		return nil, nil
	}

	type row struct {
		TypeName string `bun:"type_name"`
	}
	var rows []row

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	if namespaceFilter == "" {
		_, err = s.db.NewRaw(`
			SELECT type_name FROM kb.project_object_schema_registry
			WHERE project_id = ? AND namespace IS NULL AND enabled = true
		`, projectUUID).Exec(ctx, &rows)
	} else {
		_, err = s.db.NewRaw(`
			SELECT type_name FROM kb.project_object_schema_registry
			WHERE project_id = ? AND namespace = ? AND enabled = true
		`, projectUUID, namespaceFilter).Exec(ctx, &rows)
	}
	if err != nil {
		return nil, fmt.Errorf("namespace type lookup: %w", err)
	}

	if len(rows) == 0 {
		// No schema types registered for this namespace filter — treat as unfiltered.
		return nil, nil
	}
	allowed := make(map[string]bool, len(rows))
	for _, r := range rows {
		allowed[r.TypeName] = true
	}
	return allowed, nil
}

// executeHybridSearch performs hybrid search (FTS + vector + graph context + relationship embeddings)
func (s *Service) executeHybridSearch(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	var types []string
	if t, ok := args["types"].([]any); ok {
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}

	var labels []string
	if l, ok := args["labels"].([]any); ok {
		for _, v := range l {
			if str, ok := v.(string); ok {
				labels = append(labels, str)
			}
		}
	}

	namespaceFilter, _ := args["namespace"].(string)
	allowedTypes, err := s.getTypesForNamespace(ctx, projectID, namespaceFilter)
	if err != nil {
		return nil, err
	}

	if s.searchSvc != nil {
		unifiedReq := &search.UnifiedSearchRequest{
			Query: query,
			Limit: limit,
		}

		if rb, ok := args["recency_boost"].(float64); ok && rb > 0 {
			v := float32(rb)
			unifiedReq.RecencyBoost = &v
		}
		if rhl, ok := args["recency_half_life"].(float64); ok && rhl > 0 {
			v := float32(rhl)
			unifiedReq.RecencyHalfLife = &v
		}
		if ab, ok := args["access_boost"].(float64); ok && ab > 0 {
			v := float32(ab)
			unifiedReq.AccessBoost = &v
		}

		res, err := s.searchSvc.Search(ctx, projectUUID, unifiedReq, nil)
		if err != nil {
			s.log.WarnContext(ctx, "unified search failed, falling back to graph search",
				"error", err,
				"project_id", projectID,
			)
		} else {
			return s.wrapResult(s.mapUnifiedToSearchResponse(res, types, labels, allowedTypes))
		}
	}

	req := &graph.HybridSearchRequest{
		Query:  query,
		Types:  types,
		Labels: labels,
		Limit:  limit,
	}

	if rb, ok := args["recency_boost"].(float64); ok && rb > 0 {
		v := float32(rb)
		req.RecencyBoost = &v
	}
	if rhl, ok := args["recency_half_life"].(float64); ok && rhl > 0 {
		v := float32(rhl)
		req.RecencyHalfLife = &v
	}
	if ab, ok := args["access_boost"].(float64); ok && ab > 0 {
		v := float32(ab)
		req.AccessBoost = &v
	}

	results, err := s.graphService.HybridSearch(ctx, projectUUID, req, nil)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
	}

	// Post-hoc namespace filter for fallback path
	if allowedTypes != nil {
		filtered := results.Data[:0]
		for _, item := range results.Data {
			if item.Object != nil && allowedTypes[item.Object.Type] {
				filtered = append(filtered, item)
			}
		}
		results.Data = filtered
		results.Total = len(filtered)
	}

	return s.wrapResult(results)
}

// mapUnifiedToSearchResponse converts unified search results back to graph.SearchResponse
// for backward compatibility with existing MCP consumers. Relationship results are included
// as additional items with a synthetic object wrapper.
func (s *Service) mapUnifiedToSearchResponse(res *search.UnifiedSearchResponse, types, labels []string, allowedTypes map[string]bool) *graph.SearchResponse {
	var items []*graph.SearchResultItem

	for _, r := range res.Results {
		switch r.Type {
		case search.ItemTypeGraph:
			if len(types) > 0 && !containsStr(types, r.ObjectType) {
				continue
			}
			if allowedTypes != nil && !allowedTypes[r.ObjectType] {
				continue
			}

			canonicalID, _ := uuid.Parse(r.CanonicalID)
			objectID, _ := uuid.Parse(r.ObjectID)

			var key *string
			if r.Key != "" {
				k := r.Key
				key = &k
			}

			items = append(items, &graph.SearchResultItem{
				Object: &graph.GraphObjectResponse{
					ID:          objectID,
					CanonicalID: canonicalID,
					Type:        r.ObjectType,
					Key:         key,
					Properties:  r.Fields,
				},
				Score: r.Score,
			})

		case search.ItemTypeText:
			textID, _ := uuid.Parse(r.ID)
			textType := "text_chunk"

			props := map[string]any{
				"snippet": r.Snippet,
			}
			if r.Source != nil {
				props["source"] = *r.Source
			}
			if r.DocumentID != nil {
				props["document_id"] = *r.DocumentID
			}

			items = append(items, &graph.SearchResultItem{
				Object: &graph.GraphObjectResponse{
					ID:         textID,
					Type:       textType,
					Properties: props,
				},
				Score: r.Score,
			})
		}
	}

	if len(labels) > 0 {
		filtered := make([]*graph.SearchResultItem, 0, len(items))
		for _, item := range items {
			if item.Object.Type == "text_chunk" {
				filtered = append(filtered, item)
				continue
			}
			for _, objLabel := range item.Object.Labels {
				if containsStr(labels, objLabel) {
					filtered = append(filtered, item)
					break
				}
			}
		}
		items = filtered
	}

	return &graph.SearchResponse{
		Data:    items,
		Total:   len(items),
		HasMore: false,
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// executeSemanticSearch performs vector-based semantic search.
// Routes through search.Service so the query text is automatically embedded
// before the vector search leg runs. This ensures multi-word queries produce
// results even when FTS alone would return nothing.
func (s *Service) executeSemanticSearch(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	var types []string
	if t, ok := args["types"].([]any); ok {
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}

	namespaceFilter, _ := args["namespace"].(string)
	allowedTypes, err := s.getTypesForNamespace(ctx, projectID, namespaceFilter)
	if err != nil {
		return nil, err
	}

	// Prefer search.Service which auto-embeds the query text, giving the vector
	// leg full signal even for multi-word queries that FTS can't match well.
	if s.searchSvc != nil {
		unifiedReq := &search.UnifiedSearchRequest{
			Query: query,
			Limit: limit,
		}
		res, err := s.searchSvc.Search(ctx, projectUUID, unifiedReq, nil)
		if err != nil {
			s.log.WarnContext(ctx, "unified search failed in semantic_search, falling back",
				"error", err, "project_id", projectID)
		} else {
			return s.wrapResult(s.mapUnifiedToSearchResponse(res, types, nil, allowedTypes))
		}
	}

	// Fallback: hybrid search with heavy vector weight (no auto-embedding).
	req := &graph.HybridSearchRequest{
		Query:         query,
		Types:         types,
		Limit:         limit,
		LexicalWeight: float32Ptr(0.2),
		VectorWeight:  float32Ptr(0.8),
	}
	results, err := s.graphService.HybridSearch(ctx, projectUUID, req, nil)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}

	// Post-hoc namespace filter for fallback path
	if allowedTypes != nil {
		filtered := results.Data[:0]
		for _, item := range results.Data {
			if item.Object != nil && allowedTypes[item.Object.Type] {
				filtered = append(filtered, item)
			}
		}
		results.Data = filtered
		results.Total = len(filtered)
	}

	return s.wrapResult(results)
}

// executeFindSimilar finds entities similar to a given entity
func (s *Service) executeFindSimilar(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	entityIDStr, ok := args["entity_id"].(string)
	if !ok || entityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: entity_id")
	}

	entityID, err := uuid.Parse(entityIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid entity_id: %w", err)
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	var types []string
	if t, ok := args["types"].([]any); ok {
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}

	var typeFilter *string
	if len(types) > 0 {
		typeFilter = &types[0]
	}

	req := &graph.SimilarObjectsRequest{
		Type:  typeFilter,
		Limit: limit,
	}

	results, err := s.graphService.FindSimilarObjects(ctx, projectUUID, entityID, req)
	if err != nil {
		return nil, fmt.Errorf("find similar: %w", err)
	}

	return s.wrapResult(map[string]any{
		"similar_entities": results,
		"total":            len(results),
	})
}

// executeTraverseGraph performs multi-hop graph traversal
func (s *Service) executeTraverseGraph(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	startEntityIDStr, ok := args["start_entity_id"].(string)
	if !ok || startEntityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: start_entity_id")
	}

	startEntityID, err := uuid.Parse(startEntityIDStr)
	if err != nil {
		// Not a UUID — try resolving as an entity key.
		resolved, keyErr := s.resolveEntityIDByKey(ctx, projectUUID, startEntityIDStr)
		if keyErr != nil {
			return nil, fmt.Errorf("invalid start_entity_id: %q is not a UUID and key lookup failed: %w", startEntityIDStr, keyErr)
		}
		startEntityID = resolved
	}

	// Resolve the canonical ID for relationship traversal
	canonicalID, err := s.resolveCanonicalID(ctx, projectUUID, startEntityID)
	if err == nil && canonicalID != uuid.Nil {
		startEntityID = canonicalID
	}

	maxDepth := 2
	if d, ok := args["max_depth"].(float64); ok {
		maxDepth = int(d)
	}
	if maxDepth < 1 {
		maxDepth = 1
	}
	if maxDepth > 5 {
		maxDepth = 5
	}

	direction := "both"
	if d, ok := args["direction"].(string); ok {
		// Normalize human-readable aliases to the values expected by ExpandGraph.
		switch d {
		case "outgoing":
			direction = "out"
		case "incoming":
			direction = "in"
		default:
			direction = d
		}
	}

	var relationshipTypes []string
	if rt, ok := args["relationship_types"].([]any); ok {
		for _, v := range rt {
			if str, ok := v.(string); ok {
				relationshipTypes = append(relationshipTypes, str)
			}
		}
	}

	queryContext := ""
	if qc, ok := args["query_context"].(string); ok {
		queryContext = qc
	}

	req := &graph.TraverseGraphRequest{
		RootIDs:           []uuid.UUID{startEntityID},
		MaxDepth:          maxDepth,
		Direction:         direction,
		RelationshipTypes: relationshipTypes,
		QueryContext:      queryContext,
	}

	results, err := s.graphService.TraverseGraph(ctx, projectUUID, req)
	if err != nil {
		return nil, fmt.Errorf("traverse graph: %w", err)
	}

	return s.wrapResult(results)
}

// executeListRelationships lists relationships with optional filters
func (s *Service) executeListRelationships(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	params := graph.RelationshipListParams{
		ProjectID: projectUUID,
		Limit:     limit + 1, // Fetch one extra to check hasMore
	}

	if t, ok := args["type"].(string); ok && t != "" {
		params.Type = &t
	}

	if srcID, ok := args["source_id"].(string); ok && srcID != "" {
		id, err := uuid.Parse(srcID)
		if err != nil {
			return nil, fmt.Errorf("invalid source_id: %w", err)
		}
		params.SrcID = &id
	}

	if dstID, ok := args["target_id"].(string); ok && dstID != "" {
		id, err := uuid.Parse(dstID)
		if err != nil {
			return nil, fmt.Errorf("invalid target_id: %w", err)
		}
		params.DstID = &id
	}

	results, err := s.graphService.ListRelationships(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list relationships: %w", err)
	}

	return s.wrapResult(results)
}

// executeUpdateRelationship updates a relationship
func (s *Service) executeUpdateRelationship(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	relIDStr, ok := args["relationship_id"].(string)
	if !ok || relIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: relationship_id")
	}

	relID, err := uuid.Parse(relIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid relationship_id: %w", err)
	}

	req := &graph.PatchGraphRelationshipRequest{}

	if props, ok := args["properties"].(map[string]any); ok {
		req.Properties = props
	}

	if weight, ok := args["weight"].(float64); ok {
		w := float32(weight)
		req.Weight = &w
	}

	result, err := s.graphService.PatchRelationship(ctx, projectUUID, relID, req)
	if err != nil {
		return nil, fmt.Errorf("update relationship: %w", err)
	}

	return s.wrapResult(map[string]any{
		"success":      true,
		"relationship": result,
		"message":      "Relationship updated successfully",
	})
}

// executeDeleteRelationship soft-deletes a relationship
func (s *Service) executeDeleteRelationship(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	relIDStr, ok := args["relationship_id"].(string)
	if !ok || relIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: relationship_id")
	}

	relID, err := uuid.Parse(relIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid relationship_id: %w", err)
	}

	result, err := s.graphService.DeleteRelationship(ctx, projectUUID, relID, nil)
	if err != nil {
		return nil, fmt.Errorf("delete relationship: %w", err)
	}

	return s.wrapResult(map[string]any{
		"success":      true,
		"relationship": result,
		"message":      "Relationship deleted successfully",
	})
}

// executeListTags gets all unique tags in the project
func (s *Service) executeListTags(ctx context.Context, projectID string) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	tags, err := s.graphService.GetTags(ctx, projectUUID, nil)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	return s.wrapResult(map[string]any{
		"tags":  tags,
		"total": len(tags),
	})
}

func float32Ptr(f float32) *float32 {
	return &f
}
