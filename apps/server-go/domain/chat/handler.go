package chat

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/llm/vertex"
	"github.com/emergent/emergent-core/pkg/sse"
)

// Handler handles chat HTTP requests
type Handler struct {
	svc       *Service
	llmClient *vertex.Client
}

// NewHandler creates a new chat handler
func NewHandler(svc *Service, llmClient *vertex.Client) *Handler {
	return &Handler{svc: svc, llmClient: llmClient}
}

// ListConversations handles GET /api/v2/chat/conversations
func (h *Handler) ListConversations(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	// Parse query parameters
	limit := 50
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed < 1 || parsed > 100 {
			return apperror.ErrBadRequest.WithMessage("limit must be between 1 and 100")
		}
		limit = parsed
	}

	offset := 0
	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		parsed, err := strconv.Atoi(offsetStr)
		if err != nil || parsed < 0 {
			return apperror.ErrBadRequest.WithMessage("offset must be a non-negative integer")
		}
		offset = parsed
	}

	// Pass user ID for filtering private conversations (user.ID is the UUID from user_profiles)
	result, err := h.svc.ListConversations(c.Request().Context(), user.ProjectID, &user.ID, limit, offset)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetConversation handles GET /api/v2/chat/:id
func (h *Handler) GetConversation(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid conversation id")
	}

	// Get conversation with messages
	conv, err := h.svc.GetConversationWithMessages(c.Request().Context(), user.ProjectID, conversationID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, conv)
}

// CreateConversation handles POST /api/v2/chat/conversations
func (h *Handler) CreateConversation(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	var req CreateConversationRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate
	if err := validateCreateConversationRequest(&req); err != nil {
		return err
	}

	// user.ID is the UUID from user_profiles, not user.Sub (Zitadel ID)
	conv, err := h.svc.CreateConversation(c.Request().Context(), user.ProjectID, user.ID, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, conv)
}

// UpdateConversation handles PATCH /api/v2/chat/:id
func (h *Handler) UpdateConversation(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid conversation id")
	}

	var req UpdateConversationRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate
	if err := validateUpdateConversationRequest(&req); err != nil {
		return err
	}

	conv, err := h.svc.UpdateConversation(c.Request().Context(), user.ProjectID, conversationID, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, conv)
}

// DeleteConversation handles DELETE /api/v2/chat/:id
func (h *Handler) DeleteConversation(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid conversation id")
	}

	if err := h.svc.DeleteConversation(c.Request().Context(), user.ProjectID, conversationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// AddMessage handles POST /api/v2/chat/:id/messages
func (h *Handler) AddMessage(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid conversation id")
	}

	var req AddMessageRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate
	if err := validateAddMessageRequest(&req); err != nil {
		return err
	}

	msg, err := h.svc.AddMessage(c.Request().Context(), user.ProjectID, conversationID, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, msg)
}

// Validation helpers

func validateCreateConversationRequest(req *CreateConversationRequest) error {
	if req.Title == "" {
		return apperror.ErrBadRequest.WithMessage("title is required")
	}
	if len(req.Title) > 512 {
		return apperror.ErrBadRequest.WithMessage("title must be at most 512 characters")
	}
	if req.Message == "" {
		return apperror.ErrBadRequest.WithMessage("message is required")
	}
	if len(req.Message) > 100000 {
		return apperror.ErrBadRequest.WithMessage("message must be at most 100000 characters")
	}
	if req.CanonicalID != nil {
		if _, err := uuid.Parse(*req.CanonicalID); err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid canonicalId format")
		}
	}
	return nil
}

func validateUpdateConversationRequest(req *UpdateConversationRequest) error {
	if req.Title != nil && len(*req.Title) > 512 {
		return apperror.ErrBadRequest.WithMessage("title must be at most 512 characters")
	}
	if req.DraftText != nil && len(*req.DraftText) > 100000 {
		return apperror.ErrBadRequest.WithMessage("draftText must be at most 100000 characters")
	}
	return nil
}

func validateAddMessageRequest(req *AddMessageRequest) error {
	validRoles := map[string]bool{
		RoleUser:      true,
		RoleAssistant: true,
		RoleSystem:    true,
	}
	if !validRoles[req.Role] {
		return apperror.ErrBadRequest.WithMessage("role must be one of: user, assistant, system")
	}
	if req.Content == "" {
		return apperror.ErrBadRequest.WithMessage("content is required")
	}
	if len(req.Content) > 100000 {
		return apperror.ErrBadRequest.WithMessage("content must be at most 100000 characters")
	}
	return nil
}

func validateStreamRequest(req *StreamRequest) error {
	if strings.TrimSpace(req.Message) == "" {
		return apperror.ErrBadRequest.WithMessage("message is required")
	}
	if len(req.Message) > 100000 {
		return apperror.ErrBadRequest.WithMessage("message must be at most 100000 characters")
	}
	if req.ConversationID != nil {
		if _, err := uuid.Parse(*req.ConversationID); err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid conversationId format")
		}
	}
	if req.CanonicalID != nil {
		if _, err := uuid.Parse(*req.CanonicalID); err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid canonicalId format")
		}
	}
	return nil
}

// StreamChat handles POST /api/v2/chat/stream
// This is the SSE streaming endpoint for chat completions
func (h *Handler) StreamChat(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	// Parse and validate request BEFORE setting SSE headers
	// This allows us to return proper JSON errors for bad requests
	var req StreamRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if err := validateStreamRequest(&req); err != nil {
		return err
	}

	ctx := c.Request().Context()
	message := strings.TrimSpace(req.Message)

	// Get or create conversation
	var convID uuid.UUID
	if req.ConversationID != nil {
		// Use existing conversation
		parsed, _ := uuid.Parse(*req.ConversationID) // Already validated
		conv, err := h.svc.GetConversation(ctx, user.ProjectID, parsed)
		if err != nil {
			return err
		}
		convID = conv.ID

		// Persist the user message
		_, err = h.svc.AddMessage(ctx, user.ProjectID, convID, AddMessageRequest{
			Role:    RoleUser,
			Content: message,
		})
		if err != nil {
			return err
		}
	} else {
		// Create new conversation
		title := message
		if len(title) > 50 {
			title = title[:50] + "..."
		}

		createReq := CreateConversationRequest{
			Title:       title,
			Message:     message,
			CanonicalID: req.CanonicalID,
		}
		conv, err := h.svc.CreateConversation(ctx, user.ProjectID, user.ID, createReq)
		if err != nil {
			return err
		}
		convID = conv.ID
	}

	// Now that validation is done and conversation is ready, start SSE streaming
	w := c.Response().Writer
	sseWriter := sse.NewWriter(w)
	if err := sseWriter.Start(); err != nil {
		return apperror.ErrInternal.WithMessage("failed to start SSE stream")
	}

	// Emit meta event first
	metaEvent := sse.NewMetaEvent(convID.String())
	if err := sseWriter.WriteData(metaEvent); err != nil {
		// SSE already started, can't return error - just log and continue
		return nil
	}

	// Check for deterministic test mode
	if os.Getenv("CHAT_TEST_DETERMINISTIC") == "1" {
		// Emit synthetic tokens for testing
		for i := 0; i < 5; i++ {
			sseWriter.WriteData(sse.NewTokenEvent("token-" + strconv.Itoa(i)))
			if i < 4 {
				sseWriter.WriteData(sse.NewTokenEvent(" "))
			}
		}
		sseWriter.WriteData(sse.NewDoneEvent())
		sseWriter.Close()
		return nil
	}

	// Check if LLM client is available
	if h.llmClient == nil || !h.llmClient.IsAvailable() {
		// Emit error and synthetic response
		sseWriter.WriteData(sse.NewErrorEvent("LLM service not configured"))
		sseWriter.WriteData(sse.NewTokenEvent("I'm sorry, but the chat service is not currently available. Please try again later."))
		sseWriter.WriteData(sse.NewDoneEvent())
		sseWriter.Close()
		return nil
	}

	// Build prompt
	systemPrompt := os.Getenv("CHAT_SYSTEM_PROMPT")
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant specialized in knowledge graphs and data schemas. Answer questions clearly using markdown formatting."
	}

	// Stream tokens from LLM
	var fullResponse strings.Builder
	err := h.llmClient.GenerateStreaming(ctx, vertex.GenerateRequest{
		Prompt:       message,
		SystemPrompt: systemPrompt,
	}, func(token string) {
		fullResponse.WriteString(token)
		sseWriter.WriteData(sse.NewTokenEvent(token))
	})

	if err != nil {
		// Emit error event
		sseWriter.WriteData(sse.NewErrorEvent(err.Error()))
	} else {
		// Persist assistant response
		go func() {
			// Use a background context since the request context may be cancelled
			_, _ = h.svc.AddMessage(ctx, user.ProjectID, convID, AddMessageRequest{
				Role:    RoleAssistant,
				Content: fullResponse.String(),
			})
		}()
	}

	// Emit done event
	sseWriter.WriteData(sse.NewDoneEvent())
	sseWriter.Close()

	return nil
}
