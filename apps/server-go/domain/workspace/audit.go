package workspace

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// ToolAuditMiddleware creates Echo middleware that logs tool operations for security and debugging.
// Logs: operation type, workspace ID, user ID, timestamp, duration, request summary.
// Does NOT log file contents or command output.
func ToolAuditMiddleware(log *slog.Logger) echo.MiddlewareFunc {
	auditLog := log.With("component", "workspace-tool-audit")

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			// Extract context before handler execution
			workspaceID := c.Param("id")
			toolPath := c.Path()
			method := c.Request().Method
			userID := ""
			if user := auth.GetUser(c); user != nil {
				userID = user.ID
			}

			// Execute the handler
			err := next(c)

			duration := time.Since(start)
			status := c.Response().Status

			// Build audit log entry
			attrs := []any{
				"workspace_id", workspaceID,
				"tool", extractToolName(toolPath),
				"method", method,
				"user_id", userID,
				"status", status,
				"duration_ms", duration.Milliseconds(),
			}

			if err != nil {
				attrs = append(attrs, "error", err.Error())
				auditLog.Warn("tool operation failed", attrs...)
			} else {
				auditLog.Info("tool operation completed", attrs...)
			}

			return err
		}
	}
}

// extractToolName extracts the tool name from the route path.
// e.g., "/api/v1/agent/workspaces/:id/bash" -> "bash"
func extractToolName(path string) string {
	// Find the last segment after /:id/
	parts := splitPath(path)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

// splitPath splits a URL path by "/" and returns non-empty segments.
func splitPath(path string) []string {
	var parts []string
	for _, p := range splitString(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// splitString splits a string by a separator rune.
func splitString(s string, sep rune) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == sep {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	parts = append(parts, current)
	return parts
}
