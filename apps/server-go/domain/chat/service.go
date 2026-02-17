package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Service provides business logic for chat operations
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new chat service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("chat.svc")),
	}
}

// ListConversations returns a paginated list of conversations for a project
func (s *Service) ListConversations(ctx context.Context, projectID string, ownerUserID *string, limit, offset int) (*ListConversationsResult, error) {
	return s.repo.ListConversations(ctx, ListConversationsParams{
		ProjectID:   projectID,
		OwnerUserID: ownerUserID,
		Limit:       limit,
		Offset:      offset,
	})
}

// GetConversation retrieves a conversation by ID
func (s *Service) GetConversation(ctx context.Context, projectID string, conversationID uuid.UUID) (*Conversation, error) {
	conv, err := s.repo.GetByID(ctx, projectID, conversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, apperror.ErrNotFound.WithMessage("Conversation not found")
	}
	return conv, nil
}

// GetConversationWithMessages retrieves a conversation with all its messages
func (s *Service) GetConversationWithMessages(ctx context.Context, projectID string, conversationID uuid.UUID) (*Conversation, error) {
	conv, err := s.repo.GetByIDWithMessages(ctx, projectID, conversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, apperror.ErrNotFound.WithMessage("Conversation not found")
	}
	return conv, nil
}

// CreateConversation creates a new conversation with an initial message
func (s *Service) CreateConversation(ctx context.Context, projectID, ownerUserID string, req CreateConversationRequest) (*Conversation, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.New(400, "invalid-project-id", "Invalid project ID format")
	}

	// Parse canonical ID if provided
	var canonicalID *uuid.UUID
	if req.CanonicalID != nil {
		parsed, err := uuid.Parse(*req.CanonicalID)
		if err != nil {
			return nil, apperror.New(400, "invalid-canonical-id", "Invalid canonical ID format")
		}
		canonicalID = &parsed

		// Check if conversation already exists for this canonical ID
		existing, err := s.repo.GetByCanonicalID(ctx, projectID, parsed)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			// Return existing conversation instead of creating duplicate
			return existing, nil
		}
	}

	now := time.Now()
	conv := &Conversation{
		Title:       req.Title,
		OwnerUserID: &ownerUserID,
		IsPrivate:   true,
		ProjectID:   &projectUUID,
		CanonicalID: canonicalID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Create the initial user message
	msg := &Message{
		Role:      RoleUser,
		Content:   req.Message,
		CreatedAt: now,
	}

	if err := s.repo.CreateConversationWithMessage(ctx, conv, msg); err != nil {
		return nil, err
	}

	// Return conversation with the initial message
	conv.Messages = []Message{*msg}
	return conv, nil
}

// UpdateConversation updates a conversation's title or draft text
func (s *Service) UpdateConversation(ctx context.Context, projectID string, conversationID uuid.UUID, req UpdateConversationRequest) (*Conversation, error) {
	// First, get the existing conversation
	conv, err := s.repo.GetByID(ctx, projectID, conversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, apperror.ErrNotFound.WithMessage("Conversation not found")
	}

	// Apply updates
	if req.Title != nil {
		conv.Title = *req.Title
	}
	if req.DraftText != nil {
		conv.DraftText = req.DraftText
	}
	conv.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, projectID, conv); err != nil {
		return nil, err
	}

	return conv, nil
}

// DeleteConversation deletes a conversation and all its messages
func (s *Service) DeleteConversation(ctx context.Context, projectID string, conversationID uuid.UUID) error {
	deleted, err := s.repo.Delete(ctx, projectID, conversationID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperror.ErrNotFound.WithMessage("Conversation not found")
	}
	return nil
}

// AddMessage adds a message to a conversation
func (s *Service) AddMessage(ctx context.Context, projectID string, conversationID uuid.UUID, req AddMessageRequest) (*Message, error) {
	// Verify conversation exists
	conv, err := s.repo.GetByID(ctx, projectID, conversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, apperror.ErrNotFound.WithMessage("Conversation not found")
	}

	msg := &Message{
		ConversationID:   conversationID,
		Role:             req.Role,
		Content:          req.Content,
		RetrievalContext: req.RetrievalContext,
		CreatedAt:        time.Now(),
	}

	history, err := s.repo.GetConversationHistory(ctx, conversationID, 5)
	if err != nil {
		s.log.Warn("failed to load conversation history", logger.Error(err))
	} else if len(history) > 0 {
		summary := s.buildContextSummary(history)
		msg.ContextSummary = &summary
	}

	if err := s.repo.AddMessage(ctx, msg); err != nil {
		return nil, err
	}

	return msg, nil
}

func (s *Service) buildContextSummary(history []Message) string {
	if len(history) == 0 {
		return ""
	}

	summary := "Previous conversation:\n"
	for _, msg := range history {
		summary += msg.Role + ": " + msg.Content + "\n"
	}
	return summary
}

// GetOrCreateConversation gets an existing conversation by canonical ID or creates a new one
func (s *Service) GetOrCreateConversation(ctx context.Context, projectID, ownerUserID string, canonicalID string, title string) (*Conversation, bool, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, false, apperror.New(400, "invalid-project-id", "Invalid project ID format")
	}

	canonicalUUID, err := uuid.Parse(canonicalID)
	if err != nil {
		return nil, false, apperror.New(400, "invalid-canonical-id", "Invalid canonical ID format")
	}

	// Try to get existing
	existing, err := s.repo.GetByCanonicalID(ctx, projectID, canonicalUUID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}

	// Create new
	now := time.Now()
	conv := &Conversation{
		Title:       title,
		OwnerUserID: &ownerUserID,
		IsPrivate:   false, // Refinement chats are shared
		ProjectID:   &projectUUID,
		CanonicalID: &canonicalUUID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.Create(ctx, conv); err != nil {
		return nil, false, err
	}

	return conv, true, nil
}
