package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
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

// getSystemNamespaceTypes returns the set of type names that have any non-null namespace
// set in the schema registry. These types should be excluded from default (no-namespace)
// searches regardless of whether any non-system types are registered.
// Returns nil (no blocklist) when namespace filter is "all" or a specific namespace is requested.
func (s *Service) getSystemNamespaceTypes(ctx context.Context, projectID string, namespaceFilter string) (map[string]bool, error) {
	// Only apply blocklist for the default (no namespace) case.
	if namespaceFilter != "" {
		return nil, nil
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	type row struct {
		TypeName string `bun:"type_name"`
	}
	var rows []row
	_, err = s.db.NewRaw(`
		SELECT type_name FROM kb.project_object_schema_registry
		WHERE project_id = ? AND namespace IS NOT NULL AND enabled = true
	`, projectUUID).Exec(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("system namespace type lookup: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}
	blocked := make(map[string]bool, len(rows))
	for _, r := range rows {
		blocked[r.TypeName] = true
	}
	return blocked, nil
}

// parseMinScoreArg parses the optional "min_score" tool argument (0-1).
// Returns nil when the argument is absent (no cutoff).
func parseMinScoreArg(args map[string]any) (*float32, error) {
	raw, ok := args["min_score"]
	if !ok {
		return nil, nil
	}
	f, ok := raw.(float64)
	if !ok {
		return nil, fmt.Errorf("invalid min_score: must be a number between 0 and 1")
	}
	if f < 0 || f > 1 {
		return nil, fmt.Errorf("invalid min_score: must be between 0 and 1")
	}
	v := float32(f)
	return &v, nil
}

// buildHybridUnifiedRequest assembles the unified-search request from the
// search-hybrid args. Isolated as a pure function so the arg→request mapping
// can be unit-tested without a database.
func buildHybridUnifiedRequest(args map[string]any, query string, limit int, types, labels []string, minScore *float32, namespaceFilter string, includeChunks bool) *search.UnifiedSearchRequest {
	// Relationship (triple) candidates are never surfaced to MCP consumers:
	// mapUnifiedToSearchResponse only maps graph/text items, so running the
	// relationship-vector leg is pure waste. Force-skip it unconditionally.
	includeRelationships := false

	// Chunks are opt-in for MCP search tools: by default the request is
	// graph-only so the text leg (document chunks) never runs. include_chunks
	// re-enables both legs; chunk items are then still subject to the
	// types/allow/block/scope filters applied in mapUnifiedToSearchResponse.
	resultTypes := search.ResultTypeGraph
	if includeChunks {
		resultTypes = search.ResultTypeBoth
	}

	req := &search.UnifiedSearchRequest{
		Query:                query,
		Limit:                limit,
		ResultTypes:          resultTypes,
		Types:                types,
		Labels:               labels,
		MinScore:             minScore,
		IncludeRelationships: &includeRelationships,
	}

	// Pass namespace to unified search for DB-level filtering.
	// "all" means no filter; "" is the default (null) namespace, which the
	// schema-type allow/block lists already treat as `namespace IS NULL`, so the
	// chunk leg is scoped to null-namespace objects too rather than leaking
	// chunks extracted into non-null-namespace objects (#1910). A specific value
	// filters graph objects by their namespace field.
	switch namespaceFilter {
	case "all":
		// unfiltered
	case "":
		req.NamespaceIsNull = true
	default:
		ns := namespaceFilter
		req.Namespace = &ns
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

	return req
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

	opts := responseOptsFromArgs(args)
	if opts.FieldStrategy == "" {
		opts.FieldStrategy = "compact"
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

	// Bound the returned properties payload under the full field strategy, the
	// same way entity-query does, so a full-properties search cannot pull a huge
	// payload in one page. Only lower, never raise, the caller's limit.
	if opts.FieldStrategy == "full" {
		if max := s.effectiveEntityQueryFullMaxLimit(); limit > max {
			limit = max
		}
	}

	// Optional fused-score cutoff (0-1). nil = no cutoff, weak matches returned ranked.
	minScore, err := parseMinScoreArg(args)
	if err != nil {
		return nil, err
	}

	var types []string
	if t, ok := args["types"].([]any); ok {
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}

	scope := TypeScopeFromContext(ctx)
	var denied bool
	types, denied = scope.FilterRequestedObjectTypes(types)
	if denied {
		return envelopeSearchResponse(&graph.SearchResponse{}, opts)
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

	// Always block system-namespaced types from default searches, even when no
	// non-system types are registered (allowedTypes == nil).
	blockedTypes, err := s.getSystemNamespaceTypes(ctx, projectID, namespaceFilter)
	if err != nil {
		return nil, err
	}

	if s.searchSvc != nil {
		includeChunks, _ := args["include_chunks"].(bool)
		unifiedReq := buildHybridUnifiedRequest(args, query, limit, types, labels, minScore, namespaceFilter, includeChunks)

		res, err := s.searchSvc.Search(ctx, projectUUID, unifiedReq, nil)
		if err != nil {
			s.log.WarnContext(ctx, "unified search failed, falling back to graph search",
				"error", err,
				"project_id", projectID,
			)
		} else {
			mapped := s.mapUnifiedToSearchResponse(res, types, labels, allowedTypes, blockedTypes)
			mapped = s.normaliseSearchItemDates(ctx, projectUUID, mapped)
			mapped.Data = filterSearchItemsByScope(scope, mapped.Data)
			mapped.Total = len(mapped.Data)
			return envelopeSearchResponse(mapped, opts)
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

	// Post-hoc namespace filter for fallback path: apply both allowlist and blocklist.
	filtered := results.Data[:0]
	for _, item := range results.Data {
		if item.Object == nil {
			continue
		}
		if allowedTypes != nil && !allowedTypes[item.Object.Type] {
			continue
		}
		if blockedTypes != nil && blockedTypes[item.Object.Type] {
			continue
		}
		filtered = append(filtered, item)
	}

	// Optional fused-score cutoff on the fallback graph path (the unified path
	// applies the cutoff inside fusion). nil/0 keeps baseline behavior.
	if minScore != nil && *minScore > 0 {
		kept := filtered[:0]
		for _, item := range filtered {
			if item.Score >= *minScore {
				kept = append(kept, item)
			}
		}
		filtered = kept
	}
	results.Data = filterSearchItemsByScope(scope, filtered)
	results.Total = len(results.Data)

	return envelopeSearchResponse(results, opts)
}

// mapUnifiedToSearchResponse converts unified search results back to graph.SearchResponse
// for backward compatibility with existing MCP consumers. Relationship results are included
// as additional items with a synthetic object wrapper.
func (s *Service) mapUnifiedToSearchResponse(res *search.UnifiedSearchResponse, types, labels []string, allowedTypes map[string]bool, blockedTypes map[string]bool) *graph.SearchResponse {
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
			if blockedTypes != nil && blockedTypes[r.ObjectType] {
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
					Labels:      r.Labels,
				},
				Score:             r.Score,
				BranchID:          r.BranchID,
				BranchName:        r.BranchName,
				BranchOrigin:      r.BranchOrigin,
				BranchOriginLabel: r.BranchOriginLabel,
			})

		case search.ItemTypeText:
			textType := textChunkType

			// Chunks carry no entity type, so the type allowlist (allowedTypes) and
			// system blocklist (blockedTypes) do not gate them — they are keyed on
			// schema type names. A caller-supplied `types` list that omits
			// "text_chunk" suppresses them, and a restricted agent object-type scope
			// suppresses them by design: scope expansion rewrites `types` to the
			// scope allowlist before this point (see FilterRequestedObjectTypes).
			// A namespace restriction is enforced earlier, in the chunks query
			// itself (via kb.object_chunks → kb.graph_objects.namespace), because
			// a chunk has no namespace of its own to test here (#1902).
			if len(types) > 0 && !containsStr(types, textType) {
				continue
			}

			textID, _ := uuid.Parse(r.ID)

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
			// text_chunk items carry no labels; they are governed by
			// include_chunks, not by the entity label filter, so let them pass.
			if item.Object.Type == textChunkType {
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

	resp := &graph.SearchResponse{
		Data:    items,
		Total:   len(items),
		HasMore: false,
	}
	// A scoped search reports what it read, so the model can say its answer is
	// bounded by pending branches instead of silently presenting a partial
	// picture (#265).
	if bs := res.Metadata.BranchScope; bs != nil {
		meta := &graph.SearchResponseMeta{
			BranchScopeNote: bs.Note,
			BranchesOmitted: bs.Omitted,
		}
		for _, b := range bs.Branches {
			meta.BranchesRead = append(meta.BranchesRead, b.Name)
		}
		resp.Meta = meta
	}
	return resp
}

// normaliseSearchItemDates applies the schema-aware date normalisation to graph
// search items, so a date property read through any search tool converges to
// YYYY-MM-DD even when the row was written before the write path was fixed
// (issue #402). Text-chunk items carry no schema type and pass through.
func (s *Service) normaliseSearchItemDates(ctx context.Context, projectID uuid.UUID, resp *graph.SearchResponse) *graph.SearchResponse {
	if resp == nil || s.graphService == nil {
		return resp
	}
	for _, item := range resp.Data {
		if item == nil || item.Object == nil {
			continue
		}
		item.Object.Properties = s.graphService.NormaliseDatePropertyValues(ctx, projectID, item.Object.Type, item.Object.Properties)
	}
	return resp
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// filterSearchItemsByScope drops search result items whose object type is not
// permitted by the scope. A nil/unrestricted scope returns items unchanged (no
// allocation). It guards nil items and nil objects so the search paths can call
// it unconditionally on the final result list. The `text_chunk` pseudo-type
// (document snippet, not a graph object type) is let through: chunks are only
// present when the caller explicitly set include_chunks=true, so the agent
// type-scope must not gate them — inclusion is governed by include_chunks alone.
func filterSearchItemsByScope(scope *TypeScope, items []*graph.SearchResultItem) []*graph.SearchResultItem {
	if !scope.HasObjectRestriction() {
		return items
	}
	out := make([]*graph.SearchResultItem, 0, len(items))
	for _, item := range items {
		if item == nil || item.Object == nil {
			continue
		}
		if item.Object.Type == textChunkType || scope.AllowsObject(item.Object.Type) {
			out = append(out, item)
		}
	}
	return out
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

	opts := responseOptsFromArgs(args)
	if opts.FieldStrategy == "" {
		opts.FieldStrategy = "compact"
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

	// Optional fused-score cutoff (0-1). nil = no cutoff, weak matches returned ranked.
	minScore, err := parseMinScoreArg(args)
	if err != nil {
		return nil, err
	}

	var types []string
	if t, ok := args["types"].([]any); ok {
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}

	scope := TypeScopeFromContext(ctx)
	var denied bool
	types, denied = scope.FilterRequestedObjectTypes(types)
	if denied {
		return envelopeSearchResponse(&graph.SearchResponse{}, opts)
	}

	namespaceFilter, _ := args["namespace"].(string)
	allowedTypes, err := s.getTypesForNamespace(ctx, projectID, namespaceFilter)
	if err != nil {
		return nil, err
	}

	blockedTypes, err := s.getSystemNamespaceTypes(ctx, projectID, namespaceFilter)
	if err != nil {
		return nil, err
	}

	// Prefer search.Service which auto-embeds the query text, giving the vector
	// leg full signal even for multi-word queries that FTS can't match well.
	if s.searchSvc != nil {
		// Chunks are opt-in: default to graph-only so the text leg (document
		// chunks) never runs; include_chunks re-enables both legs.
		resultTypes := search.ResultTypeGraph
		if includeChunks, _ := args["include_chunks"].(bool); includeChunks {
			resultTypes = search.ResultTypeBoth
		}
		unifiedReq := &search.UnifiedSearchRequest{
			Query:       query,
			Limit:       limit,
			Types:       types,
			MinScore:    minScore,
			ResultTypes: resultTypes,
		}
		// Pass namespace for DB-level filtering. "" is the default (null)
		// namespace, scoped on the chunk leg to null-namespace objects (#1910);
		// "all" is unfiltered; a specific value filters by namespace field.
		switch namespaceFilter {
		case "all":
			// unfiltered
		case "":
			unifiedReq.NamespaceIsNull = true
		default:
			ns := namespaceFilter
			unifiedReq.Namespace = &ns
		}
		res, err := s.searchSvc.Search(ctx, projectUUID, unifiedReq, nil)
		if err != nil {
			s.log.WarnContext(ctx, "unified search failed in semantic_search, falling back",
				"error", err, "project_id", projectID)
		} else {
			mapped := s.mapUnifiedToSearchResponse(res, types, nil, allowedTypes, blockedTypes)
			mapped = s.normaliseSearchItemDates(ctx, projectUUID, mapped)
			mapped.Data = filterSearchItemsByScope(scope, mapped.Data)
			mapped.Total = len(mapped.Data)
			return envelopeSearchResponse(mapped, opts)
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

	// Post-hoc namespace filter for fallback path: apply both allowlist and blocklist.
	filtered := results.Data[:0]
	for _, item := range results.Data {
		if item.Object == nil {
			continue
		}
		if allowedTypes != nil && !allowedTypes[item.Object.Type] {
			continue
		}
		if blockedTypes != nil && blockedTypes[item.Object.Type] {
			continue
		}
		filtered = append(filtered, item)
	}

	// Optional fused-score cutoff on the fallback graph path (the unified path
	// applies the cutoff inside fusion). nil/0 keeps baseline behavior.
	if minScore != nil && *minScore > 0 {
		kept := filtered[:0]
		for _, item := range filtered {
			if item.Score >= *minScore {
				kept = append(kept, item)
			}
		}
		filtered = kept
	}
	results.Data = filterSearchItemsByScope(scope, filtered)
	results.Total = len(results.Data)

	return envelopeSearchResponse(results, opts)
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

	scope := TypeScopeFromContext(ctx)
	if typeFilter != nil && !scope.AllowsObject(*typeFilter) {
		return nil, fmt.Errorf("find similar: object type %q is not allowed for this agent", *typeFilter)
	}

	opts := responseOptsFromArgs(args)
	if opts.FieldStrategy == "" {
		opts.FieldStrategy = "compact"
	}

	req := &graph.SimilarObjectsRequest{
		Type:  typeFilter,
		Limit: limit,
	}

	results, err := s.graphService.FindSimilarObjects(ctx, projectUUID, entityID, req)
	if err != nil {
		return nil, fmt.Errorf("find similar: %w", err)
	}

	items := make([]map[string]any, 0, len(results))
	for _, r := range results {
		if !scope.AllowsObject(r.Type) {
			continue
		}
		items = append(items, slimSimilarResult(r, opts))
	}
	return s.wrapResultCompact(map[string]any{"similar_entities": items, "total": len(items)})
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

	scope := TypeScopeFromContext(ctx)
	var denied bool
	relationshipTypes, denied = scope.FilterRequestedRelationshipTypes(relationshipTypes)
	if denied {
		return nil, fmt.Errorf("traverse graph: none of the requested relationship types are allowed for this agent")
	}

	queryContext := ""
	if qc, ok := args["query_context"].(string); ok {
		queryContext = qc
	}

	opts := responseOptsFromArgs(args)
	if opts.FieldStrategy == "" {
		opts.FieldStrategy = "compact"
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

	results = filterTraverseResponseByScope(scope, results)

	return s.wrapResultCompact(slimTraverseResponse(results, opts))
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

	scope := TypeScopeFromContext(ctx)

	if t, ok := args["type"].(string); ok && t != "" {
		if !scope.AllowsRelationship(t) {
			return nil, fmt.Errorf("list relationships: relationship type %q is not allowed for this agent", t)
		}
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

	if scope.HasRelationshipRestriction() {
		params.Types = scope.RelationshipTypes()
	}

	opts := responseOptsFromArgs(args)

	results, err := s.graphService.ListRelationships(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list relationships: %w", err)
	}

	data := make([]map[string]any, 0, len(results.Items))
	for _, r := range results.Items {
		if !scope.AllowsRelationship(r.Type) {
			continue
		}
		data = append(data, slimRelationship(r, opts))
	}
	total := results.Total
	out := map[string]any{
		"data":     data,
		"total":    total,
		"has_more": results.NextCursor != nil,
	}
	if opts.Verbose && results.NextCursor != nil {
		out["next_cursor"] = *results.NextCursor
	}
	return s.wrapResultCompact(out)
}

// lookupRelationshipType returns the current type of a graph relationship by
// physical or canonical id. It is routed through the graph service so the mcp
// package issues no raw SQL (the mcp-authz guard forbids unclassified raw
// access).
func (s *Service) lookupRelationshipType(ctx context.Context, projectID string, relID uuid.UUID) (string, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return "", err
	}
	rel, err := s.graphService.GetRelationship(ctx, projectUUID, relID)
	if err != nil {
		return "", err
	}
	if rel == nil {
		return "", fmt.Errorf("relationship not found")
	}
	return rel.Type, nil
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

	scope := TypeScopeFromContext(ctx)
	if typ, lerr := s.lookupRelationshipType(ctx, projectID, relID); lerr == nil {
		if !scope.AllowsRelationship(typ) {
			return nil, fmt.Errorf("update relationship: relationship type is not allowed for this agent's scope")
		}
	}

	// Resolve the branch param even when omitted so an agent-run extraction
	// inherits the job's staging branch. A relationship version chain cannot
	// span branches, so a branch-scoped patch targets the branch-local HEAD row.
	var branchID *uuid.UUID
	branchRef, _ := args["branch"].(string)
	branchID, err = s.resolveOrCreateBranchID(ctx, projectID, branchRef)
	if err != nil {
		return nil, fmt.Errorf("resolve branch: %w", err)
	}
	if branchID != nil {
		relID, err = s.relationshipVersionOnBranch(ctx, projectUUID, relID, *branchID)
		if err != nil {
			return nil, fmt.Errorf("update relationship: %w", err)
		}
	}

	req := &graph.PatchGraphRelationshipRequest{}

	if props, ok := args["properties"].(map[string]any); ok {
		req.Properties = props
	}

	if weight, ok := args["weight"].(float64); ok {
		w := float32(weight)
		req.Weight = &w
	}

	opts := responseOptsFromArgs(args)

	result, err := s.graphService.PatchRelationship(ctx, projectUUID, relID, req, nil)
	if err != nil {
		return nil, fmt.Errorf("update relationship: %w", err)
	}

	return s.wrapResultCompact(map[string]any{
		"success":      true,
		"relationship": slimRelationship(result, opts),
	})
}

// executeDeleteRelationship soft-deletes one or more relationships.
//
// Single form: relationship_id. Batch form: relationship_ids[] with one shared
// reason; each item is deleted independently and reported as {index,
// relationship_id, ok, error?} so a caller retries exactly the failed indexes.
func (s *Service) executeDeleteRelationship(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	if raw, present := args["relationship_ids"]; present {
		ids, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid relationship_ids: must be an array of relationship UUIDs")
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("missing or empty relationship_ids array")
		}
		if len(ids) > batchMutationMaxItems {
			return nil, batchOverLimitError("relationship-delete", len(ids))
		}

		// One branch and one reason apply to the whole batch, matching the
		// single-item form's semantics. The per-item helper resolves the
		// branch-local HEAD for each relationship. The branch is resolved lazily
		// on the first valid id so a wholly invalid batch never creates one.
		var branchID *uuid.UUID
		branchResolved := false
		resolveBranch := func() error {
			if branchResolved {
				return nil
			}
			branchResolved = true
			branchRef, _ := args["branch"].(string)
			id, rerr := s.resolveOrCreateBranchID(ctx, projectID, branchRef)
			if rerr != nil {
				return fmt.Errorf("resolve branch: %w", rerr)
			}
			branchID = id
			return nil
		}
		reason := reasonFromArgs(args)

		type batchDeleteResult struct {
			Index          int    `json:"index"`
			RelationshipID string `json:"relationship_id"`
			OK             bool   `json:"ok"`
			Error          string `json:"error,omitempty"`
		}
		results := make([]batchDeleteResult, 0, len(ids))
		succeeded, failed := 0, 0
		for i, rawID := range ids {
			idStr, ok := rawID.(string)
			if !ok || idStr == "" {
				results = append(results, batchDeleteResult{Index: i, OK: false, Error: "invalid relationship_id: expected a non-empty UUID string"})
				failed++
				continue
			}
			if err := resolveBranch(); err != nil {
				return nil, err
			}
			if _, err := s.deleteRelationshipOne(ctx, projectUUID, projectID, idStr, branchID, reason); err != nil {
				// Per-item detail: an app error's message is written for the
				// caller (not found, forbidden), anything else collapses to the
				// internal message instead of carrying driver text into the tool
				// result. The cause is logged by the delete path.
				results = append(results, batchDeleteResult{Index: i, RelationshipID: idStr, OK: false, Error: apperror.PublicMessage(err)})
				failed++
				continue
			}
			results = append(results, batchDeleteResult{Index: i, RelationshipID: idStr, OK: true})
			succeeded++
		}

		allOK := failed == 0
		errMsg := ""
		if !allOK {
			errMsg = fmt.Sprintf("%d of %d relationships failed to delete; retry the failed indexes only", failed, len(ids))
		}
		return envelopeResult(allOK, map[string]any{
			"results": results,
			"message": fmt.Sprintf("Batch relationship delete completed: %d succeeded, %d failed", succeeded, failed),
		}, map[string]any{
			"succeeded": succeeded,
			"failed":    failed,
			"total":     len(ids),
		}, errMsg)
	}

	relIDStr, ok := args["relationship_id"].(string)
	if !ok || relIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: relationship_id")
	}
	if _, err := uuid.Parse(relIDStr); err != nil {
		return nil, fmt.Errorf("invalid relationship_id: %w", err)
	}

	// Resolve the branch param (an omitted branch inherits an agent-run
	// extraction's staging branch).
	var branchID *uuid.UUID
	branchRef, _ := args["branch"].(string)
	branchID, err = s.resolveOrCreateBranchID(ctx, projectID, branchRef)
	if err != nil {
		return nil, fmt.Errorf("resolve branch: %w", err)
	}

	relID, err := s.deleteRelationshipOne(ctx, projectUUID, projectID, relIDStr, branchID, reasonFromArgs(args))
	if err != nil {
		return nil, err
	}

	return envelopeResult(true, map[string]any{
		"relationship_id": relID.String(),
	}, nil, "")
}

// deleteRelationshipOne soft-deletes one relationship, scoped to the caller's
// project. A relationship id from another project fails the same way as a
// missing id and never touches the foreign row.
func (s *Service) deleteRelationshipOne(ctx context.Context, projectUUID uuid.UUID, projectID, relIDStr string, branchID *uuid.UUID, reason *string) (uuid.UUID, error) {
	relID, err := uuid.Parse(relIDStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid relationship_id: %w", err)
	}

	if branchID != nil {
		relID, err = s.relationshipVersionOnBranch(ctx, projectUUID, relID, *branchID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("delete relationship: %w", err)
		}
	}

	if _, err := s.graphService.DeleteRelationship(ctx, projectUUID, relID, branchID, reason, nil); err != nil {
		// A repeat delete is a no-op success: DeleteRelationship checks the
		// branch HEAD before writing, so retrying a batch cannot duplicate a
		// tombstone and must not surface a false per-item failure.
		if isAlreadyDeletedError(err) {
			return relID, nil
		}
		return uuid.Nil, fmt.Errorf("delete relationship: %w", err)
	}
	return relID, nil
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
