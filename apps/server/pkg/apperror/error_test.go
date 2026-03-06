package apperror

import (
	"errors"
	"net/http"
	"testing"
)

func TestErrorError(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		expected string
	}{
		{
			name: "without internal error",
			err: &Error{
				HTTPStatus: http.StatusNotFound,
				Code:       "not_found",
				Message:    "Resource not found",
			},
			expected: "not_found: Resource not found",
		},
		{
			name: "with internal error",
			err: &Error{
				HTTPStatus: http.StatusInternalServerError,
				Code:       "internal_error",
				Message:    "Something went wrong",
				Internal:   errors.New("database connection failed"),
			},
			expected: "internal_error: Something went wrong (database connection failed)",
		},
		{
			name: "empty message",
			err: &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "bad_request",
				Message:    "",
			},
			expected: "bad_request: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		wantNil  bool
		wantMsg  string
	}{
		{
			name: "nil internal error",
			err: &Error{
				HTTPStatus: http.StatusNotFound,
				Code:       "not_found",
				Message:    "Resource not found",
				Internal:   nil,
			},
			wantNil: true,
		},
		{
			name: "with internal error",
			err: &Error{
				HTTPStatus: http.StatusInternalServerError,
				Code:       "internal_error",
				Message:    "Something went wrong",
				Internal:   errors.New("underlying cause"),
			},
			wantNil: false,
			wantMsg: "underlying cause",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Unwrap()
			if tt.wantNil {
				if got != nil {
					t.Errorf("Unwrap() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Error("Unwrap() = nil, want non-nil")
				} else if got.Error() != tt.wantMsg {
					t.Errorf("Unwrap().Error() = %q, want %q", got.Error(), tt.wantMsg)
				}
			}
		})
	}
}

func TestErrorToEchoError(t *testing.T) {
	tests := []struct {
		name       string
		err        *Error
		wantStatus int
		wantCode   string
	}{
		{
			name: "basic error",
			err: &Error{
				HTTPStatus: http.StatusNotFound,
				Code:       "not_found",
				Message:    "Resource not found",
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name: "error with details",
			err: &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "validation_error",
				Message:    "Validation failed",
				Details: map[string]any{
					"field": "email",
					"error": "invalid format",
				},
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "validation_error",
		},
		{
			name: "internal server error",
			err: &Error{
				HTTPStatus: http.StatusInternalServerError,
				Code:       "internal_error",
				Message:    "Something went wrong",
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.ToEchoError()
			if got.Code != tt.wantStatus {
				t.Errorf("ToEchoError().Code = %d, want %d", got.Code, tt.wantStatus)
			}

			// Verify the message structure contains the error body
			msg, ok := got.Message.(map[string]any)
			if !ok {
				t.Fatal("ToEchoError().Message is not a map[string]any")
			}

			errBody, ok := msg["error"].(map[string]any)
			if !ok {
				t.Fatal("ToEchoError().Message['error'] is not a map[string]any")
			}

			if errBody["code"] != tt.wantCode {
				t.Errorf("error code = %v, want %v", errBody["code"], tt.wantCode)
			}
		})
	}
}

func TestErrorWithInternal(t *testing.T) {
	original := &Error{
		HTTPStatus: http.StatusNotFound,
		Code:       "not_found",
		Message:    "Resource not found",
	}

	internalErr := errors.New("database query failed")
	withInternal := original.WithInternal(internalErr)

	// Verify the new error has the internal error
	if withInternal.Internal != internalErr {
		t.Errorf("WithInternal().Internal = %v, want %v", withInternal.Internal, internalErr)
	}

	// Verify other fields are copied
	if withInternal.HTTPStatus != original.HTTPStatus {
		t.Errorf("WithInternal().HTTPStatus = %d, want %d", withInternal.HTTPStatus, original.HTTPStatus)
	}
	if withInternal.Code != original.Code {
		t.Errorf("WithInternal().Code = %q, want %q", withInternal.Code, original.Code)
	}
	if withInternal.Message != original.Message {
		t.Errorf("WithInternal().Message = %q, want %q", withInternal.Message, original.Message)
	}

	// Verify original is not modified
	if original.Internal != nil {
		t.Error("Original error was modified")
	}
}

func TestErrorWithMessage(t *testing.T) {
	original := &Error{
		HTTPStatus: http.StatusBadRequest,
		Code:       "bad_request",
		Message:    "Original message",
		Internal:   errors.New("internal"),
		Details:    map[string]any{"key": "value"},
	}

	newMessage := "Custom message"
	withMessage := original.WithMessage(newMessage)

	// Verify the new error has the new message
	if withMessage.Message != newMessage {
		t.Errorf("WithMessage().Message = %q, want %q", withMessage.Message, newMessage)
	}

	// Verify other fields are preserved
	if withMessage.HTTPStatus != original.HTTPStatus {
		t.Errorf("WithMessage().HTTPStatus = %d, want %d", withMessage.HTTPStatus, original.HTTPStatus)
	}
	if withMessage.Code != original.Code {
		t.Errorf("WithMessage().Code = %q, want %q", withMessage.Code, original.Code)
	}
	if withMessage.Internal != original.Internal {
		t.Errorf("WithMessage().Internal = %v, want %v", withMessage.Internal, original.Internal)
	}

	// Verify original is not modified
	if original.Message != "Original message" {
		t.Error("Original error was modified")
	}
}

func TestErrorWithDetails(t *testing.T) {
	original := &Error{
		HTTPStatus: http.StatusUnprocessableEntity,
		Code:       "validation_error",
		Message:    "Validation failed",
	}

	details := map[string]any{
		"field": "email",
		"error": "invalid format",
	}
	withDetails := original.WithDetails(details)

	// Verify the new error has the details
	if withDetails.Details == nil {
		t.Fatal("WithDetails().Details is nil")
	}
	if withDetails.Details["field"] != "email" {
		t.Errorf("WithDetails().Details['field'] = %v, want %v", withDetails.Details["field"], "email")
	}

	// Verify original is not modified
	if original.Details != nil {
		t.Error("Original error was modified")
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		code    string
		message string
	}{
		{
			name:    "not found",
			status:  http.StatusNotFound,
			code:    "not_found",
			message: "Resource not found",
		},
		{
			name:    "bad request",
			status:  http.StatusBadRequest,
			code:    "bad_request",
			message: "Invalid request",
		},
		{
			name:    "internal error",
			status:  http.StatusInternalServerError,
			code:    "internal_error",
			message: "Something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(tt.status, tt.code, tt.message)
			if err.HTTPStatus != tt.status {
				t.Errorf("New().HTTPStatus = %d, want %d", err.HTTPStatus, tt.status)
			}
			if err.Code != tt.code {
				t.Errorf("New().Code = %q, want %q", err.Code, tt.code)
			}
			if err.Message != tt.message {
				t.Errorf("New().Message = %q, want %q", err.Message, tt.message)
			}
			if err.Internal != nil {
				t.Error("New().Internal should be nil")
			}
			if err.Details != nil {
				t.Error("New().Details should be nil")
			}
		})
	}
}

func TestNewBadRequest(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "custom message",
			message: "Invalid email format",
		},
		{
			name:    "empty message",
			message: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewBadRequest(tt.message)
			if err.HTTPStatus != http.StatusBadRequest {
				t.Errorf("NewBadRequest().HTTPStatus = %d, want %d", err.HTTPStatus, http.StatusBadRequest)
			}
			if err.Code != "bad_request" {
				t.Errorf("NewBadRequest().Code = %q, want %q", err.Code, "bad_request")
			}
			if err.Message != tt.message {
				t.Errorf("NewBadRequest().Message = %q, want %q", err.Message, tt.message)
			}
		})
	}
}

func TestNewNotFound(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		id           string
		wantMessage  string
	}{
		{
			name:         "document not found",
			resourceType: "document",
			id:           "doc-123",
			wantMessage:  "document 'doc-123' not found",
		},
		{
			name:         "user not found",
			resourceType: "user",
			id:           "user-456",
			wantMessage:  "user 'user-456' not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewNotFound(tt.resourceType, tt.id)
			if err.HTTPStatus != http.StatusNotFound {
				t.Errorf("NewNotFound().HTTPStatus = %d, want %d", err.HTTPStatus, http.StatusNotFound)
			}
			if err.Code != "not_found" {
				t.Errorf("NewNotFound().Code = %q, want %q", err.Code, "not_found")
			}
			if err.Message != tt.wantMessage {
				t.Errorf("NewNotFound().Message = %q, want %q", err.Message, tt.wantMessage)
			}
		})
	}
}

func TestNewInternal(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		internalErr error
	}{
		{
			name:        "with wrapped error",
			message:     "Database query failed",
			internalErr: errors.New("connection timeout"),
		},
		{
			name:        "without wrapped error",
			message:     "Something went wrong",
			internalErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewInternal(tt.message, tt.internalErr)
			if err.HTTPStatus != http.StatusInternalServerError {
				t.Errorf("NewInternal().HTTPStatus = %d, want %d", err.HTTPStatus, http.StatusInternalServerError)
			}
			if err.Code != "internal_error" {
				t.Errorf("NewInternal().Code = %q, want %q", err.Code, "internal_error")
			}
			if err.Message != tt.message {
				t.Errorf("NewInternal().Message = %q, want %q", err.Message, tt.message)
			}
			if err.Internal != tt.internalErr {
				t.Errorf("NewInternal().Internal = %v, want %v", err.Internal, tt.internalErr)
			}
		})
	}
}

func TestNewForbidden(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "custom message",
			message: "You don't have permission to access this resource",
		},
		{
			name:    "admin only",
			message: "Admin access required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewForbidden(tt.message)
			if err.HTTPStatus != http.StatusForbidden {
				t.Errorf("NewForbidden().HTTPStatus = %d, want %d", err.HTTPStatus, http.StatusForbidden)
			}
			if err.Code != "forbidden" {
				t.Errorf("NewForbidden().Code = %q, want %q", err.Code, "forbidden")
			}
			if err.Message != tt.message {
				t.Errorf("NewForbidden().Message = %q, want %q", err.Message, tt.message)
			}
		})
	}
}

func TestToHTTPError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name: "app error",
			err: &Error{
				HTTPStatus: http.StatusNotFound,
				Code:       "not_found",
				Message:    "Resource not found",
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name: "app error with details",
			err: &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "validation_error",
				Message:    "Validation failed",
				Details:    map[string]any{"field": "email"},
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "validation_error",
		},
		{
			name:       "generic error",
			err:        errors.New("some generic error"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := ToHTTPError(tt.err)
			if status != tt.wantStatus {
				t.Errorf("ToHTTPError() status = %d, want %d", status, tt.wantStatus)
			}

			errBody, ok := body["error"].(map[string]any)
			if !ok {
				t.Fatal("ToHTTPError() body['error'] is not a map[string]any")
			}

			if errBody["code"] != tt.wantCode {
				t.Errorf("ToHTTPError() error code = %v, want %v", errBody["code"], tt.wantCode)
			}
		})
	}
}

func TestPredefinedErrors(t *testing.T) {
	// Verify predefined errors have correct status codes
	tests := []struct {
		name       string
		err        *Error
		wantStatus int
		wantCode   string
	}{
		{"ErrUnauthorized", ErrUnauthorized, http.StatusUnauthorized, "unauthorized"},
		{"ErrInvalidToken", ErrInvalidToken, http.StatusUnauthorized, "invalid_token"},
		{"ErrTokenExpired", ErrTokenExpired, http.StatusUnauthorized, "token_expired"},
		{"ErrMissingToken", ErrMissingToken, http.StatusUnauthorized, "missing_token"},
		{"ErrForbidden", ErrForbidden, http.StatusForbidden, "forbidden"},
		{"ErrInsufficientPermissions", ErrInsufficientPermissions, http.StatusForbidden, "insufficient_permissions"},
		{"ErrNotFound", ErrNotFound, http.StatusNotFound, "not_found"},
		{"ErrUserNotFound", ErrUserNotFound, http.StatusNotFound, "user_not_found"},
		{"ErrProjectNotFound", ErrProjectNotFound, http.StatusNotFound, "project_not_found"},
		{"ErrBadRequest", ErrBadRequest, http.StatusBadRequest, "bad_request"},
		{"ErrValidation", ErrValidation, http.StatusUnprocessableEntity, "validation_error"},
		{"ErrInternal", ErrInternal, http.StatusInternalServerError, "internal_error"},
		{"ErrDatabase", ErrDatabase, http.StatusInternalServerError, "database_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.HTTPStatus != tt.wantStatus {
				t.Errorf("%s.HTTPStatus = %d, want %d", tt.name, tt.err.HTTPStatus, tt.wantStatus)
			}
			if tt.err.Code != tt.wantCode {
				t.Errorf("%s.Code = %q, want %q", tt.name, tt.err.Code, tt.wantCode)
			}
		})
	}
}
