package agents

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// A2A v1.0 error envelope (google.rpc.Status) and version negotiation.

// A2AContentType is the A2A HTTP+JSON media type.
const A2AContentType = "application/a2a+json"

// A2AProtocolVersion is the only A2A protocol version this server implements.
const A2AProtocolVersion = "1.0"

// A2AVersionHeader is the header carrying the negotiated protocol version.
const A2AVersionHeader = "A2A-Version"

// A2AErrorDomain is the canonical error detail domain.
const A2AErrorDomain = "a2a-protocol.org"

// A2AErrorCode is a google.rpc.Status-style protocol error code.
type A2AErrorCode int

const (
	A2ACodeTaskNotFound                 A2AErrorCode = -32001
	A2ACodeTaskNotCancelable            A2AErrorCode = -32002
	A2ACodePushNotificationNotSupported A2AErrorCode = -32003
	A2ACodeUnsupportedOperation         A2AErrorCode = -32004
	A2ACodeMethodNotAllowed             A2AErrorCode = -32005
	A2ACodeInvalidAgentResponse         A2AErrorCode = -32006
	A2ACodeVersionNotSupported          A2AErrorCode = -32009
	A2ACodeUnauthenticated              A2AErrorCode = -32010
	A2ACodePermissionDenied             A2AErrorCode = -32011
)

// A2AErrorReason is the UPPER_SNAKE error reason emitted in the details entry.
type A2AErrorReason string

const (
	A2AReasonTaskNotFound                 A2AErrorReason = "TASK_NOT_FOUND"
	A2AReasonTaskNotCancelable            A2AErrorReason = "TASK_NOT_CANCELABLE"
	A2AReasonPushNotificationNotSupported A2AErrorReason = "PUSH_NOTIFICATION_NOT_SUPPORTED"
	A2AReasonUnsupportedOperation         A2AErrorReason = "UNSUPPORTED_OPERATION"
	A2AReasonInvalidAgentResponse         A2AErrorReason = "INVALID_AGENT_RESPONSE"
	A2AReasonVersionNotSupported          A2AErrorReason = "VERSION_NOT_SUPPORTED"
	A2AReasonMethodNotAllowed             A2AErrorReason = "METHOD_NOT_ALLOWED"
	A2AReasonUnauthenticated              A2AErrorReason = "UNAUTHENTICATED"
	A2AReasonPermissionDenied             A2AErrorReason = "PERMISSION_DENIED"
)

// HTTPStatus maps an A2A error code to its HTTP status code.
// -32001→404, -32002/-32003/-32004/-32009→400, -32006→500,
// -32005→405, -32010→401, -32011→403.
func (c A2AErrorCode) HTTPStatus() int {
	switch c {
	case A2ACodeTaskNotFound:
		return http.StatusNotFound
	case A2ACodeInvalidAgentResponse:
		return http.StatusInternalServerError
	case A2ACodeUnauthenticated:
		return http.StatusUnauthorized
	case A2ACodePermissionDenied:
		return http.StatusForbidden
	case A2ACodeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	default:
		return http.StatusBadRequest
	}
}

// a2aErrorTypeURL returns the type URL for the error's details entry.
func (r A2AErrorReason) typeURL() string {
	switch r {
	case A2AReasonTaskNotFound:
		return "type.googleapis.com/lf.a2a.v1.TaskNotFoundError"
	case A2AReasonTaskNotCancelable:
		return "type.googleapis.com/lf.a2a.v1.TaskNotCancelableError"
	case A2AReasonPushNotificationNotSupported:
		return "type.googleapis.com/lf.a2a.v1.PushNotificationNotSupportedError"
	case A2AReasonUnsupportedOperation:
		return "type.googleapis.com/lf.a2a.v1.UnsupportedOperationError"
	case A2AReasonInvalidAgentResponse:
		return "type.googleapis.com/lf.a2a.v1.InvalidAgentResponseError"
	case A2AReasonVersionNotSupported:
		return "type.googleapis.com/lf.a2a.v1.VersionNotSupportedError"
	default:
		return "type.googleapis.com/lf.a2a.v1.Error"
	}
}

// A2AErrorDetail is a single error detail entry.
type A2AErrorDetail struct {
	Type     string         `json:"@type"`
	Reason   string         `json:"reason"`
	Domain   string         `json:"domain"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// A2AErrorBody is the `error` member of the envelope.
type A2AErrorBody struct {
	Code    int              `json:"code"`
	Status  string           `json:"status"`
	Message string           `json:"message"`
	Details []A2AErrorDetail `json:"details"`
}

// A2AErrorEnvelope is the wire envelope for A2A errors.
type A2AErrorEnvelope struct {
	Error A2AErrorBody `json:"error"`
}

// A2AError is an A2A protocol error.
type A2AError struct {
	Code     A2AErrorCode
	Reason   A2AErrorReason
	Message  string
	Metadata map[string]any
}

// Error implements the error interface.
func (e *A2AError) Error() string {
	return e.Message
}

// NewA2AError constructs an A2AError from a code, reason, and message.
func NewA2AError(code A2AErrorCode, reason A2AErrorReason, message string) *A2AError {
	return &A2AError{Code: code, Reason: reason, Message: message}
}

// statusName maps an HTTP status code to the google.rpc.Status `status` string.
func statusName(httpStatus int) string {
	switch httpStatus {
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusInternalServerError:
		return "INTERNAL"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusMethodNotAllowed:
		return "METHOD_NOT_ALLOWED"
	default:
		return "INVALID_ARGUMENT"
	}
}

// Envelope renders the error as the wire envelope.
func (e *A2AError) Envelope() A2AErrorEnvelope {
	return A2AErrorEnvelope{
		Error: A2AErrorBody{
			Code:    int(e.Code),
			Status:  statusName(e.Code.HTTPStatus()),
			Message: e.Message,
			Details: []A2AErrorDetail{
				{
					Type:     e.Reason.typeURL(),
					Reason:   string(e.Reason),
					Domain:   A2AErrorDomain,
					Metadata: e.Metadata,
				},
			},
		},
	}
}

// writeA2AError writes an A2A error envelope with the A2A content type.
func writeA2AError(c echo.Context, err *A2AError) error {
	c.Response().Header().Set(echo.HeaderContentType, A2AContentType)
	return c.JSON(err.Code.HTTPStatus(), err.Envelope())
}

// writeA2AJSON writes a 200 response with the A2A content type.
func writeA2AJSON(c echo.Context, v any) error {
	c.Response().Header().Set(echo.HeaderContentType, A2AContentType)
	return c.JSON(http.StatusOK, v)
}

// ResolveA2AVersion negotiates the A2A protocol version from the A2A-Version
// header or query parameter, defaulting to A2AProtocolVersion. Returns a
// VERSION_NOT_SUPPORTED error for any other value.
func ResolveA2AVersion(c echo.Context) (string, *A2AError) {
	v := c.Request().Header.Get(A2AVersionHeader)
	if v == "" {
		v = c.QueryParam(A2AVersionHeader)
	}
	if v == "" {
		return A2AProtocolVersion, nil
	}
	if v != A2AProtocolVersion {
		return "", NewA2AError(A2ACodeVersionNotSupported, A2AReasonVersionNotSupported, "unsupported A2A protocol version "+v)
	}
	return A2AProtocolVersion, nil
}
