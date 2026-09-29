package main

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

// Default and maximum page sizes for the device object browser. The default
// mirrors the web-ui objects browser (objects.go listParams); the cap keeps a
// rogue ?limit from asking the backend for an unbounded page.
const (
	agentObjectsDefaultLimit = 25
	agentObjectsMaxLimit     = 100
)

// agentObjectProvenance validates the ?provenance= query value for the
// device object browser. Empty or "any" applies no narrowing; "created" and
// "updated" narrow to the actor's creator / latest-updater rows. Any other
// value is rejected (the "*_ok" false) so a typo surfaces as a 400 rather than
// silently browsing the wrong slice.
func agentObjectProvenance(raw string) (provenance string, ok bool) {
	switch raw {
	case "", defaultObjectProvenance:
		return defaultObjectProvenance, true
	case "created", "updated":
		return raw, true
	default:
		return "", false
	}
}

// agentObjectsLimit parses the optional ?limit= query value: empty defaults to
// agentObjectsDefaultLimit, a non-positive or non-integer value is rejected,
// and anything larger is capped at agentObjectsMaxLimit.
func agentObjectsLimit(raw string) (limit int, ok bool) {
	if raw == "" {
		return agentObjectsDefaultLimit, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, false
	}
	return min(n, agentObjectsMaxLimit), true
}

// listAgentObjects implements GET /api/agents/:id/objects — the
// device-credential-reachable JSON object browser: the objects an agent created
// or updated, cursor-paginated. It reuses the graph objects search endpoint via
// ListGraphObjectsPage with the actor pair fixed to (agent, :id).
//
// Gating is on the agent definition existing, NOT on any memory-MCP capability
// check: provenance browsing applies to every agent that has made graph writes,
// not only those wired to the "memory" MCP server. Response shape mirrors the
// graph search page: {"items": [GraphObject], "next_cursor": "..."}.
func (s *Server) listAgentObjects(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	provenance, ok := agentObjectProvenance(c.QueryParam("provenance"))
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "provenance must be one of created, updated, any"})
	}
	limit, ok := agentObjectsLimit(c.QueryParam("limit"))
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
	}

	if _, err := s.memory.GetAgentDefinition(ctx, id); err != nil {
		if isMemoryNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "unknown agent"})
		}
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}

	items, nextCursor, err := s.memory.ListGraphObjectsPage(ctx, ObjectListParams{
		BranchID:   c.QueryParam("branch"),
		Cursor:     c.QueryParam("cursor"),
		Limit:      limit,
		ActorType:  "agent",
		ActorID:    id,
		Provenance: provenance,
	})
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	if items == nil {
		items = []GraphObject{}
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "next_cursor": nextCursor})
}
