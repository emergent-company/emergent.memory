package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
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
	// Cause narrows a configuration failure to the element that is missing
	// (no_provider, no_default_model, model_unusable, model_unavailable,
	// base_url). Empty for classes the copy already describes exactly.
	Cause string `json:"cause,omitempty"`
	// Model is the generative model the project resolves to, when the failure is
	// a model-provider problem. The row names it so "the model" is not anonymous.
	Model string `json:"model,omitempty"`
	// Verified is true when the summary was checked against the project's model
	// configuration. A configuration failure with Verified=false is a guess.
	Verified bool `json:"verified"`
	// Fix is where the server says the recorded cause is fixed, when it knows a
	// page. The gateway prefers it over its own class mapping.
	Fix *ProcessingProblemFix `json:"fix,omitempty"`
}

// ProcessingProblemFix is the page that fixes one recorded cause.
type ProcessingProblemFix struct {
	Href  string `json:"href"`
	Label string `json:"label"`
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

// RetryParsingJob resets one failed document conversion to pending with a fresh
// retry budget. Unlike RetryExtractionJob it reuses the job row: a conversion is
// deterministic work, not a new model run with history to keep.
func (m *MemoryClient) RetryParsingJob(ctx context.Context, jobID string) error {
	var out struct {
		Success bool `json:"success"`
	}
	return m.do(ctx, http.MethodPost, "/api/admin/parsing-jobs/"+url.PathEscape(jobID)+"/retry", nil, &out)
}

// RetryAllExtractionJobs starts a new run for every retryable extraction unit
// in the project and returns how many were queued. includeNonRetryable is the
// caller's explicit assertion that a configuration/permission cause is fixed;
// without it the server skips those units.
func (m *MemoryClient) RetryAllExtractionJobs(ctx context.Context, includeNonRetryable bool) (int, error) {
	projectID := m.projectIDFor(ctx)
	if projectID == "" {
		return 0, fmt.Errorf("retry extractions: no active project")
	}
	var out struct {
		Data struct {
			Retried int `json:"retried"`
		} `json:"data"`
	}
	path := "/api/admin/extraction-jobs/projects/" + url.PathEscape(projectID) + "/bulk-retry"
	if includeNonRetryable {
		path += "?include_non_retryable=true"
	}
	if err := m.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return 0, err
	}
	return out.Data.Retried, nil
}

// RetryAllParsingJobs requeues the latest terminal conversion failure for each
// document in the project and returns how many jobs were reset. The
// includeNonRetryable flag has the same explicit-override meaning as above.
func (m *MemoryClient) RetryAllParsingJobs(ctx context.Context, includeNonRetryable bool) (int, error) {
	projectID := m.projectIDFor(ctx)
	if projectID == "" {
		return 0, fmt.Errorf("retry conversions: no active project")
	}
	var out struct {
		Data struct {
			Retried int `json:"retried"`
		} `json:"data"`
	}
	path := "/api/admin/parsing-jobs/projects/" + url.PathEscape(projectID) + "/bulk-retry"
	if includeNonRetryable {
		path += "?include_non_retryable=true"
	}
	if err := m.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return 0, err
	}
	return out.Data.Retried, nil
}

// RetriggerEmbeddings resets the project's failed and dead-lettered embedding
// jobs to pending and returns how many were queued. includeNonRetryable is the
// caller's explicit assertion that a configuration/permission cause is fixed;
// without it the server skips those units, so the panel's after-fixing action
// would reset nothing on the project state it exists for.
func (m *MemoryClient) RetriggerEmbeddings(ctx context.Context, includeNonRetryable bool) (int, error) {
	projectID := m.projectIDFor(ctx)
	if projectID == "" {
		return 0, fmt.Errorf("retrigger embeddings: no active project")
	}
	var out struct {
		ObjectsReset int `json:"objects_reset"`
		ChunksReset  int `json:"chunks_reset"`
	}
	path := "/api/projects/" + url.PathEscape(projectID) + "/embeddings/retrigger"
	if includeNonRetryable {
		path += "?include_non_retryable=true"
	}
	if err := m.do(ctx, http.MethodPost, path, nil, &out); err != nil {
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

// processingAreaViewAllLabel is the full-list link's text: it names the rows the
// expanded view will actually render. A truncated area therefore says "View 200
// of 264", so the link never promises more than the panel can show.
func processingAreaViewAllLabel(area ProcessingArea) string {
	if area.Truncated {
		return "View " + processingAreaCountLabel(area) + " in " + area.Label
	}
	return "View all " + strconv.Itoa(area.Count) + " in " + area.Label
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

// processingProblemsClip is the number of rows an area shows before the panel
// clips it and offers the area's full list.
const processingProblemsClip = 8

// processingAreaClip is how many of an area's problems the panel renders: the
// default clip, or every row the report carried once the reader opened the
// area's full list.
func processingAreaClip(area ProcessingArea, expanded bool) int {
	if expanded {
		return len(area.Problems)
	}
	return processingProblemsClip
}

// processingRetryAction is the per-problem retry the panel can offer.
type processingRetryAction struct {
	Href  string
	Label string
	// Confirm is a non-empty prompt shown before the POST when the failure is
	// classified non-retryable. The click still goes ahead — the user is the one
	// who fixes the cause — but the panel never implies a fresh run is likely to
	// succeed unchanged.
	Confirm string
}

// processingProblemRetry describes the retry a row can perform. It is returned
// for queues with a per-unit retry, independent of the failure's Retryable
// flag: a configuration failure is a deliberate "never retry automatically"
// decision, not a reason to leave the user without a button after they fixed
// Providers. Queues with no safe retry (schema, blueprints, email, sessions)
// return false and keep navigation only.
func processingProblemRetry(p ProcessingProblem) (processingRetryAction, bool) {
	// An internal failure is a bug in the pipeline: re-running the same input
	// reproducibly stops in the same place, so the row keeps the remediation
	// ("report it with the technical detail") and no button that cannot work.
	if p.Failure.Class == "internal" {
		return processingRetryAction{}, false
	}

	confirm := ""
	if !p.Failure.Retryable {
		confirm = "This run stopped on a " + strings.ToLower(processingClassLabel(p.Failure.Class)) +
			" problem. Retrying before fixing the cause will likely fail the same way. Retry anyway?"
	}

	switch p.Queue {
	case "object_extraction":
		if p.DocumentID != "" && p.JobID != "" {
			return processingRetryAction{
				Href:    "/documents/" + url.PathEscape(p.DocumentID) + "/extractions/" + url.PathEscape(p.JobID) + "/retry",
				Label:   "Retry",
				Confirm: confirm,
			}, true
		}
	case "document_parsing":
		if p.JobID != "" {
			return processingRetryAction{
				Href:    "/documents/conversions/" + url.PathEscape(p.JobID) + "/retry",
				Label:   "Retry",
				Confirm: confirm,
			}, true
		}
	case "source_sync":
		// The source page's own trigger, posted from here so the user does not
		// have to leave the panel to start the run.
		if p.SourceID != "" {
			return processingRetryAction{
				Href:    "/data-sources/" + url.PathEscape(p.SourceID) + "/sync",
				Label:   "Sync now",
				Confirm: confirm,
			}, true
		}
	case "discovery":
		// A failed discovery run stores its own configuration, so the panel can
		// start a fresh one from it. Without this the row was a dead end: no
		// queue-level retry, and "View" landed on the schema browser.
		if p.JobID != "" {
			return processingRetryAction{
				Href:    "/schema/discovery/" + url.PathEscape(p.JobID) + "/retry",
				Label:   "Retry discovery",
				Confirm: confirm,
			}, true
		}
	}
	return processingRetryAction{}, false
}

// processingProblemFixHref is the page where the recorded cause is fixed, when
// the product knows one. The server's own target wins: it knows whether the fix
// is "connect a provider", "select a default model" or "review the model
// settings", which the class alone cannot distinguish.
func processingProblemFixHref(p ProcessingProblem) string {
	if p.Failure.Fix != nil && p.Failure.Fix.Href != "" {
		return p.Failure.Fix.Href
	}
	switch p.Failure.Class {
	case "configuration":
		return "/settings/providers"
	case "permission":
		if p.Queue == "source_sync" && p.SourceID != "" {
			return "/data-sources/" + url.PathEscape(p.SourceID)
		}
	}
	return ""
}

// processingProblemFixLabel is the link text for processingProblemFixHref.
func processingProblemFixLabel(p ProcessingProblem) string {
	if p.Failure.Fix != nil && p.Failure.Fix.Label != "" {
		return p.Failure.Fix.Label
	}
	if p.Failure.Class == "configuration" {
		return "Configure model"
	}
	return "Reconnect source"
}

// processingProblemCanTestModel reports whether the row can offer a live model
// test. It needs a model the server actually resolved, a class a test can
// settle (configuration and permission), and a caller allowed to exercise the
// project's provider credentials.
func processingProblemCanTestModel(p ProcessingProblem, canManage bool) bool {
	if !canManage || p.Failure.Model == "" {
		return false
	}
	switch p.Failure.Class {
	case "configuration", "permission":
		return true
	}
	return false
}

// processingProblemModelLabel is the model chip's text. The server only sets
// Model when it resolved one from the project's configuration.
func processingProblemModelLabel(p ProcessingProblem) string {
	return p.Failure.Model
}

// processingProblemTestModelType is the model family a row's live test must
// exercise. The embedding queues resolve their own model, so testing one through
// the generative field would test something the failure is not about.
func processingProblemTestModelType(p ProcessingProblem) string {
	switch p.Queue {
	case "chunk_embedding", "graph_embedding", "graph_relationship_embedding":
		return "embedding"
	default:
		return "generative"
	}
}

// processingProblemTestModelField is the form field the providers test endpoint
// reads for that type, so the row posts the value the endpoint expects.
func processingProblemTestModelField(p ProcessingProblem) string {
	return defaultModelTestFields[processingProblemTestModelType(p)]
}

// processingAreaHasFixableFailures reports whether an area holds a terminal
// failure whose cause the user can fix but that the classifier marks
// non-retryable. It gates the explicit "retry all after fixing the cause"
// button, which is the only path that re-runs those units in bulk.
func processingAreaHasFixableFailures(area ProcessingArea, queue string) bool {
	for _, p := range area.Problems {
		if p.Queue != queue || p.Failure.Retryable {
			continue
		}
		switch p.Failure.Class {
		case "configuration", "permission":
			return true
		}
	}
	return false
}

// processingAreaHasFixableExtractions is the extraction case of
// processingAreaHasFixableFailures.
func processingAreaHasFixableExtractions(area ProcessingArea) bool {
	return processingAreaHasFixableFailures(area, "object_extraction")
}

// processingAreaHasFixableParsing is the conversion case of
// processingAreaHasFixableFailures.
func processingAreaHasFixableParsing(area ProcessingArea) bool {
	return processingAreaHasFixableFailures(area, "document_parsing")
}

// processingAreaHasFixableEmbeddings reports whether an area holds an embedding
// failure whose cause the user can fix but that the classifier marks
// non-retryable — the "no embedding model configured" case, which is what a
// project with no provider looks like after an import.
//
// It keys on the action kind rather than the queue, because the project
// retrigger covers exactly the units that advertise `retry_embeddings`: object
// and chunk embedding jobs reset together, while relationship embeddings
// (action `open`) have no project-wide reset and must not be implied by a
// button. The retrigger filters retryable failures by default, so the action
// posts `include_non_retryable=1` — the same explicit opt-in the extraction
// after-fixing action uses.
func processingAreaHasFixableEmbeddings(area ProcessingArea) bool {
	for _, p := range area.Problems {
		if p.Action.Kind != "retry_embeddings" || p.Failure.Retryable {
			continue
		}
		switch p.Failure.Class {
		case "configuration", "permission":
			return true
		}
	}
	return false
}

// attentionAreaPattern matches an area name as the processing-problems read
// model spells it. The gateway deliberately keeps no list of areas: the server
// owns them, and a hardcoded copy would silently stop expanding the day a queue
// is added — the same "the link does nothing" shape this panel was fixed for.
// The value is only ever compared with a report's area names and placed in a
// query string, so a shape check is enough; anything else is ignored.
var attentionAreaPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// normalizeAttentionArea coerces the `attention` page parameter to a shape an
// area name can have. Anything else means "no expansion", so a stale or
// hand-edited bookmark just shows the default clipped panel.
func normalizeAttentionArea(raw string) string {
	area := strings.ToLower(strings.TrimSpace(raw))
	if !attentionAreaPattern.MatchString(area) {
		return ""
	}
	return area
}

// attentionAnchor keeps the reader at the panel after a full-list toggle, so
// opening an area does not drop them at the top of a long page.
const attentionAnchor = "#needs-attention"

// attentionHref is the panel link that opens one area's full list. It is the
// panel itself, expanded, on the page's current filters.
//
// It used to hand the reader to a documents-list filter (/documents?status=failed).
// That filter selects by a document's derived processing status, which cannot
// represent a failed chunk-embedding run or a conversion job that never got a
// document — two of the three queues the documents area aggregates. A reader of
// a non-zero area therefore landed on "No documents match". The panel is the
// only surface that lists these runs, so the link opens it instead of a
// different population.
func (d documentPageData) attentionHref(area string) string {
	return d.requestURL(map[string]string{"attention": area}) + attentionAnchor
}

// attentionCollapseHref is the inverse of attentionHref: the same page state
// without an expanded area, so "Show fewer" keeps the search and filters.
func (d documentPageData) attentionCollapseHref() string {
	return d.requestURL(map[string]string{"attention": ""}) + attentionAnchor
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

// redirectScoped sends one of the panel's PRG responses with a target that
// already names the caller's scope, and mirrors that target into HX-Redirect
// for the boosted forms.
//
// scopeLocation scopes a handler's Location/HX-Redirect before the response
// commits: it registers the rewrite with Response.Before, which runs inside
// WriteHeader ahead of the writer (#2479 — mutating the map after the handler
// returned never reached the wire, so production answered a scoped,
// htmx-boosted POST with 'location: /documents?extractions_retried=5' and
// dropped the user on a 404 after their retry had actually started).
//
// Naming the target here keeps the retry path correct on its own — this helper
// predates the middleware fix, and it still makes the outcome independent of
// whether a base is derivable at commit time (a device session has no session
// scope to fall back on). An already-scoped target passes scopedPathFor
// untouched, so nothing double-prefixes.
//
// Other handlers may keep emitting context-free PRG targets: the middleware now
// scopes them wherever it can derive a base (the request's URL scope, or the
// session's active project for a target the hard cut would otherwise 404).

func (s *Server) redirectScoped(c echo.Context, path string) error {
	target := s.prgScoped(c, path)
	if c.Request().Header.Get("HX-Request") == "true" {
		c.Response().Header().Set("HX-Redirect", target)
	}
	return c.Redirect(http.StatusSeeOther, target)
}

// redirectScopedError is redirectScoped carrying the ?err= flash the gateway's
// forms read back.
func (s *Server) redirectScopedError(c echo.Context, path string, err error) error {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return s.redirectScoped(c, path+sep+"err="+url.QueryEscape(err.Error()))
}

// uiRetryExtraction handles the panel/history Retry button. It always starts a
// new run; the failed job remains visible in history.
func (s *Server) uiRetryExtraction(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	jobID := c.Param("jobId")
	if docID == "" || jobID == "" {
		return s.redirectScopedError(c, "/documents", errDocumentIDRequired)
	}

	if err := s.memory.RetryExtractionJob(ctx, jobID); err != nil {
		return s.redirectScopedError(c, "/documents/"+url.PathEscape(docID)+"/extractions", err)
	}
	s.invalidateProcessingProblems(ctx)
	return s.redirectScoped(c, "/documents/"+url.PathEscape(docID)+"/extractions?retried=1")
}

// uiRetryAllExtractions handles the panel's project-wide retry. The
// include_non_retryable form field is the explicit "cause is fixed" override;
// without it the server skips configuration/permission failures.
func (s *Server) uiRetryAllExtractions(c echo.Context) error {
	ctx := c.Request().Context()
	n, err := s.memory.RetryAllExtractionJobs(ctx, includeNonRetryableForm(c))
	if err != nil {
		return s.redirectScopedError(c, "/documents", err)
	}
	s.invalidateProcessingProblems(ctx)
	if n == 0 {
		return s.redirectScoped(c, "/documents?extractions_none=1")
	}
	return s.redirectScoped(c, "/documents?extractions_retried="+strconv.Itoa(n))
}

// uiRetryAllConversions handles the panel's project-wide conversion retry. The
// include_non_retryable field has the same explicit-override meaning as above.
func (s *Server) uiRetryAllConversions(c echo.Context) error {
	ctx := c.Request().Context()
	n, err := s.memory.RetryAllParsingJobs(ctx, includeNonRetryableForm(c))
	if err != nil {
		return s.redirectScopedError(c, "/documents", err)
	}
	s.invalidateProcessingProblems(ctx)
	if n == 0 {
		return s.redirectScoped(c, "/documents?conversions_none=1")
	}
	return s.redirectScoped(c, "/documents?conversions_retried="+strconv.Itoa(n))
}

// uiRetryConversion handles the per-problem Retry for a failed document
// conversion. The failed attempt is reset in place (a conversion is
// deterministic work, not a new model run), and the user lands back on the
// document when the panel knows which one it belongs to.
func (s *Server) uiRetryConversion(c echo.Context) error {
	ctx := c.Request().Context()
	jobID := c.Param("jobId")
	if jobID == "" {
		return s.redirectScopedError(c, "/documents", errDocumentIDRequired)
	}
	dest := "/documents"
	if docID := c.FormValue("document_id"); docID != "" {
		dest = "/documents/" + url.PathEscape(docID)
	}
	if err := s.memory.RetryParsingJob(ctx, jobID); err != nil {
		return s.redirectScopedError(c, dest, err)
	}
	s.invalidateProcessingProblems(ctx)
	return s.redirectScoped(c, dest+"?conversion_retried=1")
}

// includeNonRetryableForm reports whether the submitting panel button asked to
// re-run failures the classifier marks non-retryable (the "after fixing the
// cause" variant). Only an explicit truthy value opts in.
func includeNonRetryableForm(c echo.Context) bool {
	switch strings.ToLower(strings.TrimSpace(c.FormValue("include_non_retryable"))) {
	case "1", "true", "yes":
		return true
	}
	// The panel's forms carry the flag in the query string so the confirm text
	// and the request stay one URL.
	switch strings.ToLower(strings.TrimSpace(c.QueryParam("include_non_retryable"))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// uiRetriggerEmbeddings handles the area's project-wide embedding retry. The
// include_non_retryable query the after-fixing form carries is forwarded to the
// server, which filters retryable failures by default; dropping it here leaves
// the button resetting nothing on a configuration-only area.
func (s *Server) uiRetriggerEmbeddings(c echo.Context) error {
	ctx := c.Request().Context()
	n, err := s.memory.RetriggerEmbeddings(ctx, includeNonRetryableForm(c))
	if err != nil {
		return s.redirectScopedError(c, "/documents", err)
	}
	s.invalidateProcessingProblems(ctx)
	if n == 0 {
		return s.redirectScoped(c, "/documents?embeddings_none=1")
	}
	return s.redirectScoped(c, "/documents?embeddings_retried="+strconv.Itoa(n))
}
