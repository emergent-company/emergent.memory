package main

import (
	"context"
	"net/http"
)

// UnifiedSearchHit is one item of POST /api/search/unified. The endpoint
// returns a union under a single struct, so every field is optional and which
// ones are populated depends on Type ("graph" | "text" | "relationship").
type UnifiedSearchHit struct {
	Type  string  `json:"type"`
	ID    string  `json:"id"`
	Score float32 `json:"score"`

	// Text (document chunk) hits.
	Snippet    string `json:"snippet,omitempty"`
	Mode       string `json:"mode,omitempty"`
	DocumentID string `json:"document_id,omitempty"`

	// Graph (object) hits.
	ObjectID    string         `json:"object_id,omitempty"`
	CanonicalID string         `json:"canonical_id,omitempty"`
	ObjectType  string         `json:"object_type,omitempty"`
	Key         string         `json:"key,omitempty"`
	Fields      map[string]any `json:"fields,omitempty"`
}

// UnifiedSearchMetadata is the response's result accounting. The counts are
// post-fusion (how many of each type survived the limit), and the execution
// time is the server's own measurement.
type UnifiedSearchMetadata struct {
	TotalResults     int `json:"totalResults"`
	GraphResultCount int `json:"graphResultCount"`
	TextResultCount  int `json:"textResultCount"`

	ExecutionTime struct {
		TotalMs int `json:"totalMs"`
	} `json:"executionTime"`
}

// UnifiedSearchResponse is the POST /api/search/unified response envelope.
type UnifiedSearchResponse struct {
	Results  []UnifiedSearchHit    `json:"results"`
	Metadata UnifiedSearchMetadata `json:"metadata"`
}

// UnifiedSearchParams is the request subset the gateway sends. ResultTypes is
// the server's own vocabulary ("graph" | "text" | "both"; empty defaults to
// both server-side). Limit is the fused result cap — the endpoint has no
// offset, so this is the only pagination control.
type UnifiedSearchParams struct {
	Query       string
	ResultTypes string
	Limit       int
	BranchID    string
}

// SearchUnified runs POST /api/search/unified, the fused graph + document-chunk
// search. Unlike SearchObjectsUnified (same endpoint, graph only), it returns
// every item type, so the caller can render document text hits — the reason a
// project with documents but no graph objects still returns results.
//
// The relationship leg is disabled: this is the browse/search surface, which
// renders objects and chunks only, and skipping the leg keeps the request
// cheaper. Fusion stays at the server default (weighted).
func (m *MemoryClient) SearchUnified(ctx context.Context, p UnifiedSearchParams) (*UnifiedSearchResponse, error) {
	body := map[string]any{
		"query":                p.Query,
		"includeRelationships": false,
	}
	if p.ResultTypes != "" {
		body["resultTypes"] = p.ResultTypes
	}
	if p.Limit > 0 {
		body["limit"] = p.Limit
	}
	if p.BranchID != "" {
		body["branch_id"] = p.BranchID
	}

	var out UnifiedSearchResponse
	if err := m.doH(ctx, http.MethodPost, "/api/search/unified", body, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
