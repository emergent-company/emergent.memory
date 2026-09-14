package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// --- conversations ---

type Conversation struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	AgentDefinitionID string `json:"agentDefinitionId,omitempty"`
	ProjectID         string `json:"projectId"`
	CanonicalID       string `json:"canonicalId"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

type ConversationList struct {
	Conversations []Conversation `json:"conversations"`
	Total         int            `json:"total"`
}

func (m *MemoryClient) ListConversations(ctx context.Context) (*ConversationList, error) {
	var out ConversationList
	if err := m.do(ctx, http.MethodGet, "/api/chat/conversations", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateObjectConversation creates (or returns the existing) refinement
// conversation linked to an object via canonicalId, seeded with the first
// message. Memory get-or-creates by canonicalId (dedup). Returns the id.
func (m *MemoryClient) CreateObjectConversation(ctx context.Context, canonicalID, title, message string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	body := map[string]any{
		"title":       title,
		"message":     message,
		"canonicalId": canonicalID,
	}
	if err := m.do(ctx, http.MethodPost, "/api/chat/conversations", body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// --- memories ---

// minSearchScore is the relevance floor for search results. The memory
// service returns every entity ranked by score (0..1); with a small dataset
// totally unrelated notes score ~0.036–0.056 while a real match scores
// ~0.168, so 0.08 cleanly separates signal from noise. Tunable: raise it to
// require stronger matches, lower it to return more results.
const minSearchScore = 0.08

// Memory is one memory entity normalized for display: {id, content,
// category, confidence}. Mirrors the iOS/admin.py memory browser shape.
// Score carries the search relevance (0..1) from /api/search/unified and is
// 0 for list results (entity-query has no score).
type Memory struct {
	ID         string
	Content    string
	Category   string
	Confidence float64
	Score      float64
}

// searchResult is the union item shape of POST /api/search/unified (graph,
// text, or relationship results; only the graph fields are meaningful here).
type searchResult struct {
	ID          string         `json:"id"`
	ObjectID    string         `json:"object_id"`
	CanonicalID string         `json:"canonical_id"`
	ObjectType  string         `json:"object_type"`
	Snippet     string         `json:"snippet"`
	Score       float64        `json:"score"`
	Fields      map[string]any `json:"fields"`
}

// SearchMemories runs a hybrid search across all memory entities via the
// REST endpoint POST /api/search/unified (the same call admin.py and the iOS
// client make), normalized to []Memory. Results below minSearchScore are
// dropped so unrelated notes never crowd out the real matches.
func (m *MemoryClient) SearchMemories(ctx context.Context, query string) ([]Memory, error) {
	var resp struct {
		Results []searchResult `json:"results"`
	}
	body := map[string]any{"query": query, "limit": 50}
	if err := m.do(ctx, http.MethodPost, "/api/search/unified", body, &resp); err != nil {
		return nil, err
	}
	out := make([]Memory, 0, len(resp.Results))
	for _, r := range resp.Results {
		if r.Score < minSearchScore {
			continue
		}
		id := firstNonEmpty(r.CanonicalID, r.ID, r.ObjectID)
		if id == "" {
			continue
		}
		mem := memoryFromParts(id, r.ObjectType, r.Fields)
		mem.Score = r.Score
		if mem.Content == "" && r.Snippet != "" {
			mem.Content = r.Snippet
		}
		// Skip pure-noise results (e.g. relationship links) that carry
		// neither content nor a category.
		if mem.Content == "" && mem.Category == "" {
			continue
		}
		out = append(out, mem)
	}
	return out, nil
}

// ListMemories lists all memory entities via the graph objects REST endpoint
// (GET /api/graph/objects/search, no type filter — the same store the MCP
// entity-query tool reads), replacing the MCP handshake + tools/call round-trip.
func (m *MemoryClient) ListMemories(ctx context.Context) ([]Memory, error) {
	var out struct {
		Items []GraphObject `json:"items"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/graph/objects/search?limit=100", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	mems := make([]Memory, 0, len(out.Items))
	for _, o := range out.Items {
		mems = append(mems, memoryFromParts(o.ID, o.Type, o.Properties))
	}
	return mems, nil
}

// memoryFromParts normalizes one memory entity (Note or typed) to the
// {ID, Content, Category, Confidence} display shape, mirroring admin.py's
// _memory_item + _memory_content. Note entities keep their note category;
// typed entities use their type as the category with confidence 1.0.
func memoryFromParts(id, objType string, props map[string]any) Memory {
	if objType == "Note" {
		return Memory{
			ID:         id,
			Content:    strAny(props["content"]),
			Category:   strAny(props["category"]),
			Confidence: floatAny(props["confidence"]),
		}
	}
	return Memory{
		ID:         id,
		Content:    memoryContent(objType, props),
		Category:   objType,
		Confidence: 1.0,
	}
}

// memoryContent builds a human-readable summary for a non-Note entity of the
// given type (parity with admin.py _memory_content).
func memoryContent(objType string, props map[string]any) string {
	fullName := func() string {
		parts := []string{}
		for _, k := range []string{"first_name", "last_name"} {
			if v := strAny(props[k]); v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, " ")
	}
	switch strings.ToLower(objType) {
	case "person":
		n := fullName()
		rel := strAny(props["relationship"])
		switch {
		case n != "" && rel != "":
			return n + " (" + rel + ")"
		case n != "":
			return n
		default:
			return objType
		}
	case "contact":
		if n := fullName(); n != "" {
			return n
		}
		return objType
	case "task":
		return strAny(props["title"])
	case "project":
		return strAny(props["name"])
	case "calendar_event":
		return strAny(props["summary"])
	case "financial_transaction":
		if v := strAny(props["payee"]); v != "" {
			return v
		}
		return strAny(props["source"])
	case "place":
		return strAny(props["name"])
	case "note":
		if v := strAny(props["title"]); v != "" {
			return v
		}
		return strAny(props["content"])
	case "habit":
		return strAny(props["name"])
	case "notecluster":
		return strAny(props["summary"])
	}
	for _, k := range []string{"name", "title", "summary", "content"} {
		if v := strAny(props[k]); v != "" {
			return v
		}
	}
	return objType
}

// strAny renders a map value as a string; nil and missing degrade to "".
func strAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// floatAny renders a map value as a float; nil, missing, and non-numeric
// values degrade to 0.
func floatAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
