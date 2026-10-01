package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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

	// IsArchived is the conversation's archive state; archived conversations
	// are hidden from the default list. ArchivedAt is when it was archived
	// (empty for an active conversation). Both are absent from a memory
	// response that predates the archive feature, so a conversation is treated
	// as active by default.
	IsArchived bool   `json:"isArchived"`
	ArchivedAt string `json:"archivedAt,omitempty"`

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

// ListConversations returns the caller's conversations, most recently updated
// first. Archived conversations are excluded unless includeArchived is set —
// the list surfaces (rail, Sessions page, agent "recent chats") use the default
// so archive hides a session, while statistics opt in so archiving never
// rewrites historical counts (design D6).
func (m *MemoryClient) ListConversations(ctx context.Context, includeArchived bool) (*ConversationList, error) {
	path := "/api/chat/conversations"
	if includeArchived {
		path += "?includeArchived=true"
	}
	var out ConversationList
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ArchiveConversation archives one conversation (a reversible hide). It is
// idempotent on memory's side and succeeds for any conversation the caller can
// access; a foreign or unknown id surfaces as not-found.
func (m *MemoryClient) ArchiveConversation(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, "/api/chat/"+url.PathEscape(id)+"/archive", nil, nil)
}

// UnarchiveConversation clears a conversation's archive state, making it
// visible in the default list again. Symmetric with ArchiveConversation.
func (m *MemoryClient) UnarchiveConversation(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, "/api/chat/"+url.PathEscape(id)+"/unarchive", nil, nil)
}

// DeleteConversation permanently deletes one conversation (its messages cascade
// on memory's side). Irreversible; a foreign or unknown id surfaces as
// not-found.
func (m *MemoryClient) DeleteConversation(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, "/api/chat/"+url.PathEscape(id), nil, nil)
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
