package chat

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ownerForCreate returns the owner id to persist and whether the new
// conversation is private.
//
// An ownerless caller is a device credential (`core.api_tokens.user_id` is
// NULL), so its user id is "". Binding that against the uuid owner column
// fails with SQLSTATE 22P02, and a private conversation with no owner would be
// invisible to everyone — including the caller that just created it. Such
// conversations are therefore created project-shared and unowned, the same
// visibility rule the ownerless read path applies.
func ownerForCreate(ownerUserID string, wantPrivate bool) (*string, bool) {
	if ownerUserID == "" {
		return nil, false
	}
	return &ownerUserID, wantPrivate
}

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

// ListConversations returns a paginated list of conversations for a project.
// Archived conversations are excluded by default; pass includeArchived to
// include them.
func (s *Service) ListConversations(ctx context.Context, projectID string, ownerUserID *string, limit, offset int, includeArchived, includeAncestors bool) (*ListConversationsResult, error) {
	return s.repo.ListConversations(ctx, ListConversationsParams{
		ProjectID:        projectID,
		OwnerUserID:      ownerUserID,
		Limit:            limit,
		Offset:           offset,
		IncludeArchived:  includeArchived,
		IncludeAncestors: includeAncestors,
	})
}

// GetConversation retrieves a conversation by ID
func (s *Service) GetConversation(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) (*Conversation, error) {
	conv, err := s.repo.GetByID(ctx, projectID, ownerUserID, conversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, apperror.ErrNotFound.WithMessage("Conversation not found")
	}
	return conv, nil
}

// CanAccessConversation reports whether the caller can see a conversation in
// the project. The spawn-parent resolution uses it so a caller-supplied run id
// cannot graft a sub-chat onto a conversation it cannot see.
func (s *Service) CanAccessConversation(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) bool {
	conv, err := s.GetConversation(ctx, projectID, ownerUserID, conversationID)
	return err == nil && conv != nil
}

// GetConversationWithMessages retrieves a conversation with all its messages
func (s *Service) GetConversationWithMessages(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) (*Conversation, error) {
	conv, err := s.repo.GetByIDWithMessages(ctx, projectID, ownerUserID, conversationID)
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
	var projectUUIDPtr *uuid.UUID
	if projectID != "" {
		parsed, err := uuid.Parse(projectID)
		if err != nil {
			return nil, apperror.New(400, "invalid-project-id", "Invalid project ID format")
		}
		projectUUIDPtr = &parsed
	}

	// Parse canonical ID if provided
	var canonicalID *uuid.UUID
	if req.CanonicalID != nil {
		parsed, err := uuid.Parse(*req.CanonicalID)
		if err != nil {
			return nil, apperror.New(400, "invalid-canonical-id", "Invalid canonical ID format")
		}
		canonicalID = &parsed

		// Check if a visible conversation already exists for this canonical ID.
		// The owner predicate prevents returning another user's private
		// conversation; canonical (refinement) conversations are project-shared
		// (is_private = false), so any project member resolves the same row.
		existing, err := s.repo.GetByCanonicalID(ctx, projectID, ownerUserID, parsed)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			// Return existing conversation instead of creating duplicate
			return existing, nil
		}
	}

	// Canonical (refinement) conversations are project-shared: the
	// globally-unique canonical_id index permits exactly one row per canonical
	// id, so a private per-user conversation would block every other project
	// member from the object's refinement chat.
	owner, isPrivate := ownerForCreate(ownerUserID, canonicalID == nil)

	var parentConversationID *uuid.UUID
	if req.ParentConversationID != nil && *req.ParentConversationID != "" {
		parsed, err := uuid.Parse(*req.ParentConversationID)
		if err != nil {
			return nil, apperror.New(400, "invalid-parent-conversation-id", "Invalid parent conversation ID format")
		}
		parentConversationID = &parsed
	}

	now := time.Now()
	conv := &Conversation{
		Title:                req.Title,
		OwnerUserID:          owner,
		IsPrivate:            isPrivate,
		ProjectID:            projectUUIDPtr,
		CanonicalID:          canonicalID,
		ParentConversationID: parentConversationID,
		SpawnSource:          req.SpawnSource,
		CreatedAt:            now,
		UpdatedAt:            now,
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
func (s *Service) UpdateConversation(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID, req UpdateConversationRequest) (*Conversation, error) {
	// First, get the existing conversation
	conv, err := s.repo.GetByID(ctx, projectID, ownerUserID, conversationID)
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

	if err := s.repo.Update(ctx, projectID, ownerUserID, conv); err != nil {
		return nil, err
	}

	return conv, nil
}

// DeleteConversation deletes a conversation together with its spawned
// sub-chats (and, via the messages FK, their messages). Deleting a parent is
// one action in the rail, so a surviving child would otherwise become an
// unexplained orphan.
func (s *Service) DeleteConversation(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) error {
	deleted, err := s.repo.DeleteCascade(ctx, projectID, ownerUserID, conversationID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperror.ErrNotFound.WithMessage("Conversation not found")
	}
	return nil
}

// ArchiveConversation marks a conversation and its spawned sub-chats as
// archived. Idempotent: archiving an already-archived conversation still
// succeeds. A hidden parent with visible children would leave the rail unable
// to group them, so the state change follows the subtree.
func (s *Service) ArchiveConversation(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) error {
	matched, err := s.repo.SetArchivedCascade(ctx, projectID, ownerUserID, conversationID, true)
	if err != nil {
		return err
	}
	if !matched {
		return apperror.New(http.StatusNotFound, "not_found", "Conversation not found")
	}
	return nil
}

// UnarchiveConversation clears the archive state on a conversation and its
// spawned sub-chats. Idempotent: unarchiving a conversation that is not
// archived still succeeds.
func (s *Service) UnarchiveConversation(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) error {
	matched, err := s.repo.SetArchivedCascade(ctx, projectID, ownerUserID, conversationID, false)
	if err != nil {
		return err
	}
	if !matched {
		return apperror.New(http.StatusNotFound, "not_found", "Conversation not found")
	}
	return nil
}

// AddMessage adds a message to a conversation
func (s *Service) AddMessage(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID, req AddMessageRequest) (*Message, error) {
	// Verify conversation exists
	conv, err := s.repo.GetByID(ctx, projectID, ownerUserID, conversationID)
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

	history, err := s.repo.GetConversationHistory(ctx, projectID, ownerUserID, conversationID, 5)
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

// SetAgentDefinitionID sets the agent_definition_id on a conversation.
func (s *Service) SetAgentDefinitionID(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID, agentDefID *uuid.UUID) error {
	return s.repo.SetAgentDefinitionID(ctx, projectID, ownerUserID, conversationID, agentDefID)
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
	existing, err := s.repo.GetByCanonicalID(ctx, projectID, ownerUserID, canonicalUUID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}

	// Create new. Refinement chats are shared, so an ownerless caller simply
	// leaves the owner empty rather than failing on the uuid column.
	owner, _ := ownerForCreate(ownerUserID, false)

	now := time.Now()
	conv := &Conversation{
		Title:       title,
		OwnerUserID: owner,
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
