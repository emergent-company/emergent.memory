package agents

import (
	"context"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// shareAgentChatScope mirrors the reserved marker scope minted on share keys
// (declared here to avoid importing domain/apitoken's unexported constant).
const shareAgentChatScope = "share:agent-chat"

// shareBindingFromContext returns the resolved share binding, or nil.
func shareBindingFromContext(ctx context.Context) *ShareLinkBinding {
	if b, ok := ctx.Value(shareBindingCtxKey{}).(*ShareLinkBinding); ok {
		return b
	}
	return nil
}

// mustShareBinding extracts the resolved share binding. Panics if the
// RequireShareLink middleware was not applied (programmer error).
func mustShareBinding(c echo.Context) *ShareLinkBinding {
	b := shareBindingFromContext(c.Request().Context())
	if b == nil {
		panic("share: mustShareBinding called without RequireShareLink middleware")
	}
	return b
}

// shareEndUserRefCtxKey is the context key for the verified end-user ref.
type shareEndUserRefCtxKey struct{}

// verifiedEndUserRef returns the verified end-user ref set by
// RequireShareIdentity, or "".
func verifiedEndUserRef(c echo.Context) string {
	if v, ok := c.Request().Context().Value(shareEndUserRefCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// ShareHandler serves the public agent-share endpoints and owner management.
type ShareHandler struct {
	svc *ShareService
}

// NewShareHandler constructs a ShareHandler.
func NewShareHandler(svc *ShareService) *ShareHandler {
	return &ShareHandler{svc: svc}
}

// RegisterShareRoutes registers share routes. Public share routes are behind
// RequireAuth + the marker-scope resolver; owner routes are project-scoped.
func RegisterShareRoutes(e *echo.Echo, h *ShareHandler, authMiddleware *auth.Middleware) {
	// --- Public share surface (anonymous end users, bearer share key) ---
	share := e.Group("/api/share/agent")
	share.Use(authMiddleware.RequireAuth())
	share.Use(h.RequireShareLink())

	share.GET("", h.PublicConfig)

	// Identity-bearing routes: the signed X-End-User-Ref (+ signature) is verified.
	identity := share.Group("")
	identity.Use(h.RequireShareIdentity(true))
	identity.GET("/sessions", h.ListSessions)
	identity.GET("/sessions/:id", h.GetSession)
	identity.POST("/sessions/:id/archive", h.ArchiveSession)
	identity.POST("/stream", h.Stream)
	identity.GET("/questions", h.ListQuestions)
	identity.POST("/sessions/:id/approvals/:questionId", h.Approve)

	// Session creation verifies the signature but does NOT require the end-user
	// row to pre-exist (it creates it on first use).
	share.POST("/sessions", h.CreateSession, h.RequireShareIdentity(false))

	// --- Owner management (project-scoped) ---
	owner := e.Group("/api/projects/:projectId")
	owner.Use(authMiddleware.RequireAuth())
	owner.Use(authMiddleware.RequireProjectTokenScope())
	owner.Use(authMiddleware.RequireProjectMember())

	owner.GET("/agent-definitions/:id/share-links", h.ListLinks, authMiddleware.RequireAPITokenScopes("agents:read"))
	owner.POST("/agent-definitions/:id/share-links", h.CreateLink, authMiddleware.RequireAPITokenScopes("agents:write"))
	owner.GET("/share-links/:linkId", h.GetLink, authMiddleware.RequireAPITokenScopes("agents:read"))
	owner.PATCH("/share-links/:linkId", h.UpdateLink, authMiddleware.RequireAPITokenScopes("agents:write"))
	owner.DELETE("/share-links/:linkId", h.DeleteLink, authMiddleware.RequireAPITokenScopes("agents:write"))
	owner.POST("/share-links/:linkId/rotate", h.RotateLink, authMiddleware.RequireAPITokenScopes("agents:write"))
	owner.GET("/share-links/:linkId/usage", h.GetUsage, authMiddleware.RequireAPITokenScopes("agents:read"))
	owner.GET("/share-links/:linkId/reveal", h.RevealLink, authMiddleware.RequireAPITokenScopes("agents:read"))
	owner.GET("/share-sessions", h.ListProjectSessions, authMiddleware.RequireAPITokenScopes("agents:read"))
	owner.GET("/share-sessions/:id", h.GetProjectSession, authMiddleware.RequireAPITokenScopes("agents:read"))
}

// RequireShareLink is the authorization chain for share routes: RequireAuth ->
// require the marker scope -> resolve the link from the API token ID. It sets
// the resolved project/org in context and deliberately ignores client-supplied
// X-Project-ID / X-Org-ID headers.
func (h *ShareHandler) RequireShareLink() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := auth.MustGetUser(c)
			if user.APITokenID == "" {
				return apperror.New(http.StatusUnauthorized, "unauthorized", "share token required")
			}
			if !user.HasScope(shareAgentChatScope) {
				return apperror.NewForbidden("missing share:agent-chat scope")
			}

			binding, err := h.svc.ResolveLinkByTokenID(c.Request().Context(), user.APITokenID)
			if err != nil {
				return err
			}

			// Authoritative project/org come from the resolved link — never the
			// client headers.
			user.ProjectID = binding.ProjectID
			user.OrgID = binding.OrgID

			ctx := c.Request().Context()
			ctx = context.WithValue(ctx, shareBindingCtxKey{}, binding)
			ctx = auth.ContextWithProjectID(ctx, binding.ProjectID)
			ctx = auth.ContextWithOrgID(ctx, binding.OrgID)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// RequireShareIdentity verifies the gateway-minted end-user reference on routes
// that carry identity. It reads X-End-User-Ref + X-End-User-Ref-Sig (never a
// query param or body field), verifies the HMAC signature, and — when
// requireExisting is true — requires the (link, ref) row to exist in
// kb.agent_share_end_users. The verified ref is stored in context.
func (h *ShareHandler) RequireShareIdentity(requireExisting bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			binding := mustShareBinding(c)

			ref := c.Request().Header.Get(shareEndUserRefHeader)
			sig := c.Request().Header.Get(shareEndUserRefSigHeader)

			if err := h.svc.VerifyEndUserRefSig(ref, sig); err != nil {
				return err
			}
			if requireExisting {
				if err := h.svc.EnsureEndUserExists(c.Request().Context(), binding.Link.ID, ref); err != nil {
					return err
				}
			}

			ctx := context.WithValue(c.Request().Context(), shareEndUserRefCtxKey{}, ref)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// --- Public share handlers ---

// PublicConfig handles GET /api/share/agent.
func (h *ShareHandler) PublicConfig(c echo.Context) error {
	return c.JSON(http.StatusOK, h.svc.PublicConfig(mustShareBinding(c)))
}

// createShareSessionRequest is the body for POST /api/share/agent/sessions.
// The end-user ref is NOT accepted from the body — it comes from the signed
// X-End-User-Ref header.
type createShareSessionRequest struct {
	Email string `json:"email"`
	Title string `json:"title"`
}

// CreateSession handles POST /api/share/agent/sessions.
func (h *ShareHandler) CreateSession(c echo.Context) error {
	binding := mustShareBinding(c)
	var req createShareSessionRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	endUserRef := verifiedEndUserRef(c)
	dto, err := h.svc.CreateSession(c.Request().Context(), binding, endUserRef, req.Email, req.Title)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, dto)
}

// ListSessions handles GET /api/share/agent/sessions.
func (h *ShareHandler) ListSessions(c echo.Context) error {
	binding := mustShareBinding(c)
	filter := c.QueryParam("filter")
	if filter == "" {
		filter = "active"
	}
	dto, err := h.svc.ListSessions(c.Request().Context(), binding, verifiedEndUserRef(c), filter)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// GetSession handles GET /api/share/agent/sessions/:id.
func (h *ShareHandler) GetSession(c echo.Context) error {
	binding := mustShareBinding(c)
	dto, err := h.svc.GetSession(c.Request().Context(), binding, c.Param("id"), verifiedEndUserRef(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// ArchiveSession handles POST /api/share/agent/sessions/:id/archive.
func (h *ShareHandler) ArchiveSession(c echo.Context) error {
	binding := mustShareBinding(c)
	if err := h.svc.ArchiveSession(c.Request().Context(), binding, c.Param("id"), verifiedEndUserRef(c)); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "archived"})
}

// streamShareRequest is the body for POST /api/share/agent/stream. The end-user
// ref comes from the signed header, not the body.
type streamShareRequest struct {
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
}

// Stream handles POST /api/share/agent/stream (SSE).
func (h *ShareHandler) Stream(c echo.Context) error {
	binding := mustShareBinding(c)

	var req streamShareRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if req.SessionID == "" {
		return apperror.NewBadRequest("sessionId is required")
	}
	if req.Message == "" {
		return apperror.NewBadRequest("message is required")
	}
	endUserRef := verifiedEndUserRef(c)

	session, err := h.svc.GetSessionEntity(c.Request().Context(), binding, req.SessionID, endUserRef)
	if err != nil {
		return err
	}
	if session.IsArchived {
		return apperror.New(http.StatusConflict, "conflict", "session is archived")
	}

	writer := sse.NewWriter(c.Response().Writer)
	_ = writer.Start()

	thinkingSeq := 0
	streamCallback := func(event StreamEvent) {
		switch event.Type {
		case StreamEventTextDelta:
			_ = writer.WriteData(sse.NewTokenEvent(event.Text))
		case StreamEventThinking:
			thinkingSeq++
			_ = writer.WriteData(sse.NewThinkingEvent(strconv.Itoa(thinkingSeq), event.Role, event.Text))
		case StreamEventToolCallStart:
			_ = writer.WriteData(sse.NewMCPToolEvent(event.Tool, "started", event.Input, ""))
		case StreamEventToolCallEnd:
			status := "completed"
			if event.Error != "" {
				status = "error"
			}
			_ = writer.WriteData(sse.NewMCPToolEvent(event.Tool, status, event.Output, event.Error))
		case StreamEventError:
			_ = writer.WriteData(sse.NewErrorEvent(event.Error))
		case StreamEventToolApproval:
			_ = writer.WriteData(sse.NewApprovalEvent(event.Tool, event.Input, event.QuestionID))
		}
	}

	h.svc.LogAccess(c.Request().Context(), binding.Link.ID, endUserRef, HashIP(clientIP(c)), "stream")

	result, err := h.svc.StreamMessage(c.Request().Context(), binding, session, req.Message, streamCallback)
	if err != nil {
		_ = writer.WriteData(sse.NewErrorEvent(err.Error()))
		return nil
	}

	if result != nil {
		_ = writer.WriteData(sse.NewDoneEventWithRun(result.RunID))
	} else {
		_ = writer.WriteData(sse.NewDoneEvent())
	}
	return nil
}

// ListQuestions handles GET /api/share/agent/questions.
func (h *ShareHandler) ListQuestions(c echo.Context) error {
	binding := mustShareBinding(c)
	sessionID := c.QueryParam("sessionId")
	dto, err := h.svc.PendingQuestions(c.Request().Context(), binding, verifiedEndUserRef(c), sessionID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// approveShareRequest is the body for POST .../approvals/:questionId. The
// end-user ref comes from the signed header, not the body.
type approveShareRequest struct {
	Response string `json:"response"`
	Message  string `json:"message"`
}

// Approve handles POST /api/share/agent/sessions/:id/approvals/:questionId.
func (h *ShareHandler) Approve(c echo.Context) error {
	binding := mustShareBinding(c)

	sessionID := c.Param("id")
	questionID := c.Param("questionId")

	var req approveShareRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if req.Response == "" {
		return apperror.NewBadRequest("response is required")
	}
	endUserRef := verifiedEndUserRef(c)

	dto, err := h.svc.RespondToQuestion(c.Request().Context(), binding, sessionID, endUserRef, questionID, req.Response, req.Message, HashIP(clientIP(c)))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, SuccessResponse(dto))
}

// --- Owner management handlers ---

// CreateLink handles POST /api/projects/:projectId/agent-definitions/:id/share-links.
func (h *ShareHandler) CreateLink(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	agentDefID := c.Param("id")

	var req struct {
		Label  string                `json:"label"`
		Config *ShareLinkConfigInput `json:"config"`
	}
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	dto, err := h.svc.CreateLink(c.Request().Context(), projectID, agentDefID, req.Label, user.ID, req.Config)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, dto)
}

// ListLinks handles GET /api/projects/:projectId/agent-definitions/:id/share-links.
func (h *ShareHandler) ListLinks(c echo.Context) error {
	dto, err := h.svc.ListLinks(c.Request().Context(), c.Param("projectId"), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// GetLink handles GET /api/projects/:projectId/share-links/:linkId.
func (h *ShareHandler) GetLink(c echo.Context) error {
	dto, err := h.svc.GetLink(c.Request().Context(), c.Param("linkId"), c.Param("projectId"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// UpdateLink handles PATCH /api/projects/:projectId/share-links/:linkId.
func (h *ShareHandler) UpdateLink(c echo.Context) error {
	user := auth.MustGetUser(c)
	var req struct {
		Label  string                `json:"label"`
		Config *ShareLinkConfigInput `json:"config"`
	}
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	dto, err := h.svc.UpdateLink(c.Request().Context(), c.Param("linkId"), c.Param("projectId"), user.ID, req.Label, req.Config)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// DeleteLink handles DELETE /api/projects/:projectId/share-links/:linkId.
func (h *ShareHandler) DeleteLink(c echo.Context) error {
	user := auth.MustGetUser(c)
	if err := h.svc.RevokeLink(c.Request().Context(), c.Param("linkId"), c.Param("projectId"), user.ID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// RotateLink handles POST /api/projects/:projectId/share-links/:linkId/rotate.
func (h *ShareHandler) RotateLink(c echo.Context) error {
	user := auth.MustGetUser(c)
	dto, err := h.svc.RotateLink(c.Request().Context(), c.Param("linkId"), c.Param("projectId"), user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// GetUsage handles GET /api/projects/:projectId/share-links/:linkId/usage.
func (h *ShareHandler) GetUsage(c echo.Context) error {
	dto, err := h.svc.GetUsage(c.Request().Context(), c.Param("linkId"), c.Param("projectId"), 90)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// RevealLink handles GET /api/projects/:projectId/share-links/:linkId/reveal.
func (h *ShareHandler) RevealLink(c echo.Context) error {
	user := auth.MustGetUser(c)
	key, err := h.svc.RevealKey(c.Request().Context(), c.Param("linkId"), c.Param("projectId"), user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"key": key})
}

// ListProjectSessions handles GET /api/projects/:projectId/share-sessions.
func (h *ShareHandler) ListProjectSessions(c echo.Context) error {
	dto, err := h.svc.ListSessionsByProject(c.Request().Context(), c.Param("projectId"), oauthUserID(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// GetProjectSession handles GET /api/projects/:projectId/share-sessions/:id,
// returning the session's plain transcript. Project ownership is enforced in
// the service (the client-supplied :projectId is never trusted) and, for
// OAuth sessions, the caller must be a project member.
func (h *ShareHandler) GetProjectSession(c echo.Context) error {
	messages, err := h.svc.GetSessionTranscriptByID(c.Request().Context(), c.Param("projectId"), c.Param("id"), oauthUserID(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string][]ShareTranscriptMessage{"messages": messages})
}

// oauthUserID returns the authenticated user's id for OAuth/session auth, or ""
// for API-token auth. Owner read endpoints pass this to the service so it can
// enforce project membership for OAuth callers only — API-token callers are
// already scoped to their project by RequireProjectTokenScope, and their token owner
// id is not necessarily a project member.
func oauthUserID(c echo.Context) string {
	user := auth.MustGetUser(c)
	if user.APITokenID == "" {
		return user.ID
	}
	return ""
}

// --- helpers ---

// clientIP returns the best-effort client IP (never trust X-Forwarded-For
// blindly — used only for hashed audit logging).
func clientIP(c echo.Context) string {
	return c.RealIP()
}
