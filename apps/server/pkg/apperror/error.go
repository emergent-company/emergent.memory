package apperror

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

// Error represents an application error with HTTP status and error code
type Error struct {
	HTTPStatus int
	Code       string
	Message    string
	Internal   error
	Details    map[string]any
}

// Error implements the error interface
func (e *Error) Error() string {
	if e.Internal != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Internal)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the internal error
func (e *Error) Unwrap() error {
	return e.Internal
}

// ToEchoError converts the app error to an echo.HTTPError for proper handling
func (e *Error) ToEchoError() *echo.HTTPError {
	errBody := map[string]any{
		"code":    e.Code,
		"message": e.Message,
	}
	if len(e.Details) > 0 {
		errBody["details"] = e.Details
	}
	return echo.NewHTTPError(e.HTTPStatus, map[string]any{
		"error": errBody,
	})
}

// WithInternal returns a copy of the error with an internal error attached
func (e *Error) WithInternal(err error) *Error {
	return &Error{
		HTTPStatus: e.HTTPStatus,
		Code:       e.Code,
		Message:    e.Message,
		Internal:   err,
	}
}

// WithMessage returns a copy of the error with a custom message
func (e *Error) WithMessage(message string) *Error {
	return &Error{
		HTTPStatus: e.HTTPStatus,
		Code:       e.Code,
		Message:    message,
		Internal:   e.Internal,
		Details:    e.Details,
	}
}

// WithDetails returns a copy of the error with details attached
func (e *Error) WithDetails(details map[string]any) *Error {
	return &Error{
		HTTPStatus: e.HTTPStatus,
		Code:       e.Code,
		Message:    e.Message,
		Internal:   e.Internal,
		Details:    details,
	}
}

// New creates a new application error
func New(status int, code, message string) *Error {
	return &Error{
		HTTPStatus: status,
		Code:       code,
		Message:    message,
	}
}

// Common error definitions
var (
	// Authentication errors
	ErrUnauthorized = New(http.StatusUnauthorized, "unauthorized", "Authentication required")
	ErrInvalidToken = New(http.StatusUnauthorized, "invalid_token", "Invalid or expired token")
	ErrTokenExpired = New(http.StatusUnauthorized, "token_expired", "Token has expired")
	ErrMissingToken = New(http.StatusUnauthorized, "missing_token", "Missing authorization token")

	// Authorization errors
	ErrForbidden               = New(http.StatusForbidden, "forbidden", "Access denied")
	ErrInsufficientPermissions = New(http.StatusForbidden, "insufficient_permissions", "Insufficient permissions")

	// Resource errors
	ErrNotFound        = New(http.StatusNotFound, "not_found", "Resource not found")
	ErrUserNotFound    = New(http.StatusNotFound, "user_not_found", "User not found")
	ErrProjectNotFound = New(http.StatusNotFound, "project_not_found", "Project not found")
	ErrConflict        = New(http.StatusConflict, "conflict", "Resource already exists")

	// Validation errors
	ErrBadRequest = New(http.StatusBadRequest, "bad_request", "Invalid request")
	ErrValidation = New(http.StatusUnprocessableEntity, "validation_error", "Validation failed")

	// Server errors
	ErrInternal = New(http.StatusInternalServerError, "internal_error", "An internal error occurred")
	ErrDatabase = New(http.StatusInternalServerError, "database_error", "Database operation failed")
)

// ToHTTPError converts an app error to an HTTP-friendly format
func ToHTTPError(err error) (int, map[string]any) {
	if appErr, ok := err.(*Error); ok {
		errBody := map[string]any{
			"code":    appErr.Code,
			"message": appErr.Message,
		}
		if len(appErr.Details) > 0 {
			errBody["details"] = appErr.Details
		}
		return appErr.HTTPStatus, map[string]any{
			"error": errBody,
		}
	}

	// Default to internal server error for unknown errors
	return http.StatusInternalServerError, map[string]any{
		"error": map[string]any{
			"code":    "internal_error",
			"message": "An internal error occurred",
		},
	}
}

// NewBadRequest creates a bad request error with a custom message
func NewBadRequest(message string) *Error {
	return ErrBadRequest.WithMessage(message)
}

// NewNotFound creates a not found error for a resource type and ID
func NewNotFound(resourceType, id string) *Error {
	return ErrNotFound.WithMessage(fmt.Sprintf("%s '%s' not found", resourceType, id))
}

// NewInternal creates an internal error with a message and optional wrapped error
func NewInternal(message string, err error) *Error {
	return &Error{
		HTTPStatus: http.StatusInternalServerError,
		Code:       "internal_error",
		Message:    message,
		Internal:   err,
	}
}

// NewForbidden creates a forbidden error with a custom message
func NewForbidden(message string) *Error {
	return ErrForbidden.WithMessage(message)
}
