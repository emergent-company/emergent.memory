package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/chat/citations"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/a2ui"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
	"github.com/emergent-company/emergent.memory/pkg/llm/vertex"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/sse"
	"github.com/emergent-company/emergent.memory/pkg/tracing"
)

// errNoProviderMsg is the user-facing message returned when no LLM provider
// is configured. It lists all supported providers, not just Google/Vertex.
const errNoProviderMsg = "No LLM provider configured for this project. " +
	"Please configure a Google AI, Vertex AI, OpenAI, or DeepSeek credential in your project settings."

// Handler handles chat HTTP requests
type Handler struct {
	svc              *Service
	llmClient        *vertex.Client
	searchSvc        *search.Service
	agentExecutor    *agents.AgentExecutor
	agentRepo        *agents.Repository
	credSvc          *provider.CredentialService
	modelFactory     *adk.ModelFactory
	apiTokenSvc      *apitoken.Service // optional: mints ephemeral tokens for sandbox agents
	docSvc           *documents.Service
	uploadHandler    *documents.UploadHandler
	domainClassifier mcp.DomainClassifierHandler
	askV2Default     bool // server-level default for v2 code-gen agent
	log              *slog.Logger
}

// NewHandler creates a new chat handler
func NewHandler(svc *Service, llmClient *vertex.Client, searchSvc *search.Service, agentExecutor *agents.AgentExecutor, agentRepo *agents.Repository, credSvc *provider.CredentialService, modelFactory *adk.ModelFactory, apiTokenSvc *apitoken.Service, docSvc *documents.Service, uploadHandler *documents.UploadHandler, domainClassifier mcp.DomainClassifierHandler, cfg *config.Config, log *slog.Logger) *Handler {
	return &Handler{
		svc:              svc,
		llmClient:        llmClient,
		searchSvc:        searchSvc,
		agentExecutor:    agentExecutor,
		agentRepo:        agentRepo,
		credSvc:          credSvc,
		modelFactory:     modelFactory,
		apiTokenSvc:      apiTokenSvc,
		docSvc:           docSvc,
		uploadHandler:    uploadHandler,
		domainClassifier: domainClassifier,
		askV2Default:     cfg.AskV2,
		log:              log.With(logger.Scope("chat.handler")),
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
// @Param        includeArchived query bool false "Include archived conversations (default false)"
// @Param        includeAncestors query bool false "Include visible ancestors of listed sub-chats (default false)"
// @Success      200 {object} ListConversationsResult "List of conversations"
// @Failure      400 {object} apperror.Error "Invalid parameters"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/conversations [get]
// @Security     bearerAuth
func (h *Handler) ListConversations(c echo.Context) error {
	user := auth.MustGetUser(c)

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

	includeArchived := false
	if iaStr := c.QueryParam("includeArchived"); iaStr != "" {
		parsed, err := strconv.ParseBool(iaStr)
		if err != nil {
			return apperror.NewBadRequest("includeArchived must be a boolean")
		}
		includeArchived = parsed
	}

	// includeAncestors is opt-in: the web rail needs a groupable page (a child
	// can be newer than its parent and push the parent off the limit), while
	// other API callers keep the exact pagination contract.
	includeAncestors := false
	if iaStr := c.QueryParam("includeAncestors"); iaStr != "" {
		parsed, err := strconv.ParseBool(iaStr)
		if err != nil {
			return apperror.NewBadRequest("includeAncestors must be a boolean")
		}
		includeAncestors = parsed
	}

	// Pass the caller's user id for the owner-or-shared filter. An ownerless
	// device credential carries no user id (""), so the repository scopes it to
	// project-shared conversations instead of comparing "" against a uuid
	// column (SQLSTATE 22P02).
	result, err := h.svc.ListConversations(c.Request().Context(), user.ProjectID, &user.ID, limit, offset, includeArchived, includeAncestors)
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
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid conversation id")
	}

	// Get conversation with messages
	conv, err := h.svc.GetConversationWithMessages(c.Request().Context(), user.ProjectID, user.ID, conversationID)
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
	user := auth.MustGetUser(c)

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
	user := auth.MustGetUser(c)

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

	conv, err := h.svc.UpdateConversation(c.Request().Context(), user.ProjectID, user.ID, conversationID, req)
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
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid conversation id")
	}

	if err := h.svc.DeleteConversation(c.Request().Context(), user.ProjectID, user.ID, conversationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// ArchiveConversation handles POST /api/chat/:id/archive
// @Summary      Archive conversation
// @Description  Archives a conversation (non-destructive: messages and history are preserved). Idempotent.
// @Tags         chat
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Conversation ID (UUID)"
// @Success      200 {object} map[string]string "Archive status"
// @Failure      400 {object} apperror.Error "Invalid conversation ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Conversation not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/{id}/archive [post]
// @Security     bearerAuth
func (h *Handler) ArchiveConversation(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.NewBadRequest("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.NewBadRequest("invalid conversation id")
	}

	if err := h.svc.ArchiveConversation(c.Request().Context(), user.ProjectID, user.ID, conversationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "archived"})
}

// UnarchiveConversation handles POST /api/chat/:id/unarchive
// @Summary      Unarchive conversation
// @Description  Restores an archived conversation to the active list. Idempotent.
// @Tags         chat
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        id path string true "Conversation ID (UUID)"
// @Success      200 {object} map[string]string "Unarchive status"
// @Failure      400 {object} apperror.Error "Invalid conversation ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Conversation not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/{id}/unarchive [post]
// @Security     bearerAuth
func (h *Handler) UnarchiveConversation(c echo.Context) error {
	user := auth.MustGetUser(c)

	if user.ProjectID == "" {
		return apperror.NewBadRequest("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.NewBadRequest("invalid conversation id")
	}

	if err := h.svc.UnarchiveConversation(c.Request().Context(), user.ProjectID, user.ID, conversationID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "unarchived"})
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
	user := auth.MustGetUser(c)

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

	msg, err := h.svc.AddMessage(c.Request().Context(), user.ProjectID, user.ID, conversationID, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, msg)
}

// GetConversationHistory returns the full unified session timeline for a conversation.
// Each item in the response is a typed event: run lifecycle, user/assistant messages,
// tool calls, and tool results — ordered chronologically across all agent runs.
//
// GET /api/chat/:id/history
func (h *Handler) GetConversationHistory(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("x-project-id header required")
	}

	conversationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid conversation id")
	}

	ctx := c.Request().Context()

	// Load the conversation to verify ownership and get session_id.
	conv, err := h.svc.GetConversationWithMessages(ctx, user.ProjectID, user.ID, conversationID)
	if err != nil {
		return err
	}
	if conv == nil {
		return apperror.ErrNotFound.WithMessage("conversation not found")
	}

	if conv.SessionID == nil {
		// No agent runs exist yet — synthesize the transcript from the
		// conversation's stored kb.chat_messages (role/content/created_at) so
		// user/assistant rows surface even before a run starts (e.g.
		// document-classification or get-or-create flows that write chat
		// messages but never execute an agent turn). Shape mirrors the
		// "message" items produced by GetConversationFullHistory so the gateway
		// renders both uniformly.
		return c.JSON(http.StatusOK, map[string]any{
			"conversation_id": conversationID,
			"items":           synthesizeConversationMessageItems(conv.Messages),
		})
	}

	items, err := h.agentRepo.GetConversationFullHistory(ctx, conv.SessionID.String())
	if err != nil {
		return apperror.ErrInternal.WithMessage("failed to load conversation history")
	}

	// Merge the conversation's stored kb.chat_messages user messages into the
	// run timeline (interleaved chronologically by created_at) so a
	// get-or-create conversation that seeded a user message and then ran an
	// agent turn shows the user message alongside the run items. Identical
	// (role+content+time) rows present in both stores are deduplicated.
	items = mergeConversationUserMessages(items, conv.Messages)

	return c.JSON(http.StatusOK, map[string]any{
		"conversation_id": conversationID,
		"session_id":      conv.SessionID,
		"items":           items,
	})
}

// conversationMessageItem mirrors the "message" timeline item shape returned by
// agents.Repository.GetConversationFullHistory (kind/role/content/created_at)
// for a single stored kb.chat_messages row.
//
// RunID and StepNumber are intentionally left as their zero values: a stored
// chat row has no owning run or step, and ""/0 is the truthful encoding. The
// only in-repo consumer of these items (mcp's session-get-messages) forwards
// them opaquely, and the transcript renderer keys on kind/role/content/
// created_at; run-scoped "message" items are unaffected.
func conversationMessageItem(m Message) *agents.ConversationHistoryItem {
	content := map[string]any{"text": m.Content}
	// A card-only turn stores its surfaces in retrieval_context; surface them so
	// a chat-message replay (no agent run) still renders the card (#427).
	if len(m.RetrievalContext) > 0 {
		var rc struct {
			A2UI []a2ui.Message `json:"a2ui"`
		}
		if err := json.Unmarshal(m.RetrievalContext, &rc); err == nil && len(rc.A2UI) > 0 {
			content["a2ui"] = rc.A2UI
		}
	}
	return &agents.ConversationHistoryItem{
		Kind:      "message",
		CreatedAt: m.CreatedAt,
		Role:      m.Role,
		Content:   content,
	}
}

// synthesizeConversationMessageItems converts stored chat messages into
// timeline "message" items, in stored (created_at ASC) order.
func synthesizeConversationMessageItems(msgs []Message) []*agents.ConversationHistoryItem {
	items := make([]*agents.ConversationHistoryItem, 0, len(msgs))
	for i := range msgs {
		items = append(items, conversationMessageItem(msgs[i]))
	}
	return items
}

// conversationItemText extracts the plain-text payload of a message timeline
// item's content object.
func conversationItemText(it *agents.ConversationHistoryItem) string {
	if it == nil || it.Content == nil {
		return ""
	}
	text, _ := it.Content["text"].(string)
	return text
}

// conversationHistoryPrefix and conversationCurrentMarker delimit the prompt
// that streamAgentChat constructs for a multi-turn agent chat: prior turns are
// summarised under the first marker and the raw current user turn follows the
// second (see streamAgentChat's "Prior conversation context" builder).
const (
	conversationHistoryPrefix = "## Prior conversation context\n"
	conversationCurrentMarker = "## Current user message\n"
)

// conversationRunUserText normalizes a run-scoped user message's persisted text
// back to the raw user turn.
//
// The executor persists req.UserMessage verbatim (executor.go persistMessage),
// while streamAgentChat feeds it a history-augmented prompt on every turn after
// the first. Matching raw chat rows against the normalized suffix is what makes
// the transcript dedupe effective; the raw text and the once-augmented text are
// otherwise never equal.
func conversationRunUserText(content string) string {
	if !strings.HasPrefix(content, conversationHistoryPrefix) {
		return content
	}
	if i := strings.LastIndex(content, conversationCurrentMarker); i >= 0 {
		return content[i+len(conversationCurrentMarker):]
	}
	return content
}

// mergeConversationUserMessages splices a conversation's stored kb.chat_messages
// user rows into the run-based timeline returned by GetConversationFullHistory.
//
// The run timeline is emitted per-run as run_start, that run's messages/tool
// calls, run_end, and run_end.CreatedAt is the run's *start* time. Re-sorting the
// combined slice by CreatedAt would therefore hoist every run_end above its own
// run's messages, so run items keep their emitted relative order and stored rows
// are only inserted at run boundaries (see conversationRunUserText's callers).
//
// Dedupe rule: an agent turn is written twice — once to kb.chat_messages (raw
// text, earlier) and once to kb.agent_run_messages (persisted req.UserMessage,
// later, history-augmented on multi-turn chats). A stored user row is treated as
// a duplicate only when an unmatched run-scoped user message with the same raw
// text exists. Matching by nearest-preceding timestamp (the chat row is written
// before the run starts) keeps counts correct: extra identical rows without a
// run counterpart are still emitted, and two identical turns each matched by
// their own run are both suppressed.
func mergeConversationUserMessages(runItems []*agents.ConversationHistoryItem, msgs []Message) []*agents.ConversationHistoryItem {
	if len(msgs) == 0 {
		return runItems
	}

	type storedUser struct {
		msg     Message
		item    *agents.ConversationHistoryItem
		matched bool
	}
	stored := make([]*storedUser, 0, len(msgs))
	for i := range msgs {
		if msgs[i].Role != RoleUser {
			// Assistant/tool turns are already captured run-scoped for
			// agent-backed conversations; only stored user rows can precede a
			// run (e.g. get-or-create seeding).
			continue
		}
		stored = append(stored, &storedUser{msg: msgs[i], item: conversationMessageItem(msgs[i])})
	}
	if len(stored) == 0 {
		return runItems
	}
	sort.SliceStable(stored, func(i, j int) bool {
		return stored[i].msg.CreatedAt.Before(stored[j].msg.CreatedAt)
	})

	// Pair each run-scoped user message with the closest preceding stored row
	// of identical raw text. Walk run items in emitted order so a later run
	// cannot steal an earlier run's counterpart.
	for _, it := range runItems {
		if it == nil || it.Kind != "message" || it.Role != RoleUser {
			continue
		}
		runText := conversationRunUserText(conversationItemText(it))
		best := -1
		for i := range stored {
			if stored[i].matched || stored[i].msg.Content != runText {
				continue
			}
			if !stored[i].msg.CreatedAt.After(it.CreatedAt) {
				// Prefer the latest stored row at or before the run start.
				if best == -1 || stored[i].msg.CreatedAt.After(stored[best].msg.CreatedAt) {
					best = i
				}
			} else if best == -1 {
				// No preceding candidate yet; remember the earliest following
				// row as a fallback (stored is time-ordered, so this is stable).
				best = i
			}
		}
		if best >= 0 {
			stored[best].matched = true
		}
	}

	kept := make([]*agents.ConversationHistoryItem, 0, len(stored))
	for _, s := range stored {
		if !s.matched {
			kept = append(kept, s.item)
		}
	}
	if len(kept) == 0 {
		return runItems
	}

	out := make([]*agents.ConversationHistoryItem, 0, len(runItems)+len(kept))
	ci := 0
	for _, it := range runItems {
		// A run_start anchors a run block; flush stored rows that predate it so
		// they land before the run that follows. run_end is deliberately not a
		// boundary — its CreatedAt equals the run's start, so flushing on it
		// would reorder messages ahead of the run.
		if it != nil && it.Kind == "run_start" {
			for ci < len(kept) && !kept[ci].CreatedAt.After(it.CreatedAt) {
				out = append(out, kept[ci])
				ci++
			}
		}
		out = append(out, it)
	}
	// Rows newer than the last run (or orphaned inside its window) go last.
	out = append(out, kept[ci:]...)
	return out
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

// streamMessage is the user turn the agent receives: the typed text, or the
// surface action framed for it.
//
// A surface action is a message, not a protocol of its own — the A2A transport
// already frames it this way — so both transports share one wording and an
// agent cannot tell which client delivered the click.
func streamMessage(req *StreamRequest) string {
	message := strings.TrimSpace(req.Message)
	if req.SurfaceAction == nil {
		return message
	}
	return agents.A2UIActionMessage(agents.A2UIAction{
		SurfaceID: strings.TrimSpace(req.SurfaceAction.SurfaceID),
		Action:    req.SurfaceAction.Action,
	}, message)
}

// pendingQuestionAnswer is what a chat turn records on the conversation's
// pending question when it answers it.
//
// A decision-bearing A2UI card sends the option the user clicked
// (`action.componentId` + `action.response`), so the question records that
// option rather than the framed prose the agent reads (#223). Every other turn
// — typed text, or a card whose action carries no option payload — records the
// message itself, exactly as before.
func pendingQuestionAnswer(req *StreamRequest, message string) string {
	if req != nil && req.SurfaceAction != nil {
		if answer, ok := surfaceAnswerFromAction(req.SurfaceAction.Action); ok {
			return answer
		}
	}
	return message
}

// surfaceAnswerFromAction extracts the answer value a surface action carries: a
// non-empty `componentId` names the card the user acted on, and a scalar
// `response` is the value they picked (approval and proposal cards both emit
// `{componentId, response}`). A click without that shape — e.g. a code card's
// copy button — is not a decision and stays a chat turn.
func surfaceAnswerFromAction(action any) (string, bool) {
	m, ok := action.(map[string]any)
	if !ok {
		return "", false
	}
	if componentID, _ := m["componentId"].(string); strings.TrimSpace(componentID) == "" {
		return "", false
	}
	switch v := m["response"].(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return "", false
		}
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	}
	return "", false
}

// pauseAnswerableByChat reports whether a paused run may be resumed by a chat
// message or a surface action. A tool-confirmation pause is decided by the
// approval card that carries its question id, never by free text or an A2UI
// surface action — the surface-action contract keeps that guarantee.
func pauseAnswerableByChat(sc *agents.SuspendSignal) bool {
	return sc == nil || sc.Reason != agents.SuspendReasonAwaitingToolConfirm
}

func validateStreamRequest(req *StreamRequest) error {
	hasSurfaceAction := req.SurfaceAction != nil
	if strings.TrimSpace(req.Message) == "" && !hasSurfaceAction {
		return apperror.ErrBadRequest.WithMessage("message is required")
	}
	if len(req.Message) > 100000 {
		return apperror.ErrBadRequest.WithMessage("message must be at most 100000 characters")
	}
	if hasSurfaceAction {
		surfaceID := strings.TrimSpace(req.SurfaceAction.SurfaceID)
		if surfaceID == "" {
			return apperror.ErrBadRequest.WithMessage("surfaceAction.surfaceId is required")
		}
		// A surface belongs to the conversation that rendered it: without one the
		// turn would open a new conversation titled by the framed action text.
		if req.ConversationID == nil || strings.TrimSpace(*req.ConversationID) == "" {
			return apperror.ErrBadRequest.WithMessage("surfaceAction requires conversationId")
		}
		// The agent reads the action as JSON; an absent or empty payload would be
		// framed as `null` and leave it nothing to act on.
		action, ok := req.SurfaceAction.Action.(map[string]any)
		if !ok || len(action) == 0 {
			return apperror.ErrBadRequest.WithMessage("surfaceAction.action must be a non-empty object")
		}
		// The framed turn is what the agent reads and the conversation records, so
		// it is bounded by the same message limit as typed text.
		if len(streamMessage(req)) > 100000 {
			return apperror.ErrBadRequest.WithMessage("surface action is too large to send as a message")
		}
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

// chatKeepaliveInterval is how often the SSE stream emits a comment frame
// during idle gaps to keep every hop from tripping an idle timeout.
const chatKeepaliveInterval = 25 * time.Second

// startKeepalive emits SSE comment frames (": ping") on the given interval for
// the lifetime of the stream. Long-lived chat streams can sit idle for minutes
// while a tool call or model generation is in flight; periodic comment frames
// keep every hop (browser → gateway → Traefik → server) from tripping an idle
// timeout. The goroutine exits when ctx is done (client disconnect), a write
// fails, or the returned stop func is called. The stop func cancels the
// goroutine and joins it, closing the window where it writes to the response
// writer after the handler returns. interval and log are injectable so the
// keepalive can be exercised with a short tick in tests.
func startKeepalive(ctx context.Context, w *sse.Writer, interval time.Duration, log *slog.Logger) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := w.WriteComment("ping"); err != nil {
					if log != nil {
						log.Warn("chat keepalive write failed", slog.String("error", err.Error()))
					}
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
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
	user := auth.MustGetUser(c)

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

	// Inject project ID into context so the credential resolver (ResolveAny) can
	// look up org-level credentials even when the caller authenticates via user JWT
	// (which does not set X-Project-ID in the auth context).
	if auth.ProjectIDFromContext(ctx) == "" && user.ProjectID != "" {
		ctx = auth.ContextWithProjectID(ctx, user.ProjectID)
	}
	if req.Namespace != "" {
		ctx = auth.ContextWithNamespace(ctx, req.Namespace)
	}
	// opening the SSE stream so clients get a proper HTTP error code, not a
	// success status with an error buried in the stream.
	if h.modelFactory != nil {
		probeModel, probeErr := h.modelFactory.CreateModel(ctx)
		if probeErr != nil {
			errMsg := probeErr.Error()
			if strings.Contains(errMsg, "no LLM credentials configured") ||
				strings.Contains(errMsg, "no_provider") ||
				strings.Contains(errMsg, "provider config found for organization") ||
				strings.Contains(errMsg, "no generative model configured") {
				return apperror.New(http.StatusServiceUnavailable, "no_provider", errNoProviderMsg)
			}
			return apperror.New(http.StatusServiceUnavailable, "provider_error",
				friendlyProviderError(probeErr))
		}
		if closer, ok := probeModel.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	// Start chat span before conversation is resolved (conversation_id added below)
	ctx, span := tracing.Start(ctx, "chat.handle_message",
		attribute.String("memory.project.id", user.ProjectID),
	)
	defer span.End()

	message := streamMessage(&req)

	// If agentDefinitionId is provided, validate it exists. On a new
	// conversation it is bound at creation; on an existing conversation it is
	// bound below on the first agent-backed turn (get-or-create flows such as
	// object-chat leave agent_definition_id unset and only carry the agent in
	// the redirect URL).
	var agentDefID *uuid.UUID
	if req.AgentDefinitionID != nil {
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
		// Use existing conversation. If it is still unbound and the request
		// carries an agent definition id, bind it below on this first
		// agent-backed turn; a conversation that already has an agent keeps it.
		parsed, _ := uuid.Parse(*req.ConversationID) // Already validated
		var err error
		conv, err = h.svc.GetConversation(ctx, user.ProjectID, user.ID, parsed)
		if err != nil {
			return err
		}

		// Bind the agent on the first agent-backed turn of an unbound
		// conversation. Get-or-create flows (object-chat, canonical
		// conversations) leave agent_definition_id unset and only carry the
		// agent in the chat request, so without this bind the conversation
		// would take the legacy direct-LLM path instead of the agent executor.
		if conv.AgentDefinitionID == nil && agentDefID != nil {
			conv.AgentDefinitionID = agentDefID
			if err := h.svc.SetAgentDefinitionID(ctx, user.ProjectID, user.ID, conv.ID, agentDefID); err != nil {
				h.log.Warn("failed to bind agent to conversation on first turn",
					slog.String("conversation_id", conv.ID.String()),
					slog.String("error", err.Error()),
				)
			}
		}

		// Persist the user message
		_, err = h.svc.AddMessage(ctx, user.ProjectID, user.ID, conv.ID, AddMessageRequest{
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
			if err := h.svc.SetAgentDefinitionID(ctx, user.ProjectID, user.ID, conv.ID, agentDefID); err != nil {
				h.log.Warn("failed to set agent_definition_id on conversation",
					slog.String("conversation_id", conv.ID.String()),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	// Annotate span with conversation_id now that we have it
	span.SetAttributes(attribute.String("memory.chat.conversation_id", conv.ID.String()))

	// Now that validation is done and conversation is ready, start SSE streaming
	w := c.Response().Writer
	sseWriter := sse.NewWriter(w)
	if err := sseWriter.Start(); err != nil {
		return apperror.ErrInternal.WithMessage("failed to start SSE stream")
	}

	// Keep the stream alive across long idle gaps (slow tool calls, model busy,
	// approval pauses) so no intermediate proxy times out the connection.
	stopKeepalive := startKeepalive(ctx, sseWriter, chatKeepaliveInterval, h.log)
	defer stopKeepalive()

	// Emit meta event first
	metaEvent := sse.NewMetaEvent(conv.ID.String())
	if err := sseWriter.WriteData(metaEvent); err != nil {
		// SSE already started, can't return error - just log and continue
		return nil
	}

	// Branch: agent-backed vs direct-LLM flow
	if conv.AgentDefinitionID != nil {
		// The agent run must outlive the SSE request. When the browser↔gateway
		// connection drops (reload, navigation, network blip) the request
		// context is cancelled; bound to it, the executor would hard-fail the
		// run with "agent stopped: context canceled" even though nothing was
		// wrong server-side (issue #1149). Run on a detached context so a
		// request-context cancellation only detaches this SSE consumer: the run
		// stays bounded by the executor's per-step watchdog (defaultRunTimeout)
		// and is stoppable via POST /api/chat/runs/:runId/cancel, and its
		// progress is persisted to the run record either way.
		runCtx := agents.DetachedRunContext(ctx)
		h.log.Info("chat agent run detached from request context",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("agent_definition_id", conv.AgentDefinitionID.String()),
		)

		// If this conversation's run is parked on an ask_user question, this
		// message is the answer: resume that run instead of starting a fresh one.
		// A decision-bearing A2UI card answers with the option that was clicked,
		// which the question records as its answer (#223). A tool-confirmation
		// pause is refused inside resumeForAnswer, so a surface action there
		// stays an ordinary turn — its own approval card owns that decision.
		if q, qErr := h.agentRepo.FindPendingQuestionByConversation(ctx, user.ProjectID, conv.ID.String()); qErr == nil && q != nil {
			if handled := h.resumeForAnswer(runCtx, sseWriter, conv, q, pendingQuestionAnswer(&req, message), message, user); handled {
				_ = sseWriter.WriteData(sse.NewDoneEvent())
				sseWriter.Close()
				return nil
			}
		}

		agentResult := h.streamAgentChat(runCtx, conv, message, user.ProjectID, user.OrgID, user.ID, sseWriter, "", "", "")
		sseWriter.WriteData(sse.NewDoneEvent())
		sseWriter.Close()
		if agentResult != nil && agentResult.Cleanup != nil {
			go agentResult.Cleanup()
		}
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

	// Stream tokens from LLM — prefer the per-org model factory (resolves DB credentials),
	// fall back to the legacy server-wide vertex client if the factory is unavailable.
	var fullResponse strings.Builder
	var llmErr error
	if h.modelFactory != nil {
		llmModel, createErr := h.modelFactory.CreateModel(ctx)
		if createErr != nil {
			llmErr = createErr
		} else {
			modelName := h.modelFactory.ModelName()
			llmCtx, llmSpan := tracing.Start(ctx, "chat.llm_generate",
				attribute.String("memory.llm.model", modelName),
			)
			llmReq := &adkmodel.LLMRequest{
				Model: modelName,
				Contents: []*genai.Content{
					{
						Role:  "user",
						Parts: []*genai.Part{{Text: message}},
					},
				},
				Config: &genai.GenerateContentConfig{
					SystemInstruction: &genai.Content{
						Parts: []*genai.Part{{Text: systemPrompt}},
					},
				},
			}
			for resp, iterErr := range llmModel.GenerateContent(llmCtx, llmReq, true) {
				if iterErr != nil {
					llmErr = iterErr
					break
				}
				if resp == nil || resp.Content == nil {
					continue
				}
				for _, part := range resp.Content.Parts {
					if part != nil && part.Text != "" {
						fullResponse.WriteString(part.Text)
						sseWriter.WriteData(sse.NewTokenEvent(part.Text))
					}
				}
			}
			if llmErr != nil {
				llmSpan.RecordError(llmErr)
				llmSpan.SetStatus(codes.Error, llmErr.Error())
			} else {
				llmSpan.SetStatus(codes.Ok, "")
			}
			llmSpan.End()
			if closer, ok := llmModel.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
		}
	} else if h.llmClient != nil && h.llmClient.IsAvailable() {
		llmCtx, llmSpan := tracing.Start(ctx, "chat.llm_generate",
			attribute.String("memory.llm.model", h.llmClient.Model()),
		)
		llmErr = h.llmClient.GenerateStreaming(llmCtx, vertex.GenerateRequest{
			Prompt:       message,
			SystemPrompt: systemPrompt,
		}, func(token string) {
			fullResponse.WriteString(token)
			sseWriter.WriteData(sse.NewTokenEvent(token))
		})
		if llmErr != nil {
			llmSpan.RecordError(llmErr)
			llmSpan.SetStatus(codes.Error, llmErr.Error())
		} else {
			llmSpan.SetStatus(codes.Ok, "")
		}
		llmSpan.End()
	} else {
		llmErr = fmt.Errorf("no LLM provider configured")
	}

	if llmErr != nil {
		// Emit error event
		span.RecordError(llmErr)
		span.SetStatus(codes.Error, llmErr.Error())
		sseWriter.WriteData(sse.NewErrorEvent(friendlyProviderError(llmErr)))
	} else {
		// Persist the assistant response synchronously, on the caller's DB
		// handle. A fire-and-forget goroutine here shares that handle with the
		// request goroutine, and the handle is not always a pool: under
		// internal/testutil.BaseSuite it is the per-test transaction (one
		// connection). Two statements racing on one connection make pgx's
		// stdlib driver reset the shared result reader and the other
		// statement's Rows.Next panics (issue #302).
		if _, err := h.svc.AddMessage(ctx, user.ProjectID, user.ID, conv.ID, AddMessageRequest{
			Role:             RoleAssistant,
			Content:          fullResponse.String(),
			RetrievalContext: retrievalCtx,
		}); err != nil {
			h.log.Warn("failed to persist assistant response",
				slog.String("conversation_id", conv.ID.String()),
				slog.String("error", err.Error()),
			)
		}
	}

	// Emit done event
	sseWriter.WriteData(sse.NewDoneEvent())
	sseWriter.Close()

	if llmErr == nil {
		span.SetStatus(codes.Ok, "")
	}

	return nil
}

// CancelRun handles POST /api/chat/runs/:runId/cancel — the device-scoped
// stop route (issue #81). It is guarded by chat:use, but scope alone is not an
// authorization: the run must belong to a conversation in the caller's project
// that the caller OWNS. A device credential is project-bound, so without the
// owner check it could stop another member's run by naming its id.
//
// The durable "cancelling" transition is committed first (visible to every
// server instance); the local executor notification is only a fast path. A run
// that already reached a terminal state reports cancelled:false with its
// status instead of pretending the cancel succeeded.
//
// @Summary      Cancel a chat run
// @Description  Stops an agent-backed run that belongs to one of the caller's own conversations.
// @Tags         chat
// @Accept       json
// @Produce      json
// @Param        runId path string true "Agent run ID (UUID)"
// @Success      200 {object} map[string]any "Run cancellation requested"
// @Failure      400 {object} apperror.Error "Invalid run id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Run not found or not owned by the caller"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/chat/runs/{runId}/cancel [post]
// @Security     bearerAuth
func (h *Handler) CancelRun(c echo.Context) error {
	user := auth.MustGetUser(c)

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}
	// A malformed id can never name a run; treat it as not found so the route
	// does not become an id-format oracle.
	if _, err := uuid.Parse(runID); err != nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	ctx := c.Request().Context()

	// Ownership gate. An empty result covers unknown runs, scheduled runs with
	// no chat conversation, runs in another project, and runs in another
	// member's conversation — all indistinguishable to the caller.
	conversationID, err := h.svc.repo.OwnedConversationIDForRun(ctx, user.ProjectID, user.ID, runID)
	if err != nil {
		return apperror.NewInternal("failed to resolve run ownership", err)
	}
	if conversationID == "" {
		return apperror.NewNotFound("AgentRun", runID)
	}

	accepted, err := h.agentRepo.RequestRunCancellation(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to cancel run", err)
	}
	if !accepted {
		status := string(agents.RunStatusError)
		if current, findErr := h.agentRepo.FindRunByID(ctx, runID); findErr == nil && current != nil {
			status = string(current.Status)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"success": true,
			"data": map[string]any{
				"message":   "Run already " + status,
				"runId":     runID,
				"cancelled": false,
				"status":    status,
			},
		})
	}

	// Fast-path notification for an in-flight run in this process. Runs on
	// other instances observe the persisted "cancelling" status at their next
	// step boundary, so correctness does not depend on this call.
	if h.agentExecutor != nil {
		h.agentExecutor.Cancel(runID, agents.UserCancelReason)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"message":   "Run cancellation requested",
			"runId":     runID,
			"cancelled": true,
			"status":    string(agents.RunStatusCancelling),
		},
	})
}

func (h *Handler) formatSearchContext(results []search.UnifiedSearchResultItem) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	needNewline := false
	for _, item := range results {
		switch item.Type {
		case search.ItemTypeGraph, search.ItemTypeRelationship:
			continue
		case search.ItemTypeText:
			snippet := item.Snippet
			if len(snippet) > 300 {
				snippet = snippet[:300] + "…"
			}
			if needNewline {
				b.WriteString("\n")
			}
			b.WriteString("- ")
			b.WriteString(snippet)
			needNewline = true
		}
	}
	return b.String()
}

// friendlyProviderError returns a short, human-readable message when err is a
// well-known LLM provider failure (expired/invalid API key, quota exceeded,
// etc.), or falls back to the raw error string for anything else.
func friendlyProviderError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "API_KEY_INVALID"),
		strings.Contains(msg, "API key expired"),
		strings.Contains(msg, "API key not valid"):
		return "The configured LLM API key is invalid or has expired. " +
			"Update it with: memory provider configure-project <provider> --api-key <new-key>"
	case strings.Contains(msg, "RESOURCE_EXHAUSTED"),
		strings.Contains(msg, "quota"):
		return "LLM quota exceeded. Check your quota limits or switch to a different model."
	case strings.Contains(msg, "PERMISSION_DENIED"):
		return "Permission denied by the LLM provider. Verify that the API key has access to the model API."
	case strings.Contains(msg, "no LLM provider"), strings.Contains(msg, "no_provider"):
		return "No LLM provider is configured for this project. " +
			"Run: memory provider configure-project <provider> --api-key <key>"
	case strings.Contains(msg, "credential resolution failed"),
		strings.Contains(msg, "encryption not configured"),
		strings.Contains(msg, "failed to decrypt"),
		strings.Contains(msg, "EncryptionNonce"):
		return "LLM provider credentials could not be decrypted. " +
			"The server encryption key may have changed. " +
			"Re-configure with: memory provider configure-project <provider> --api-key <key>"
	default:
		return "Agent execution failed: " + msg
	}
}

// streamAgentChat handles the agent-backed chat flow. It loads the agent definition,
// builds conversation history, calls the agent executor with a StreamCallback, and
// maps streaming events to SSE events. Final assistant text is persisted to kb.chat_messages.
func (h *Handler) streamAgentChat(ctx context.Context, conv *Conversation, message, projectID, orgID, userID string, sseWriter *sse.Writer, parentRunID, rootRunID, systemPromptAppendix string) *agents.ExecuteResult {
	agentDefID := conv.AgentDefinitionID.String()

	// Load the agent definition
	def, err := h.agentRepo.FindDefinitionByID(ctx, agentDefID, &projectID)
	if err != nil || def == nil {
		h.log.Error("failed to load agent definition for chat",
			slog.String("agent_definition_id", agentDefID),
			slog.String("conversation_id", conv.ID.String()),
		)
		sseWriter.WriteData(sse.NewErrorEvent("Failed to load agent definition"))
		return nil
	}

	// Load conversation history (last 10 messages for context), scoped to the
	// caller so a foreign conversation id yields no prior messages.
	history, err := h.svc.repo.GetConversationHistory(ctx, projectID, userID, conv.ID, 10)
	if err != nil {
		h.log.Warn("failed to load conversation history for agent chat",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("error", err.Error()),
		)
		// Continue without history — agent will still work
	}

	// Build the user message with history prefix for multi-turn context.
	// Filter out the current user message from history — it is persisted before
	// this point and would otherwise appear duplicated in both sections.
	userMessage := message
	var priorHistory []Message
	for _, msg := range history {
		if !(msg.Role == RoleUser && msg.Content == message) {
			priorHistory = append(priorHistory, msg)
		}
	}
	if len(priorHistory) > 0 {
		var historyBuf strings.Builder
		historyBuf.WriteString("## Prior conversation context\n")
		for _, msg := range priorHistory {
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

	// Collect any A2UI surfaces emitted during the run so they can contribute
	// to citation derivation alongside the answer text.
	var surfaces []a2ui.Message

	// Build the StreamCallback that maps executor events to SSE events
	streamCallback := func(event agents.StreamEvent) {
		switch event.Type {
		case agents.StreamEventTextDelta:
			fullResponse.WriteString(event.Text)
			_ = sseWriter.WriteData(sse.NewTokenEvent(event.Text))
		case agents.StreamEventThinking:
			_ = sseWriter.WriteData(sse.NewThinkingEvent(event.ID, event.Role, event.Text, event.Done))
		case agents.StreamEventToolCallStart:
			_ = sseWriter.WriteData(sse.NewMCPToolEventWithID(event.Tool, "started", event.ID, event.Input, ""))
		case agents.StreamEventToolCallEnd:
			status := event.Status
			if status == "" {
				status = "completed"
				if event.Error != "" {
					status = "error"
				}
			}
			_ = sseWriter.WriteData(sse.NewMCPToolEventWithID(event.Tool, status, event.ID, event.Output, event.Error))
		case agents.StreamEventError:
			_ = sseWriter.WriteData(sse.NewErrorEvent(event.Error))
		case agents.StreamEventToolApproval:
			_ = sseWriter.WriteData(sse.NewApprovalEvent(event.Tool, event.Input, event.QuestionID))
		case agents.StreamEventA2UI:
			// Persist/replay only the authoritative frame: progressive frames are
			// partial and a teardown deletes the surface, so appending them would
			// store a duplicated or already-deleted card (#427).
			if event.Final {
				surfaces = event.A2UI
			}
			_ = sseWriter.WriteData(sse.NewUIEvent(event.SurfaceID, event.A2UI))
		case agents.StreamEventSubagent:
			_ = sseWriter.WriteData(sse.NewSubagentEvent(event.SubagentID, event.ParentRunID, event.RunID, event.AgentName, event.Task, event.Status, event.Error))
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
			StrategyType: "chat-session:" + agentDefID,
			CronSchedule: "0 0 * * *", // required by schema but ignored
			TriggerType:  "manual",
		}
		if err := h.agentRepo.Create(ctx, dummyAgent); err != nil {
			h.log.Error("failed to create dummy agent for chat session",
				slog.String("error", err.Error()),
				slog.String("agent_definition_id", agentDefID),
			)
			sseWriter.WriteData(sse.NewErrorEvent("Failed to create agent session: " + err.Error()))
			return nil
		}
	}

	// Check for deterministic test mode or missing executor
	if os.Getenv("CHAT_TEST_DETERMINISTIC") == "1" || h.agentExecutor == nil {
		h.log.Info("agent executor is nil or deterministic mode enabled, using stub mode")

		// Create a stub run so we can test the trace persistence
		run, err := h.agentRepo.CreateRun(ctx, dummyAgent.ID)
		if err != nil {
			h.log.Error("failed to create stub run", slog.String("error", err.Error()))
			sseWriter.WriteData(sse.NewErrorEvent("Failed to create stub run"))
			return nil
		}
		runID := run.ID

		// Additive run-id frame: the first meta (written before the branch) only
		// carries the conversation. Emit a second meta now that the run exists so
		// a client can name it when asking to stop (#67).
		_ = sseWriter.WriteData(sse.NewMetaEventWithRun(conv.ID.String(), runID))

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
		// Mint an ephemeral sandbox token if the agent has a sandbox config and the service is available
		var authToken, ephemeralTokenID string
		if h.apiTokenSvc != nil && def.SandboxConfig != nil && len(def.SandboxConfig) > 0 && projectID != "" {
			ttl := 2 * time.Hour // default sandbox TTL
			var mintErr error
			ephemeralTokenID, authToken, mintErr = h.apiTokenSvc.CreateEphemeral(ctx, projectID, orgID, userID, ttl)
			if mintErr != nil {
				h.log.Warn("failed to mint ephemeral sandbox token, sandbox will run without API access",
					slog.String("project_id", projectID),
					slog.String("error", mintErr.Error()),
				)
				authToken = ""
				ephemeralTokenID = ""
			}
		}

		// Ensure this conversation has a backing session (create once, reuse on
		// all turns). The repository resolution is atomic — it locks the
		// conversation row and writes the Session + backlink in one
		// transaction — so concurrent first turns cannot create duplicate
		// sessions. A resolution failure is still non-fatal here: the chat turn
		// proceeds without session linkage rather than failing the user's
		// message, and the error is logged for diagnosis.
		agentNameForSession := def.Name
		sessionID, sessionErr := h.agentRepo.EnsureConversationSession(ctx, conv.ID.String(), projectID, &agentNameForSession)
		if sessionErr != nil {
			h.log.Warn("failed to ensure session for conversation",
				slog.String("conversation_id", conv.ID.String()),
				slog.String("error", sessionErr.Error()),
			)
			sessionID = ""
		}

		// Execute the real agent
		execReq := agents.ExecuteRequest{
			Agent:                dummyAgent,
			AgentDefinition:      def,
			ProjectID:            projectID,
			OrgID:                orgID,
			UserID:               userID, // propagate for ask_user notifications
			UserMessage:          userMessage,
			StreamCallback:       streamCallback,
			AuthToken:            authToken,
			EphemeralTokenID:     ephemeralTokenID,
			SessionID:            sessionID,
			SystemPromptAppendix: systemPromptAppendix,
			TrustedInternal:      true, // session UI is a trusted surface (full internal coordination)
			RenderA2UI:           true, // chat SSE renders A2UI surfaces (StreamEventA2UI)
		}
		if parentRunID != "" {
			execReq.ParentRunID = &parentRunID
		}
		if rootRunID != "" {
			execReq.RootRunID = &rootRunID
		}
		// Additive run-id frame: Execute creates the run internally, so this
		// callback fires as soon as the row exists and before the agent takes
		// its first step. The earlier meta frame stays exactly as it was; a
		// client that ignores this second one is unaffected (#67).
		execReq.OnRunCreated = func(runID string) {
			_ = sseWriter.WriteData(sse.NewMetaEventWithRun(conv.ID.String(), runID))
		}
		result, err = h.agentExecutor.Execute(ctx, execReq)
	}

	if err != nil {
		h.log.Error("agent execution failed",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("agent_definition_id", agentDefID),
			slog.String("error", err.Error()),
		)
		_ = sseWriter.WriteData(sse.NewErrorEvent(friendlyProviderError(err)))
		return nil
	}

	// Safety net: Execute() returns (result, nil) even on pipeline failures —
	// the error is encoded in result.Status, not as a Go error.  The
	// StreamCallback normally emits a StreamEventError during execution, but
	// some early-failure paths (session creation, context cancellation) skip
	// the callback.  Emit the error event here so the client always sees it.
	responseText := fullResponse.String()
	if result != nil && result.Status == agents.RunStatusError && responseText == "" {
		if errMsg, ok := result.Summary["error"].(string); ok && errMsg != "" {
			h.log.Warn("agent run ended with error status",
				slog.String("conversation_id", conv.ID.String()),
				slog.String("agent_definition_id", agentDefID),
				slog.String("run_id", result.RunID),
				slog.String("error", errMsg),
			)
			sseWriter.WriteData(sse.NewErrorEvent(friendlyProviderError(fmt.Errorf("%s", errMsg))))
		}
		return result
	}

	// Safety net: a reasoner answering entirely in reasoning_content (thinking
	// mode) never delivers a non-Thought text part, so the executor surfaces the
	// answer as StreamEventThinking events and fullResponse stays empty even
	// though the run completed and persisted the final text. When that happens,
	// recover the answer from the run's persisted assistant content (or the
	// result summary) and emit it as a single token event so the client still
	// receives the final text and the assistant row is written to
	// kb.chat_messages below.
	if responseText == "" && result != nil && result.Status == agents.RunStatusSuccess {
		fallback := ""
		if runMsgs, err := h.agentRepo.FindMessagesByRunID(ctx, result.RunID); err == nil {
			fallback = lastAgentReplyText(runMsgs)
		}
		if fallback == "" {
			if t, ok := result.Summary["final_response"].(string); ok && t != "" {
				fallback = t
			}
		}
		if fallback != "" {
			responseText = fallback
			if err := sseWriter.WriteData(sse.NewTokenEvent(fallback)); err != nil {
				h.log.Warn("failed to stream fallback answer token",
					slog.String("conversation_id", conv.ID.String()),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	// Persist assistant response to kb.chat_messages with agent_run_id reference.
	//
	// A paused run is special: the streamed text is the executor's synthetic
	// "Execution paused…" sentinel, and the question the user is expected to
	// answer lives in kb.agent_questions. Persisting only the sentinel left the
	// conversation looking stalled with no visible prompt — the operator could
	// not tell what it had been asked. Persist the question (and its options)
	// instead, keeping any genuine pre-pause text in front of it.
	if result != nil && result.Status == agents.RunStatusPaused {
		h.persistPausedRunMessage(ctx, projectID, userID, conv, result, responseText)
	} else if responseText != "" || len(surfaces) > 0 {
		// A card-only answer has no text: persist the surface's summary so the
		// turn is replayable instead of vanishing from history (#427).
		text := responseText
		if text == "" {
			text = a2ui.SummaryText(surfaces)
		}
		h.persistAssistantMessage(ctx, projectID, userID, conv, result, text, surfaces)
	}

	// Derive and emit grounded citations after the run completes and before the
	// caller's `done` event. Non-fatal: derivation/load errors are logged and
	// never fail the turn. Only emitted when at least one citation exists.
	if result != nil && result.RunID != "" {
		toolCalls, tcErr := h.agentRepo.FindToolCallsByRunID(ctx, result.RunID)
		if tcErr != nil {
			h.log.Warn("failed to load tool calls for citations",
				slog.String("run_id", result.RunID),
				slog.String("error", tcErr.Error()),
			)
		} else {
			outputs := make([]citations.ToolCall, 0, len(toolCalls))
			for _, tc := range toolCalls {
				if tc != nil {
					outputs = append(outputs, citations.ToolCall{Output: tc.Output})
				}
			}
			candidates := citations.Candidates(outputs)
			cits := citations.Derive(responseText, surfaces, candidates)
			if len(cits) > 0 {
				if err := sseWriter.WriteData(sse.NewCitationsEvent(cits)); err != nil {
					h.log.Warn("failed to stream citations event",
						slog.String("conversation_id", conv.ID.String()),
						slog.String("error", err.Error()),
					)
				}
			}
		}
	}

	return result
}

// lastAgentReplyText returns the text of the last agent-authored message in a
// run's persisted messages, or "" when none carries text.
//
// The executor persists an assistant turn under the ADK event Author — the
// sanitized agent name (e.g. "coder"), never the literal "assistant" — so this
// classifies each row with agents.IsAgentReplyRole rather than comparing Role to
// "assistant" (which would match no real run and silently drop the answer). The
// predicate also excludes the user/tool/tool_result/system sides, so a raw tool
// result can never be surfaced as the assistant's answer.
func lastAgentReplyText(msgs []*agents.AgentRunMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] == nil || !agents.IsAgentReplyRole(msgs[i].Role) {
			continue
		}
		if t := agentRunMessageText(msgs[i].Content); t != "" {
			return t
		}
	}
	return ""
}

// agentRunMessageText extracts the plain-text payload of an agent run message's
// content object ({"text": "..."}), returning "" when absent.
func agentRunMessageText(content map[string]any) string {
	if content == nil {
		return ""
	}
	text, _ := content["text"].(string)
	return text
}

// persistAssistantMessage stores an assistant turn in kb.chat_messages, tagged
// with the run that produced it.
//
// The write is deliberately synchronous. It used to run in a fire-and-forget
// goroutine, but that goroutine shares the handler's DB handle — and that handle
// is not always a pool: under internal/testutil.BaseSuite it is the per-test
// transaction, i.e. one connection. The handler issues its citation query
// (FindToolCallsByRunID) immediately after this call, so a background writer
// racing it on the same connection makes pgx's stdlib driver reset the shared
// result reader; the citation query's Rows.Next then panics with
// "index out of range [0] with length 0" (issue #302). One extra round trip to
// make the turn durable before the stream reports done is the cheaper trade.
func (h *Handler) persistAssistantMessage(ctx context.Context, projectID, userID string, conv *Conversation, result *agents.ExecuteResult, text string, surfaces []a2ui.Message) {
	if text == "" && len(surfaces) == 0 {
		return
	}
	var retrievalCtx json.RawMessage
	retrieval := make(map[string]any)
	if result != nil && result.RunID != "" {
		retrieval["agent_run_id"] = result.RunID
	}
	if len(surfaces) > 0 {
		// Raw surfaces ride along so a history item can render the card, while
		// the row's text keeps every text-only replay path non-empty.
		retrieval["a2ui"] = surfaces
	}
	if len(retrieval) > 0 {
		if rc, err := json.Marshal(retrieval); err == nil {
			retrievalCtx = rc
		}
	}
	if _, err := h.svc.AddMessage(ctx, projectID, userID, conv.ID, AddMessageRequest{
		Role:             RoleAssistant,
		Content:          text,
		RetrievalContext: retrievalCtx,
	}); err != nil {
		h.log.Warn("failed to persist assistant message",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("error", err.Error()),
		)
	}
}

// pauseSentinels are the synthetic texts the executor streams when it parks a
// run. They carry no information for the user and must not become the only
// assistant turn in the conversation.
var pauseSentinels = []string{
	"Execution paused. Waiting for user response to your question.",
	"Execution paused. Waiting for user approval of tool call(s).",
	"Step limit reached. Run has been paused.",
}

func stripPauseSentinels(text string) string {
	for _, s := range pauseSentinels {
		text = strings.ReplaceAll(text, s, "")
	}
	return strings.TrimSpace(text)
}

// persistPausedRunMessage writes the pending question into the conversation
// history so reopening the chat shows what the agent asked, not a bare stall.
func (h *Handler) persistPausedRunMessage(ctx context.Context, projectID, userID string, conv *Conversation, result *agents.ExecuteResult, streamedText string) {
	body := stripPauseSentinels(streamedText)

	questionID, _ := result.Summary["question_id"].(string)
	if questionID != "" && h.agentRepo != nil {
		if q, err := h.agentRepo.FindQuestionByID(ctx, questionID); err == nil && q != nil {
			qtext := formatQuestionForChat(q)
			if body != "" {
				body += "\n\n" + qtext
			} else {
				body = qtext
			}
		}
	}

	if body == "" {
		body = "Execution paused."
	}
	h.persistAssistantMessage(ctx, projectID, userID, conv, result, body, nil)
}

// formatQuestionForChat renders a question and its options as markdown.
func formatQuestionForChat(q *agents.AgentQuestion) string {
	var b strings.Builder
	b.WriteString(q.Question)
	if len(q.Options) > 0 {
		b.WriteString("\n\n")
		for _, o := range q.Options {
			b.WriteString("- **")
			b.WriteString(o.Label)
			b.WriteString("**")
			if o.Description != "" {
				b.WriteString(" — ")
				b.WriteString(o.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\nReply with an option or your own answer.")
	}
	return b.String()
}

// resumeForAnswer answers a pending ask_user question and streams the resumed
// run into the caller's SSE stream.
//
// Before this, a message typed while a question was pending started a brand-new
// run: the paused run and its question were left orphaned (the question stayed
// pending forever) and the fresh agent had no idea what the user was replying
// to. Returns false when the question cannot be answered by this turn, in which
// case the caller falls through to a normal turn.
//
// answer is the value recorded on the question; message is what the resumed
// agent reads. Typed text uses the message for both; an A2UI card click records
// the option the user picked while the agent still reads the framed surface
// action (#223).
func (h *Handler) resumeForAnswer(ctx context.Context, sseWriter *sse.Writer, conv *Conversation, q *agents.AgentQuestion, answer, message string, user *auth.AuthUser) bool {
	if h.agentExecutor == nil || h.agentRepo == nil {
		return false
	}

	run, err := h.agentRepo.FindRunByID(ctx, q.RunID)
	if err != nil || run == nil || run.Status != agents.RunStatusPaused {
		return false
	}

	// Tool-confirmation pauses are answered by the approval card, which records
	// the approve/reject decision. Free text there is not an answer — and neither
	// is an A2UI surface action.
	if !pauseAnswerableByChat(agents.SuspendSignalFromMap(run.SuspendContext)) {
		return false
	}

	// Refuse BEFORE claiming the question. Claiming first and then discovering
	// the run cannot continue (execution disabled, step budget exhausted) would
	// consume the question permanently and leave the run paused with nothing to
	// answer — the exact stall this fix exists to remove.
	if err := h.agentExecutor.EnsureExecutionEnabled(); err != nil {
		_ = sseWriter.WriteData(sse.NewErrorEvent("Agent execution is disabled: " + err.Error()))
		return true
	}
	if run.StepCount >= agents.MaxTotalStepsPerRun {
		_ = sseWriter.WriteData(sse.NewErrorEvent("This run reached its step limit and cannot be resumed. Send a new message to start a fresh run."))
		return true
	}

	if answer == "" {
		answer = message
	}
	claimed, err := h.agentRepo.AnswerQuestion(ctx, q.ID, answer, user.ID)
	if err != nil {
		_ = sseWriter.WriteData(sse.NewErrorEvent("Failed to record answer"))
		return true
	}
	if !claimed {
		// Someone else answered or cancelled it first; treat as a normal turn.
		return false
	}

	agent, err := h.agentRepo.FindByID(ctx, run.AgentID, nil)
	if err != nil || agent == nil {
		_ = h.agentRepo.ReopenQuestion(ctx, q.ID)
		_ = sseWriter.WriteData(sse.NewErrorEvent("Failed to resume the paused run"))
		return true
	}
	agentDef, _ := h.agentRepo.ResolveDefinitionForAgent(ctx, agent)

	maxSteps := agents.MaxTotalStepsPerRun
	resumedFrom := run.ID
	preCreated, err := h.agentRepo.CreateRunWithOptions(ctx, agents.CreateRunOptions{
		AgentID:          run.AgentID,
		MaxSteps:         &maxSteps,
		ResumedFrom:      &resumedFrom,
		InitialStepCount: run.StepCount,
		TriggerMetadata:  run.TriggerMetadata,
		// The resumed run owns the work item; MarkRunResumed finalizes the
		// paused checkpoint, so the subject must not be lost on the child.
		SubjectObjectID:   run.SubjectObjectID,
		SubjectObjectType: run.SubjectObjectType,
	})
	if err != nil {
		_ = h.agentRepo.ReopenQuestion(ctx, q.ID)
		_ = sseWriter.WriteData(sse.NewErrorEvent("Failed to resume the paused run"))
		return true
	}
	if run.SuspendContext != nil {
		sc := make(map[string]any, len(run.SuspendContext)+1)
		for k, v := range run.SuspendContext {
			sc[k] = v
		}
		sc["resume_run_id"] = preCreated.ID
		_ = h.agentRepo.UpdateSuspendContext(ctx, run.ID, sc)
	}

	userMessage := fmt.Sprintf(
		"Previously you asked: %q\nThe user responded: %q\nContinue from where you left off.",
		q.Question, message,
	)

	var fullResponse strings.Builder
	var surfaces []a2ui.Message
	streamCallback := func(event agents.StreamEvent) {
		switch event.Type {
		case agents.StreamEventTextDelta:
			fullResponse.WriteString(event.Text)
			_ = sseWriter.WriteData(sse.NewTokenEvent(event.Text))
		case agents.StreamEventThinking:
			_ = sseWriter.WriteData(sse.NewThinkingEvent(event.ID, event.Role, event.Text, event.Done))
		case agents.StreamEventToolCallStart:
			_ = sseWriter.WriteData(sse.NewMCPToolEventWithID(event.Tool, "started", event.ID, event.Input, ""))
		case agents.StreamEventToolCallEnd:
			status := event.Status
			if status == "" {
				status = "completed"
				if event.Error != "" {
					status = "error"
				}
			}
			_ = sseWriter.WriteData(sse.NewMCPToolEventWithID(event.Tool, status, event.ID, event.Output, event.Error))
		case agents.StreamEventError:
			_ = sseWriter.WriteData(sse.NewErrorEvent(event.Error))
		case agents.StreamEventToolApproval:
			_ = sseWriter.WriteData(sse.NewApprovalEvent(event.Tool, event.Input, event.QuestionID))
		case agents.StreamEventA2UI:
			// Persist/replay only the authoritative frame: progressive frames are
			// partial and a teardown deletes the surface, so appending them would
			// store a duplicated or already-deleted card (#427).
			if event.Final {
				surfaces = event.A2UI
			}
			_ = sseWriter.WriteData(sse.NewUIEvent(event.SurfaceID, event.A2UI))
		case agents.StreamEventSubagent:
			_ = sseWriter.WriteData(sse.NewSubagentEvent(event.SubagentID, event.ParentRunID, event.RunID, event.AgentName, event.Task, event.Status, event.Error))
		}
	}

	// The resumed run outlives the SSE request for the same reason the initial
	// run does (issue #1149): a browser reload must not fail it.
	runCtx := agents.DetachedRunContext(ctx)
	result, err := h.agentExecutor.Resume(runCtx, run, agents.ExecuteRequest{
		Agent:           agent,
		AgentDefinition: agentDef,
		ProjectID:       user.ProjectID,
		OrgID:           user.OrgID,
		UserMessage:     userMessage,
		UserID:          user.ID,
		StreamCallback:  streamCallback,
		RenderA2UI:      true, // chat SSE resume renders A2UI surfaces (StreamEventA2UI)
		PreCreatedRun:   preCreated,
	})
	if err != nil {
		h.log.Error("failed to resume run after chat answer",
			slog.String("run_id", run.ID),
			slog.String("question_id", q.ID),
			slog.String("error", err.Error()),
		)
		// Hand the question back and reach a terminal state on both runs: a
		// consumed question with a still-paused parent has no recovery path
		// (the stale reaper only touches `running`), and the pre-created run
		// would otherwise leak as `running`.
		_ = h.agentRepo.ReopenQuestion(ctx, q.ID)
		_ = h.agentRepo.FailRunWithSteps(ctx, run.ID, fmt.Sprintf("resume failed: %s", err.Error()), run.StepCount)
		_ = h.agentRepo.FailRunWithSteps(ctx, preCreated.ID, "resume failed before start", run.StepCount)
		_ = sseWriter.WriteData(sse.NewErrorEvent(friendlyProviderError(err)))
		return true
	}

	if result != nil {
		if result.Cleanup != nil {
			go result.Cleanup()
		}

		// A reasoner that answers entirely in thinking mode delivers no text
		// delta, so recover the answer from the run's persisted messages (same
		// safety net as the first leg in streamAgentChat).
		text := fullResponse.String()
		if text == "" && result.Status == agents.RunStatusSuccess {
			if runMsgs, msgErr := h.agentRepo.FindMessagesByRunID(ctx, result.RunID); msgErr == nil {
				text = lastAgentReplyText(runMsgs)
			}
			if text == "" {
				if t, ok := result.Summary["final_response"].(string); ok {
					text = t
				}
			}
			if text != "" {
				_ = sseWriter.WriteData(sse.NewTokenEvent(text))
			}
		}

		switch result.Status {
		case agents.RunStatusPaused:
			h.persistPausedRunMessage(ctx, user.ProjectID, user.ID, conv, result, text)
		case agents.RunStatusError:
			if errMsg, ok := result.Summary["error"].(string); ok && errMsg != "" {
				_ = sseWriter.WriteData(sse.NewErrorEvent(friendlyProviderError(fmt.Errorf("%s", errMsg))))
			}
		default:
			if text != "" || len(surfaces) > 0 {
				persistText := text
				if persistText == "" {
					persistText = a2ui.SummaryText(surfaces)
				}
				h.persistAssistantMessage(ctx, user.ProjectID, user.ID, conv, result, persistText, surfaces)
			}
		}
	}
	return true
}

// QueryStreamRequest is the request body for the stateless query endpoint.
type QueryStreamRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id,omitempty"` // optional: continue a previous session
	Branch         string `json:"branch,omitempty"`
	Namespace      string `json:"namespace,omitempty"`       // optional: scopes all MCP tool calls to this namespace
	ParentRunID    string `json:"parent_run_id,omitempty"`   // optional: calling agent's run ID for parent→child linkage
	RootRunID      string `json:"root_run_id,omitempty"`     // optional: top-level orchestration run ID
	SpawnSource    string `json:"spawn_source,omitempty"`    // optional: mechanism that opened the conversation (e.g. "search-knowledge")
	ResponseFormat string `json:"response_format,omitempty"` // optional: hint prepended to message to control answer shape/verbosity

	// Per-call graph type scope. The internal graph-query-agent carries no
	// allowlist of its own, so a caller-supplied scope is threaded into its run
	// and preserved end-to-end. It can only NARROW the internal run, never widen
	// it (issue #1695 follow-up; used by the search-knowledge MCP loopback).
	ObjectTypes       []string `json:"object_types,omitempty"`
	RelationshipTypes []string `json:"relationship_types,omitempty"`

	// Per-call branch read boundary (#265; used by the search-knowledge MCP
	// loopback). BranchScope carries the caller's scope when it has one.
	BranchScope *branchscope.Config `json:"branch_scope,omitempty"`
	// BranchScopeMode restates the caller's boundary when it is not a scope:
	// "main_only" (an agent run that reads the main graph only) or "absent" (no
	// boundary at all, e.g. a human MCP client). Other /query clients omit it and
	// keep the pre-existing behaviour.
	BranchScopeMode string `json:"branch_scope_mode,omitempty"`
}

// withQueryTypeScope threads a caller-supplied graph type scope into the internal
// query agent's run context. The internal graph-query-agent definition carries no
// allowlist, so the executor's own scope derivation is a no-op and this scope is
// preserved for the whole run. A nil/empty scope leaves the context unchanged
// (unrestricted). It can only NARROW the unrestricted internal run, never widen it
// (issue #1695 follow-up; used by the search-knowledge MCP loopback).
func withQueryTypeScope(ctx context.Context, req QueryStreamRequest) context.Context {
	if req.ObjectTypes == nil && req.RelationshipTypes == nil {
		return ctx
	}
	return mcp.ContextWithTypeScope(ctx, mcp.NewTypeScope(req.ObjectTypes, req.RelationshipTypes))
}

// withQueryBranchScope installs the originating caller's branch boundary on the
// internal query agent's run. Unlike a delegated sub-agent — whose own declaration
// governs, an undeclared one meaning main only — this run executes on the caller's
// behalf, so the boundary is installed as fixed and must not be replaced by the
// internal definition's absent scope. It can only narrow the originating run,
// never widen it (#265).
//
// The three modes exist because "no branch reads" and "no boundary at all" are
// different callers: an agent run is main only and must not name branches, while a
// human MCP client never had a boundary and keeps its branch arguments.
func withQueryBranchScope(ctx context.Context, req QueryStreamRequest) (context.Context, error) {
	switch req.BranchScopeMode {
	case "scoped":
		if req.BranchScope == nil {
			return ctx, fmt.Errorf("branch_scope_mode %q requires branch_scope", req.BranchScopeMode)
		}
		scope, err := mcp.NewBranchScope(*req.BranchScope)
		if err != nil {
			return ctx, err
		}
		return mcp.ContextWithFixedBranchScope(ctx, scope), nil
	case "main_only":
		// Explicit main-only boundary: the internal run may not name a branch.
		return mcp.ContextWithBranchScope(ctx, nil), nil
	case "absent":
		// The caller had no boundary; the internal run keeps that state instead of
		// being narrowed to main only.
		return mcp.ContextWithAbsentBranchScope(ctx), nil
	case "":
		// Other /query clients: a bare branch_scope is still honoured as a scope.
		if req.BranchScope != nil {
			scope, err := mcp.NewBranchScope(*req.BranchScope)
			if err != nil {
				return ctx, err
			}
			return mcp.ContextWithFixedBranchScope(ctx, scope), nil
		}
		// No mode and no scope: a direct /query caller (the web UI's "Ask the
		// graph" is one). It never had a branch boundary, and letting the executor
		// install the main-only default would silently drop the branch it asked
		// about, so restate the absence.
		return mcp.ContextWithAbsentBranchScope(ctx), nil
	default:
		return ctx, fmt.Errorf("branch_scope_mode %q is not one of scoped, main_only, absent", req.BranchScopeMode)
	}
}

// shouldReuseToolSession reports whether a /query request is a stateless tool
// call that must reuse one conversation instead of creating a row per call.
//
// It is deliberately false when the caller has an agent run context: an in-run
// sub-chat keeps the #2266 parent-conversation linkage and its per-call row.
// An explicit session_id also wins, and a request without a tool kind
// (spawn_source) keeps the pre-existing per-call behaviour so generic /query
// clients (e.g. the web UI's "Ask the graph") are unchanged.
func shouldReuseToolSession(parentRunID, conversationID, spawnSource string) bool {
	return parentRunID == "" && conversationID == "" && strings.TrimSpace(spawnSource) != ""
}

// toolSessionIdentity returns the stable caller identity used in a stateless
// tool-session key. The resolved internal user id is the strongest identity and
// is shared by every credential shape (human JWT, user-owned emt_* API key,
// agent MCP endpoint key). An ownerless credential has no user id, so its API
// token id keeps unrelated machine callers apart.
func toolSessionIdentity(caller *auth.AuthUser) string {
	if caller == nil {
		return ""
	}
	if caller.ID != "" {
		return caller.ID
	}
	return caller.APITokenID
}

// QueryStream handles POST /api/projects/:projectId/query.
// It finds (or lazily creates) the project's graph-query-agent and streams the response
// using the same agent chat path as /api/chat/stream. The agent is internal — it never
// appears in the agent definitions list. The conversation it runs in IS persisted (so
// messages, runs and pending questions stay traceable) and, when the caller supplies a
// parent, is linked to that parent conversation.
func (h *Handler) QueryStream(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req QueryStreamRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return apperror.ErrBadRequest.WithMessage("message is required")
	}
	if req.Branch != "" {
		message = fmt.Sprintf("[Branch: %s]\n\n%s", req.Branch, message)
	}
	if req.ResponseFormat != "" {
		// Append to user message so the LLM sees it as an immediate instruction
		// before generating the final answer (system-only injection is ignored by some models).
		message = message + "\n\n[Response format: " + req.ResponseFormat + "]"
	}

	// Accept parent/root run IDs from headers as well (for MCP tool callers that can't modify the body).
	parentRunID := req.ParentRunID
	if parentRunID == "" {
		parentRunID = c.Request().Header.Get("X-Parent-Run-Id")
	}
	rootRunID := req.RootRunID
	if rootRunID == "" {
		rootRunID = c.Request().Header.Get("X-Root-Run-Id")
	}

	ctx := c.Request().Context()

	// Start query.run span — mirrors ask.run / agent.run, covers the full query execution.
	msgPreview := message
	if len(msgPreview) > 200 {
		msgPreview = msgPreview[:200] + "..."
	}
	ctx, span := tracing.Start(ctx, "query.run",
		attribute.String("memory.project.id", projectID),
		attribute.String("memory.query.user_id", user.ID),
		attribute.String("memory.query.message_preview", msgPreview),
	)
	defer span.End()

	// Ensure the project ID from the URL param is in the context so that the
	// credential resolver (ResolveAny) can look up org-level credentials even
	// when the caller authenticates via user JWT (which does not set X-Project-ID).
	if auth.ProjectIDFromContext(ctx) == "" && projectID != "" {
		ctx = auth.ContextWithProjectID(ctx, projectID)
	}
	if req.Namespace != "" {
		ctx = auth.ContextWithNamespace(ctx, req.Namespace)
	}
	ctx = withQueryTypeScope(ctx, req)
	ctx, branchScopeErr := withQueryBranchScope(ctx, req)
	if branchScopeErr != nil {
		return apperror.NewBadRequest("branch_scope: " + branchScopeErr.Error())
	}

	// Fail fast if no LLM provider is configured. Probe the model factory before
	// opening the SSE stream so clients get a proper HTTP error code, not a
	// success status with an error buried in the stream.
	if h.modelFactory != nil {
		probeModel, probeErr := h.modelFactory.CreateModel(ctx)
		if probeErr != nil {
			errMsg := probeErr.Error()
			if strings.Contains(errMsg, "no LLM credentials configured") ||
				strings.Contains(errMsg, "no_provider") ||
				strings.Contains(errMsg, "provider config found for organization") ||
				strings.Contains(errMsg, "no generative model configured") {
				return apperror.New(http.StatusServiceUnavailable, "no_provider", errNoProviderMsg)
			}
			return apperror.New(http.StatusServiceUnavailable, "provider_error",
				friendlyProviderError(probeErr))
		}
		if closer, ok := probeModel.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	// Ensure the graph-query-agent exists (idempotent, internal visibility).
	agentDef, err := h.agentRepo.EnsureGraphQueryAgent(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("failed to ensure graph-query-agent", err)
	}

	agentDefUUID, err := uuid.Parse(agentDef.ID)
	if err != nil {
		return apperror.NewInternal("invalid graph-query-agent ID", err)
	}

	// Reuse an existing conversation if the caller provided one, otherwise create a new one.
	var conv *Conversation
	if req.ConversationID != "" {
		convUUID, parseErr := uuid.Parse(req.ConversationID)
		if parseErr == nil {
			conv, _ = h.svc.GetConversation(ctx, projectID, user.ID, convUUID)
		}
	}
	if conv == nil {
		if shouldReuseToolSession(parentRunID, req.ConversationID, req.SpawnSource) {
			// Stateless tool call (a direct MCP/API caller with no agent run
			// context): one reused conversation per (project, caller identity,
			// tool), not one per call. The in-run path below is untouched.
			conv, _, err = h.svc.GetOrCreateToolSession(ctx, projectID, user.ID, toolSessionIdentity(user), req.SpawnSource, message)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return apperror.NewInternal("failed to resolve tool session", err)
			}
		} else {
			title := message
			if len(title) > 50 {
				title = title[:50] + "..."
			}

			// Persist the spawning conversation when the caller supplied a parent
			// run (the search-knowledge loopback forwards its own run id). The run's
			// session resolves to the conversation; only link one the caller can see
			// in this project, so a caller-supplied run id cannot graft a chat onto
			// a foreign conversation.
			var parentConversationID string
			if parentRunID != "" && h.agentRepo != nil {
				if id := h.agentRepo.ConversationIDForRun(ctx, parentRunID); id != "" {
					if parentUUID, parseErr := uuid.Parse(id); parseErr == nil &&
						h.svc.CanAccessConversation(ctx, projectID, user.ID, parentUUID) {
						parentConversationID = id
					}
				}
			}

			createReq := CreateConversationRequest{Title: title, Message: message}
			if parentConversationID != "" {
				createReq.ParentConversationID = &parentConversationID
			}
			if req.SpawnSource != "" {
				// Public callers reach this endpoint too; cap the label so a
				// caller-supplied source cannot store an unbounded string in the rail.
				// Truncate by rune, not byte, so a multi-byte label never becomes
				// invalid UTF-8 and fails the insert.
				source := req.SpawnSource
				if runes := []rune(source); len(runes) > 64 {
					source = string(runes[:64])
				}
				createReq.SpawnSource = &source
			}

			var createErr error
			conv, createErr = h.svc.CreateConversation(ctx, projectID, user.ID, createReq)
			if createErr != nil {
				span.RecordError(createErr)
				span.SetStatus(codes.Error, createErr.Error())
				return apperror.NewInternal("failed to create conversation", createErr)
			}
		}
	}
	conv.AgentDefinitionID = &agentDefUUID
	span.SetAttributes(attribute.String("memory.query.conversation_id", conv.ID.String()))
	if err := h.svc.SetAgentDefinitionID(ctx, projectID, user.ID, conv.ID, &agentDefUUID); err != nil {
		h.log.Warn("failed to set agent_definition_id on query conversation",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("error", err.Error()),
		)
	}

	// Start SSE stream.
	sseWriter := sse.NewWriter(c.Response().Writer)
	if err := sseWriter.Start(); err != nil {
		return apperror.ErrInternal.WithMessage("failed to start SSE stream")
	}

	if err := sseWriter.WriteData(sse.NewMetaEvent(conv.ID.String())); err != nil {
		return nil
	}

	queryResult := h.streamAgentChat(ctx, conv, message, projectID, user.OrgID, user.ID, sseWriter, parentRunID, rootRunID, req.ResponseFormat)
	span.SetStatus(codes.Ok, "")
	// Emit done event with run_id so callers (e.g. search-knowledge MCP tool) can trace the internal run.
	var queryRunID string
	if queryResult != nil {
		queryRunID = queryResult.RunID
	}
	sseWriter.WriteData(sse.NewDoneEventWithRun(queryRunID))
	sseWriter.Close()
	if queryResult != nil && queryResult.Cleanup != nil {
		go queryResult.Cleanup()
	}
	return nil
}

// AskStreamRequest is the request body for the stateless CLI assistant endpoint.
type AskStreamRequest struct {
	Message string `json:"message"`
	Runtime string `json:"runtime,omitempty"` // "python" (default) or "go"
	V2      bool   `json:"v2,omitempty"`      // use code-generation agent variant
}

// AskStream handles POST /api/projects/:projectId/ask and POST /api/ask.
// It finds (or lazily creates) the project's cli-assistant-agent and streams the response.
// The agent is aware of the authentication/project context and adapts its behaviour accordingly:
//   - No auth       → RequireAuth middleware blocks the request before reaching here
//   - Auth, no project → streams a helpful SSE response explaining how to set up a project
//   - Auth + project   → full agent execution with documentation + task tools
//
// The handler accepts OAuth tokens, emt_* project tokens, and standalone API keys.
func (h *Handler) AskStream(c echo.Context) error {
	user := auth.MustGetUser(c)

	// Resolve project ID: URL param > API token project > X-Project-ID header.
	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}

	var req AskStreamRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return apperror.ErrBadRequest.WithMessage("message is required")
	}

	ctx := c.Request().Context()

	// Start ask.run span — mirrors agent.run, covers the full ask execution.
	msgPreview := message
	if len(msgPreview) > 200 {
		msgPreview = msgPreview[:200] + "..."
	}
	ctx, span := tracing.Start(ctx, "ask.run",
		attribute.String("memory.project.id", projectID),
		attribute.String("memory.ask.user_id", user.ID),
		attribute.String("memory.ask.message_preview", msgPreview),
	)
	defer span.End()

	// Inject project ID into context so credential resolver can locate org-level creds.
	if auth.ProjectIDFromContext(ctx) == "" && projectID != "" {
		ctx = auth.ContextWithProjectID(ctx, projectID)
	}

	// Build context-awareness prefix that is prepended to the user message so the
	// agent always knows the current auth/project state without needing a tool call.
	contextPrefix := buildAskContextPrefix(user, projectID)
	augmentedMessage := contextPrefix + message

	// When there is no project context, use the user's org's first project as infrastructure
	// (for agent/conversation DB rows which require a valid project FK), while still telling
	// the agent via the context prefix that no project is selected.
	// This allows global tools (list_traces, create_project, etc.) to work without a project.
	agentProjectID := projectID
	if agentProjectID == "" {
		// Look up infrastructure project (scoped to org if known, otherwise first available).
		if infraProject, err2 := h.agentRepo.GetFirstProjectIDByOrgID(ctx, user.OrgID); err2 == nil && infraProject != "" {
			agentProjectID = infraProject
			// Inject the infra project into context for LLM provider resolution.
			ctx = auth.ContextWithProjectID(ctx, agentProjectID)
		}
	}
	if agentProjectID == "" {
		// No project available at all — fall back to a helpful static message.
		sseWriter := sse.NewWriter(c.Response().Writer)
		if err := sseWriter.Start(); err != nil {
			return apperror.ErrInternal.WithMessage("failed to start SSE stream")
		}
		if err := sseWriter.WriteData(sse.NewMetaEvent("00000000-0000-0000-0000-000000000000")); err != nil {
			return nil
		}
		sseWriter.WriteData(sse.NewTokenEvent("No projects found in your account. Create one first:\n\n```bash\nmemory projects create --name \"My Project\"\n```"))
		sseWriter.WriteData(sse.NewDoneEvent())
		sseWriter.Close()
		return nil
	}

	// Probe the LLM provider before opening the SSE stream so callers get a proper
	// HTTP error instead of a success status with an error buried in the stream.
	if h.modelFactory != nil {
		probeModel, probeErr := h.modelFactory.CreateModel(ctx)
		if probeErr != nil {
			errMsg := probeErr.Error()
			if strings.Contains(errMsg, "no LLM credentials configured") ||
				strings.Contains(errMsg, "no_provider") ||
				strings.Contains(errMsg, "provider config found for organization") ||
				strings.Contains(errMsg, "no generative model configured") {
				return apperror.New(http.StatusServiceUnavailable, "no_provider", errNoProviderMsg)
			}
			return apperror.New(http.StatusServiceUnavailable, "provider_error",
				friendlyProviderError(probeErr))
		}
		if closer, ok := probeModel.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	// Ensure the cli-assistant-agent exists for this project (idempotent, internal).
	// Use v2 code-gen variant if requested by client or enabled server-wide.
	useV2 := req.V2 || h.askV2Default
	var agentDef *agents.AgentDefinition
	var ensureErr error
	if useV2 {
		agentDef, ensureErr = h.agentRepo.EnsureCliAssistantAgentV2(ctx, agentProjectID, req.Runtime)
	} else {
		agentDef, ensureErr = h.agentRepo.EnsureCliAssistantAgent(ctx, agentProjectID, req.Runtime)
	}
	if ensureErr != nil {
		return apperror.NewInternal("failed to ensure cli-assistant-agent", ensureErr)
	}

	agentDefUUID, err := uuid.Parse(agentDef.ID)
	if err != nil {
		return apperror.NewInternal("invalid cli-assistant-agent ID", err)
	}

	// Create a transient conversation for this ask (not persisted to user history).
	// Pass the original projectID (may be "") — CreateConversation handles "" as NULL project.
	title := message
	if len(title) > 50 {
		title = title[:50] + "..."
	}
	conv, err := h.svc.CreateConversation(ctx, projectID, user.ID, CreateConversationRequest{
		Title:   title,
		Message: augmentedMessage,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return apperror.NewInternal("failed to create conversation", err)
	}
	conv.AgentDefinitionID = &agentDefUUID
	span.SetAttributes(attribute.String("memory.ask.conversation_id", conv.ID.String()))
	if err := h.svc.SetAgentDefinitionID(ctx, projectID, user.ID, conv.ID, &agentDefUUID); err != nil {
		h.log.Warn("failed to set agent_definition_id on ask conversation",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("error", err.Error()),
		)
	}

	// Start SSE stream.
	sseWriter := sse.NewWriter(c.Response().Writer)
	if err := sseWriter.Start(); err != nil {
		return apperror.ErrInternal.WithMessage("failed to start SSE stream")
	}

	if err := sseWriter.WriteData(sse.NewMetaEvent(conv.ID.String())); err != nil {
		return nil
	}

	askResult := h.streamAgentChat(ctx, conv, augmentedMessage, agentProjectID, user.OrgID, user.ID, sseWriter, "", "", "")
	span.SetStatus(codes.Ok, "")
	sseWriter.WriteData(sse.NewDoneEvent())
	sseWriter.Close()
	if askResult != nil && askResult.Cleanup != nil {
		go askResult.Cleanup()
	}
	return nil
}

// buildAskContextPrefix constructs a short context block prepended to the user's
// RememberStreamRequest is the request body for the domain-aware remember endpoint.
type RememberStreamRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id,omitempty"` // optional: continue a previous session
	SchemaPolicy   string `json:"schema_policy,omitempty"`   // "reuse_only" (default), "auto", "ask", "enrich"
	DryRun         bool   `json:"dry_run,omitempty"`         // if true, extraction branch is not merged
	Namespace      string `json:"namespace,omitempty"`       // optional: scope graph objects to a namespace
	ParentRunID    string `json:"parent_run_id,omitempty"`
	RootRunID      string `json:"root_run_id,omitempty"`
	// Mode controls response style: "stream" (default SSE), "sync" (JSON, wait), "async" (JSON 202, fire-and-forget).
	Mode string `json:"mode,omitempty"`
	// Guide is an optional natural-language hint that tells the agent how to interpret
	// the content (e.g. "this is my shopping list for my birthday party").
	// It is injected as a prefix in the agent message for both text and file paths.
	Guide string `json:"guide,omitempty"`
	// SourceURL is the link a shared capture came from (the iOS share extension
	// posts a Safari link here). It is recorded on the document as source_url
	// with source_type "share", so the document points at its origin instead of
	// only carrying the URL as prose.
	SourceURL string `json:"source_url,omitempty"`
}

// rememberDocumentParams builds the document a text remember call persists.
// A shared capture carries a source URL, so it becomes a "share" document that
// points at its origin (a shared link is a document with a source, not prose
// that happens to contain a URL) instead of the anonymous "manual" document an
// ordinary remember creates.
func rememberDocumentParams(projectID, userID, message, sourceURL string) documents.CreateParams {
	filename := "remember.txt"
	sourceType := "manual"
	params := documents.CreateParams{
		ProjectID:       projectID,
		CreatedByUserID: documents.OptionalUserID(userID),
		Filename:        &filename,
		Content:         &message,
		SourceType:      &sourceType,
	}
	if origin := strings.TrimSpace(sourceURL); isHTTPURL(origin) {
		sharedType := "share"
		sharedName := "shared-link.txt"
		params.SourceType = &sharedType
		params.SourceURL = &origin
		params.Filename = &sharedName
	}
	return params
}

// isHTTPURL reports whether s is an absolute http(s) URL.
//
// `source_url` is rendered as a link and used as the document's display name,
// and `POST /remember` is a general endpoint (the share extension is only one
// caller), so the field must not accept arbitrary text: a non-URL origin is
// remembered as text and the document keeps its manual shape.
func isHTTPURL(s string) bool {
	parsed, err := url.Parse(s)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != ""
}

// rememberFileText resolves the text a file remember runs on: the converted
// content when the parser produced any, otherwise the caller's guide/comment.
// A photo with no OCR text (a picture rather than a document photo) is still a
// capture the user meant to keep, and the comment is the only text there is;
// returning "" means neither exists, which is the no_content failure.
//
// fromGuide reports that the text is the guide's, so the caller does not also
// prepend the same guide to the agent message a second time.
func rememberFileText(content *string, guide string) (text string, fromGuide bool) {
	if content != nil {
		if converted := strings.TrimSpace(*content); converted != "" {
			return converted, false
		}
	}
	trimmedGuide := strings.TrimSpace(guide)
	return trimmedGuide, trimmedGuide != ""
}

// RememberStream handles POST /api/projects/:projectId/remember.
// It is the unified domain-aware memory endpoint: raw text is persisted as a document,
// the domain-remember-agent classifies it, discovers or reuses a schema pack, and
// extraction is auto-queued by finalize-discovery.
//
// Response mode is controlled by the "mode" field (or X-Remember-Mode header):
//   - "stream" (default) — SSE stream of agent events
//   - "sync"             — wait for completion, return JSON {run_id, status, summary}
//   - "async"            — fire-and-forget, return 202 JSON {run_id}
func (h *Handler) RememberStream(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req RememberStreamRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return apperror.ErrBadRequest.WithMessage("message is required")
	}

	// Normalise schema_policy — default to "reuse_only".
	schemaPolicy := req.SchemaPolicy
	if schemaPolicy == "" {
		schemaPolicy = "reuse_only"
	}
	if schemaPolicy != "auto" && schemaPolicy != "reuse_only" && schemaPolicy != "ask" && schemaPolicy != "enrich" {
		return apperror.ErrBadRequest.WithMessage("schema_policy must be one of: auto, reuse_only, ask, enrich")
	}

	// Normalise mode — accept header fallback.
	mode := req.Mode
	if mode == "" {
		mode = c.Request().Header.Get("X-Remember-Mode")
	}
	if mode == "" {
		mode = "stream"
	}
	if mode != "stream" && mode != "sync" && mode != "async" {
		return apperror.ErrBadRequest.WithMessage("mode must be one of: stream, sync, async")
	}

	// Accept parent/root run IDs from headers as well.
	parentRunID := req.ParentRunID
	if parentRunID == "" {
		parentRunID = c.Request().Header.Get("X-Parent-Run-Id")
	}
	rootRunID := req.RootRunID
	if rootRunID == "" {
		rootRunID = c.Request().Header.Get("X-Root-Run-Id")
	}

	ctx := c.Request().Context()

	msgPreview := message
	if len(msgPreview) > 200 {
		msgPreview = msgPreview[:200] + "..."
	}
	ctx, span := tracing.Start(ctx, "remember.run",
		attribute.String("memory.project.id", projectID),
		attribute.String("memory.remember.user_id", user.ID),
		attribute.String("memory.remember.schema_policy", schemaPolicy),
		attribute.String("memory.remember.mode", mode),
		attribute.Bool("memory.remember.dry_run", req.DryRun),
		attribute.String("memory.remember.message_preview", msgPreview),
	)
	defer span.End()

	if auth.ProjectIDFromContext(ctx) == "" && projectID != "" {
		ctx = auth.ContextWithProjectID(ctx, projectID)
	}
	if req.Namespace != "" {
		ctx = auth.ContextWithNamespace(ctx, req.Namespace)
	}

	// Probe LLM provider before opening SSE stream.
	if h.modelFactory == nil {
		return apperror.New(http.StatusServiceUnavailable, "no_provider", errNoProviderMsg)
	}
	if h.modelFactory != nil {
		probeModel, probeErr := h.modelFactory.CreateModel(ctx)
		if probeErr != nil {
			errMsg := probeErr.Error()
			if strings.Contains(errMsg, "no LLM credentials configured") ||
				strings.Contains(errMsg, "no_provider") ||
				strings.Contains(errMsg, "provider config found for organization") ||
				strings.Contains(errMsg, "no generative model configured") {
				return apperror.New(http.StatusServiceUnavailable, "no_provider", errNoProviderMsg)
			}
			return apperror.New(http.StatusServiceUnavailable, "provider_error",
				friendlyProviderError(probeErr))
		}
		if closer, ok := probeModel.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	// Persist raw text as a document so the agent can call classify-document with its ID.
	// documents.Service.Create deduplicates by content hash, so repeated identical calls are safe.
	var documentID string
	if h.docSvc != nil {
		doc, _, docErr := h.docSvc.Create(ctx, rememberDocumentParams(projectID, user.ID, message, req.SourceURL))
		if docErr != nil {
			return apperror.NewInternal("failed to create document for remember", docErr)
		}
		documentID = doc.ID
		span.SetAttributes(attribute.String("memory.remember.document_id", documentID))
	}

	// Resolve which agent definition powers this /remember call.
	// Default: the canonical "domain-remember-agent" (idempotent, auto-created).
	// Override: a project setting (category=remember_config, key=agent_name,
	// value={"name":"<custom-agent-definition-name>"}) redirects to a different
	// definition — used for experiments without touching the baseline agent.
	var agentDef *agents.AgentDefinition
	if setting, settingErr := h.agentRepo.GetProjectSetting(ctx, projectID,
		agents.SettingsCategoryRememberConfig, agents.SettingsKeyRememberAgentName); settingErr == nil &&
		setting != nil {
		if customName, ok := setting.Value["name"].(string); ok && customName != "" && customName != agents.CanonicalRememberAgentName {
			var lookupErr error
			agentDef, lookupErr = h.agentRepo.FindDefinitionByName(ctx, projectID, customName)
			if lookupErr != nil || agentDef == nil {
				h.log.Warn("remember: custom agent not found, falling back to canonical",
					slog.String("custom_name", customName),
					slog.String("project_id", projectID),
				)
				agentDef = nil
			}
		}
	}
	if agentDef == nil {
		var ensureErr error
		if schemaPolicy == "enrich" {
			agentDef, ensureErr = h.agentRepo.EnsureEnrichRememberAgent(ctx, projectID)
		} else {
			agentDef, ensureErr = h.agentRepo.EnsureDomainRememberAgent(ctx, projectID, schemaPolicy)
		}
		if ensureErr != nil {
			return apperror.NewInternal("failed to ensure domain-remember-agent", ensureErr)
		}
	}

	agentDefUUID, err := uuid.Parse(agentDef.ID)
	if err != nil {
		return apperror.NewInternal("invalid domain-remember-agent ID", err)
	}

	// Build agent message: pre-classify so the agent skips classify-document and
	// goes directly to finalize-discovery (LLM can't skip the classification step).
	var agentMessage string
	if documentID != "" {
		// Default: when no classifier is wired (tests, minimal server) treat every
		// document as a new domain so the agent always has an actionable stage.
		classifiedStage := demoteStageForPolicy("new_domain", schemaPolicy)
		var classifiedPackName, classifiedSchemaID, classifiedReason string
		if h.domainClassifier != nil {
			snap, classErr := h.domainClassifier.ClassifyDocument(ctx, projectID, documentID)
			if classErr == nil {
				stage := snap.Stage
				// Empty stage means no schema packs are installed — treat as new_domain.
				if stage == "" {
					stage = "new_domain"
				}
				classifiedStage = demoteStageForPolicy(stage, schemaPolicy)
				classifiedPackName = snap.SuggestedPackName
				if classifiedPackName == "" {
					classifiedPackName = snap.Label
				}
				if snap.SchemaID != nil {
					classifiedSchemaID = *snap.SchemaID
				}
				classifiedReason = snap.LLMReason
			} else {
				h.log.Warn("pre-classify failed, using new_domain fallback",
					slog.String("document_id", documentID),
					slog.String("error", classErr.Error()),
				)
			}
		}
		agentMessage = fmt.Sprintf("document_id: %s\nschema_policy: %s\ndry_run: %v\nclassified_stage: %s\nclassified_pack_name: %s\nclassified_schema_id: %s\nclassified_reason: %s\n\n%s",
			documentID, schemaPolicy, req.DryRun, classifiedStage, classifiedPackName, classifiedSchemaID, classifiedReason, message)
	} else {
		// Fallback (no docSvc injected): pass message directly so agent still runs.
		agentMessage = fmt.Sprintf("schema_policy: %s\ndry_run: %v\n\n%s",
			schemaPolicy, req.DryRun, message)
	}

	// Prepend guide hint when provided so the agent knows how to interpret the content.
	if req.Guide != "" {
		agentMessage = "guide: " + req.Guide + "\n" + agentMessage
	}

	// Reuse an existing conversation if the caller provided one, otherwise create a new one.
	var conv *Conversation
	if req.ConversationID != "" {
		convUUID, parseErr := uuid.Parse(req.ConversationID)
		if parseErr == nil {
			conv, _ = h.svc.GetConversation(ctx, projectID, user.ID, convUUID)
		}
	}
	if conv == nil {
		title := message
		if len(title) > 50 {
			title = title[:50] + "..."
		}
		var createErr error
		conv, createErr = h.svc.CreateConversation(ctx, projectID, user.ID, CreateConversationRequest{
			Title:   title,
			Message: agentMessage,
		})
		if createErr != nil {
			span.RecordError(createErr)
			span.SetStatus(codes.Error, createErr.Error())
			return apperror.NewInternal("failed to create conversation", createErr)
		}
	}
	conv.AgentDefinitionID = &agentDefUUID
	span.SetAttributes(attribute.String("memory.remember.conversation_id", conv.ID.String()))
	if err := h.svc.SetAgentDefinitionID(ctx, projectID, user.ID, conv.ID, &agentDefUUID); err != nil {
		h.log.Warn("failed to set agent_definition_id on remember conversation",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("error", err.Error()),
		)
	}

	switch mode {
	case "async":
		// Fire-and-forget: launch agent in background, return 202 with run_id immediately.
		dummyName := "Remember session for " + agentDef.Name
		dummyAgent, _ := h.agentRepo.FindByName(ctx, projectID, dummyName)
		if dummyAgent == nil {
			dummyAgent = &agents.Agent{
				ProjectID:         projectID,
				Name:              dummyName,
				StrategyType:      "remember-session:" + agentDef.ID,
				AgentDefinitionID: &agentDef.ID,
				CronSchedule:      "0 0 * * *",
				TriggerType:       "manual",
			}
			if err := h.agentRepo.Create(ctx, dummyAgent); err != nil {
				return apperror.NewInternal("failed to create agent session for async remember", err)
			}
		} else if dummyAgent.AgentDefinitionID == nil || *dummyAgent.AgentDefinitionID != agentDef.ID {
			// Backfill FK for agents created before this fix.
			dummyAgent.AgentDefinitionID = &agentDef.ID
			_ = h.agentRepo.Update(ctx, dummyAgent)
		}
		run, runErr := h.agentRepo.CreateRun(ctx, dummyAgent.ID)
		if runErr != nil {
			return apperror.NewInternal("failed to create async run record", runErr)
		}
		preCreated := &agents.AgentRun{ID: run.ID, AgentID: dummyAgent.ID}
		execReq := agents.ExecuteRequest{
			Agent:           dummyAgent,
			AgentDefinition: agentDef,
			ProjectID:       projectID,
			OrgID:           user.OrgID,
			UserID:          user.ID,
			UserMessage:     agentMessage,
			PreCreatedRun:   preCreated,
			TrustedInternal: true, // session UI is a trusted surface (full internal coordination)
		}
		if parentRunID != "" {
			execReq.ParentRunID = &parentRunID
		}
		if rootRunID != "" {
			execReq.RootRunID = &rootRunID
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					h.log.Error("panic in async remember goroutine",
						slog.String("run_id", run.ID),
						slog.Any("panic", r),
					)
					bgCtx2 := context.Background()
					if updateErr := h.agentRepo.FailRun(bgCtx2, run.ID, fmt.Sprintf("panic: %v", r)); updateErr != nil {
						h.log.Warn("async remember: failed to mark run as failed after panic",
							slog.String("run_id", run.ID),
							slog.String("error", updateErr.Error()),
						)
					}
				}
			}()
			bgCtx := context.Background()
			if req.Namespace != "" {
				bgCtx = auth.ContextWithNamespace(bgCtx, req.Namespace)
			}
			bgCtx = auth.ContextWithProjectID(bgCtx, projectID)
			result, execErr := h.agentExecutor.Execute(bgCtx, execReq)
			if execErr != nil {
				h.log.Warn("async remember: agent execution failed",
					slog.String("run_id", run.ID),
					slog.String("error", execErr.Error()),
				)
				if updateErr := h.agentRepo.FailRun(bgCtx, run.ID, execErr.Error()); updateErr != nil {
					h.log.Warn("async remember: failed to mark run as failed",
						slog.String("run_id", run.ID),
						slog.String("error", updateErr.Error()),
					)
				}
			}
			if result != nil && result.Cleanup != nil {
				result.Cleanup()
			}
		}()
		return c.JSON(http.StatusAccepted, map[string]any{
			"run_id":      run.ID,
			"status":      "running",
			"document_id": documentID,
		})

	case "sync":
		// Synchronous: wait for the agent to finish, return JSON result.
		dummyName := "Remember session for " + agentDef.Name
		dummyAgent, _ := h.agentRepo.FindByName(ctx, projectID, dummyName)
		if dummyAgent == nil {
			dummyAgent = &agents.Agent{
				ProjectID:         projectID,
				Name:              dummyName,
				StrategyType:      "remember-session:" + agentDef.ID,
				AgentDefinitionID: &agentDef.ID,
				CronSchedule:      "0 0 * * *",
				TriggerType:       "manual",
			}
			if err := h.agentRepo.Create(ctx, dummyAgent); err != nil {
				return apperror.NewInternal("failed to create agent session for sync remember", err)
			}
		} else if dummyAgent.AgentDefinitionID == nil || *dummyAgent.AgentDefinitionID != agentDef.ID {
			// Backfill FK for agents created before this fix.
			dummyAgent.AgentDefinitionID = &agentDef.ID
			_ = h.agentRepo.Update(ctx, dummyAgent)
		}
		execReq := agents.ExecuteRequest{
			Agent:           dummyAgent,
			AgentDefinition: agentDef,
			ProjectID:       projectID,
			OrgID:           user.OrgID,
			UserID:          user.ID,
			UserMessage:     agentMessage,
			TrustedInternal: true, // session UI is a trusted surface (full internal coordination)
		}
		if parentRunID != "" {
			execReq.ParentRunID = &parentRunID
		}
		if rootRunID != "" {
			execReq.RootRunID = &rootRunID
		}
		result, execErr := h.agentExecutor.Execute(ctx, execReq)
		if execErr != nil {
			return apperror.NewInternal("agent execution failed", execErr)
		}
		if result != nil && result.Cleanup != nil {
			defer result.Cleanup()
		}
		span.SetStatus(codes.Ok, "")
		return c.JSON(http.StatusOK, map[string]any{
			"run_id":      result.RunID,
			"status":      string(result.Status),
			"summary":     result.Summary,
			"document_id": documentID,
		})

	default: // "stream"
		sseWriter := sse.NewWriter(c.Response().Writer)
		if err := sseWriter.Start(); err != nil {
			return apperror.ErrInternal.WithMessage("failed to start SSE stream")
		}
		if err := sseWriter.WriteData(sse.NewMetaEvent(conv.ID.String())); err != nil {
			return nil
		}
		rememberResult := h.streamAgentChat(ctx, conv, agentMessage, projectID, user.OrgID, user.ID, sseWriter, parentRunID, rootRunID, "")
		span.SetStatus(codes.Ok, "")
		var rememberRunID, rememberTraceID string
		if rememberResult != nil {
			rememberRunID = rememberResult.RunID
			if rememberRunID != "" {
				if run, err := h.agentRepo.FindRunByID(ctx, rememberRunID); err == nil && run != nil && run.TraceID != nil {
					rememberTraceID = *run.TraceID
				}
			}
		}
		sseWriter.WriteData(sse.NewDoneEventWithRunAndTrace(rememberRunID, rememberTraceID))
		sseWriter.Close()
		if rememberResult != nil && rememberResult.Cleanup != nil {
			go rememberResult.Cleanup()
		}
		return nil
	}
}

// RememberFileRequest holds parameters parsed from the multipart /remember/file form.
type RememberFileRequest struct {
	SchemaPolicy   string
	DryRun         bool
	Guide          string
	Namespace      string
	ConversationID string
	ParentRunID    string
	RootRunID      string
	Mode           string
}

// RememberFile handles POST /api/projects/:projectId/remember/file.
// It accepts a multipart form with a "file" field plus optional parameters,
// uploads the file, waits for the parsing pipeline to produce plaintext content,
// and then runs the domain-remember-agent on that content — identical to RememberStream
// but driven from a file rather than raw text.
//
// Multipart form fields:
//
//	file            — required: the file to upload and remember
//	schema_policy   — optional: "auto" | "reuse_only" (default) | "ask"
//	dry_run         — optional: "true" to skip merging the extraction branch
//	guide           — optional: natural-language hint for the agent (stored in doc metadata)
//	namespace       — optional: scope graph objects to a namespace
//	conversation_id — optional: reuse an existing conversation
//	mode            — optional: "stream" (default) | "sync" | "async"
func (h *Handler) RememberFile(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	if h.uploadHandler == nil {
		return apperror.New(http.StatusServiceUnavailable, "storage_unavailable",
			"File upload is not available — storage service is not configured")
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("file is required")
	}

	req := RememberFileRequest{
		SchemaPolicy:   strings.TrimSpace(c.FormValue("schema_policy")),
		Guide:          strings.TrimSpace(c.FormValue("guide")),
		Namespace:      strings.TrimSpace(c.FormValue("namespace")),
		ConversationID: strings.TrimSpace(c.FormValue("conversation_id")),
		ParentRunID:    strings.TrimSpace(c.FormValue("parent_run_id")),
		RootRunID:      strings.TrimSpace(c.FormValue("root_run_id")),
		Mode:           strings.TrimSpace(c.FormValue("mode")),
		DryRun:         c.FormValue("dry_run") == "true",
	}

	// Normalise schema_policy.
	if req.SchemaPolicy == "" {
		req.SchemaPolicy = "reuse_only"
	}
	if req.SchemaPolicy != "auto" && req.SchemaPolicy != "reuse_only" && req.SchemaPolicy != "ask" && req.SchemaPolicy != "enrich" {
		return apperror.ErrBadRequest.WithMessage("schema_policy must be one of: auto, reuse_only, ask, enrich")
	}

	// Normalise mode.
	if req.Mode == "" {
		req.Mode = c.Request().Header.Get("X-Remember-Mode")
	}
	if req.Mode == "" {
		req.Mode = "stream"
	}
	if req.Mode != "stream" && req.Mode != "sync" && req.Mode != "async" {
		return apperror.ErrBadRequest.WithMessage("mode must be one of: stream, sync, async")
	}

	if req.ParentRunID == "" {
		req.ParentRunID = c.Request().Header.Get("X-Parent-Run-Id")
	}
	if req.RootRunID == "" {
		req.RootRunID = c.Request().Header.Get("X-Root-Run-Id")
	}

	ctx := c.Request().Context()
	if auth.ProjectIDFromContext(ctx) == "" && projectID != "" {
		ctx = auth.ContextWithProjectID(ctx, projectID)
	}
	if req.Namespace != "" {
		ctx = auth.ContextWithNamespace(ctx, req.Namespace)
	}

	// Probe LLM provider before doing expensive upload work.
	if h.modelFactory == nil {
		return apperror.New(http.StatusServiceUnavailable, "no_provider", errNoProviderMsg)
	}
	probeModel, probeErr := h.modelFactory.CreateModel(ctx)
	if probeErr != nil {
		errMsg := probeErr.Error()
		if strings.Contains(errMsg, "no LLM credentials configured") ||
			strings.Contains(errMsg, "no_provider") ||
			strings.Contains(errMsg, "provider config found for organization") ||
			strings.Contains(errMsg, "no generative model configured") {
			return apperror.New(http.StatusServiceUnavailable, "no_provider", errNoProviderMsg)
		}
		return apperror.New(http.StatusServiceUnavailable, "provider_error",
			friendlyProviderError(probeErr))
	}
	if closer, ok := probeModel.(interface{ Close() error }); ok {
		_ = closer.Close()
	}

	// Build metadata — store guide so it's queryable on the document record.
	var metadata map[string]any
	if req.Guide != "" {
		metadata = map[string]any{"guide": req.Guide}
	}

	ctx2, uploadSpan := tracing.Start(ctx, "remember.file.upload",
		attribute.String("memory.project.id", projectID),
		attribute.String("memory.remember.user_id", user.ID),
		attribute.String("memory.remember.filename", fh.Filename),
	)
	uploadResult, uploadErr := h.uploadHandler.UploadForRemember(ctx2, user.OrgID, projectID, user.ID, fh, metadata)
	uploadSpan.End()
	if uploadErr != nil {
		return uploadErr
	}
	documentID := uploadResult.DocumentID

	// Poll for conversion completion (max 120s, 2s interval).
	// Duplicate documents are already converted — skip polling.
	if !uploadResult.IsDuplicate && h.docSvc != nil {
		const pollTimeout = 120 * time.Second
		const pollInterval = 2 * time.Second
		deadline := time.Now().Add(pollTimeout)
		for time.Now().Before(deadline) {
			doc, docErr := h.docSvc.GetByID(ctx, projectID, documentID)
			if docErr != nil {
				break // proceed anyway; content may still be available
			}
			status := ""
			if doc.ConversionStatus != nil {
				status = *doc.ConversionStatus
			}
			if status == "completed" {
				break
			}
			if status == "failed" {
				return apperror.New(http.StatusUnprocessableEntity, "conversion_failed",
					"File could not be converted to text. Check that the file type is supported.")
			}
			time.Sleep(pollInterval)
		}
	}

	// Fetch parsed plaintext content.
	var message string
	// Set when the parser produced no text and the guide is the remembered
	// text: the guide is then already the message, so it must not also be
	// prepended to the agent message below.
	var guideIsTheText bool
	if h.docSvc != nil {
		content, contentErr := h.docSvc.GetContent(ctx, projectID, documentID)
		if contentErr != nil {
			// A read failure is not the same as "the file had no text": the
			// poll above can time out before conversion finishes, and storage
			// can fail outright. Neither means the user's upload was empty, so
			// degrade to the guide (a valid capture on its own) and record why,
			// rather than reporting a misleading no_content.
			h.log.Warn("remember/file: converted content unreadable",
				slog.String("document_id", documentID),
				slog.String("error", contentErr.Error()),
			)
		}
		message, guideIsTheText = rememberFileText(content, req.Guide)
		if message == "" {
			if contentErr != nil {
				return apperror.New(http.StatusServiceUnavailable, "conversion_unavailable",
					"The uploaded file could not be read yet. Retry in a moment.")
			}
			return apperror.New(http.StatusUnprocessableEntity, "no_content",
				"File was uploaded but no text content could be extracted. "+
					"Ensure the file is a supported format and not empty.")
		}
	}

	ctx, span := tracing.Start(ctx, "remember.run",
		attribute.String("memory.project.id", projectID),
		attribute.String("memory.remember.user_id", user.ID),
		attribute.String("memory.remember.schema_policy", req.SchemaPolicy),
		attribute.String("memory.remember.mode", req.Mode),
		attribute.Bool("memory.remember.dry_run", req.DryRun),
		attribute.String("memory.remember.document_id", documentID),
		attribute.String("memory.remember.filename", fh.Filename),
	)
	defer span.End()

	var agentDef *agents.AgentDefinition
	if setting, settingErr := h.agentRepo.GetProjectSetting(ctx, projectID,
		agents.SettingsCategoryRememberConfig, agents.SettingsKeyRememberAgentName); settingErr == nil &&
		setting != nil {
		if customName, ok := setting.Value["name"].(string); ok && customName != "" && customName != agents.CanonicalRememberAgentName {
			var lookupErr error
			agentDef, lookupErr = h.agentRepo.FindDefinitionByName(ctx, projectID, customName)
			if lookupErr != nil || agentDef == nil {
				h.log.Warn("remember/file: custom agent not found, falling back to canonical",
					slog.String("custom_name", customName),
					slog.String("project_id", projectID),
				)
				agentDef = nil
			}
		}
	}
	if agentDef == nil {
		var ensureErr error
		if req.SchemaPolicy == "enrich" {
			agentDef, ensureErr = h.agentRepo.EnsureEnrichRememberAgent(ctx, projectID)
		} else {
			agentDef, ensureErr = h.agentRepo.EnsureDomainRememberAgent(ctx, projectID, req.SchemaPolicy)
		}
		if ensureErr != nil {
			return apperror.NewInternal("failed to ensure domain-remember-agent", ensureErr)
		}
	}

	agentDefUUID, err := uuid.Parse(agentDef.ID)
	if err != nil {
		return apperror.NewInternal("invalid domain-remember-agent ID", err)
	}

	// Pre-classify the uploaded document.
	var classifiedStage, classifiedPackName, classifiedSchemaID string
	if h.domainClassifier != nil {
		snap, classErr := h.domainClassifier.ClassifyDocument(ctx, projectID, documentID)
		if classErr == nil {
			classifiedStage = demoteStageForPolicy(snap.Stage, req.SchemaPolicy)
			classifiedPackName = snap.SuggestedPackName
			if classifiedPackName == "" {
				classifiedPackName = snap.Label
			}
			if snap.SchemaID != nil {
				classifiedSchemaID = *snap.SchemaID
			}
		} else {
			h.log.Warn("pre-classify failed for remember/file, agent will classify",
				slog.String("document_id", documentID),
				slog.String("error", classErr.Error()),
			)
		}
	}

	agentMessage := fmt.Sprintf("document_id: %s\nschema_policy: %s\ndry_run: %v\nclassified_stage: %s\nclassified_pack_name: %s\nclassified_schema_id: %s\n\n%s",
		documentID, req.SchemaPolicy, req.DryRun, classifiedStage, classifiedPackName, classifiedSchemaID, message)
	if req.Guide != "" && !guideIsTheText {
		agentMessage = "guide: " + req.Guide + "\n" + agentMessage
	}

	// Build or reuse conversation.
	var conv *Conversation
	if req.ConversationID != "" {
		convUUID, parseErr := uuid.Parse(req.ConversationID)
		if parseErr == nil {
			conv, _ = h.svc.GetConversation(ctx, projectID, user.ID, convUUID)
		}
	}
	if conv == nil {
		title := fh.Filename
		if req.Guide != "" {
			title = req.Guide
			if len(title) > 50 {
				title = title[:50] + "..."
			}
		} else if len(title) > 50 {
			title = title[:50] + "..."
		}
		var createErr error
		conv, createErr = h.svc.CreateConversation(ctx, projectID, user.ID, CreateConversationRequest{
			Title:   title,
			Message: agentMessage,
		})
		if createErr != nil {
			span.RecordError(createErr)
			span.SetStatus(codes.Error, createErr.Error())
			return apperror.NewInternal("failed to create conversation", createErr)
		}
	}
	conv.AgentDefinitionID = &agentDefUUID
	span.SetAttributes(attribute.String("memory.remember.conversation_id", conv.ID.String()))
	if err := h.svc.SetAgentDefinitionID(ctx, projectID, user.ID, conv.ID, &agentDefUUID); err != nil {
		h.log.Warn("failed to set agent_definition_id on remember/file conversation",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("error", err.Error()),
		)
	}

	switch req.Mode {
	case "async":
		dummyName := "Remember session for " + agentDef.Name
		dummyAgent, _ := h.agentRepo.FindByName(ctx, projectID, dummyName)
		if dummyAgent == nil {
			dummyAgent = &agents.Agent{
				ProjectID:         projectID,
				Name:              dummyName,
				StrategyType:      "remember-session:" + agentDef.ID,
				AgentDefinitionID: &agentDef.ID,
				CronSchedule:      "0 0 * * *",
				TriggerType:       "manual",
			}
			if err := h.agentRepo.Create(ctx, dummyAgent); err != nil {
				return apperror.NewInternal("failed to create agent session for async remember/file", err)
			}
		} else if dummyAgent.AgentDefinitionID == nil || *dummyAgent.AgentDefinitionID != agentDef.ID {
			dummyAgent.AgentDefinitionID = &agentDef.ID
			_ = h.agentRepo.Update(ctx, dummyAgent)
		}
		run, runErr := h.agentRepo.CreateRun(ctx, dummyAgent.ID)
		if runErr != nil {
			return apperror.NewInternal("failed to create async run record", runErr)
		}
		preCreated := &agents.AgentRun{ID: run.ID, AgentID: dummyAgent.ID}
		execReq := agents.ExecuteRequest{
			Agent:           dummyAgent,
			AgentDefinition: agentDef,
			ProjectID:       projectID,
			OrgID:           user.OrgID,
			UserID:          user.ID,
			UserMessage:     agentMessage,
			PreCreatedRun:   preCreated,
			TrustedInternal: true, // session UI is a trusted surface (full internal coordination)
		}
		if req.ParentRunID != "" {
			execReq.ParentRunID = &req.ParentRunID
		}
		if req.RootRunID != "" {
			execReq.RootRunID = &req.RootRunID
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					h.log.Error("panic in async remember/file goroutine",
						slog.String("run_id", run.ID),
						slog.Any("panic", r),
					)
					bgCtx2 := context.Background()
					if updateErr := h.agentRepo.FailRun(bgCtx2, run.ID, fmt.Sprintf("panic: %v", r)); updateErr != nil {
						h.log.Warn("async remember/file: failed to mark run as failed after panic",
							slog.String("run_id", run.ID),
							slog.String("error", updateErr.Error()),
						)
					}
				}
			}()
			bgCtx := context.Background()
			if req.Namespace != "" {
				bgCtx = auth.ContextWithNamespace(bgCtx, req.Namespace)
			}
			bgCtx = auth.ContextWithProjectID(bgCtx, projectID)
			result, execErr := h.agentExecutor.Execute(bgCtx, execReq)
			if execErr != nil {
				h.log.Warn("async remember/file: agent execution failed",
					slog.String("run_id", run.ID),
					slog.String("error", execErr.Error()),
				)
				if updateErr := h.agentRepo.FailRun(bgCtx, run.ID, execErr.Error()); updateErr != nil {
					h.log.Warn("async remember/file: failed to mark run as failed",
						slog.String("run_id", run.ID),
						slog.String("error", updateErr.Error()),
					)
				}
			}
			if result != nil && result.Cleanup != nil {
				result.Cleanup()
			}
		}()
		return c.JSON(http.StatusAccepted, map[string]any{
			"run_id":      run.ID,
			"status":      "running",
			"document_id": documentID,
		})

	case "sync":
		dummyName := "Remember session for " + agentDef.Name
		dummyAgent, _ := h.agentRepo.FindByName(ctx, projectID, dummyName)
		if dummyAgent == nil {
			dummyAgent = &agents.Agent{
				ProjectID:         projectID,
				Name:              dummyName,
				StrategyType:      "remember-session:" + agentDef.ID,
				AgentDefinitionID: &agentDef.ID,
				CronSchedule:      "0 0 * * *",
				TriggerType:       "manual",
			}
			if err := h.agentRepo.Create(ctx, dummyAgent); err != nil {
				return apperror.NewInternal("failed to create agent session for sync remember/file", err)
			}
		} else if dummyAgent.AgentDefinitionID == nil || *dummyAgent.AgentDefinitionID != agentDef.ID {
			dummyAgent.AgentDefinitionID = &agentDef.ID
			_ = h.agentRepo.Update(ctx, dummyAgent)
		}
		execReq := agents.ExecuteRequest{
			Agent:           dummyAgent,
			AgentDefinition: agentDef,
			ProjectID:       projectID,
			OrgID:           user.OrgID,
			UserID:          user.ID,
			UserMessage:     agentMessage,
			TrustedInternal: true, // session UI is a trusted surface (full internal coordination)
		}
		if req.ParentRunID != "" {
			execReq.ParentRunID = &req.ParentRunID
		}
		if req.RootRunID != "" {
			execReq.RootRunID = &req.RootRunID
		}
		result, execErr := h.agentExecutor.Execute(ctx, execReq)
		if execErr != nil {
			return apperror.NewInternal("agent execution failed", execErr)
		}
		if result != nil && result.Cleanup != nil {
			defer result.Cleanup()
		}
		span.SetStatus(codes.Ok, "")
		return c.JSON(http.StatusOK, map[string]any{
			"run_id":      result.RunID,
			"status":      string(result.Status),
			"summary":     result.Summary,
			"document_id": documentID,
		})

	default: // "stream"
		sseWriter := sse.NewWriter(c.Response().Writer)
		if err := sseWriter.Start(); err != nil {
			return apperror.ErrInternal.WithMessage("failed to start SSE stream")
		}
		if err := sseWriter.WriteData(sse.NewMetaEvent(conv.ID.String())); err != nil {
			return nil
		}
		rememberResult := h.streamAgentChat(ctx, conv, agentMessage, projectID, user.OrgID, user.ID, sseWriter, req.ParentRunID, req.RootRunID, "")
		span.SetStatus(codes.Ok, "")
		var rememberRunID, rememberTraceID string
		if rememberResult != nil {
			rememberRunID = rememberResult.RunID
			if rememberRunID != "" {
				if run, err := h.agentRepo.FindRunByID(ctx, rememberRunID); err == nil && run != nil && run.TraceID != nil {
					rememberTraceID = *run.TraceID
				}
			}
		}
		sseWriter.WriteData(sse.NewDoneEventWithRunAndTrace(rememberRunID, rememberTraceID))
		sseWriter.Close()
		if rememberResult != nil && rememberResult.Cleanup != nil {
			go rememberResult.Cleanup()
		}
		return nil
	}
}

// message so the cli-assistant-agent is always aware of auth/project state.
// The block is formatted as a system note rather than part of the question.
func buildAskContextPrefix(user *auth.AuthUser, projectID string) string {
	var sb strings.Builder
	sb.WriteString("<context>\n")

	if user == nil || user.ID == "" {
		sb.WriteString("Authentication: NOT authenticated\n")
	} else {
		sb.WriteString("Authentication: authenticated\n")
		if user.Email != "" {
			sb.WriteString("User: " + user.Email + "\n")
		}
		if projectID != "" {
			sb.WriteString("Project ID: " + projectID + "\n")
		} else {
			sb.WriteString("Project: none (no project context active)\n")
		}
		if user.APITokenProjectID != "" {
			sb.WriteString("Auth method: project API token (emt_*)\n")
		} else if user.Sub == "standalone" {
			sb.WriteString("Auth method: standalone API key\n")
		} else {
			sb.WriteString("Auth method: OAuth\n")
		}
	}

	sb.WriteString("</context>\n\n")
	return sb.String()
}

// demoteStageForPolicy applies schema-policy enforcement to a classifier stage.
// When policy is "reuse_only" and the classifier found no matching schema
// (stage=="new_domain"), the stage is demoted to "no_match" so the agent
// skips schema creation entirely rather than routing to a potentially
// unrelated existing schema.
func demoteStageForPolicy(stage, schemaPolicy string) string {
	if stage == "new_domain" && schemaPolicy == "reuse_only" {
		return "no_match"
	}
	return stage
}

// ForgetStreamRequest is the request body for the forget endpoint.
type ForgetStreamRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id,omitempty"`
	Strategy       string `json:"strategy,omitempty"`      // "auto" | "confirm" (default) | "ask"
	CascadeDepth   int    `json:"cascade_depth,omitempty"` // 1 | 2 (default) | 3
	DryRun         bool   `json:"dry_run,omitempty"`
	ParentRunID    string `json:"parent_run_id,omitempty"`
	RootRunID      string `json:"root_run_id,omitempty"`
	// Mode controls response style: "stream" (default SSE), "sync" (JSON, wait), "async" (JSON 202).
	Mode string `json:"mode,omitempty"`
}

// buildForgetAgentMessage constructs the agent message injected as context for a forget run.
func buildForgetAgentMessage(userQuery, strategy string, cascadeDepth int, dryRun bool) string {
	dryRunStr := "false"
	if dryRun {
		dryRunStr = "true"
	}
	return fmt.Sprintf("strategy: %s\ncascade_depth: %d\ndry_run: %s\n\n%s",
		strategy, cascadeDepth, dryRunStr, userQuery)
}

// ForgetStream handles POST /api/projects/:projectId/forget.
// It finds graph objects matching the user's natural-language query and soft-deletes them
// using the forget-agent. Soft-deletes are reversible via entity-restore.
func (h *Handler) ForgetStream(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req ForgetStreamRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return apperror.ErrBadRequest.WithMessage("message is required")
	}

	// Normalise strategy — default to "confirm".
	strategy := req.Strategy
	if strategy == "" {
		strategy = "confirm"
	}
	if strategy != "auto" && strategy != "confirm" && strategy != "ask" {
		return apperror.ErrBadRequest.WithMessage("strategy must be one of: auto, confirm, ask")
	}

	// Normalise cascade_depth — default to 2.
	cascadeDepth := req.CascadeDepth
	if cascadeDepth == 0 {
		cascadeDepth = 2
	}
	if cascadeDepth < 1 || cascadeDepth > 3 {
		return apperror.ErrBadRequest.WithMessage("cascade_depth must be 1, 2, or 3")
	}

	// Normalise mode.
	mode := req.Mode
	if mode == "" {
		mode = c.Request().Header.Get("X-Forget-Mode")
	}
	if mode == "" {
		mode = "stream"
	}
	if mode != "stream" && mode != "sync" && mode != "async" {
		return apperror.ErrBadRequest.WithMessage("mode must be one of: stream, sync, async")
	}

	parentRunID := req.ParentRunID
	if parentRunID == "" {
		parentRunID = c.Request().Header.Get("X-Parent-Run-Id")
	}
	rootRunID := req.RootRunID
	if rootRunID == "" {
		rootRunID = c.Request().Header.Get("X-Root-Run-Id")
	}

	ctx := c.Request().Context()
	if auth.ProjectIDFromContext(ctx) == "" && projectID != "" {
		ctx = auth.ContextWithProjectID(ctx, projectID)
	}

	// Probe LLM provider before opening SSE stream.
	if h.modelFactory == nil {
		return apperror.New(http.StatusServiceUnavailable, "no_provider",
			"No LLM provider configured for this project.")
	}

	// Ensure the forget-agent exists (idempotent).
	agentDef, err := h.agentRepo.EnsureForgetAgent(ctx, projectID, strategy, cascadeDepth)
	if err != nil {
		return apperror.NewInternal("failed to ensure forget-agent", err)
	}

	agentDefUUID, err := uuid.Parse(agentDef.ID)
	if err != nil {
		return apperror.NewInternal("invalid forget-agent ID", err)
	}

	agentMessage := buildForgetAgentMessage(message, strategy, cascadeDepth, req.DryRun)

	// Reuse or create conversation.
	var conv *Conversation
	if req.ConversationID != "" {
		convUUID, parseErr := uuid.Parse(req.ConversationID)
		if parseErr == nil {
			conv, _ = h.svc.GetConversation(ctx, projectID, user.ID, convUUID)
		}
	}
	if conv == nil {
		title := message
		if len(title) > 50 {
			title = title[:50] + "..."
		}
		var createErr error
		conv, createErr = h.svc.CreateConversation(ctx, projectID, user.ID, CreateConversationRequest{
			Title:   title,
			Message: agentMessage,
		})
		if createErr != nil {
			return apperror.NewInternal("failed to create conversation", createErr)
		}
	}
	conv.AgentDefinitionID = &agentDefUUID
	if err := h.svc.SetAgentDefinitionID(ctx, projectID, user.ID, conv.ID, &agentDefUUID); err != nil {
		h.log.Warn("failed to set agent_definition_id on forget conversation",
			slog.String("conversation_id", conv.ID.String()),
			slog.String("error", err.Error()),
		)
	}

	switch mode {
	case "async":
		dummyName := "Forget session for " + agentDef.Name
		dummyAgent, _ := h.agentRepo.FindByName(ctx, projectID, dummyName)
		if dummyAgent == nil {
			dummyAgent = &agents.Agent{
				ProjectID:         projectID,
				Name:              dummyName,
				StrategyType:      "forget-session:" + agentDef.ID,
				AgentDefinitionID: &agentDef.ID,
				CronSchedule:      "0 0 * * *",
				TriggerType:       "manual",
			}
			if err := h.agentRepo.Create(ctx, dummyAgent); err != nil {
				return apperror.NewInternal("failed to create agent session for async forget", err)
			}
		}
		run, runErr := h.agentRepo.CreateRun(ctx, dummyAgent.ID)
		if runErr != nil {
			return apperror.NewInternal("failed to create async run record", runErr)
		}
		preCreated := &agents.AgentRun{ID: run.ID, AgentID: dummyAgent.ID}
		execReq := agents.ExecuteRequest{
			Agent:           dummyAgent,
			AgentDefinition: agentDef,
			ProjectID:       projectID,
			OrgID:           user.OrgID,
			UserID:          user.ID,
			UserMessage:     agentMessage,
			PreCreatedRun:   preCreated,
			TrustedInternal: true, // session UI is a trusted surface (full internal coordination)
		}
		if parentRunID != "" {
			execReq.ParentRunID = &parentRunID
		}
		if rootRunID != "" {
			execReq.RootRunID = &rootRunID
		}
		go func() {
			bgCtx := context.Background()
			bgCtx = auth.ContextWithProjectID(bgCtx, projectID)
			result, execErr := h.agentExecutor.Execute(bgCtx, execReq)
			if execErr != nil {
				h.log.Warn("async forget: agent execution failed",
					slog.String("run_id", run.ID),
					slog.String("error", execErr.Error()),
				)
				_ = h.agentRepo.FailRun(bgCtx, run.ID, execErr.Error())
			}
			if result != nil && result.Cleanup != nil {
				result.Cleanup()
			}
		}()
		return c.JSON(http.StatusAccepted, map[string]any{
			"run_id": run.ID,
			"status": "running",
		})

	case "sync":
		dummyName := "Forget session for " + agentDef.Name
		dummyAgent, _ := h.agentRepo.FindByName(ctx, projectID, dummyName)
		if dummyAgent == nil {
			dummyAgent = &agents.Agent{
				ProjectID:         projectID,
				Name:              dummyName,
				StrategyType:      "forget-session:" + agentDef.ID,
				AgentDefinitionID: &agentDef.ID,
				CronSchedule:      "0 0 * * *",
				TriggerType:       "manual",
			}
			if err := h.agentRepo.Create(ctx, dummyAgent); err != nil {
				return apperror.NewInternal("failed to create agent session for sync forget", err)
			}
		}
		execReq := agents.ExecuteRequest{
			Agent:           dummyAgent,
			AgentDefinition: agentDef,
			ProjectID:       projectID,
			OrgID:           user.OrgID,
			UserID:          user.ID,
			UserMessage:     agentMessage,
			TrustedInternal: true, // session UI is a trusted surface (full internal coordination)
		}
		if parentRunID != "" {
			execReq.ParentRunID = &parentRunID
		}
		if rootRunID != "" {
			execReq.RootRunID = &rootRunID
		}
		result, execErr := h.agentExecutor.Execute(ctx, execReq)
		if execErr != nil {
			return apperror.NewInternal("agent execution failed", execErr)
		}
		if result != nil && result.Cleanup != nil {
			defer result.Cleanup()
		}
		return c.JSON(http.StatusOK, map[string]any{
			"run_id":  result.RunID,
			"status":  string(result.Status),
			"summary": result.Summary,
		})

	default: // "stream"
		sseWriter := sse.NewWriter(c.Response().Writer)
		if err := sseWriter.Start(); err != nil {
			return apperror.ErrInternal.WithMessage("failed to start SSE stream")
		}
		if err := sseWriter.WriteData(sse.NewMetaEvent(conv.ID.String())); err != nil {
			return nil
		}
		forgetResult := h.streamAgentChat(ctx, conv, agentMessage, projectID, user.OrgID, user.ID, sseWriter, parentRunID, rootRunID, "")
		var forgetRunID string
		if forgetResult != nil {
			forgetRunID = forgetResult.RunID
		}
		sseWriter.WriteData(sse.NewDoneEventWithRun(forgetRunID))
		sseWriter.Close()
		if forgetResult != nil && forgetResult.Cleanup != nil {
			go forgetResult.Cleanup()
		}
		return nil
	}
}
