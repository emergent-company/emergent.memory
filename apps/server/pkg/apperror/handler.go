package apperror

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
)

// pgError extracts a *pgconn.PgError from anywhere in the error chain.
// Returns nil if err is not (or does not wrap) a Postgres error.
func pgError(err error) *pgconn.PgError {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr
	}
	return nil
}

// pgErrorMessage builds a human-readable message from a PgError using the
// constraint name, table, and column when available.
func pgErrorMessage(pgErr *pgconn.PgError) string {
	switch pgErr.Code {
	case "23505": // unique_violation
		if pgErr.ConstraintName != "" {
			return fmt.Sprintf("duplicate value violates unique constraint %q", pgErr.ConstraintName)
		}
		return "a record with that value already exists"
	case "23503": // foreign_key_violation
		if pgErr.ConstraintName != "" {
			return fmt.Sprintf("foreign key constraint %q violated — referenced record does not exist", pgErr.ConstraintName)
		}
		return "referenced record does not exist"
	case "23502": // not_null_violation
		if pgErr.ColumnName != "" {
			return fmt.Sprintf("column %q must not be null", pgErr.ColumnName)
		}
		return "a required field is missing"
	case "23514": // check_violation
		if pgErr.ConstraintName != "" {
			return fmt.Sprintf("value violates check constraint %q", pgErr.ConstraintName)
		}
		return "a field value failed a database check constraint"
	}
	return pgErr.Message
}

// HTTPErrorHandler returns an Echo error handler that formats errors in a standard JSON format.
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

		// Handle our custom app errors first (errors.As unwraps fmt.Errorf chains)
		var appErr *Error
		if errors.As(err, &appErr) {
			code = appErr.HTTPStatus
			errorObj["code"] = appErr.Code
			errorObj["message"] = appErr.Message
		} else if pgErr := pgError(err); pgErr != nil {
			// Map Postgres error codes to HTTP status codes so DB constraint
			// violations never surface as opaque 500s.
			msg := pgErrorMessage(pgErr)
			switch pgErr.Code {
			case "23505": // unique_violation
				code = http.StatusConflict
				errorObj["code"] = "conflict"
				errorObj["message"] = msg
			case "23503": // foreign_key_violation
				code = http.StatusConflict
				errorObj["code"] = "conflict"
				errorObj["message"] = msg
			case "23502": // not_null_violation
				code = http.StatusBadRequest
				errorObj["code"] = "bad_request"
				errorObj["message"] = msg
			case "23514": // check_violation
				code = http.StatusBadRequest
				errorObj["code"] = "bad_request"
				errorObj["message"] = msg
			default:
				// Unknown Postgres error — stays 500, logged with pg code for diagnosis
				log.Error("unhandled postgres error",
					slog.String("pg_code", pgErr.Code),
					slog.String("pg_message", pgErr.Message),
					slog.String("constraint", pgErr.ConstraintName),
					slog.String("table", pgErr.TableName),
				)
			}
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

		// Format standard JSON error response
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
