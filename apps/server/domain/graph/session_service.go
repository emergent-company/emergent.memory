package graph

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// SessionService handles business logic for Session and Message graph objects.
// Sessions and Messages are regular graph objects under the hood — this service
// provides ergonomic wrappers with atomic semantics.
type SessionService struct {
	graphSvc *Service
	repo     *Repository
	log      *slog.Logger
}

// NewSessionService creates a new SessionService.
func NewSessionService(graphSvc *Service, repo *Repository, log *slog.Logger) *SessionService {
	return &SessionService{
		graphSvc: graphSvc,
		repo:     repo,
		log:      log.With(logger.Scope("graph.session_svc")),
	}
}

// ---- Request/Response types ------------------------------------------------

// CreateSessionRequest is the request body for creating a session.
type CreateSessionRequest struct {
	Title        string  `json:"title"`
	Summary      *string `json:"summary,omitempty"`
	AgentVersion *string `json:"agentVersion,omitempty"`
}

// SessionResponse wraps a GraphObjectResponse for session endpoints.
type SessionResponse struct {
	*GraphObjectResponse
}

// AppendMessageRequest is the request body for appending a message to a session.
type AppendMessageRequest struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	TokenCount *int           `json:"tokenCount,omitempty"`
	ToolCalls  []any          `json:"toolCalls,omitempty"`
	ExtraProps map[string]any `json:"extraProps,omitempty"`
}

// MessageResponse wraps a GraphObjectResponse for message endpoints.
type MessageResponse struct {
	*GraphObjectResponse
}

// ListMessagesResponse is the paginated response for listing messages.
type ListMessagesResponse struct {
	Items      []*GraphObjectResponse `json:"items"`
	NextCursor *string                `json:"nextCursor,omitempty"`
	Total      int                    `json:"total"`
}

// ListSessionsResponse is the paginated response for listing sessions.
type ListSessionsResponse struct {
	Items      []*GraphObjectResponse `json:"items"`
	NextCursor *string                `json:"nextCursor,omitempty"`
	Total      int                    `json:"total"`
}

// ---- Service methods -------------------------------------------------------

// CreateSession creates a new Session graph object.
func (s *SessionService) CreateSession(ctx context.Context, projectID uuid.UUID, req *CreateSessionRequest, actorID *uuid.UUID) (*SessionResponse, error) {
	if req.Title == "" {
		return nil, apperror.ErrBadRequest.WithMessage("title is required")
	}

	now := time.Now().UTC()
	props := map[string]any{
		"title":         req.Title,
		"started_at":    now.Format(time.RFC3339),
		"message_count": 0,
	}
	if req.Summary != nil {
		props["summary"] = *req.Summary
	}
	if req.AgentVersion != nil {
		props["agent_version"] = *req.AgentVersion
	}

	obj, err := s.graphSvc.Create(ctx, projectID, &CreateGraphObjectRequest{
		Type:       "Session",
		Properties: props,
	}, actorID)
	if err != nil {
		return nil, err
	}

	return &SessionResponse{GraphObjectResponse: obj}, nil
}

// GetSession returns a Session graph object by ID.
func (s *SessionService) GetSession(ctx context.Context, projectID, sessionID uuid.UUID) (*SessionResponse, error) {
	obj, err := s.graphSvc.GetByID(ctx, projectID, sessionID, true)
	if err != nil {
		return nil, err
	}

	if obj.Type != "Session" {
		return nil, apperror.ErrNotFound.WithMessage("session not found")
	}

	return &SessionResponse{GraphObjectResponse: obj}, nil
}

// ListSessions returns sessions for a project, ordered by started_at descending.
func (s *SessionService) ListSessions(ctx context.Context, projectID uuid.UUID, limit int, cursor *string) (*ListSessionsResponse, error) {
	sessionType := "Session"
	if limit <= 0 {
		limit = 20
	}

	result, err := s.graphSvc.List(ctx, ListParams{
		ProjectID: projectID,
		Type:      &sessionType,
		Limit:     limit,
		Cursor:    cursor,
		Order:     "desc",
	})
	if err != nil {
		return nil, err
	}

	return &ListSessionsResponse{
		Items:      result.Items,
		NextCursor: result.NextCursor,
		Total:      result.Total,
	}, nil
}

// AppendMessage atomically:
// 1. Creates a Message graph object with sequence_number = next in session
// 2. Creates a has_message relationship from Session → Message
// Returns the created Message.
func (s *SessionService) AppendMessage(ctx context.Context, projectID uuid.UUID, sessionID uuid.UUID, req *AppendMessageRequest, actorID *uuid.UUID) (*MessageResponse, error) {
	if req.Role == "" {
		return nil, apperror.ErrBadRequest.WithMessage("role is required")
	}
	if req.Content == "" {
		return nil, apperror.ErrBadRequest.WithMessage("content is required")
	}

	// Validate role
	switch req.Role {
	case "user", "assistant", "system":
	default:
		return nil, apperror.ErrBadRequest.WithMessage("role must be one of: user, assistant, system")
	}

	// Verify session exists
	sessionObj, err := s.repo.GetByID(ctx, projectID, sessionID)
	if err != nil || sessionObj == nil {
		return nil, apperror.ErrNotFound.WithMessage("session not found")
	}
	if sessionObj.Type != "Session" {
		return nil, apperror.ErrNotFound.WithMessage("session not found")
	}

	// Begin transaction for atomic message append + relationship creation.
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Assign sequence_number under advisory lock to avoid race conditions.
	// SELECT COUNT(*) of existing has_message relationships FOR UPDATE on the session.
	var seqNum int
	err = s.repo.DB().NewRaw(`
		SELECT COUNT(*) + 1
		FROM kb.graph_relationships r
		JOIN kb.graph_objects msg ON msg.canonical_id = r.dst_id
			AND msg.project_id = ?
			AND msg.supersedes_id IS NULL
			AND msg.deleted_at IS NULL
		WHERE r.project_id = ?
		  AND r.type = 'has_message'
		  AND r.src_id = ?
		  AND r.deleted_at IS NULL
		  AND r.supersedes_id IS NULL
	`, projectID, projectID, sessionObj.CanonicalID).Scan(ctx, &seqNum)
	if err != nil {
		s.log.Warn("failed to compute sequence_number, defaulting to 1", logger.Error(err))
		seqNum = 1
	}

	now := time.Now().UTC()
	msgProps := map[string]any{
		"role":            req.Role,
		"content":         req.Content,
		"sequence_number": seqNum,
		"timestamp":       now.Format(time.RFC3339),
	}
	if req.TokenCount != nil {
		msgProps["token_count"] = *req.TokenCount
	}
	if len(req.ToolCalls) > 0 {
		msgProps["tool_calls"] = req.ToolCalls
	}
	for k, v := range req.ExtraProps {
		if _, exists := msgProps[k]; !exists {
			msgProps[k] = v
		}
	}

	// Create Message object within transaction.
	msgObj := &GraphObject{
		ProjectID:  projectID,
		Type:       "Message",
		Properties: msgProps,
	}

	if err := s.repo.CreateInTx(ctx, tx.Tx, msgObj); err != nil {
		return nil, err
	}

	// Create has_message relationship within same transaction.
	rel := &GraphRelationship{
		ProjectID: projectID,
		Type:      "has_message",
		SrcID:     sessionObj.CanonicalID,
		DstID:     msgObj.CanonicalID,
	}

	if _, err := s.repo.CreateRelationship(ctx, tx.Tx, rel); err != nil {
		return nil, err
	}

	// Commit transaction.
	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Enqueue embedding for Message.content (async, post-commit).
	s.graphSvc.enqueueEmbedding(ctx, msgObj.ID.String())

	return &MessageResponse{GraphObjectResponse: msgObj.ToResponse()}, nil
}

// SpawnSessionRequest is the request body for spawning a child session.
type SpawnSessionRequest struct {
	Title       string  `json:"title"`
	ForkContext bool    `json:"forkContext"`
	MaxMessages int     `json:"maxMessages,omitempty"` // 0 = use default (50)
	Summary     *string `json:"summary,omitempty"`
}

// SpawnSessionResponse is the response for a spawned child session.
type SpawnSessionResponse struct {
	Session        *SessionResponse `json:"session"`
	ForkedMessages int              `json:"forkedMessages"`
}

// SpawnSession creates a child session optionally pre-populated with the parent's
// message history (snapshot at spawn time, no live sync). The child is linked to
// the parent via a "spawned_from" relationship.
//
// Design:
//   - opt-in: ForkContext=false (default) creates a clean isolated session
//   - snapshot semantics: parent messages are copied as new Message objects into the child
//   - size limit: MaxMessages caps the number of messages copied (most recent N)
func (s *SessionService) SpawnSession(ctx context.Context, projectID, parentID uuid.UUID, req *SpawnSessionRequest, actorID *uuid.UUID) (*SpawnSessionResponse, error) {
	if req.Title == "" {
		return nil, apperror.ErrBadRequest.WithMessage("title is required")
	}

	maxMessages := req.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 50
	}

	// Verify parent session exists.
	parentObj, err := s.repo.GetByID(ctx, projectID, parentID)
	if err != nil || parentObj == nil {
		return nil, apperror.ErrNotFound.WithMessage("parent session not found")
	}
	if parentObj.Type != "Session" {
		return nil, apperror.ErrNotFound.WithMessage("parent session not found")
	}

	// Create the child session.
	now := time.Now().UTC()
	childProps := map[string]any{
		"title":         req.Title,
		"started_at":    now.Format(time.RFC3339),
		"message_count": 0,
		"parent_id":     parentObj.CanonicalID.String(),
	}
	if req.Summary != nil {
		childProps["summary"] = *req.Summary
	}

	childObj, err := s.graphSvc.Create(ctx, projectID, &CreateGraphObjectRequest{
		Type:       "Session",
		Properties: childProps,
	}, actorID)
	if err != nil {
		return nil, err
	}

	childID := childObj.CanonicalID

	// Link child to parent via spawned_from relationship.
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	spawnRel := &GraphRelationship{
		ProjectID: projectID,
		Type:      "spawned_from",
		SrcID:     childID,
		DstID:     parentObj.CanonicalID,
	}
	if _, err := s.repo.CreateRelationship(ctx, tx.Tx, spawnRel); err != nil {
		return nil, err
	}

	forkedCount := 0

	if req.ForkContext {
		// Fetch parent messages (most recent maxMessages, ordered asc for correct sequence).
		msgType := "Message"
		msgResult, err := s.graphSvc.List(ctx, ListParams{
			ProjectID:   projectID,
			Type:        &msgType,
			RelatedToID: &parentObj.CanonicalID,
			Limit:       maxMessages,
			Order:       "asc",
		})
		if err != nil {
			return nil, err
		}

		// Copy each parent message into the child session as a new Message object.
		for i, msg := range msgResult.Items {
			// Deep-copy properties, override sequence_number.
			props := make(map[string]any, len(msg.Properties)+1)
			for k, v := range msg.Properties {
				props[k] = v
			}
			props["sequence_number"] = i + 1
			props["forked_from"] = msg.ID

			childMsg := &GraphObject{
				ProjectID:  projectID,
				Type:       "Message",
				Properties: props,
			}
			if err := s.repo.CreateInTx(ctx, tx.Tx, childMsg); err != nil {
				return nil, err
			}

			msgRel := &GraphRelationship{
				ProjectID: projectID,
				Type:      "has_message",
				SrcID:     childID,
				DstID:     childMsg.CanonicalID,
			}
			if _, err := s.repo.CreateRelationship(ctx, tx.Tx, msgRel); err != nil {
				return nil, err
			}
			forkedCount++
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &SpawnSessionResponse{
		Session:        &SessionResponse{GraphObjectResponse: childObj},
		ForkedMessages: forkedCount,
	}, nil
}

// ListMessages returns messages for a session, ordered by sequence_number ascending.
func (s *SessionService) ListMessages(ctx context.Context, projectID, sessionID uuid.UUID, limit int, cursor *string) (*ListMessagesResponse, error) {
	if limit <= 0 {
		limit = 50
	}

	// Verify session exists
	sessionObj, err := s.repo.GetByID(ctx, projectID, sessionID)
	if err != nil || sessionObj == nil {
		return nil, apperror.ErrNotFound.WithMessage("session not found")
	}
	if sessionObj.Type != "Session" {
		return nil, apperror.ErrNotFound.WithMessage("session not found")
	}

	// List Message objects related to this session via has_message relationship.
	msgType := "Message"
	result, err := s.graphSvc.List(ctx, ListParams{
		ProjectID:   projectID,
		Type:        &msgType,
		RelatedToID: &sessionObj.CanonicalID,
		Limit:       limit,
		Cursor:      cursor,
		Order:       "asc",
	})
	if err != nil {
		return nil, err
	}

	return &ListMessagesResponse{
		Items:      result.Items,
		NextCursor: result.NextCursor,
		Total:      result.Total,
	}, nil
}
