package apperror

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

// HTTPErrorHandler returns an Echo error handler that formats errors to match NestJS format.
// This is the canonical error handler used by both production and test servers.
func HTTPErrorHandler(log *slog.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}

		// Default error response
		code := http.StatusInternalServerError
		errorObj := map[string]any{
			"code":    "internal_error",
			"message": "An internal error occurred",
		}

		// Handle our custom app errors first
		if appErr, ok := err.(*Error); ok {
			code = appErr.HTTPStatus
			errorObj["code"] = appErr.Code
			errorObj["message"] = appErr.Message
		} else if he, ok := err.(*echo.HTTPError); ok {
			// Handle Echo HTTP errors
			code = he.Code

			// Check if the message is a structured error map (e.g., from RequireScopes)
			if msgMap, ok := he.Message.(map[string]any); ok {
				if errInner, ok := msgMap["error"].(map[string]any); ok {
					// Copy all fields from the inner error object
					for k, v := range errInner {
						errorObj[k] = v
					}
				}
			} else if msg, ok := he.Message.(string); ok {
				errorObj["message"] = msg
				// Map HTTP status to error code
				switch code {
				case http.StatusUnauthorized:
					errorObj["code"] = "unauthorized"
				case http.StatusForbidden:
					errorObj["code"] = "forbidden"
				case http.StatusNotFound:
					errorObj["code"] = "not_found"
				case http.StatusBadRequest:
					errorObj["code"] = "bad_request"
				case http.StatusConflict:
					errorObj["code"] = "conflict"
				case http.StatusUnprocessableEntity:
					errorObj["code"] = "validation_error"
				}
			}
		}

		// Log error (5xx errors get logged at error level)
		if code >= 500 {
			log.Error("request error",
				slog.Int("status", code),
				slog.String("error", err.Error()),
			)
		}

		// Format response to match NestJS error format
		response := map[string]any{
			"error": errorObj,
		}

		// Send error response
		if c.Request().Method == http.MethodHead {
			c.NoContent(code)
		} else {
			c.JSON(code, response)
		}
	}
}
