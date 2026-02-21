package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/domain/agents"
	"github.com/emergent-company/emergent/domain/search"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
	"github.com/emergent-company/emergent/pkg/llm/vertex"
	"github.com/emergent-company/emergent/pkg/logger"
	"github.com/emergent-company/emergent/pkg/sse"
)

// Handler handles chat HTTP requests
type Handler struct {
	svc           *Service
	llmClient     *vertex.Client
	searchSvc     *search.Service
	agentExecutor *agents.AgentExecutor
	agentRepo     *agents.Repository
	log           *slog.Logger
}

// NewHandler creates a new chat handler
func NewHandler(svc *Service, llmClient *vertex.Client, searchSvc *search.Service, agentExecutor *agents.AgentExecutor, agentRepo *agents.Repository, log *slog.Logger) *Handler {
	return &Handler{
		svc:           svc,
		llmClient:     llmClient,
		searchSvc:     searchSvc,
		agentExecutor: agentExecutor,
		agentRepo:     agentRepo,
		log:           log.With(logger.Scope("chat.handler")),
	}
}

// ListConversations handles GET /api/chat/conversations
// @Summary      List chat conversations
// @Description  Returns all chat conversations for the current project with pagination support
// @Tags         chat
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        limit query int false "Max results (1-100, default 50)" minimum(1) maximum(100)
// @Param        offset query int false "Offset for pagination" minimum(0)
// @Success      200 {object} ListConversationsResult "List of conversations"
// @Failure      400 {object} apperror.Error "Invalid parameters"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/conversations [get]
// @Security     bearerAuth
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

// GetConversation handles GET /api/chat/:id
// @Summary      Get conversation with messages
// @Description  Returns a single conversation with all its messages
// @Tags         chat
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Conversation ID (UUID)"
// @Success      200 {object} ConversationWithMessages "Conversation with messages"
// @Failure      400 {object} apperror.Error "Invalid conversation ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Conversation not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/{id} [get]
// @Security     bearerAuth
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

// CreateConversation handles POST /api/chat/conversations
// @Summary      Create conversation
// @Description  Creates a new chat conversation with an initial message
// @Tags         chat
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        request body CreateConversationRequest true "Conversation creation request"
// @Success      201 {object} Conversation "Conversation created"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/conversations [post]
// @Security     bearerAuth
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

// UpdateConversation handles PATCH /api/chat/:id
// @Summary      Update conversation
// @Description  Updates conversation properties (title, draft text)
// @Tags         chat
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Conversation ID (UUID)"
// @Param        request body UpdateConversationRequest true "Update request"
// @Success      200 {object} Conversation "Updated conversation"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Conversation not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/{id} [patch]
// @Security     bearerAuth
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

// DeleteConversation handles DELETE /api/chat/:id
// @Summary      Delete conversation
// @Description  Permanently deletes a conversation and all its messages
// @Tags         chat
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Conversation ID (UUID)"
// @Success      200 {object} map[string]string "Deletion status"
// @Failure      400 {object} apperror.Error "Invalid conversation ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Conversation not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/{id} [delete]
// @Security     bearerAuth
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

// AddMessage handles POST /api/chat/:id/messages
// @Summary      Add message to conversation
// @Description  Adds a new message to an existing conversation
// @Tags         chat
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Conversation ID (UUID)"
// @Param        request body AddMessageRequest true "Message content"
// @Success      201 {object} Message "Message created"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Conversation not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/{id}/messages [post]
// @Security     bearerAuth
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
	if req.AgentDefinitionID != nil {
		if _, err := uuid.Parse(*req.AgentDefinitionID); err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid agentDefinitionId format")
		}
	}
	return nil
}

// StreamChat handles POST /api/chat/stream
// This is the SSE streaming endpoint for chat completions
// @Summary      Stream chat completion
// @Description  Streams AI chat responses using Server-Sent Events (SSE). Creates or continues a conversation with streaming token delivery.
// @Tags         chat
// @Accept       json
// @Produce      text/event-stream
// @Param        X-Project-ID header string true "Project ID"
// @Param        request body StreamRequest true "Stream request"
// @Success      200 {string} string "SSE stream of tokens"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/stream [post]
// @Security     bearerAuth
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

	// If agentDefinitionId is provided on a new conversation, validate it exists
	var agentDefID *uuid.UUID
	if req.AgentDefinitionID != nil && req.ConversationID == nil {
		parsed, _ := uuid.Parse(*req.AgentDefinitionID) // Already validated format
		def, err := h.agentRepo.FindDefinitionByID(ctx, parsed.String(), &user.ProjectID)
		if err != nil {
			return apperror.ErrInternal.WithMessage("failed to look up agent definition")
		}
		if def == nil {
			return apperror.ErrBadRequest.WithMessage("agent definition not found")
		}
		agentDefID = &parsed
	}

	// Get or create conversation
	var conv *Conversation
	if req.ConversationID != nil {
		// Use existing conversation — ignore agentDefinitionId from request body
		parsed, _ := uuid.Parse(*req.ConversationID) // Already validated
		var err error
		conv, err = h.svc.GetConversation(ctx, user.ProjectID, parsed)
		if err != nil {
			return err
		}

		// Persist the user message
		_, err = h.svc.AddMessage(ctx, user.ProjectID, conv.ID, AddMessageRequest{
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
		var err error
		conv, err = h.svc.CreateConversation(ctx, user.ProjectID, user.ID, createReq)
		if err != nil {
			return err
		}

		// Set agent_definition_id on the new conversation if requested
		if agentDefID != nil {
			conv.AgentDefinitionID = agentDefID
			if err := h.svc.SetAgentDefinitionID(ctx, user.ProjectID, conv.ID, agentDefID); err != nil {
				h.log.Warn("failed to set agent_definition_id on conversation",
					slog.String("conversation_id", conv.ID.String()),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	// Now that validation is done and conversation is ready, start SSE streaming
	w := c.Response().Writer
	sseWriter := sse.NewWriter(w)
	if err := sseWriter.Start(); err != nil {
		return apperror.ErrInternal.WithMessage("failed to start SSE stream")
	}

	// Emit meta event first
	metaEvent := sse.NewMetaEvent(conv.ID.String())
	if err := sseWriter.WriteData(metaEvent); err != nil {
		// SSE already started, can't return error - just log and continue
		return nil
	}

	// Branch: agent-backed vs direct-LLM flow
	if conv.AgentDefinitionID != nil {
		h.streamAgentChat(ctx, conv, message, user.ProjectID, sseWriter)
		sseWriter.WriteData(sse.NewDoneEvent())
		sseWriter.Close()
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

	// RAG: search knowledge graph for context (non-blocking — failure doesn't prevent chat)
	var searchResults *search.UnifiedSearchResponse
	if h.searchSvc != nil {
		projectUUID, parseErr := uuid.Parse(user.ProjectID)
		if parseErr == nil {
			res, searchErr := h.searchSvc.Search(ctx, projectUUID, &search.UnifiedSearchRequest{
				Query: message,
				Limit: 10,
			}, nil)
			if searchErr != nil {
				h.log.Warn("RAG search failed, continuing without context",
					slog.String("error", searchErr.Error()),
				)
			} else {
				searchResults = res
			}
		}
	}

	// Build prompt
	systemPrompt := os.Getenv("CHAT_SYSTEM_PROMPT")
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant specialized in knowledge graphs and data schemas. Answer questions clearly using markdown formatting."
	}

	var retrievalCtx json.RawMessage
	if searchResults != nil && len(searchResults.Results) > 0 {
		contextStr := h.formatSearchContext(searchResults.Results)
		if contextStr != "" {
			systemPrompt += "\n\n## Relevant Knowledge\nUse the following information to help answer the user's question:\n" + contextStr
		}
		if raw, err := json.Marshal(searchResults.Results); err == nil {
			retrievalCtx = raw
		}
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
			_, _ = h.svc.AddMessage(ctx, user.ProjectID, conv.ID, AddMessageRequest{
				Role:             RoleAssistant,
				Content:          fullResponse.String(),
				RetrievalContext: retrievalCtx,
			})
		}()
	}

	// Emit done event
	sseWriter.WriteData(sse.NewDoneEvent())
	sseWriter.Close()

	return nil
}

func (h *Handler) formatSearchContext(results []search.UnifiedSearchResultItem) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	for i, item := range results {
		switch item.Type {
		case search.ItemTypeGraph:
			b.WriteString("- **")
			b.WriteString(item.ObjectType)
			b.WriteString("**: ")
			b.WriteString(item.Key)
			if len(item.Fields) > 0 {
				b.WriteString(" — ")
				fieldIdx := 0
				for k, v := range item.Fields {
					if fieldIdx > 0 {
						b.WriteString(", ")
					}
					b.WriteString(k)
					b.WriteString("=")
					b.WriteString(formatFieldValue(v))
					fieldIdx++
					if fieldIdx >= 5 {
						break
					}
				}
			}
		case search.ItemTypeRelationship:
			b.WriteString("- ")
			b.WriteString(item.TripletText)
		case search.ItemTypeText:
			snippet := item.Snippet
			if len(snippet) > 300 {
				snippet = snippet[:300] + "…"
			}
			b.WriteString("- ")
			b.WriteString(snippet)
		}
		if i < len(results)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func formatFieldValue(v any) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case string:
		if len(val) > 100 {
			return val[:100] + "…"
		}
		return val
	default:
		raw, err := json.Marshal(val)
		if err != nil {
			return "?"
		}
		s := string(raw)
		if len(s) > 100 {
			return s[:100] + "…"
		}
		return s
	}
}

// streamAgentChat handles the agent-backed chat flow. It loads the agent definition,
// builds conversation history, calls the agent executor with a StreamCallback, and
// maps streaming events to SSE events. Final assistant text is persisted to kb.chat_messages.
func (h *Handler) streamAgentChat(ctx context.Context, conv *Conversation, message, projectID string, sseWriter *sse.Writer) {
	agentDefID := conv.AgentDefinitionID.String()

	// Load the agent definition
	def, err := h.agentRepo.FindDefinitionByID(ctx, agentDefID, &projectID)
	if err != nil || def == nil {
		h.log.Error("failed to load agent definition for chat",
			slog.String("agent_definition_id", agentDefID),
			slog.String("conversation_id", conv.ID.String()),
		)
		sseWriter.WriteData(sse.NewErrorEvent("Failed to load agent definition"))
		return
	}

	// Load conversation history (last 10 messages for context)
	history, err := h.svc.repo.GetConversationHistory(ctx, conv.ID, 10)
	if err != nil {
		h.log.Warn("failed to load conversation history for agent chat",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("error", err.Error()),
		)
		// Continue without history — agent will still work
	}

	// Build the user message with history prefix for multi-turn context
	userMessage := message
	if len(history) > 0 {
		var historyBuf strings.Builder
		historyBuf.WriteString("## Prior conversation context\n")
		for _, msg := range history {
			// Skip the current user message (already the last in history if persisted before)
			historyBuf.WriteString(msg.Role)
			historyBuf.WriteString(": ")
			content := msg.Content
			if len(content) > 2000 {
				content = content[:2000] + "..."
			}
			historyBuf.WriteString(content)
			historyBuf.WriteString("\n\n")
		}
		historyBuf.WriteString("## Current user message\n")
		historyBuf.WriteString(message)
		userMessage = historyBuf.String()
	}

	// Collect the full response text for persistence
	var fullResponse strings.Builder

	// Build the StreamCallback that maps executor events to SSE events
	streamCallback := func(event agents.StreamEvent) {
		switch event.Type {
		case agents.StreamEventTextDelta:
			fullResponse.WriteString(event.Text)
			sseWriter.WriteData(sse.NewTokenEvent(event.Text))
		case agents.StreamEventToolCallStart:
			sseWriter.WriteData(sse.NewMCPToolEvent(event.Tool, "started", event.Input, ""))
		case agents.StreamEventToolCallEnd:
			status := "completed"
			if event.Error != "" {
				status = "error"
			}
			sseWriter.WriteData(sse.NewMCPToolEvent(event.Tool, status, event.Output, event.Error))
		case agents.StreamEventError:
			sseWriter.WriteData(sse.NewErrorEvent(event.Error))
		}
	}

	var result *agents.ExecuteResult

	// Ensure a dummy Agent exists for this AgentDefinition so the executor has a valid agent_id
	// This is a workaround for kb.agent_runs requiring a valid agent_id
	dummyAgentName := "Chat session for " + def.Name
	dummyAgent, _ := h.agentRepo.FindByName(ctx, projectID, dummyAgentName)
	if dummyAgent == nil {
		dummyAgent = &agents.Agent{
			ProjectID:    projectID,
			Name:         dummyAgentName,
			StrategyType: def.Name,
			CronSchedule: "0 0 * * *", // required by schema but ignored
			TriggerType:  "webhook",
		}
		_ = h.agentRepo.Create(ctx, dummyAgent)
	}

	// Check for deterministic test mode or missing executor
	if os.Getenv("CHAT_TEST_DETERMINISTIC") == "1" || h.agentExecutor == nil {
		h.log.Info("agent executor is nil or deterministic mode enabled, using stub mode")

		// Create a stub run so we can test the trace persistence
		run, err := h.agentRepo.CreateRun(ctx, dummyAgent.ID)
		if err != nil {
			h.log.Error("failed to create stub run", slog.String("error", err.Error()))
			sseWriter.WriteData(sse.NewErrorEvent("Failed to create stub run"))
			return
		}
		runID := run.ID

		// Create a stub tool call in the trace
		_ = h.agentRepo.CreateToolCall(ctx, &agents.AgentRunToolCall{
			RunID:    runID,
			ToolName: "search_entities",
			Input:    map[string]any{"query": "test"},
			Output:   map[string]any{"found": true},
		})

		// Create a stub message in the trace
		_ = h.agentRepo.CreateMessage(ctx, &agents.AgentRunMessage{
			RunID:   runID,
			Role:    "assistant",
			Content: map[string]any{"text": "I found it."},
		})

		// Emit synthetic events to the SSE stream using the callback
		// This simulates the actual execution flow
		streamCallback(agents.StreamEvent{
			Type:  agents.StreamEventToolCallStart,
			Tool:  "search_entities",
			Input: map[string]any{"query": "test"},
		})
		time.Sleep(10 * time.Millisecond)
		streamCallback(agents.StreamEvent{
			Type:   agents.StreamEventToolCallEnd,
			Tool:   "search_entities",
			Output: map[string]any{"found": true},
		})

		textParts := []string{"I ", "found ", "it."}
		for _, part := range textParts {
			streamCallback(agents.StreamEvent{
				Type: agents.StreamEventTextDelta,
				Text: part,
			})
		}

		result = &agents.ExecuteResult{RunID: runID}
		// err is already nil
	} else {
		// Execute the real agent
		result, err = h.agentExecutor.Execute(ctx, agents.ExecuteRequest{
			Agent:           dummyAgent,
			AgentDefinition: def,
			ProjectID:       projectID,
			UserMessage:     userMessage,
			StreamCallback:  streamCallback,
		})
	}

	if err != nil {
		h.log.Error("agent execution failed",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("agent_definition_id", agentDefID),
			slog.String("error", err.Error()),
		)
		sseWriter.WriteData(sse.NewErrorEvent("Agent execution failed: " + err.Error()))
		return
	}

	// Persist assistant response to kb.chat_messages with agent_run_id reference
	responseText := fullResponse.String()
	if responseText != "" {
		var retrievalCtx json.RawMessage
		if result != nil {
			rc, _ := json.Marshal(map[string]string{"agent_run_id": result.RunID})
			retrievalCtx = rc
		}

		go func() {
			_, _ = h.svc.AddMessage(ctx, projectID, conv.ID, AddMessageRequest{
				Role:             RoleAssistant,
				Content:          responseText,
				RetrievalContext: retrievalCtx,
			})
		}()
	}
}
