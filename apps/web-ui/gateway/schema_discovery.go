package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- schema discovery UI (Schema page card + per-document trigger) ---

// SchemaDiscoveryView is the Schema page's discovery card. Latest is the
// project's newest discovery job (nil before the first run); DocumentCount is
// how many documents "all documents" would consider, so the action can name its
// scope. Err is a non-fatal load failure: the card says the state is
// unavailable instead of taking the whole schema page down.
type SchemaDiscoveryView struct {
	Latest        *discoveryJob
	DocumentCount int
	Err           error
}

// loadSchemaDiscovery reads the newest job and the project's document count.
// The reads degrade independently: a failed count still renders the job card,
// and a failed job list still lets the user start a run.
func (s *Server) loadSchemaDiscovery(ctx context.Context) *SchemaDiscoveryView {
	view := &SchemaDiscoveryView{}
	jobs, err := s.memory.ListDiscoveryJobs(ctx)
	if err != nil {
		view.Err = userFacingError(err)
	} else if len(jobs) > 0 {
		view.Latest = &jobs[0]
	}
	if page, err := s.memory.ListDocuments(ctx, DocumentListParams{Limit: 1}); err == nil {
		view.DocumentCount = page.Total
	}
	return view
}

// uiSchemaDiscoveryStart handles POST /schema/discovery/start: the Schema
// page's "all documents" action. The server resolves the document set
// (scope=project), so the form carries no document ids.
func (s *Server) uiSchemaDiscoveryStart(c echo.Context) error {
	ctx := c.Request().Context()
	if _, err := s.memory.StartProjectDiscovery(ctx); err != nil {
		return redirectWithError(c, "/schema", err)
	}
	s.invalidateProcessingProblems(ctx)
	return c.Redirect(http.StatusSeeOther, "/schema?discovered=1#discovery")
}

// uiSchemaDiscoveryRetry handles POST /schema/discovery/retry/:jobID. It backs
// both the card's Retry button and the Documents "Needs attention" panel row
// for a failed discovery run (the panel posts to the same endpoint).
func (s *Server) uiSchemaDiscoveryRetry(c echo.Context) error {
	ctx := c.Request().Context()
	jobID := strings.TrimSpace(c.Param("jobID"))
	if jobID == "" {
		return redirectWithError(c, "/schema", errDiscoveryJobIDRequired)
	}
	if _, err := s.memory.RetryDiscoveryJob(ctx, jobID); err != nil {
		return redirectWithError(c, "/schema", err)
	}
	s.invalidateProcessingProblems(ctx)
	return c.Redirect(http.StatusSeeOther, "/schema?discovered=1#discovery")
}

// uiDocumentDiscover handles POST /documents/:id/discover: run discovery over
// one document and return to it. Open to any project member, like the
// extraction trigger next to it.
func (s *Server) uiDocumentDiscover(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, "/documents", errDocumentIDRequired)
	}
	back := "/documents/" + url.PathEscape(id)
	if _, err := s.memory.StartDocumentDiscovery(ctx, id); err != nil {
		return redirectWithError(c, back, err)
	}
	s.invalidateProcessingProblems(ctx)
	return c.Redirect(http.StatusSeeOther, back+"?discovering=1")
}

// discoveryJobSummary is the one-line state under the card's status badge.
func discoveryJobSummary(job *discoveryJob) string {
	if job == nil {
		return ""
	}
	switch {
	case job.isRunning():
		if job.Progress.Message != "" {
			return job.Progress.Message
		}
		return "Analysing documents…"
	case job.Status == "failed":
		return "The last run failed; the technical detail is below."
	case job.Status == "completed":
		types := len(job.DiscoveredTypes)
		rels := len(job.DiscoveredRelationships)
		return fmt.Sprintf("%d object type%s and %d relationship%s proposed.",
			types, plural(types), rels, plural(rels))
	case job.Status == "cancelled":
		return "The last run was cancelled."
	default:
		return job.Status
	}
}

// discoveryJobTriggerLabel names who started the run, so an automatic sync run
// is not mistaken for one the user started.
func discoveryJobTriggerLabel(job *discoveryJob) string {
	if job == nil {
		return ""
	}
	switch job.Trigger {
	case "source_sync":
		return "Started automatically after a source sync."
	case "manual":
		return "Started manually."
	default:
		return ""
	}
}

// errDiscoveryJobIDRequired guards the discovery retry route: the job id is a
// path segment, and an empty one must not become a request to the jobs root.
var errDiscoveryJobIDRequired = errors.New("discovery job id is required")
