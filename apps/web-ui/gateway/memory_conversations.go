package main

import (
	"context"
	"fmt"
	"net/http"
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

	// Run-control state the gateway derives for the session rail badge (not
	// present on memory's list response — populated by chatRailData). json:"-"
	// keeps them out of the /api/conversations response.
	Bucket           string `json:"-"`
	PendingApprovals int    `json:"-"`
	PendingQuestions int    `json:"-"`
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

// minSearchScore is the relevance floor for unified-search results. The memory
// service returns every entity ranked by score (0..1); with a small dataset
// totally unrelated notes score ~0.036–0.056 while a real match scores
// ~0.168, so 0.08 cleanly separates signal from noise. Tunable: raise it to
// require stronger matches, lower it to return more results.
const minSearchScore = 0.08

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

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
