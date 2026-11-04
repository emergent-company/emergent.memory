package main

import (
	"errors"
	"net/url"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// --- schema discovery page ---
//
// Discovery was startable only through the API, CLI and MCP: no page listed the
// runs or re-ran a failed one, so the attention panel's "Schema discovery" row
// had nowhere to send the user. This page is that somewhere.

// discoveryStatusLabel names a job state in the run's own vocabulary.
func discoveryStatusLabel(status string) string {
	switch status {
	case "pending":
		return "Queued"
	case "analyzing_documents", "extracting_types", "refining_schemas", "creating_pack":
		return "Running"
	case "completed":
		return "Completed"
	case "failed":
		return "Failed"
	case "cancelled":
		return "Cancelled"
	case "":
		return ""
	default:
		return status
	}
}

// discoveryStatusIntent colours a job state.
func discoveryStatusIntent(status string) ui.BadgeIntent {
	switch status {
	case "pending", "analyzing_documents", "extracting_types", "refining_schemas", "creating_pack":
		return ui.BadgeInfo
	case "completed":
		return ui.BadgeSuccess
	case "failed":
		return ui.BadgeError
	default:
		return ui.BadgeNeutral
	}
}

// uiSchemaDiscovery renders the project's discovery runs, newest first.
func (s *Server) uiSchemaDiscovery(c echo.Context) error {
	ctx := c.Request().Context()
	jobs, err := s.memory.ListDiscoveryJobs(ctx)
	return s.page(c, pageTitle("Schema discovery"), SchemaDiscoveryPage(jobs, userFacingError(err)))
}

// uiRetryDiscovery starts a new run from a failed job's recorded configuration.
// The failed attempt stays in history; nothing is reset.
func (s *Server) uiRetryDiscovery(c echo.Context) error {
	ctx := c.Request().Context()
	jobID := c.Param("jobId")
	const dest = "/schema/discovery"
	if jobID == "" {
		return s.redirectScopedError(c, dest, errors.New("a discovery job is required"))
	}
	if _, err := s.memory.RetryDiscoveryJob(ctx, jobID); err != nil {
		return s.redirectScopedError(c, dest, err)
	}
	// The panel's schema area reads the same report; drop the cached copy so the
	// new run is visible immediately.
	s.invalidateProcessingProblems(ctx)
	return s.redirectScoped(c, dest)
}

// discoveryRetryHref is the retry target for one run.
func discoveryRetryHref(jobID string) string {
	return "/schema/discovery/" + url.PathEscape(jobID) + "/retry"
}
