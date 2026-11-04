// Package errors provides SDK-specific error types for the Memory API client.
package errors

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Error represents an error returned by the Memory API.
type Error struct {
	StatusCode int                    `json:"status_code"`
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	base := fmt.Sprintf("[%d] %s", e.StatusCode, e.Message)
	if e.Code != "" {
		base = fmt.Sprintf("[%d] %s: %s", e.StatusCode, e.Code, e.Message)
	}
	if missing := missingScopes(e.Details); len(missing) > 0 {
		base += fmt.Sprintf(" (missing scope: %s)", strings.Join(missing, ", "))
	}
	return base
}

// missingScopes extracts the "missing" scope list from error details.
// The server surfaces the required-but-absent scopes under details.missing
// (e.g. {"missing": ["chat:use"]}); surfacing it here turns a generic
// "Insufficient permissions" into an actionable message.
func missingScopes(details map[string]interface{}) []string {
	if details == nil {
		return nil
	}
	raw, ok := details["missing"]
	if !ok {
		return nil
	}
	var out []string
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, v...)
	}
	return out
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

// IsConflict returns true if the error is a 409 Conflict error.
func IsConflict(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.StatusCode == http.StatusConflict
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
