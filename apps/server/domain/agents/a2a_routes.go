package agents

import (
	"bytes"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterA2ARoutes registers the A2A v1.0 HTTP+JSON route set at the server
// root (outside /api/), mirroring RegisterACPRoutes and agentcompat/routes.go.
//
// Discovery (global card) is unauthenticated; everything else requires a Bearer
// emt_* project token with the stated scope. Project addressing is
// credential-scoped (token-bound), never path-scoped.
func RegisterA2ARoutes(e *echo.Echo, h *A2AHandler, authMiddleware *auth.Middleware) {
	// --- Discovery (global card: no auth) ---
	e.GET("/.well-known/agent-card.json", h.GlobalAgentCardHandler)

	// --- Extended card (agents:read) ---
	extended := e.Group("/extendedAgentCard")
	extended.Use(a2aErrorEnvelopeMiddleware)
	extended.Use(authMiddleware.RequireAuth())
	extended.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	extended.GET("", h.ExtendedAgentCardHandler)

	// --- Message flow (agents:write) ---
	// ":send" and ":stream" are literal path segments (they begin with 'm', not
	// ':'), so Echo treats /message:send and /message:stream as static paths.
	messageWrite := e.Group("/message")
	messageWrite.Use(a2aErrorEnvelopeMiddleware)
	// message:send is JSON (buffered envelope); message:stream is SSE and must
	// not be buffered, so its auth/scope runs under a dedicated non-buffering
	// envelope middleware.
	messageWrite.POST(":send", h.SendMessage, authMiddleware.RequireAuth(), authMiddleware.RequireAPITokenScopes("agents:write"))
	messageWrite.POST(":stream", h.StreamMessage, a2aStreamingAuthMiddleware(authMiddleware, "agents:write"))

	// --- Task read (agents:read) ---
	tasksRead := e.Group("/tasks")
	tasksRead.Use(a2aErrorEnvelopeMiddleware)
	tasksRead.Use(authMiddleware.RequireAuth())
	tasksRead.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	tasksRead.GET("", h.ListTasks)
	tasksRead.GET("/:id", h.GetTask)
	tasksRead.GET("/:id/pushNotificationConfigs", h.PushNotificationConfigsUnsupported)
	tasksRead.GET("/:id/pushNotificationConfigs/:configId", h.PushNotificationConfigsUnsupported)

	// --- Task write (agents:write) ---
	tasksWrite := e.Group("/tasks")
	tasksWrite.Use(a2aErrorEnvelopeMiddleware)
	tasksWrite.Use(authMiddleware.RequireAuth())
	tasksWrite.Use(authMiddleware.RequireAPITokenScopes("agents:write"))
	tasksWrite.POST("/:id/pushNotificationConfigs", h.PushNotificationConfigsUnsupported)
	tasksWrite.PUT("/:id/pushNotificationConfigs/:configId", h.PushNotificationConfigsUnsupported)
	tasksWrite.DELETE("/:id/pushNotificationConfigs", h.PushNotificationConfigsUnsupported)
	tasksWrite.DELETE("/:id/pushNotificationConfigs/:configId", h.PushNotificationConfigsUnsupported)

	// --- Task actions: POST /tasks/{id}:cancel and /tasks/{id}:subscribe ---
	//
	// Echo parses any path segment beginning with ':' as a path parameter, so a
	// literal /tasks/:id:cancel route would be captured as a parameter literally
	// named "id:cancel" and c.Param("id") would be empty. Instead we register a
	// single terminal wildcard POST /tasks/* and dispatch on the trailing action
	// suffix in DispatchTaskAction, which enforces the per-action scope
	// (:cancel → agents:write, :subscribe → agents:read) inside the dispatcher.
	tasksAction := e.Group("/tasks")
	tasksAction.Use(a2aErrorEnvelopeMiddleware)
	// Auth runs under the streaming envelope middleware (converts 401 to the A2A
	// envelope for :subscribe); per-action scope is enforced inside the
	// dispatcher via requireA2AScope.
	tasksAction.POST("/*", h.DispatchTaskAction, a2aStreamingAuthMiddleware(authMiddleware))
}

// ---------------------------------------------------------------------------
// POST /tasks/{id}:cancel and /tasks/{id}:subscribe dispatcher
// ---------------------------------------------------------------------------

const (
	a2aActionCancel    = "cancel"
	a2aActionSubscribe = "subscribe"
)

// parseTaskActionSuffix splits a wildcard value like "<id>:cancel" into the task
// id and action. It returns ok=false when the value has no recognised action
// suffix or an empty task id.
func parseTaskActionSuffix(wildcard string) (taskID, action string, ok bool) {
	i := strings.LastIndex(wildcard, ":")
	if i <= 0 || i == len(wildcard)-1 {
		return "", "", false
	}
	taskID = wildcard[:i]
	action = wildcard[i+1:]
	if taskID == "" || (action != a2aActionCancel && action != a2aActionSubscribe) {
		return "", "", false
	}
	return taskID, action, true
}

// resolveTaskAction parses the trailing action suffix from the terminal wildcard
// and enforces the per-action API-token scope, returning an A2A error on failure
// so auth/scope rejections stay in the A2A wire format.
func resolveTaskAction(c echo.Context) (taskID, action string, a2aErr *A2AError) {
	taskID, action, ok := parseTaskActionSuffix(c.Param("*"))
	if !ok {
		return "", "", NewA2AError(A2ACodeMethodNotAllowed, A2AReasonMethodNotAllowed, "unsupported task action")
	}

	var scope string
	switch action {
	case a2aActionCancel:
		scope = "agents:write"
	case a2aActionSubscribe:
		scope = "agents:read"
	}

	if a2aErr := requireA2AScope(c, scope); a2aErr != nil {
		return "", "", a2aErr
	}
	return taskID, action, nil
}

// DispatchTaskAction handles POST /tasks/{id}:cancel and POST /tasks/{id}:subscribe.
func (h *A2AHandler) DispatchTaskAction(c echo.Context) error {
	taskID, action, a2aErr := resolveTaskAction(c)
	if a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	// Expose the extracted task id as the "id" path parameter the concrete
	// handlers already read.
	c.SetParamNames("id")
	c.SetParamValues(taskID)

	switch action {
	case a2aActionCancel:
		return h.CancelTask(c)
	case a2aActionSubscribe:
		return h.SubscribeTask(c)
	default:
		return writeA2AError(c, NewA2AError(A2ACodeMethodNotAllowed, A2AReasonMethodNotAllowed, "unsupported task action"))
	}
}

// requireA2AScope enforces an API-token scope for an A2A operation, mirroring
// RequireAPITokenScopes semantics: enforcement applies only when the caller is
// authenticated via an emt_* API token (APITokenID != ""). OAuth/standalone
// sessions pass through. Returns an A2A error so rejections use the A2A
// envelope, not the platform's generic error shape.
//
// Authentication is enforced upstream by a2aStreamingAuthMiddleware (which wraps
// this dispatcher's route), so this performs only the scope check and does not
// duplicate the auth guard.
func requireA2AScope(c echo.Context, scope string) *A2AError {
	user := auth.GetUser(c)
	if user != nil && user.APITokenID != "" && !user.HasScope(scope) {
		return NewA2AError(A2ACodePermissionDenied, A2AReasonPermissionDenied, "insufficient permissions")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Push notification configs (unimplemented → PUSH_NOTIFICATION_NOT_SUPPORTED)
// ---------------------------------------------------------------------------

// PushNotificationConfigsUnsupported handles all A2A push-notification-config
// operations. This milestone does not implement push notifications, so every
// such operation returns the explicit PUSH_NOTIFICATION_NOT_SUPPORTED envelope
// (HTTP 400) required by the conformance spec rather than a generic 404.
func (h *A2AHandler) PushNotificationConfigsUnsupported(c echo.Context) error {
	if _, a2aErr := ResolveA2AVersion(c); a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}
	return writeA2AError(c, NewA2AError(A2ACodePushNotificationNotSupported, A2AReasonPushNotificationNotSupported, "push notification configs are not supported"))
}

// ---------------------------------------------------------------------------
// A2A error-envelope conversion (scoped to the A2A route groups only)
// ---------------------------------------------------------------------------

// isA2AStreamingPath reports whether a request path targets an SSE endpoint
// (message:stream, task subscribe). These write incremental data directly and
// must not be buffered by a2aErrorEnvelopeMiddleware.
func isA2AStreamingPath(path string) bool {
	return strings.HasSuffix(path, ":stream") || strings.HasSuffix(path, ":subscribe")
}

// a2aResponseCapture is a minimal http.ResponseWriter that buffers the response
// so a2aErrorEnvelopeMiddleware can rewrite platform auth/scope errors into the
// A2A error envelope. It is only used for non-streaming routes.
type a2aResponseCapture struct {
	status int
	header http.Header
	body   bytes.Buffer
}

func newA2AResponseCapture() *a2aResponseCapture {
	return &a2aResponseCapture{header: http.Header{}}
}

func (w *a2aResponseCapture) Header() http.Header { return w.header }

func (w *a2aResponseCapture) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
}

func (w *a2aResponseCapture) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(b)
}

// a2aStreamingAuthMiddleware runs the auth (and optional scope) middleware under
// a buffered response and converts auth/scope failures into the A2A error
// envelope, then hands off to the handler WITHOUT buffering so SSE bodies are
// not held. It is used on the streaming routes (:stream, :subscribe), where
// a2aErrorEnvelopeMiddleware intentionally skips buffering.
func a2aStreamingAuthMiddleware(authMiddleware *auth.Middleware, scopes ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		requireAuth := authMiddleware.RequireAuth()
		terminal := echo.HandlerFunc(func(echo.Context) error { return nil })
		var requireScope echo.MiddlewareFunc
		if len(scopes) > 0 {
			requireScope = authMiddleware.RequireAPITokenScopes(scopes...)
		}
		return func(c echo.Context) error {
			capture := newA2AResponseCapture()
			original := c.Response().Writer
			c.Response().Writer = capture

			chain := terminal
			if requireScope != nil {
				chain = requireScope(terminal)
			}
			err := requireAuth(chain)(c)

			c.Response().Writer = original
			c.Response().Status = 0
			c.Response().Committed = false
			c.Response().Size = 0

			if err != nil {
				return writeA2AError(c, a2aErrorFrom(err))
			}
			if capture.status >= 400 && capture.header.Get(echo.HeaderContentType) != A2AContentType {
				return writeA2AError(c, a2aErrorFromStatus(capture.status))
			}
			return next(c)
		}
	}
}

// a2aErrorEnvelopeMiddleware converts platform auth/scope errors (and any other
// non-A2A error the chain produces) into the A2A google.rpc.Status envelope.
// It is applied only to the A2A route groups, so non-A2A routes keep the
// platform error shape. RequireAuth writes its 401 directly (and returns nil),
// so the middleware buffers the response to rewrite it; RequireAPITokenScopes
// returns an *echo.HTTPError, which is converted from the returned error.
func a2aErrorEnvelopeMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if isA2AStreamingPath(c.Request().URL.Path) {
			return next(c)
		}

		capture := newA2AResponseCapture()
		original := c.Response().Writer
		c.Response().Writer = capture

		err := next(c)

		// Restore the real writer and reset Echo's response state before writing
		// any replacement body.
		c.Response().Writer = original
		c.Response().Status = 0
		c.Response().Committed = false
		c.Response().Size = 0

		if err != nil {
			return writeA2AError(c, a2aErrorFrom(err))
		}

		// A middleware may have written a platform error directly and returned
		// nil (RequireAuth's authError). If that body is not already an A2A
		// envelope, rewrite it.
		if capture.status >= 400 && capture.header.Get(echo.HeaderContentType) != A2AContentType {
			return writeA2AError(c, a2aErrorFromStatus(capture.status))
		}

		// Forward the buffered response unchanged.
		for k, vs := range capture.header {
			for _, v := range vs {
				original.Header().Add(k, v)
			}
		}
		if capture.status == 0 {
			capture.status = http.StatusOK
		}
		original.WriteHeader(capture.status)
		_, _ = original.Write(capture.body.Bytes())
		return nil
	}
}

// a2aErrorFrom converts a platform error (apperror.Error, echo.HTTPError) into
// an A2AError, passing A2AErrors through unchanged.
func a2aErrorFrom(err error) *A2AError {
	if err == nil {
		return nil
	}
	var a2aErr *A2AError
	if errors.As(err, &a2aErr) {
		return a2aErr
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return a2aErrorFromStatus(appErr.HTTPStatus)
	}
	var he *echo.HTTPError
	if errors.As(err, &he) {
		return a2aErrorFromStatus(he.Code)
	}
	return a2aErrorFromStatus(http.StatusInternalServerError)
}

// a2aErrorFromStatus maps an HTTP status to an A2A auth/scope/error envelope.
func a2aErrorFromStatus(status int) *A2AError {
	switch status {
	case http.StatusUnauthorized:
		return NewA2AError(A2ACodeUnauthenticated, A2AReasonUnauthenticated, "authentication required")
	case http.StatusForbidden:
		return NewA2AError(A2ACodePermissionDenied, A2AReasonPermissionDenied, "insufficient permissions")
	default:
		return NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "request failed")
	}
}
