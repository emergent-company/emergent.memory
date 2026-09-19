package agents

import (
	"github.com/labstack/echo/v4"
)

// StreamMessage handles POST /message:stream (SSE). Stub — the streaming body is
// implemented by a later lane; it returns UNSUPPORTED_OPERATION for now.
func (h *A2AHandler) StreamMessage(c echo.Context) error {
	return writeA2AError(c, NewA2AError(A2ACodeUnsupportedOperation, A2AReasonUnsupportedOperation, "not implemented"))
}
