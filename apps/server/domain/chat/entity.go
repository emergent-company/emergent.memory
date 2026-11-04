package chat

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Conversation represents a chat conversation from kb.chat_conversations table
type Conversation struct {
	bun.BaseModel `bun:"table:kb.chat_conversations"`

	ID          uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	Title       string     `bun:"title,notnull" json:"title"`
	OwnerUserID *string    `bun:"owner_user_id,type:uuid" json:"ownerUserId,omitempty"`
	IsPrivate   bool       `bun:"is_private" json:"isPrivate"`
	ProjectID   *uuid.UUID `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	DraftText   *string    `bun:"draft_text" json:"draftText,omitempty"`

	// Object reference for refinement chats
	ObjectID    *uuid.UUID `bun:"object_id,type:uuid" json:"objectId,omitempty"`
	CanonicalID *uuid.UUID `bun:"canonical_id,type:uuid" json:"canonicalId,omitempty"`

	// Tool configuration
	EnabledTools []string `bun:"enabled_tools,array" json:"enabledTools,omitempty"`

	// Agent-backed chat: links conversation to an agent definition for tool-calling capabilities
	AgentDefinitionID *uuid.UUID `bun:"agent_definition_id,type:uuid" json:"agentDefinitionId,omitempty"`

	// SessionID links this conversation to its backing kb.sessions row.
	// Created on first agent-backed stream; reused on subsequent turns.
	SessionID *uuid.UUID `bun:"session_id,type:uuid" json:"sessionId,omitempty"`

	// ParentConversationID links a sub-chat to the conversation whose agent run
	// spawned it. It is empty for a user-started chat and for runs with no chat
	// parent (scheduled, board, direct API callers). The link is persisted when
	// the sub-chat is created so the rail can group it without re-deriving the
	// parent from run rows on every read.
	ParentConversationID *uuid.UUID `bun:"parent_conversation_id,type:uuid" json:"parentConversationId,omitempty"`

	// SpawnSource names the mechanism that opened the sub-chat (for example
	// "search-knowledge"). Informational: it lets the rail label a nested row
	// without needing the internal agent definition.
	SpawnSource *string `bun:"spawn_source" json:"spawnSource,omitempty"`

	// Archive state — non-destructive hide. IsArchived is the list filter
	// predicate; ArchivedAt is set on archive and cleared on unarchive.
	IsArchived bool       `bun:"is_archived,notnull,default:false" json:"isArchived"`
	ArchivedAt *time.Time `bun:"archived_at" json:"archivedAt,omitempty"`

	// Timestamps
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`

	// Relations (for eager loading)
	Messages []Message `bun:"rel:has-many,join:id=conversation_id" json:"messages,omitempty"`
}

// Message represents a chat message from kb.chat_messages table
type Message struct {
	bun.BaseModel `bun:"table:kb.chat_messages"`

	ID             uuid.UUID       `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ConversationID uuid.UUID       `bun:"conversation_id,type:uuid,notnull" json:"conversationId"`
	Role           string          `bun:"role,notnull" json:"role"` // user, assistant, system
	Content        string          `bun:"content,notnull" json:"content"`
	Citations      json.RawMessage `bun:"citations,type:jsonb" json:"citations,omitempty"`
	CreatedAt      time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	ContextSummary   *string         `bun:"context_summary" json:"contextSummary,omitempty"`
	RetrievalContext json.RawMessage `bun:"retrieval_context,type:jsonb" json:"retrievalContext,omitempty"`

	// Relation
	Conversation *Conversation `bun:"rel:belongs-to,join:conversation_id=id" json:"-"`
}

// MessageRole constants
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// ListConversationsParams contains parameters for listing conversations
type ListConversationsParams struct {
	ProjectID       string
	OwnerUserID     *string
	Limit           int
	Offset          int
	IncludeArchived bool
	// IncludeAncestors makes the list self-contained for grouping: when a page
	// contains a sub-chat whose parent falls outside the page (or outside the
	// limit), the visible ancestor chain is fetched too. The ancestors obey the
	// same project/owner/archive filters and are merged back by updated_at, so
	// the page is never longer than necessary but is always groupable.
	IncludeAncestors bool
}

// ListConversationsResult contains the result of listing conversations
type ListConversationsResult struct {
	Conversations []Conversation `json:"conversations"`
	Total         int            `json:"total"`
}

// CreateConversationRequest is the request body for creating a conversation
type CreateConversationRequest struct {
	Title       string  `json:"title" validate:"required,max=512"`
	Message     string  `json:"message" validate:"required,max=100000"`
	CanonicalID *string `json:"canonicalId,omitempty" validate:"omitempty,uuid"`

	// ParentConversationID and SpawnSource are set only by internal loopback
	// callers (QueryStream). json:"-" keeps them off the public create body: a
	// client must not be able to graft its conversation onto another one.
	ParentConversationID *string `json:"-" validate:"omitempty,uuid"`
	SpawnSource          *string `json:"-" validate:"omitempty,max=64"`
}

// UpdateConversationRequest is the request body for updating a conversation
type UpdateConversationRequest struct {
	Title     *string `json:"title,omitempty" validate:"omitempty,max=512"`
	DraftText *string `json:"draftText,omitempty" validate:"omitempty,max=100000"`
}

// AddMessageRequest is the request body for adding a message
type AddMessageRequest struct {
	Role             string          `json:"role" validate:"required,oneof=user assistant system"`
	Content          string          `json:"content" validate:"required,max=100000"`
	RetrievalContext json.RawMessage `json:"retrievalContext,omitempty"`
}

// ConversationWithMessages is the response when getting a conversation with messages
type ConversationWithMessages struct {
	Conversation
	Messages []Message `json:"messages"`
}

// Citation represents a citation in a message
type Citation struct {
	ChunkID    string  `json:"chunkId"`
	DocumentID string  `json:"documentId"`
	Content    string  `json:"content"`
	Score      float64 `json:"score,omitempty"`
}

// StreamRequest is the request body for starting a stream
type StreamRequest struct {
	ConversationID    *string `json:"conversationId,omitempty" validate:"omitempty,uuid"`
	Message           string  `json:"message" validate:"required,max=100000"`
	CanonicalID       *string `json:"canonicalId,omitempty" validate:"omitempty,uuid"`
	AgentDefinitionID *string `json:"agentDefinitionId,omitempty" validate:"omitempty,uuid"`
	Namespace         string  `json:"namespace,omitempty"`
}
