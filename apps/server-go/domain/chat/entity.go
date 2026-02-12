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
	IsPrivate   bool       `bun:"is_private,default:true" json:"isPrivate"`
	ProjectID   *uuid.UUID `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	DraftText   *string    `bun:"draft_text" json:"draftText,omitempty"`

	// Object reference for refinement chats
	ObjectID    *uuid.UUID `bun:"object_id,type:uuid" json:"objectId,omitempty"`
	CanonicalID *uuid.UUID `bun:"canonical_id,type:uuid" json:"canonicalId,omitempty"`

	// Tool configuration
	EnabledTools []string `bun:"enabled_tools,array" json:"enabledTools,omitempty"`

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
	ProjectID   string
	OwnerUserID *string
	Limit       int
	Offset      int
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
}

// UpdateConversationRequest is the request body for updating a conversation
type UpdateConversationRequest struct {
	Title     *string `json:"title,omitempty" validate:"omitempty,max=512"`
	DraftText *string `json:"draftText,omitempty" validate:"omitempty,max=100000"`
}

// AddMessageRequest is the request body for adding a message
type AddMessageRequest struct {
	Role    string `json:"role" validate:"required,oneof=user assistant system"`
	Content string `json:"content" validate:"required,max=100000"`
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
	ConversationID *string `json:"conversationId,omitempty" validate:"omitempty,uuid"`
	Message        string  `json:"message" validate:"required,max=100000"`
	CanonicalID    *string `json:"canonicalId,omitempty" validate:"omitempty,uuid"`
}
