package agents

import (
	"github.com/labstack/echo/v4"
)

// Message-flow handler stubs. Bodies are implemented by a later lane; these
// return UNSUPPORTED_OPERATION so the package compiles and the routes are
// registered against concrete handlers.

// SendMessage handles POST /message:send.
func (h *A2AHandler) SendMessage(c echo.Context) error {
	return writeA2AError(c, NewA2AError(A2ACodeUnsupportedOperation, A2AReasonUnsupportedOperation, "not implemented"))
}

// GetTask handles GET /tasks/{id}.
func (h *A2AHandler) GetTask(c echo.Context) error {
	return writeA2AError(c, NewA2AError(A2ACodeUnsupportedOperation, A2AReasonUnsupportedOperation, "not implemented"))
}

// ListTasks handles GET /tasks.
func (h *A2AHandler) ListTasks(c echo.Context) error {
	return writeA2AError(c, NewA2AError(A2ACodeUnsupportedOperation, A2AReasonUnsupportedOperation, "not implemented"))
}

// CancelTask handles POST /tasks/{id}:cancel.
func (h *A2AHandler) CancelTask(c echo.Context) error {
	return writeA2AError(c, NewA2AError(A2ACodeUnsupportedOperation, A2AReasonUnsupportedOperation, "not implemented"))
}

// SubscribeTask handles POST /tasks/{id}:subscribe.
func (h *A2AHandler) SubscribeTask(c echo.Context) error {
	return writeA2AError(c, NewA2AError(A2ACodeUnsupportedOperation, A2AReasonUnsupportedOperation, "not implemented"))
}
