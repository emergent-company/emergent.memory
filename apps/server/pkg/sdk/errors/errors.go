// Package errors provides SDK-specific error types for the Emergent API client.
package errors

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Error represents an error returned by the Emergent API.
type Error struct {
	StatusCode int                    `json:"status_code"`
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("[%d] %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("[%d] %s", e.StatusCode, e.Message)
}

// IsNotFound returns true if the error is a 404 Not Found error.
func IsNotFound(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.StatusCode == http.StatusNotFound
	}
	return false
}

// IsForbidden returns true if the error is a 403 Forbidden error.
func IsForbidden(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.StatusCode == http.StatusForbidden
	}
	return false
}

// IsUnauthorized returns true if the error is a 401 Unauthorized error.
func IsUnauthorized(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.StatusCode == http.StatusUnauthorized
	}
	return false
}

// IsBadRequest returns true if the error is a 400 Bad Request error.
func IsBadRequest(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.StatusCode == http.StatusBadRequest
	}
	return false
}

// ParseErrorResponse parses an HTTP error response into an Error.
func ParseErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Error{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("failed to read error response: %v", err),
		}
	}

	// Try to parse as JSON error response
	var apiErr struct {
		Error struct {
			Code    string                 `json:"code"`
			Message string                 `json:"message"`
			Details map[string]interface{} `json:"details"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return &Error{
			StatusCode: resp.StatusCode,
			Code:       apiErr.Error.Code,
			Message:    apiErr.Error.Message,
			Details:    apiErr.Error.Details,
		}
	}

	// Fallback to plain text
	return &Error{
		StatusCode: resp.StatusCode,
		Message:    string(body),
	}
}
