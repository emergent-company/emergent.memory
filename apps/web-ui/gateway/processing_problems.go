package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// --- processing problems (needs-attention panel) ---
//
// The panel is driven by one server read model
// (GET /api/projects/:id/processing-problems) instead of one fetch per queue.
// The gateway only renders what the server classifies; it never guesses a
// failure cause from a raw queue name.

// ProcessingProblemsReport mirrors domain/processingproblems.Report.
type ProcessingProblemsReport struct {
	ProjectID   string           `json:"projectId"`
	Total       int              `json:"total"`
	Areas       []ProcessingArea `json:"areas"`
	GeneratedAt string           `json:"generatedAt"`
}

// ProcessingArea is one group of problems (documents, graph, sources, …).
type ProcessingArea struct {
	Area  string `json:"area"`
	Label string `json:"label"`
	// Count is the exact number of problems in this area. Problems may carry
	// fewer rows than that (the server caps one queue's contribution), which
	// Truncated reports.
	Count     int                 `json:"count"`
	Truncated bool                `json:"truncated"`
	Summary   string              `json:"summary"`
	ByClass   []ProcessingClass   `json:"byClass"`
	Problems  []ProcessingProblem `json:"problems"`
}

// ProcessingClass is the count of one failure class inside an area.
type ProcessingClass struct {
	Class string `json:"class"`
	Count int    `json:"count"`
}

// ProcessingProblem is one unit whose latest attempt is a terminal failure.
type ProcessingProblem struct {
	Queue        string                   `json:"queue"`
	Area         string                   `json:"area"`
	JobID        string                   `json:"jobId"`
	UnitID       string                   `json:"unitId"`
	Label        string                   `json:"label"`
	DocumentID   string                   `json:"documentId,omitempty"`
	SourceID     string                   `json:"sourceId,omitempty"`
	Status       string                   `json:"status"`
	Attempts     int                      `json:"attempts"`
	MaxAttempts  int                      `json:"maxAttempts"`
	Failure      ProcessingProblemFailure `json:"failure"`
	ErrorMessage string                   `json:"errorMessage,omitempty"`
	OccurredAt   string                   `json:"occurredAt"`
	Action       ProcessingProblemAction  `json:"action"`
	Drilldown    string                   `json:"drilldownHref,omitempty"`
}

// ProcessingProblemFailure is the server's classification of a terminal error.
type ProcessingProblemFailure struct {
	Class       string `json:"class"`
	Summary     string `json:"summary"`
	Remediation string `json:"remediation"`
	Retryable   bool   `json:"retryable"`
}

// ProcessingProblemAction is the next step the server recommends.
type ProcessingProblemAction struct {
	Kind         string `json:"kind"`
	Label        string `json:"label"`
	Href         string `json:"href,omitempty"`
	RequiresRole string `json:"requiresRole,omitempty"`
}

// GetProcessingProblems fetches the project's terminal-failure report.
func (m *MemoryClient) GetProcessingProblems(ctx context.Context) (*ProcessingProblemsReport, error) {
	projectID := m.projectIDFor(ctx)
	if projectID == "" {
		return nil, fmt.Errorf("processing problems: no active project")
	}
	var out ProcessingProblemsReport
	if err := m.doH(ctx, http.MethodGet, "/api/projects/"+url.PathEscape(projectID)+"/processing-problems", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RetryExtractionJob starts a new extraction run for a failed or dead-lettered
// job. The old job stays in history with its error.
func (m *MemoryClient) RetryExtractionJob(ctx context.Context, jobID string) error {
	var out struct {
		Success bool `json:"success"`
	}
	return m.do(ctx, http.MethodPost, "/api/admin/extraction-jobs/"+url.PathEscape(jobID)+"/retry", nil, &out)
}

// RetryAllExtractionJobs starts a new run for every retryable extraction unit
// in the project and returns how many were queued.
func (m *MemoryClient) RetryAllExtractionJobs(ctx context.Context) (int, error) {
	projectID := m.projectIDFor(ctx)
	if projectID == "" {
		return 0, fmt.Errorf("retry extractions: no active project")
	}
	var out struct {
		Data struct {
			Retried int `json:"retried"`
		} `json:"data"`
	}
	if err := m.do(ctx, http.MethodPost, "/api/admin/extraction-jobs/projects/"+url.PathEscape(projectID)+"/bulk-retry", nil, &out); err != nil {
		return 0, err
	}
	return out.Data.Retried, nil
}

// RetryAllParsingJobs requeues the latest terminal conversion failure for each
// document in the project and returns how many jobs were reset.
func (m *MemoryClient) RetryAllParsingJobs(ctx context.Context) (int, error) {
	projectID := m.projectIDFor(ctx)
	if projectID == "" {
		return 0, fmt.Errorf("retry conversions: no active project")
	}
	var out struct {
		Data struct {
			Retried int `json:"retried"`
		} `json:"data"`
	}
	if err := m.do(ctx, http.MethodPost, "/api/admin/parsing-jobs/projects/"+url.PathEscape(projectID)+"/bulk-retry", nil, &out); err != nil {
		return 0, err
	}
	return out.Data.Retried, nil
}

// RetriggerEmbeddings resets the project's failed and dead-lettered embedding
// jobs to pending and returns how many were queued.
func (m *MemoryClient) RetriggerEmbeddings(ctx context.Context) (int, error) {
	projectID := m.projectIDFor(ctx)
	if projectID == "" {
		return 0, fmt.Errorf("retrigger embeddings: no active project")
	}
	var out struct {
		ObjectsReset int `json:"objects_reset"`
		ChunksReset  int `json:"chunks_reset"`
	}
	if err := m.do(ctx, http.MethodPost, "/api/projects/"+url.PathEscape(projectID)+"/embeddings/retrigger", nil, &out); err != nil {
		return 0, err
	}
	return out.ObjectsReset + out.ChunksReset, nil
}

// --- cached access for the sidebar badge ---

const processingProblemsTTL = 30 * time.Second

type processingProblemsEntry struct {
	report *ProcessingProblemsReport
	at     time.Time
}

// cachedProcessingProblems returns the report, reusing a recent result for the
// sidebar badge. Without the cache every full-page render would run the
// eleven-queue aggregate.
func (s *Server) cachedProcessingProblems(ctx context.Context) (*ProcessingProblemsReport, error) {
	projectID := s.currentProjectID(ctx)
	if projectID == "" {
		return nil, nil
	}

	s.problemsMu.Lock()
	entry, ok := s.problemsCache[projectID]
	s.problemsMu.Unlock()
	if ok && time.Since(entry.at) < processingProblemsTTL {
		return entry.report, nil
	}

	report, err := s.memory.GetProcessingProblems(ctx)
	if err != nil {
		return nil, err
	}

	s.problemsMu.Lock()
	if s.problemsCache == nil {
		s.problemsCache = make(map[string]processingProblemsEntry)
	}
	s.problemsCache[projectID] = processingProblemsEntry{report: report, at: time.Now()}
	s.problemsMu.Unlock()
	return report, nil
}

// invalidateProcessingProblems drops the cached report after a mutation that
// changes the failure set, so the panel and badge reflect the retry at once.
func (s *Server) invalidateProcessingProblems(ctx context.Context) {
	projectID := s.currentProjectID(ctx)
	if projectID == "" {
		return
	}
	s.problemsMu.Lock()
	delete(s.problemsCache, projectID)
	s.problemsMu.Unlock()
}

// currentProjectID resolves the active project from the session, falling back
// to the static project id used by dev/API-key mode.
func (s *Server) currentProjectID(ctx context.Context) string {
	if sc, ok := sessionContextFrom(ctx); ok && sc.ProjectID != "" {
		return sc.ProjectID
	}
	return s.cfg.MemoryProjectID
}

// processingDocumentsCount is the number of document-related problems. It
// backs the Documents nav badge, which deliberately does not count graph,
// source, email or agent failures.
func processingDocumentsCount(report *ProcessingProblemsReport) int {
	if report == nil {
		return 0
	}
	for _, area := range report.Areas {
		if area.Area == "documents" {
			return area.Count
		}
	}
	return 0
}

// processingAreaCountLabel renders an area's size honestly. A truncated area
// shows the rows the report carried against the true count ("200 of 264") so
// the page size can never be read as the backlog; an untruncated one prints the
// exact number.
func processingAreaCountLabel(area ProcessingArea) string {
	if area.Truncated {
		return strconv.Itoa(len(area.Problems)) + " of " + strconv.Itoa(area.Count)
	}
	return strconv.Itoa(area.Count)
}

// processingAreaAction returns the area-level action to render once in the
// panel header (currently only embeddings have a single project-wide retry).
// A problem whose failure is not retryable contributes no action: re-running a
// configuration or input failure without fixing the cause is the loop the
// panel exists to stop.
func processingAreaAction(area ProcessingArea) (ProcessingProblemAction, bool) {
	for _, p := range area.Problems {
		if p.Action.Kind == "retry_embeddings" && p.Failure.Retryable {
			return p.Action, true
		}
	}
	return ProcessingProblemAction{}, false
}

// processingAreaHasRetryableExtractions reports whether the documents area has
// at least one retryable extraction problem, so the bulk retry is not offered
// on a parsing-only area or on failures that would fail again unchanged.
func processingAreaHasRetryableExtractions(area ProcessingArea) bool {
	for _, p := range area.Problems {
		if p.Action.Kind == "retry_extraction" && p.Failure.Retryable {
			return true
		}
	}
	return false
}

// processingAreaHasParsingFailures reports whether the documents area contains
// a retryable conversion failure, so the bulk conversion retry is only offered
// when it can act.
func processingAreaHasParsingFailures(area ProcessingArea) bool {
	for _, p := range area.Problems {
		if p.Queue == "document_parsing" && p.Failure.Retryable {
			return true
		}
	}
	return false
}

// processingAreaHref is the full view for an area, used when the panel clips
// its per-problem list.
func processingAreaHref(area string) string {
	switch area {
	case "documents":
		return "/documents?status=failed"
	case "graph":
		return "/embeddings"
	case "sources":
		return "/data-sources"
	case "email":
		return "/settings/members"
	case "agents":
		return "/sessions"
	default:
		return "/schema"
	}
}

// processingBadgeCount keeps the sidebar badge short ("99+").
func processingBadgeCount(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

// processingClassLabel is the gateway's label for a server failure class. The
// server owns the class; the gateway owns its presentation.
func processingClassLabel(class string) string {
	switch class {
	case "configuration":
		return "Configuration"
	case "provider":
		return "Provider error"
	case "invalid_output":
		return "Invalid output"
	case "input":
		return "Unreadable input"
	case "permission":
		return "Permission"
	case "conflict":
		return "Conflict"
	case "limit":
		return "Output limit"
	case "internal":
		return "Internal error"
	default:
		return "Unknown"
	}
}

// processingClassIntent colours the class chip. Causes the user can fix
// (configuration, input, permission, conflict) are warnings; provider and
// pipeline faults are errors.
func processingClassIntent(class string) ui.BadgeIntent {
	switch class {
	case "configuration", "input", "permission", "conflict":
		return ui.BadgeWarning
	default:
		return ui.BadgeError
	}
}

// extractionJobStatusLabel names an extraction job state in the run's own
// vocabulary.
func extractionJobStatusLabel(status string, deadLettered bool) string {
	if deadLettered {
		return "Failed permanently"
	}
	switch status {
	case "queued", "pending":
		return "Queued"
	case "running", "processing":
		return "Extracting"
	case "completed":
		return "Completed"
	case "requires_review":
		return "Review needed"
	case "failed":
		return "Failed"
	case "dead_letter":
		return "Failed permanently"
	case "cancelled":
		return "Cancelled"
	case "":
		return ""
	default:
		return status
	}
}

// extractionJobStatusIntent colours an extraction job state.
func extractionJobStatusIntent(status string, deadLettered bool) ui.BadgeIntent {
	if deadLettered {
		return ui.BadgeError
	}
	switch status {
	case "running", "processing", "queued", "pending":
		return ui.BadgeInfo
	case "completed":
		return ui.BadgeSuccess
	case "failed", "dead_letter":
		return ui.BadgeError
	case "requires_review":
		return ui.BadgeWarning
	default:
		return ui.BadgeNeutral
	}
}

// --- handlers ---

// uiRetryExtraction handles the panel/history Retry button. It always starts a
// new run; the failed job remains visible in history.
func (s *Server) uiRetryExtraction(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	jobID := c.Param("jobId")
	if docID == "" || jobID == "" {
		return redirectWithError(c, "/documents", errDocumentIDRequired)
	}

	if err := s.memory.RetryExtractionJob(ctx, jobID); err != nil {
		return redirectWithError(c, "/documents/"+url.PathEscape(docID)+"/extractions", err)
	}
	s.invalidateProcessingProblems(ctx)
	return c.Redirect(http.StatusSeeOther, "/documents/"+url.PathEscape(docID)+"/extractions?retried=1")
}

// uiRetryAllExtractions handles the panel's project-wide retry.
func (s *Server) uiRetryAllExtractions(c echo.Context) error {
	ctx := c.Request().Context()
	n, err := s.memory.RetryAllExtractionJobs(ctx)
	if err != nil {
		return redirectWithError(c, "/documents", err)
	}
	s.invalidateProcessingProblems(ctx)
	if n == 0 {
		return c.Redirect(http.StatusSeeOther, "/documents?extractions_none=1")
	}
	return c.Redirect(http.StatusSeeOther, "/documents?extractions_retried="+strconv.Itoa(n))
}

// uiRetryAllConversions handles the panel's project-wide conversion retry.
func (s *Server) uiRetryAllConversions(c echo.Context) error {
	ctx := c.Request().Context()
	n, err := s.memory.RetryAllParsingJobs(ctx)
	if err != nil {
		return redirectWithError(c, "/documents", err)
	}
	s.invalidateProcessingProblems(ctx)
	if n == 0 {
		return c.Redirect(http.StatusSeeOther, "/documents?conversions_none=1")
	}
	return c.Redirect(http.StatusSeeOther, "/documents?conversions_retried="+strconv.Itoa(n))
}

// uiRetriggerEmbeddings handles the graph area's retry.
func (s *Server) uiRetriggerEmbeddings(c echo.Context) error {
	ctx := c.Request().Context()
	n, err := s.memory.RetriggerEmbeddings(ctx)
	if err != nil {
		return redirectWithError(c, "/documents", err)
	}
	s.invalidateProcessingProblems(ctx)
	if n == 0 {
		return c.Redirect(http.StatusSeeOther, "/documents?embeddings_none=1")
	}
	return c.Redirect(http.StatusSeeOther, "/documents?embeddings_retried="+strconv.Itoa(n))
}
