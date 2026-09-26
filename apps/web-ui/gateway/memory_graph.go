package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GraphObject is one extracted knowledge-graph object (entity) from
// GET /api/graph/objects/search or /objects/:id.
type GraphObject struct {
	ID          string         `json:"id"`
	CanonicalID string         `json:"canonical_id"`
	BranchID    string         `json:"branch_id"`
	Type        string         `json:"type"`
	Key         string         `json:"key"`
	Status      string         `json:"status"`
	Properties  map[string]any `json:"properties"`
	Labels      []string       `json:"labels"`
	CreatedAt   string         `json:"created_at"`
	// EmbeddingStatus is the per-object embedding-job state derived by the
	// server: embedded | pending | processing | failed | dead_letter | missing.
	EmbeddingStatus string `json:"embedding_status"`
	// EmbeddingUpdatedAt is the RFC3339 time the object's embedding was last
	// written (empty when not embedded).
	EmbeddingUpdatedAt string `json:"embedding_updated_at"`
}

// ObjectSearchResult is one scored search hit (FTS or hybrid). Score semantics
// are mode-specific: FTS returns a relevance score, hybrid returns the fused
// lexical/vector score.
type ObjectSearchResult struct {
	Object GraphObject
	Score  float32
}

// SimilarObject is one object returned by the vector-similarity endpoint
// (GET /api/graph/objects/:id/similar). Distance is the pgvector cosine
// distance (0..2 — LOWER is more similar).
type SimilarObject struct {
	ID          string         `json:"id"`
	CanonicalID string         `json:"canonical_id"`
	Version     int            `json:"version"`
	Distance    float32        `json:"distance"`
	Type        string         `json:"type"`
	Key         string         `json:"key"`
	Status      string         `json:"status"`
	Properties  map[string]any `json:"properties"`
	Labels      []string       `json:"labels"`
	CreatedAt   string         `json:"created_at"`
}

// GraphRelationship is one extracted knowledge-graph relationship (edge) from
// GET /api/graph/relationships/search.
type GraphRelationship struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	SrcID      string         `json:"src_id"`
	DstID      string         `json:"dst_id"`
	Properties map[string]any `json:"properties"`
}

// UpdateObjectRequest patches a graph object (delta-merge semantics).
type UpdateObjectRequest struct {
	Key           *string        `json:"key,omitempty"`
	Status        *string        `json:"status,omitempty"`
	Labels        []string       `json:"labels,omitempty"`
	ReplaceLabels bool           `json:"replaceLabels,omitempty"`
	Properties    map[string]any `json:"properties,omitempty"`
	BranchID      *string        `json:"branch_id,omitempty"`
}

// CreateRelationshipRequest creates a graph relationship (edge).
type CreateRelationshipRequest struct {
	Type       string         `json:"type"`
	SrcID      string         `json:"src_id"`
	DstID      string         `json:"dst_id"`
	Properties map[string]any `json:"properties,omitempty"`
	BranchID   *string        `json:"branch_id,omitempty"`
}

// CreateObjectRequest creates a graph object (POST /api/graph/objects).
type CreateObjectRequest struct {
	Type       string         `json:"type"`
	Key        *string        `json:"key,omitempty"`
	Status     *string        `json:"status,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Labels     []string       `json:"labels,omitempty"`
	BranchID   *string        `json:"branch_id,omitempty"`
}

// GetGraphObject fetches a single graph object by id. Unlike the list/search
// endpoints, this returns objects regardless of which branch they live on
// (extracted objects land on a staging branch).
func (m *MemoryClient) GetGraphObject(ctx context.Context, objectID string) (*GraphObject, error) {
	var out GraphObject
	if err := m.doH(ctx, http.MethodGet, "/api/graph/objects/"+objectID, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListGraphObjects lists graph objects, optionally scoped to a branch and/or
// filtered by type or ids. Empty branchID omits the branch filter (main
// branch); empty typeFilter omits the type filter; ids may be empty.
func (m *MemoryClient) ListGraphObjects(ctx context.Context, branchID, typeFilter string, ids []string) ([]GraphObject, error) {
	q := url.Values{}
	q.Set("limit", "100")
	// The total is never read here (only Items is), and the exact COUNT(*) is
	// the endpoint's latency floor on large projects — skip it (#733).
	q.Set("include_total", "false")
	if typeFilter != "" {
		q.Set("type", typeFilter)
	}
	if len(ids) > 0 {
		q.Set("ids", strings.Join(ids, ","))
	}
	if branchID != "" {
		q.Set("branch_id", branchID)
	}
	var out struct {
		Items []GraphObject `json:"items"`
		Total int           `json:"total"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/graph/objects/search?"+q.Encode(), nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// ListGraphObjectsPage lists a single page of graph objects (most-recent-first),
// cursor-paginated. Empty branchID omits the branch filter; empty typeFilter
// omits the type filter. nextCursor is empty when there are no more pages.
func (m *MemoryClient) ListGraphObjectsPage(ctx context.Context, branchID, typeFilter, cursor string, limit int) ([]GraphObject, string, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	// The total is never read here, and the exact COUNT(*) is the endpoint's
	// latency floor on large projects — skip it (#733).
	q.Set("include_total", "false")
	if typeFilter != "" {
		q.Set("type", typeFilter)
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if branchID != "" {
		q.Set("branch_id", branchID)
	}
	var out struct {
		Items      []GraphObject `json:"items"`
		NextCursor string        `json:"next_cursor"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/graph/objects/search?"+q.Encode(), nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, "", err
	}
	return out.Items, out.NextCursor, nil
}

// CountObjects returns the total number of graph objects, optionally scoped to
// a branch (GET /api/graph/objects/count).
func (m *MemoryClient) CountObjects(ctx context.Context, branchID string) (int, error) {
	path := "/api/graph/objects/count"
	if branchID != "" {
		path += "?branch_id=" + url.QueryEscape(branchID)
	}
	var out struct {
		Count int `json:"count"`
	}
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

// GetObjectEdges returns the relationships (edges) touching one object,
// combining incoming and outgoing edges.
func (m *MemoryClient) GetObjectEdges(ctx context.Context, objectID string) ([]GraphRelationship, error) {
	var out struct {
		Incoming []GraphRelationship `json:"incoming"`
		Outgoing []GraphRelationship `json:"outgoing"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/graph/objects/"+objectID+"/edges", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return append(out.Incoming, out.Outgoing...), nil
}

// GetSimilarObjects finds objects similar to the given object via vector
// similarity (GET /api/graph/objects/:id/similar). Response is a bare array.
func (m *MemoryClient) GetSimilarObjects(ctx context.Context, objectID string, limit int) ([]SimilarObject, error) {
	path := "/api/graph/objects/" + objectID + "/similar"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	var out []SimilarObject
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListGraphRelationships lists graph relationships, optionally scoped to a
// branch. Empty branchID omits the branch filter (main branch).
func (m *MemoryClient) ListGraphRelationships(ctx context.Context, branchID string) ([]GraphRelationship, error) {
	q := url.Values{}
	q.Set("limit", "100")
	if branchID != "" {
		q.Set("branch_id", branchID)
	}
	var out struct {
		Items []GraphRelationship `json:"items"`
		Total int                 `json:"total"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/graph/relationships/search?"+q.Encode(), nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// UpdateObject patches a graph object, returning the new versioned object.
// NOTE: the returned object has a NEW ID; callers must use it, not the old one.
func (m *MemoryClient) UpdateObject(ctx context.Context, id string, req *UpdateObjectRequest) (*GraphObject, error) {
	var out GraphObject
	if err := m.doH(ctx, http.MethodPatch, "/api/graph/objects/"+url.PathEscape(id), req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateObject creates a graph object (POST /api/graph/objects).
func (m *MemoryClient) CreateObject(ctx context.Context, req *CreateObjectRequest) (*GraphObject, error) {
	var out GraphObject
	if err := m.doH(ctx, http.MethodPost, "/api/graph/objects", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateRelationship creates a graph relationship.
func (m *MemoryClient) CreateRelationship(ctx context.Context, req *CreateRelationshipRequest) error {
	var out any
	return m.doH(ctx, http.MethodPost, "/api/graph/relationships", req, m.documentHeaders(ctx), &out)
}

// SearchObjectsFTS full-text-searches graph objects. typeFilter may be empty.
func (m *MemoryClient) SearchObjectsFTS(ctx context.Context, query, typeFilter string) ([]GraphObject, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("limit", "50")
	if typeFilter != "" {
		q.Set("types", typeFilter)
	}
	var wire struct {
		Data []struct {
			Object GraphObject `json:"object"`
		} `json:"data"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/graph/objects/fts?"+q.Encode(), nil, m.documentHeaders(ctx), &wire); err != nil {
		return nil, err
	}
	out := make([]GraphObject, 0, len(wire.Data))
	for _, d := range wire.Data {
		out = append(out, d.Object)
	}
	return out, nil
}

// searchObjectsWire is the shared response shape of both search endpoints
// (FTS and hybrid).
type searchObjectsWire struct {
	Data []struct {
		Object GraphObject `json:"object"`
		Score  float32     `json:"score"`
	} `json:"data"`
	Total   int  `json:"total"`
	HasMore bool `json:"hasMore"`
}

// searchObjectsRequestBody is the POST body for the hybrid search endpoint.
type searchObjectsRequestBody struct {
	Query    string   `json:"query"`
	Limit    int      `json:"limit"`
	Offset   int      `json:"offset"`
	Types    []string `json:"types,omitempty"`
	BranchID string   `json:"branchId,omitempty"`
}

// SearchObjects text-searches graph objects in one of two modes: "fulltext"
// (GET /api/graph/objects/fts) or "hybrid" (POST /api/graph/search, which
// auto-embeds the query and fuses lexical/vector results). types is a
// comma-joined list of type names (empty = no type filter). hasMore reports
// whether more results exist beyond this page.
func (m *MemoryClient) SearchObjects(ctx context.Context, mode, query, types, branchID string, limit, offset int) ([]ObjectSearchResult, bool, error) {
	var wire searchObjectsWire
	switch mode {
	case "hybrid":
		body := searchObjectsRequestBody{Query: query, Limit: limit, Offset: offset}
		if types != "" {
			body.Types = strings.Split(types, ",")
		}
		if branchID != "" {
			body.BranchID = branchID
		}
		if err := m.doH(ctx, http.MethodPost, "/api/graph/search", body, m.documentHeaders(ctx), &wire); err != nil {
			return nil, false, err
		}
	default: // "fulltext"
		q := url.Values{}
		q.Set("q", query)
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset))
		if types != "" {
			q.Set("types", types)
		}
		if branchID != "" {
			q.Set("branch_id", branchID)
		}
		if err := m.doH(ctx, http.MethodGet, "/api/graph/objects/fts?"+q.Encode(), nil, m.documentHeaders(ctx), &wire); err != nil {
			return nil, false, err
		}
	}

	results := make([]ObjectSearchResult, 0, len(wire.Data))
	for _, d := range wire.Data {
		results = append(results, ObjectSearchResult{Object: d.Object, Score: d.Score})
	}
	return results, wire.HasMore, nil
}
