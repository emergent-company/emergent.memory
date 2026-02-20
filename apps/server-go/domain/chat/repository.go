package chat

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
	"github.com/emergent-company/emergent/pkg/pgutils"
)

// Repository handles chat database operations
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new chat repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("chat.repo")),
	}
}

// ListConversations retrieves conversations with pagination
func (r *Repository) ListConversations(ctx context.Context, params ListConversationsParams) (*ListConversationsResult, error) {
	// Default and max limits
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Build query with RLS context via project_id
	query := r.db.NewSelect().
		Model((*Conversation)(nil)).
		Where("project_id = ?", params.ProjectID)

	// Apply owner filter if provided
	if params.OwnerUserID != nil {
		query = query.Where("(owner_user_id = ? OR is_private = false)", *params.OwnerUserID)
	}

	// Get total count
	total, err := r.db.NewSelect().
		Model((*Conversation)(nil)).
		Where("project_id = ?", params.ProjectID).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count conversations: %w", err)
	}

	// Order by updated_at DESC (most recent first)
	query = query.Order("updated_at DESC").
		Offset(params.Offset).
		Limit(params.Limit)

	conversations := []Conversation{}
	if err := query.Scan(ctx, &conversations); err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}

	return &ListConversationsResult{
		Conversations: conversations,
		Total:         total,
	}, nil
}

// GetByID retrieves a conversation by ID
func (r *Repository) GetByID(ctx context.Context, projectID string, conversationID uuid.UUID) (*Conversation, error) {
	var conv Conversation
	err := r.db.NewSelect().
		Model(&conv).
		Where("id = ?", conversationID).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	return &conv, nil
}

// GetByIDWithMessages retrieves a conversation with all its messages
func (r *Repository) GetByIDWithMessages(ctx context.Context, projectID string, conversationID uuid.UUID) (*Conversation, error) {
	var conv Conversation
	err := r.db.NewSelect().
		Model(&conv).
		Relation("Messages", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Order("created_at ASC")
		}).
		Where("conversation.id = ?", conversationID).
		Where("conversation.project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation with messages: %w", err)
	}

	return &conv, nil
}

// GetByCanonicalID retrieves a conversation by canonical ID (for object refinement chats)
func (r *Repository) GetByCanonicalID(ctx context.Context, projectID string, canonicalID uuid.UUID) (*Conversation, error) {
	var conv Conversation
	err := r.db.NewSelect().
		Model(&conv).
		Where("canonical_id = ?", canonicalID).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation by canonical ID: %w", err)
	}

	return &conv, nil
}

// Create creates a new conversation
func (r *Repository) Create(ctx context.Context, conv *Conversation) error {
	_, err := r.db.NewInsert().
		Model(conv).
		Returning("*").
		Exec(ctx)

	if err != nil {
		if pgutils.IsUniqueViolation(err) {
			return apperror.New(409, "duplicate", "A conversation with this canonical ID already exists")
		}
		if pgutils.IsForeignKeyViolation(err) {
			return apperror.New(400, "invalid-reference", "Referenced project or object not found")
		}
		r.log.Error("failed to create conversation", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// Update updates a conversation
func (r *Repository) Update(ctx context.Context, projectID string, conv *Conversation) error {
	result, err := r.db.NewUpdate().
		Model(conv).
		WherePK().
		Where("project_id = ?", projectID).
		Returning("*").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to update conversation", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("Conversation not found")
	}

	return nil
}

// Delete deletes a conversation (cascades to messages via FK)
func (r *Repository) Delete(ctx context.Context, projectID string, conversationID uuid.UUID) (bool, error) {
	result, err := r.db.NewDelete().
		Model((*Conversation)(nil)).
		Where("id = ?", conversationID).
		Where("project_id = ?", projectID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete conversation", logger.Error(err))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// AddMessage adds a message to a conversation
func (r *Repository) AddMessage(ctx context.Context, msg *Message) error {
	_, err := r.db.NewInsert().
		Model(msg).
		Returning("*").
		Exec(ctx)

	if err != nil {
		if pgutils.IsForeignKeyViolation(err) {
			return apperror.ErrNotFound.WithMessage("Conversation not found")
		}
		r.log.Error("failed to add message", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	// Update conversation's updated_at
	_, err = r.db.NewUpdate().
		Model((*Conversation)(nil)).
		Set("updated_at = NOW()").
		Where("id = ?", msg.ConversationID).
		Exec(ctx)

	if err != nil {
		r.log.Warn("failed to update conversation timestamp", logger.Error(err))
		// Don't fail the operation for this
	}

	return nil
}

// GetMessages retrieves messages for a conversation
func (r *Repository) GetMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	messages := []Message{}
	err := r.db.NewSelect().
		Model(&messages).
		Where("conversation_id = ?", conversationID).
		Order("created_at ASC").
		Limit(limit).
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	return messages, nil
}

// CreateConversationWithMessage creates a conversation and its first message in a transaction
func (r *Repository) CreateConversationWithMessage(ctx context.Context, conv *Conversation, msg *Message) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Create conversation
		_, err := tx.NewInsert().
			Model(conv).
			Returning("*").
			Exec(ctx)
		if err != nil {
			if pgutils.IsUniqueViolation(err) {
				return apperror.New(409, "duplicate", "A conversation with this canonical ID already exists")
			}
			if pgutils.IsForeignKeyViolation(err) {
				return apperror.New(400, "invalid-reference", "Referenced project or object not found")
			}
			return apperror.ErrDatabase.WithInternal(err)
		}

		// Set conversation ID on message
		msg.ConversationID = conv.ID

		// Create message
		_, err = tx.NewInsert().
			Model(msg).
			Returning("*").
			Exec(ctx)
		if err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}

		return nil
	})
}

// GetConversationHistory retrieves the last N messages for a conversation
func (r *Repository) GetConversationHistory(ctx context.Context, conversationID uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	messages := []Message{}
	err := r.db.NewSelect().
		Model(&messages).
		Where("conversation_id = ?", conversationID).
		Order("created_at DESC").
		Limit(limit).
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("get conversation history: %w", err)
	}

	// Reverse to chronological order (oldest first)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// SetAgentDefinitionID updates the agent_definition_id on a conversation.
func (r *Repository) SetAgentDefinitionID(ctx context.Context, projectID string, conversationID uuid.UUID, agentDefID *uuid.UUID) error {
	_, err := r.db.NewUpdate().
		Model((*Conversation)(nil)).
		Set("agent_definition_id = ?", agentDefID).
		Set("updated_at = NOW()").
		Where("id = ?", conversationID).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}
