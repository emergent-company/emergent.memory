package workspace

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// ToolRestrictionMiddleware checks if the tool being invoked is in the agent definition's
// allowed tools list. If the tool is not allowed, it returns HTTP 403.
// If config is nil or has no tools restriction, all tools are allowed.
func ToolRestrictionMiddleware(config *AgentWorkspaceConfig, log *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// If no config or workspace disabled, skip restriction
			if config == nil || !config.Enabled {
				return next(c)
			}

			// If no tools list configured, all tools are allowed
			if len(config.Tools) == 0 {
				return next(c)
			}

			// Extract tool name from the request path
			toolName := extractToolFromPath(c.Path())
			if toolName == "" {
				return next(c)
			}

			if !config.IsToolAllowed(toolName) {
				log.Warn("tool access denied by workspace config",
					"tool", toolName,
					"allowed_tools", config.Tools,
					"path", c.Path(),
				)
				return c.JSON(http.StatusForbidden, map[string]string{
					"error": fmt.Sprintf("tool %q is not allowed for this agent; allowed tools: %s",
						toolName, strings.Join(config.Tools, ", ")),
				})
			}

			return next(c)
		}
	}
}

// extractToolFromPath extracts the tool name from a workspace tool endpoint path.
// e.g. "/api/v1/agent/workspaces/:id/bash" -> "bash"
func extractToolFromPath(path string) string {
	// Tool endpoints follow the pattern: .../workspaces/:id/<tool>
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}

	lastPart := parts[len(parts)-1]
	// Only return if it's a known tool name
	for _, valid := range ValidToolNames {
		if lastPart == valid {
			return lastPart
		}
	}
	return ""
}
