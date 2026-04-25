package graph

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// SessionHandler handles HTTP requests for session and message operations.
type SessionHandler struct {
	svc *SessionService
	log *slog.Logger
}

// NewSessionHandler creates a new SessionHandler.
func NewSessionHandler(svc *SessionService, log *slog.Logger) *SessionHandler {
	return &SessionHandler{
		svc: svc,
		log: log.With(logger.Scope("graph.session_handler")),
	}
}

// CreateSession creates a new Session.
// @Summary      Create a session
// @Description  Creates a new Session graph object to track an AI agent conversation.
// @Tags         sessions
// @Accept       json
// @Produce      json
// @Param        request body CreateSessionRequest true "Session creation parameters"
// @Param        X-Project-ID header string true "Project ID"
// @Success      201 {object} SessionResponse
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/graph/sessions [post]
// @Security     bearerAuth
func (h *SessionHandler) CreateSession(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req CreateSessionRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	actorID, _ := getUserID(c)
	result, err := h.svc.CreateSession(c.Request().Context(), projectID, &req, actorID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, result)
}

// ListSessions lists sessions for the project.
// @Summary      List sessions
// @Description  Returns a paginated list of Session objects for the project.
// @Tags         sessions
// @Produce      json
// @Param        limit query int false "Max results (default: 20)"
// @Param        cursor query string false "Pagination cursor"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} ListSessionsResponse
// @Failure      400 {object} apperror.Error "Invalid parameters"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/graph/sessions [get]
// @Security     bearerAuth
func (h *SessionHandler) ListSessions(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	var cursor *string
	if cur := c.QueryParam("cursor"); cur != "" {
		cursor = &cur
	}

	result, err := h.svc.ListSessions(c.Request().Context(), projectID, limit, cursor)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetSession retrieves a single session by ID.
// @Summary      Get a session
// @Description  Returns a Session graph object by its ID.
// @Tags         sessions
// @Produce      json
// @Param        id path string true "Session ID"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} SessionResponse
// @Failure      400 {object} apperror.Error "Invalid ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Session not found"
// @Router       /api/graph/sessions/{id} [get]
// @Security     bearerAuth
func (h *SessionHandler) GetSession(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid session id")
	}

	result, err := h.svc.GetSession(c.Request().Context(), projectID, sessionID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// AppendMessage appends a message to a session.
// @Summary      Append a message to a session
// @Description  Atomically creates a Message object and links it to the session via a has_message relationship. Assigns sequence_number automatically.
// @Tags         sessions
// @Accept       json
// @Produce      json
// @Param        id path string true "Session ID"
// @Param        request body AppendMessageRequest true "Message parameters"
// @Param        X-Project-ID header string true "Project ID"
// @Success      201 {object} MessageResponse
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Session not found"
// @Router       /api/graph/sessions/{id}/messages [post]
// @Security     bearerAuth
func (h *SessionHandler) AppendMessage(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid session id")
	}

	var req AppendMessageRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	actorID, _ := getUserID(c)
	result, err := h.svc.AppendMessage(c.Request().Context(), projectID, sessionID, &req, actorID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, result)
}

// SpawnSession spawns a child session from a parent, optionally forking context.
// @Summary      Spawn a child session
// @Description  Creates a child session linked to the parent via a spawned_from relationship. When forkContext is true, the parent's message history is copied into the child as a snapshot.
// @Tags         sessions
// @Accept       json
// @Produce      json
// @Param        id path string true "Parent Session ID"
// @Param        request body SpawnSessionRequest true "Spawn parameters"
// @Param        X-Project-ID header string true "Project ID"
// @Success      201 {object} SpawnSessionResponse
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Parent session not found"
// @Router       /api/graph/sessions/{id}/spawn [post]
// @Security     bearerAuth
func (h *SessionHandler) SpawnSession(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	parentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid session id")
	}

	var req SpawnSessionRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	actorID, _ := getUserID(c)
	result, err := h.svc.SpawnSession(c.Request().Context(), projectID, parentID, &req, actorID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, result)
}

// ListMessages lists messages for a session.
// @Summary      List messages in a session
// @Description  Returns a paginated list of Message objects belonging to a session, ordered by sequence_number ascending.
// @Tags         sessions
// @Produce      json
// @Param        id path string true "Session ID"
// @Param        limit query int false "Max results (default: 50)"
// @Param        cursor query string false "Pagination cursor"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} ListMessagesResponse
// @Failure      400 {object} apperror.Error "Invalid parameters"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Session not found"
// @Router       /api/graph/sessions/{id}/messages [get]
// @Security     bearerAuth
func (h *SessionHandler) ListMessages(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid session id")
	}

	limit := 50
	if l := c.QueryParam("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	var cursor *string
	if cur := c.QueryParam("cursor"); cur != "" {
		cursor = &cur
	}

	result, err := h.svc.ListMessages(c.Request().Context(), projectID, sessionID, limit, cursor)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}
