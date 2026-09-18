package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/google/uuid"
)

// ResponseOpts controls how graph DTOs are slimmed for LLM-facing MCP tool results.
type ResponseOpts struct {
	Fields        []string // property projection; empty = all properties
	Verbose       bool
	FieldStrategy string // "", "full", "compact", "minimal"
}

// responseOptsFromArgs parses MCP tool arguments into ResponseOpts, coercing
// defaults. It never returns an error.
//
//   - fields: accepts []any (each a string) or []string; empty input → nil.
//   - verbose: accepts bool.
//   - field_strategy: accepts a string, lowercased; only "full", "compact" or
//     "minimal" are accepted; anything else → "".
func responseOptsFromArgs(args map[string]any) ResponseOpts {
	if args == nil {
		return ResponseOpts{}
	}

	var opts ResponseOpts

	switch f := args["fields"].(type) {
	case []string:
		if len(f) > 0 {
			opts.Fields = f
		}
	case []any:
		for _, v := range f {
			if s, ok := v.(string); ok && s != "" {
				opts.Fields = append(opts.Fields, s)
			}
		}
	}

	if v, ok := args["verbose"].(bool); ok {
		opts.Verbose = v
	}

	if s, ok := args["field_strategy"].(string); ok {
		opts.FieldStrategy = strings.ToLower(s)
	}
	switch opts.FieldStrategy {
	case "full", "compact", "minimal":
	default:
		opts.FieldStrategy = ""
	}

	return opts
}

// propertiesForStrategy returns the properties map to emit for the given field
// strategy. "minimal" and "compact" emit no properties; "" and "full" emit the
// full map (or the Fields projection when Fields is non-empty).
func propertiesForStrategy(props map[string]any, opts ResponseOpts) map[string]any {
	switch opts.FieldStrategy {
	case "minimal", "compact":
		return nil
	}
	if len(opts.Fields) == 0 {
		return props
	}
	return projectProperties(props, opts.Fields)
}

// projectProperties builds a map containing only the requested property keys.
// The "name" key is always included when present in the source properties,
// even if not requested.
func projectProperties(props map[string]any, fields []string) map[string]any {
	out := make(map[string]any, len(fields)+1)
	for _, f := range fields {
		if v, ok := props[f]; ok {
			out[f] = v
		}
	}
	if _, ok := out["name"]; !ok {
		if v, ok := props["name"]; ok {
			out["name"] = v
		}
	}
	return out
}

// nameForStrategy derives the display name from properties["name"]. It is
// omitted under the "minimal" strategy or when the value is not a non-empty
// string.
func nameForStrategy(props map[string]any, opts ResponseOpts) (string, bool) {
	if opts.FieldStrategy == "minimal" || props == nil {
		return "", false
	}
	s, ok := props["name"].(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

// slimEntity renders a graph object as a compact map for LLM consumption.
//
// Default keys: id, type, key, name, properties, labels.
// Verbose adds: version_id, version, status, namespace, branch_id,
// schema_version, created_at, deleted_at, change_summary, content_hash,
// external_source, external_id, external_url, external_parent_id, synced_at,
// external_updated_at, revision_count, relationship_count, supersedes_id.
//
// org_id, project_id, canonical_id and entity_id are never emitted.
func slimEntity(o *graph.GraphObjectResponse, opts ResponseOpts) map[string]any {
	if o == nil {
		return nil
	}

	props := propertiesForStrategy(o.Properties, opts)
	name, hasName := nameForStrategy(o.Properties, opts)

	out := map[string]any{
		"id":   o.CanonicalID.String(),
		"type": o.Type,
	}
	if o.Key != nil && *o.Key != "" {
		out["key"] = *o.Key
	}
	if hasName {
		out["name"] = name
	}
	if props != nil {
		out["properties"] = props
	}
	if len(o.Labels) > 0 {
		out["labels"] = o.Labels
	}

	if opts.Verbose {
		if o.ID != uuid.Nil {
			out["version_id"] = o.ID.String()
		}
		if o.Version != 0 {
			out["version"] = o.Version
		}
		if o.Status != nil && *o.Status != "" {
			out["status"] = *o.Status
		}
		if o.Namespace != nil && *o.Namespace != "" {
			out["namespace"] = *o.Namespace
		}
		if o.BranchID != nil {
			out["branch_id"] = o.BranchID.String()
		}
		if o.SchemaVersion != nil && *o.SchemaVersion != "" {
			out["schema_version"] = *o.SchemaVersion
		}
		if !o.CreatedAt.IsZero() {
			out["created_at"] = o.CreatedAt
		}
		// GraphObjectResponse carries no updated_at field; nothing to emit.
		if o.DeletedAt != nil && !o.DeletedAt.IsZero() {
			out["deleted_at"] = *o.DeletedAt
		}
		if len(o.ChangeSummary) > 0 {
			out["change_summary"] = o.ChangeSummary
		}
		if o.ContentHash != nil && *o.ContentHash != "" {
			out["content_hash"] = *o.ContentHash
		}
		if o.ExternalSource != nil && *o.ExternalSource != "" {
			out["external_source"] = *o.ExternalSource
		}
		if o.ExternalID != nil && *o.ExternalID != "" {
			out["external_id"] = *o.ExternalID
		}
		if o.ExternalURL != nil && *o.ExternalURL != "" {
			out["external_url"] = *o.ExternalURL
		}
		if o.ExternalParentID != nil && *o.ExternalParentID != "" {
			out["external_parent_id"] = *o.ExternalParentID
		}
		if o.SyncedAt != nil && !o.SyncedAt.IsZero() {
			out["synced_at"] = *o.SyncedAt
		}
		if o.ExternalUpdatedAt != nil && !o.ExternalUpdatedAt.IsZero() {
			out["external_updated_at"] = *o.ExternalUpdatedAt
		}
		if o.RevisionCount != nil {
			out["revision_count"] = *o.RevisionCount
		}
		if o.RelationshipCount != nil {
			out["relationship_count"] = *o.RelationshipCount
		}
		if o.SupersedesID != nil {
			out["supersedes_id"] = o.SupersedesID.String()
		}
	}

	return out
}

// slimRelationship renders a graph relationship as a compact map.
//
// Default keys: id, type, src_id, dst_id, label, weight, properties.
// Verbose adds: version_id, version, branch_id, created_at, deleted_at,
// change_summary, inverse_relationship.
//
// project_id, canonical_id and entity_id are never emitted. The field strategy
// does not apply to relationships.
func slimRelationship(r *graph.GraphRelationshipResponse, opts ResponseOpts) map[string]any {
	if r == nil {
		return nil
	}

	out := map[string]any{
		"id":     r.CanonicalID.String(),
		"type":   r.Type,
		"src_id": r.SrcID.String(),
		"dst_id": r.DstID.String(),
	}
	if r.Label != nil && *r.Label != "" {
		out["label"] = *r.Label
	}
	if r.Weight != nil {
		out["weight"] = *r.Weight
	}
	if len(r.Properties) > 0 {
		out["properties"] = r.Properties
	}

	if opts.Verbose {
		if r.ID != uuid.Nil {
			out["version_id"] = r.ID.String()
		}
		if r.Version != 0 {
			out["version"] = r.Version
		}
		if r.BranchID != nil {
			out["branch_id"] = r.BranchID.String()
		}
		if !r.CreatedAt.IsZero() {
			out["created_at"] = r.CreatedAt
		}
		if r.DeletedAt != nil && !r.DeletedAt.IsZero() {
			out["deleted_at"] = *r.DeletedAt
		}
		if len(r.ChangeSummary) > 0 {
			out["change_summary"] = r.ChangeSummary
		}
		if r.InverseRelationship != nil {
			out["inverse_relationship"] = slimRelationship(r.InverseRelationship, opts)
		}
	}

	return out
}

// slimSearchResponse renders a search response as a compact map:
// {data, total, has_more}. Verbose adds per-item lexical_score, vector_score
// and vector_dist, plus a top-level meta (passed through as-is).
func slimSearchResponse(res *graph.SearchResponse, opts ResponseOpts) map[string]any {
	if res == nil {
		return nil
	}

	out := map[string]any{
		"data":     slimSearchData(res.Data, opts),
		"total":    res.Total,
		"has_more": res.HasMore,
	}
	if opts.Verbose && res.Meta != nil {
		out["meta"] = res.Meta
	}

	return out
}

func slimSearchData(items []*graph.SearchResultItem, opts ResponseOpts) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if it == nil {
			continue
		}
		item := map[string]any{
			"object": slimEntity(it.Object, opts),
			"score":  it.Score,
		}
		if opts.Verbose {
			if it.LexicalScore != nil {
				item["lexical_score"] = *it.LexicalScore
			}
			if it.VectorScore != nil {
				item["vector_score"] = *it.VectorScore
			}
			if it.VectorDist != nil {
				item["vector_dist"] = *it.VectorDist
			}
		}
		out = append(out, item)
	}
	return out
}

// slimTraverseResponse renders a traverse response as a compact map:
// {nodes, edges, truncated, roots}. Verbose adds pagination and timing metadata.
func slimTraverseResponse(res *graph.TraverseGraphResponse, opts ResponseOpts) map[string]any {
	if res == nil {
		return nil
	}

	nodes := make([]map[string]any, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		nodes = append(nodes, slimTraverseNode(n, opts))
	}
	edges := make([]map[string]any, 0, len(res.Edges))
	for _, e := range res.Edges {
		edges = append(edges, slimTraverseEdge(e))
	}

	out := map[string]any{
		"nodes":     nodes,
		"edges":     edges,
		"truncated": res.Truncated,
		"roots":     res.Roots,
	}

	if opts.Verbose {
		out["max_depth_reached"] = res.MaxDepthReached
		out["total_nodes"] = res.TotalNodes
		out["has_next_page"] = res.HasNextPage
		out["has_previous_page"] = res.HasPreviousPage
		if res.NextCursor != nil && *res.NextCursor != "" {
			out["next_cursor"] = *res.NextCursor
		}
		if res.PreviousCursor != nil && *res.PreviousCursor != "" {
			out["previous_cursor"] = *res.PreviousCursor
		}
		out["approx_position_start"] = res.ApproxPositionStart
		out["approx_position_end"] = res.ApproxPositionEnd
		out["page_direction"] = res.PageDirection
		if res.QueryTimeMs != nil {
			out["query_time_ms"] = *res.QueryTimeMs
		}
		if res.ResultCount != nil {
			out["result_count"] = *res.ResultCount
		}
	}

	return out
}

func slimTraverseNode(n *graph.TraverseNode, opts ResponseOpts) map[string]any {
	if n == nil {
		return nil
	}

	props := propertiesForStrategy(n.Properties, opts)
	name, hasName := nameForStrategy(n.Properties, opts)

	out := map[string]any{
		"id":    n.CanonicalID.String(),
		"type":  n.Type,
		"depth": n.Depth,
	}
	if n.Key != nil && *n.Key != "" {
		out["key"] = *n.Key
	}
	if len(n.Labels) > 0 {
		out["labels"] = n.Labels
	}
	if hasName {
		out["name"] = name
	}
	if props != nil {
		out["properties"] = props
	}

	if opts.Verbose {
		if n.ID != uuid.Nil {
			out["version_id"] = n.ID.String()
		}
		if n.PhaseIndex != nil {
			out["phase_index"] = *n.PhaseIndex
		}
		if len(n.Paths) > 0 {
			out["paths"] = n.Paths
		}
	}

	return out
}

func slimTraverseEdge(e *graph.TraverseEdge) map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"id":     e.ID.String(),
		"type":   e.Type,
		"src_id": e.SrcID.String(),
		"dst_id": e.DstID.String(),
	}
}

// slimSimilarResult renders a similar-objects result as a compact map:
// {id, type, key, name, properties, distance}. id falls back to the physical ID
// when CanonicalID is nil. Verbose adds version, status, branch_id, created_at,
// labels.
func slimSimilarResult(sr *graph.SimilarObjectResult, opts ResponseOpts) map[string]any {
	if sr == nil {
		return nil
	}

	id := sr.ID
	if sr.CanonicalID != nil {
		id = *sr.CanonicalID
	}
	props := propertiesForStrategy(sr.Properties, opts)
	name, hasName := nameForStrategy(sr.Properties, opts)

	out := map[string]any{
		"id":       id.String(),
		"type":     sr.Type,
		"distance": sr.Distance,
	}
	if sr.Key != nil && *sr.Key != "" {
		out["key"] = *sr.Key
	}
	if hasName {
		out["name"] = name
	}
	if props != nil {
		out["properties"] = props
	}

	if opts.Verbose {
		if sr.Version != nil {
			out["version"] = *sr.Version
		}
		if sr.Status != nil && *sr.Status != "" {
			out["status"] = *sr.Status
		}
		if sr.BranchID != nil {
			out["branch_id"] = sr.BranchID.String()
		}
		if sr.CreatedAt != nil && !sr.CreatedAt.IsZero() {
			out["created_at"] = *sr.CreatedAt
		}
		if len(sr.Labels) > 0 {
			out["labels"] = sr.Labels
		}
	}

	return out
}

// structuredContentFromJSON returns the JSON object encoded in jsonBytes as a
// map[string]any, or nil when the top-level value is not a JSON object.
// MCP 2025-06-18 requires structuredContent to be a root object; arrays and
// primitives are intentionally left nil (spec-legal) while the text content
// block still carries the serialized payload.
func structuredContentFromJSON(jsonBytes []byte) map[string]any {
	var obj map[string]any
	if err := json.Unmarshal(jsonBytes, &obj); err != nil {
		return nil
	}
	return obj
}

// wrapResultCompact is like wrapResult but marshals with json.Marshal (compact,
// no indentation) for slimmer LLM-facing tool results.
func (s *Service) wrapResultCompact(data any) (*ToolResult, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	return &ToolResult{
		Content: []ContentBlock{
			{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
		StructuredContent: structuredContentFromJSON(jsonBytes),
	}, nil
}
