package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

// --- document preview ---

// Original-file preview strategies, selected from a document's media type.
const (
	previewModePDF      = "pdf"
	previewModeImage    = "image"
	previewModeText     = "text"
	previewModeDownload = "download"
)

// maxTextPreviewChars caps the server-side text/markdown preview so a large
// document cannot bloat the overview response.
const maxTextPreviewChars = 20000

// documentPreview is the resolved preview shown on the document overview. Mode
// picks the rendering strategy; Text carries a capped body for text/markdown;
// Err records a failed server-side read so the card degrades to a note instead
// of failing the whole page.
type documentPreview struct {
	Mode      string
	Text      string
	Truncated bool
	Err       error
}

// documentFilePath is the gateway URL that streams a document's original bytes
// inline.
func documentFilePath(id string) string {
	return "/documents/" + url.PathEscape(id) + "/file"
}

// documentExtractionsPath is the document's extraction-history URL.
func documentExtractionsPath(id string) string {
	return "/documents/" + url.PathEscape(id) + "/extractions"
}

// previewModeFor classifies a document into a preview strategy. Text/markdown
// (by MIME or by a .md/.markdown filename) is read server-side; PDFs, images,
// and everything else stream through the file proxy — the latter as a download
// link.
func previewModeFor(doc *Document) string {
	mime := strings.ToLower(doc.MimeType)
	switch {
	case mime == "application/pdf":
		return previewModePDF
	case strings.HasPrefix(mime, "image/"):
		return previewModeImage
	case strings.HasPrefix(mime, "text/"),
		mime == "application/markdown",
		mime == "application/x-markdown",
		isMarkdownFilename(doc.Filename):
		return previewModeText
	default:
		return previewModeDownload
	}
}

func isMarkdownFilename(name string) bool {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "md", "markdown":
		return true
	default:
		return false
	}
}

// errEmptyDocumentContent degrades a text preview whose content endpoint
// returned nothing, so the card shows the unavailable note instead of a blank
// box.
var errEmptyDocumentContent = errors.New("document has no stored text content to preview")

// loadDocumentPreview resolves the overview preview for doc. Only text/markdown
// content is read server-side (and capped); a read failure or empty body
// degrades the card. PDFs and images need no pre-fetch — they stream through
// the file proxy. Text comes from the stored-content endpoint, which also
// serves raw-content documents that have no original file to download.
func (s *Server) loadDocumentPreview(ctx context.Context, doc *Document) documentPreview {
	p := documentPreview{Mode: previewModeFor(doc)}
	if p.Mode != previewModeText {
		return p
	}
	content, err := s.memory.GetDocumentContent(ctx, doc.ID)
	if err != nil {
		p.Err = err
		return p
	}
	if content == "" {
		p.Err = errEmptyDocumentContent
		return p
	}
	if runes := []rune(content); len(runes) > maxTextPreviewChars {
		content = string(runes[:maxTextPreviewChars])
		p.Truncated = true
	}
	p.Text = content
	return p
}

// --- extraction history ---

// extractionJobDetail is the lazily-loaded per-job payload for the Extraction
// history view: the job's staging branch (when resolvable), the objects and
// relationships it created, and the branch's merge readiness.
type extractionJobDetail struct {
	Job           ExtractionJob
	Branch        *Branch
	BranchID      string
	Objects       []GraphObject
	Relationships []GraphRelationship
	Readiness     *MergeReadiness
	NoBranch      bool
	// LeftForReview is the count of objects left on the branch after a partial
	// merge (carried on the lazy-detail URL as ?review=N). Zero when unknown or
	// nothing is outstanding.
	LeftForReview int
	// LeftForReviewObjects names the specific objects a partial merge left on
	// the branch (resolution "skipped" or "suggested"), resolved to their
	// display labels. Non-nil only when LeftForReview > 0 and the branch is
	// still open, so the list — not just a count — is visible after a merge
	// (#242).
	LeftForReviewObjects []mergeReviewItem
	Err                  error
}

// mergeReviewHint carries post-merge leftover state to the extraction history
// page: which job still has items on its branch for review.
type mergeReviewHint struct {
	JobID string
	Left  int
}

// findExtractionJob resolves one job of a document from the document's job
// list.
func (s *Server) findExtractionJob(ctx context.Context, documentID, jobID string) (*ExtractionJob, error) {
	jobs, err := s.memory.ListExtractionJobs(ctx, documentID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(jobs, func(j ExtractionJob) bool { return j.ID == jobID })
	if i < 0 {
		return nil, fmt.Errorf("extraction job %s not found", shortID(jobID))
	}
	return &jobs[i], nil
}

// extractionJobBranch resolves the staging branch a job wrote to. ok is false
// when the job produced nothing or the branch can no longer be resolved. The
// branch record is attached whenever the id is still listed, so the detail can
// render its name, readiness, and merge action.
func (s *Server) extractionJobBranch(ctx context.Context, job *ExtractionJob) (string, *Branch, bool) {
	// ListBranches is fetched at most once per resolution and reused by both
	// the modern (staging_branch_id) and legacy (first created object) paths.
	var branches []Branch
	if list, err := s.memory.ListBranches(ctx); err != nil {
		captureError(err)
	} else {
		branches = list
	}
	branchByID := func(id string) *Branch {
		if i := slices.IndexFunc(branches, func(b Branch) bool { return b.ID == id }); i >= 0 {
			return &branches[i]
		}
		return nil
	}

	if job.StagingBranchID != "" {
		if b := branchByID(job.StagingBranchID); b != nil {
			return b.ID, b, true
		}
		// The staging branch no longer exists (deleted on merge); fall through
		// to the legacy object fallback.
	}
	ids := job.ObjectIDs()
	if len(ids) == 0 {
		return "", nil, false
	}
	obj, err := s.memory.GetGraphObject(ctx, ids[0])
	if err != nil || obj == nil || obj.BranchID == "" {
		captureError(err)
		return "", nil, false
	}
	return obj.BranchID, branchByID(obj.BranchID), true
}

// loadExtractionJobDetail assembles the per-job detail. Every step degrades
// gracefully: a failed object/relationship/readiness fetch keeps the counts
// already gathered rather than failing the partial.
func (s *Server) loadExtractionJobDetail(ctx context.Context, documentID, jobID string) extractionJobDetail {
	job, err := s.findExtractionJob(ctx, documentID, jobID)
	if err != nil {
		return extractionJobDetail{Err: err}
	}
	return s.assembleExtractionJobDetail(ctx, *job)
}

// resolveExtractionJob fetches a job's rich DTO (trace spans, cost, error
// details, retry count) via the single-job endpoint, falling back to the list
// DTO when that endpoint is absent (a server predating the trace fields). The
// fallback job carries no spans or cost, which the page degrades accordingly.
func (s *Server) resolveExtractionJob(ctx context.Context, documentID, jobID string) (*ExtractionJob, error) {
	job, err := s.memory.GetExtractionJob(ctx, jobID)
	if err == nil {
		return job, nil
	}
	return s.findExtractionJob(ctx, documentID, jobID)
}

// loadExtractionJobPage assembles the extraction-run sub-page detail: the rich
// job DTO (spans, trace id, cost, error details, retry count) plus the
// resolved objects/relationships/branch/readiness. It shares the branch/
// object/relationship assembly with the history page's lazy partial.
func (s *Server) loadExtractionJobPage(ctx context.Context, documentID, jobID string) extractionJobDetail {
	job, err := s.resolveExtractionJob(ctx, documentID, jobID)
	if err != nil {
		return extractionJobDetail{Err: err}
	}
	return s.assembleExtractionJobDetail(ctx, *job)
}

// assembleExtractionJobDetail resolves a job's staging branch, created objects,
// relationships, and merge readiness around an already-fetched job DTO. Shared
// by the history lazy-detail (list DTO) and the extraction-run page (rich DTO).
func (s *Server) assembleExtractionJobDetail(ctx context.Context, job ExtractionJob) extractionJobDetail {
	var d extractionJobDetail
	d.Job = job

	ids := job.ObjectIDs()
	branchID, branch, ok := s.extractionJobBranch(ctx, &job)
	if !ok {
		// The staging branch can no longer be resolved (it produced no objects,
		// or was deleted on merge). Fall back to listing the job's created
		// objects on the main branch so the object/relationship counts survive
		// a merge instead of showing 0.
		d.NoBranch = true
		if len(ids) == 0 {
			return d
		}
		objects, err := s.memory.ListGraphObjects(ctx, "", "", ids)
		if err != nil {
			captureError(err)
			return d
		}
		d.Objects = objects
		d.Relationships = s.collectExtractionRelationships(ctx, "", objects)
		return d
	}
	d.BranchID = branchID
	d.Branch = branch

	// Guard the empty id set: ListGraphObjects with no ids would list the whole
	// branch, showing objects this job never created.
	if len(ids) == 0 {
		return d
	}
	objects, err := s.memory.ListGraphObjects(ctx, branchID, "", ids)
	if err != nil {
		d.Err = err
		return d
	}
	d.Objects = objects
	d.Relationships = s.collectExtractionRelationships(ctx, branchID, objects)

	// Readiness only answers "can this merge now?" — a merged branch has
	// nothing to merge, so skip the extra request and leave it nil.
	if branch == nil || branch.MergedAt == "" {
		if readiness, err := s.memory.GetMergeReadiness(ctx, branchID); err != nil {
			captureError(err)
		} else {
			d.Readiness = readiness
		}
	}
	return d
}

// sortExtractionJobsNewestFirst orders jobs by creation time, newest first. The
// list endpoint already returns newest-first, but the order is load-bearing for
// the view, so it is enforced here.
func sortExtractionJobsNewestFirst(jobs []ExtractionJob) {
	slices.SortStableFunc(jobs, func(a, b ExtractionJob) int {
		return strings.Compare(b.CreatedAt, a.CreatedAt)
	})
}

// extractionJobActive reports whether an extraction job is still running and
// should keep polling: pending/queued/running/processing are non-terminal.
func extractionJobActive(status string) bool {
	switch strings.ToLower(status) {
	case "pending", "queued", "running", "processing":
		return true
	default:
		return false
	}
}

// extractionDurationSeconds returns the wall-clock duration of an extraction
// job in seconds (CompletedAt − StartedAt, both RFC3339), or 0 when either is
// missing, unparseable, or completion precedes start.
func extractionDurationSeconds(startedAt, completedAt string) float64 {
	if startedAt == "" || completedAt == "" {
		return 0
	}
	start, err1 := time.Parse(time.RFC3339, startedAt)
	end, err2 := time.Parse(time.RFC3339, completedAt)
	if err1 != nil || err2 != nil || !end.After(start) {
		return 0
	}
	return end.Sub(start).Seconds()
}

// extractionTraceMeta derives the model and token totals from a job's spans for
// the summary strip, reusing runTraceMetaFromRun (first span naming a model,
// token counts summed across all spans).
func extractionTraceMeta(spans []TraceSpan) (model string, inTokens, outTokens int64) {
	m := runTraceMetaFromRun(&AgentRun{Spans: spans})
	return m.Model, m.InputTokens, m.OutputTokens
}

// formatTraceCostUSD renders a trace cost in US dollars with four decimals
// ("0.0123"). The caller gates on the pointer's presence, so a zero cost still
// renders as "0.0000".
func formatTraceCostUSD(usd float64) string {
	return strconv.FormatFloat(usd, 'f', 4, 64)
}

// prettyJSON marshals v with two-space indentation for the error-details
// disclosure, falling back to the Go fmt on a marshal failure (never an error
// page).
func prettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// --- handlers ---

// uiDocumentChunks renders the document Chunks sub-page: the shared detail
// header, the sub-navigation rail, and the existing chunks section.
func (s *Server) uiDocumentChunks(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	doc, err := s.memory.GetDocument(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Document"), DocumentChunksPage(nil, nil, userFacingError(err)))
	}
	chunks, err := s.memory.ListChunks(ctx, id)
	captureError(err)
	return s.page(c, pageTitle(documentName(*doc), "Chunks"), DocumentChunksPage(doc, chunks, nil))
}

// uiDocumentExtractions renders the Extraction history sub-page: every
// extraction job for the document, newest first, each lazily loading its
// detail. ?merged=N carries the merge-success flash; ?review=M&job=<id> carries
// the partial-merge warning and the leftover hint for that job's action.
func (s *Server) uiDocumentExtractions(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	doc, err := s.memory.GetDocument(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Document"), DocumentExtractionsPage(nil, nil, userFacingError(err), "", "", nil, nil))
	}
	jobs, err := s.memory.ListExtractionJobs(ctx, id)
	if err != nil {
		return s.page(c, pageTitle(documentName(*doc), "Extraction history"), DocumentExtractionsPage(doc, nil, userFacingError(err), "", "", nil, nil))
	}
	sortExtractionJobsNewestFirst(jobs)
	flashMsg, warnMsg, hint := mergeFlashFromQuery(c)
	if flashMsg == "" && c.QueryParam("retried") != "" {
		flashMsg = "Retry started. A new extraction run was queued; the failed attempt stays in history."
	}
	return s.page(c, pageTitle(documentName(*doc), "Extraction history"), DocumentExtractionsPage(doc, jobs, nil, flashMsg, warnMsg, flashError(c), hint))
}

// mergeFlashFromQuery decodes the PRG query left by uiDocumentMerge. It returns
// the success flash, the warning flash (partial merge), and the leftover hint
// for the merged job's action.
func mergeFlashFromQuery(c echo.Context) (flashMsg, warnMsg string, hint *mergeReviewHint) {
	merged := strings.TrimSpace(c.QueryParam("merged"))
	review := strings.TrimSpace(c.QueryParam("review"))
	jobID := strings.TrimSpace(c.QueryParam("job"))
	if review != "" {
		n := merged
		if n == "" {
			n = "0"
		}
		warnMsg = fmt.Sprintf("Merged %s object(s); %s left for review — the branch stays open.", n, review)
		hint = &mergeReviewHint{JobID: jobID, Left: atoiOrZero(review)}
		return "", warnMsg, hint
	}
	if merged != "" {
		flashMsg = fmt.Sprintf("Merged %s object(s) into main.", merged)
	}
	return flashMsg, "", nil
}

// atoiOrZero parses a non-negative integer, returning 0 on any failure.
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// uiDocumentExtractionDetail renders the HTMX partial for one extraction job:
// the objects and relationships that run produced on its staging branch, its
// branch name and merge readiness, and the merge action when available.
func (s *Server) uiDocumentExtractionDetail(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	detail := s.loadExtractionJobDetail(ctx, docID, c.Param("jobId"))
	// A partial merge redirects back to the history page with ?review=N&job=<id>;
	// the lazy-detail URL threads that count here so the merged job's action can
	// show how many objects are still left on the branch for review.
	detail.LeftForReview = atoiOrZero(c.QueryParam("review"))
	// List the specific objects left for review, not just the count. The branch
	// stays open after a partial merge, so re-running the dry-run preview yields
	// the objects the merge did not apply, resolved against the branch's objects.
	if detail.LeftForReview > 0 && detail.BranchID != "" && detail.Branch != nil && detail.Branch.MergedAt == "" {
		detail.LeftForReviewObjects = s.loadLeftForReviewObjects(ctx, detail.BranchID, detail.Objects)
	}
	renderPartial(c.Response().Writer, c.Request(), ExtractionJobDetail(docID, detail))
	return nil
}

// uiDocumentExtractionPage renders the dedicated extraction-run sub-page: the
// run's summary strip, its OpenTelemetry trace waterfall, its objects/
// relationships/branch/merge block, and its error details. The trace fragment
// polls itself while the run is active.
func (s *Server) uiDocumentExtractionPage(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	doc, err := s.memory.GetDocument(ctx, docID)
	if err != nil {
		return s.page(c, pageTitle("Extraction run"), DocumentExtractionPage(nil, extractionJobDetail{}, userFacingError(err)))
	}
	detail := s.loadExtractionJobPage(ctx, docID, c.Param("jobId"))
	// detail.Err is the job-fetch error (nil once the job resolved, even if its
	// branch/object sub-detail degraded); it surfaces as the whole-page error.
	return s.page(c, pageTitle(documentName(*doc), "Extraction run"), DocumentExtractionPage(doc, detail, detail.Err))
}

// uiDocumentExtractionTrace renders the polling fragment for one extraction
// run: the summary strip, the trace panel, and an out-of-band refresh of the
// run's status badge, so a job that finishes while the user is on the page gets
// its final duration, token counts, and estimated cost without a reload. The
// wrapper polls itself only while the job is active; once terminal it carries
// no polling attributes and the interval self-terminates.
func (s *Server) uiDocumentExtractionTrace(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	jobID := c.Param("jobId")
	detail := s.loadExtractionJobPage(ctx, docID, jobID)
	if detail.Err != nil {
		// A transient fetch failure must not erase the last-known summary or
		// status badge. HX-Reswap: none tells htmx to perform no swap at all,
		// so the previously rendered polling root (with its hx-get/hx-trigger)
		// stays in the DOM and the interval keeps retrying, while this response
		// remains a valid 200 fragment. No OOB status group is emitted (it
		// would blank the header badge) and no zeroed summary strip is
		// rendered. renderPartial sets no swap header itself, so this header is
		// the only swap signal htmx sees.
		captureError(detail.Err)
		c.Response().Header().Set("HX-Reswap", "none")
		renderPartial(c.Response().Writer, c.Request(), templ.ComponentFunc(func(context.Context, io.Writer) error { return nil }))
		return nil
	}
	poll := extractionJobActive(detail.Job.Status)
	renderPartial(c.Response().Writer, c.Request(), extractionRunBody(docID, jobID, detail, poll, true))
	return nil
}

// inlinePreviewMediaType reports whether a stored document's media type is safe
// to render inline in the browser. PDFs and non-SVG images are; image/svg+xml
// is excluded (SVG can carry script), and every other type — notably
// text/html — is not.
func inlinePreviewMediaType(contentType string) bool {
	mimeType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch {
	case mimeType == "application/pdf":
		return true
	case strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml":
		return true
	default:
		return false
	}
}

// uiDocumentFile proxies a document's original file bytes, inline for
// previewable media types and as an attachment otherwise. The bytes are
// attacker-controlled and their Content-Type is stored as supplied at upload,
// so only the inline allowlist may render in the browser: streaming text/html
// or image/svg+xml inline would execute script in the gateway origin (a stored
// XSS). Everything else is forced to download as a neutral type, with nosniff
// so the browser cannot sniff its way around that. A backend 404 (missing file)
// maps to a gateway 404; any other failure to a 502.
func (s *Server) uiDocumentFile(c echo.Context) error {
	body, contentType, err := s.memory.DownloadDocument(c.Request().Context(), c.Param("id"))
	if err != nil {
		if isMemoryNotFound(err) {
			return c.NoContent(http.StatusNotFound)
		}
		return c.NoContent(http.StatusBadGateway)
	}
	defer func() { _ = body.Close() }()

	if inlinePreviewMediaType(contentType) {
		c.Response().Header().Set(echo.HeaderContentType, contentType)
		c.Response().Header().Set(echo.HeaderContentDisposition, "inline")
	} else {
		c.Response().Header().Set(echo.HeaderContentType, "application/octet-stream")
		c.Response().Header().Set(echo.HeaderContentDisposition, "attachment")
	}
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := io.Copy(c.Response(), body); err != nil {
		return err
	}
	return nil
}

// uiDocumentMerge merges the staging branch of one extraction job into main
// with the fixed "partial" policy, then PRG-redirects to the history page with
// a truthful result query (?merged=N, plus ?review=M&job=<id> when the merge was
// partial). A job without a resolvable branch, or a merge failure, redirects
// with the real error.
func (s *Server) uiDocumentMerge(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	jobID := c.Param("jobId")
	historyPath := documentExtractionsPath(docID)

	res, _, err := s.mergeExtractionBranch(ctx, docID, jobID)
	if err != nil {
		return redirectWithError(c, historyPath, err)
	}
	redirectAfterMutation(c, historyPath+mergeResultQuery(res, jobID))
	return nil
}

// mergeExtractionBranch resolves the job's staging branch and merges it into
// main with the partial policy, returning the server's truthful result counts
// and the merged branch id (for resolving its review-ready notifications).
// Shared by uiDocumentMerge and uiInboxMerge so both report the same split.
func (s *Server) mergeExtractionBranch(ctx context.Context, docID, jobID string) (*BranchMergeResult, string, error) {
	job, err := s.findExtractionJob(ctx, docID, jobID)
	if err != nil {
		return nil, "", err
	}
	branchID, _, ok := s.extractionJobBranch(ctx, job)
	if !ok {
		return nil, "", fmt.Errorf("merge: this extraction has no staging branch to merge")
	}
	res, err := s.memory.MergeBranch(ctx, branchID, "partial")
	return res, branchID, err
}

// mergeResultQuery encodes a merge result into the PRG query the history page
// decodes: always ?merged=<mergedCount>; when the merge left anything for
// review (?review=<left>), it also carries ?job=<id> so the merged job's action
// can show the leftover count. left is max(left_for_review, conflict): either
// way the branch stays open.
func mergeResultQuery(res *BranchMergeResult, jobID string) string {
	if res == nil {
		return "?merged=0"
	}
	q := url.Values{"merged": {strconv.Itoa(mergeAppliedObjects(res))}}
	left := res.LeftForReviewCount
	if res.ConflictCount > left {
		left = res.ConflictCount
	}
	if left > 0 {
		q.Set("review", strconv.Itoa(left))
		if jobID != "" {
			q.Set("job", jobID)
		}
	}
	return "?" + q.Encode()
}

// mergeAppliedObjects returns how many source objects a merge applies to main
// (0 for a nil result): new objects plus fast-forwards, the only statuses the
// "partial" policy actually applies — similar objects are suggested for review
// and conflicts are preserved on the target. It is used both to predict the
// merge in the confirm dialog and to report what a completed merge did.
//
// It must NOT read MergedCount: that field counts source objects cloned to the
// target by a PRIOR merge (the merge ledger), so it is 0 on a first merge —
// which disabled the confirm button forever — and stale on a re-preview.
func mergeAppliedObjects(res *BranchMergeResult) int {
	if res == nil {
		return 0
	}
	return res.AddedCount + res.FastForwardCount
}

// mergePreviewAdded returns how many new objects a preview predicts will be
// added to main (0 for a nil result).
func mergePreviewAdded(res *BranchMergeResult) int {
	if res == nil {
		return 0
	}
	return res.AddedCount
}

// mergePreviewUpdated returns how many existing objects a preview predicts will
// be updated: the fast-forward count (0 for a nil result), which already
// includes deleted→fast-forward objects (the server counts "deleted" as
// fast_forward when it builds the response).
//
// It must NOT add SimilarCount. The preview requests the "partial" preset,
// which resolveMergePolicy resolves to similarity="suggest" and
// conflicts="preserve_target": applyMerge skips similar objects and leaves them
// on the branch, so they are already counted by mergePreviewLeft. Adding them
// here listed the same objects as both "updated" and "left for review", and
// contradicted the confirm label (which counts only what is applied).
func mergePreviewUpdated(res *BranchMergeResult) int {
	if res == nil {
		return 0
	}
	return res.FastForwardCount
}

// mergePreviewConflicts returns how many conflicting objects a preview predicts
// stay unresolved on the branch (0 for a nil result).
func mergePreviewConflicts(res *BranchMergeResult) int {
	if res == nil {
		return 0
	}
	return res.ConflictCount
}

// mergePreviewLeft returns how many objects a preview predicts stay on the
// branch for review: max(left_for_review, conflict). 0 for a nil result.
func mergePreviewLeft(res *BranchMergeResult) int {
	if res == nil {
		return 0
	}
	left := res.LeftForReviewCount
	if res.ConflictCount > left {
		left = res.ConflictCount
	}
	return left
}

// mergePreviewTotal renders the branch's total object count for the preview
// summary (0 for a nil result).
func mergePreviewTotal(res *BranchMergeResult) string {
	if res == nil {
		return "0"
	}
	return strconv.Itoa(res.TotalObjects)
}

// mergePreviewDisabled reports whether the preview's confirm button should be
// disabled: when nothing would merge cleanly there is no merge to run.
func mergePreviewDisabled(res *BranchMergeResult) bool {
	return mergeAppliedObjects(res) <= 0
}

// mergePreviewConfirmLabel is the preview's confirm button copy, naming the
// clean-merge count so the action reads truthfully ("Merge 3 objects into
// main"). A zero count still shows a neutral label (the button is disabled).
func mergePreviewConfirmLabel(res *BranchMergeResult) string {
	n := mergeAppliedObjects(res)
	if n <= 0 {
		return "Merge into main"
	}
	return "Merge " + strconv.Itoa(n) + " object" + plural(n) + " into main"
}

// mergeReviewItem is one object a merge left (or would leave) on the source
// branch for review, decorated with its resolved display label and any
// similar/conflict target so the UI can name what needs attention instead of
// showing only a count.
type mergeReviewItem struct {
	ObjectID      string // version id for the object-detail deep-link
	Label         string // resolved display label (key, else type + short id)
	Type          string
	Resolution    string // "skipped" (conflict) | "suggested" (similar)
	SimilarTarget string // display name of the matched similar object, when any
	Conflicts     string // comma-joined conflicting keys, when any
}

// mergeReviewItems resolves a merge's left-for-review entries to display items,
// joining each entry with the graph object (matched by canonical id, or version
// id as a fallback) so the list reads like the extraction object list. The
// resolved objects come from the caller's already-fetched branch object list.
func mergeReviewItems(res *BranchMergeResult, objects []GraphObject) []mergeReviewItem {
	if res == nil {
		return nil
	}
	left := res.LeftForReviewObjects()
	if len(left) == 0 {
		return nil
	}
	byCanonical := make(map[string]GraphObject, len(objects))
	for _, o := range objects {
		if o.CanonicalID != "" {
			byCanonical[o.CanonicalID] = o
		}
		byCanonical[o.ID] = o
	}
	items := make([]mergeReviewItem, 0, len(left))
	for _, entry := range left {
		item := mergeReviewItem{
			ObjectID:      entry.CanonicalID,
			Resolution:    entry.Resolution,
			SimilarTarget: entry.SimilarTargetName,
			Conflicts:     strings.Join(entry.Conflicts, ", "),
		}
		if o, ok := byCanonical[entry.CanonicalID]; ok {
			item.ObjectID = o.ID
			item.Label = graphObjectLabel(o)
			item.Type = o.Type
		} else {
			item.Label = shortID(entry.CanonicalID)
		}
		items = append(items, item)
	}
	return items
}

// loadLeftForReviewObjects re-derives the objects a partial merge left on an
// open branch by re-running a dry-run preview and resolving their labels from
// the branch's already-fetched object list. A preview failure degrades to nil
// (the count-only badge remains). Used by the extraction detail after a merge
// redirect so the specific items — not just the count — are listed (#242).
func (s *Server) loadLeftForReviewObjects(ctx context.Context, branchID string, objects []GraphObject) []mergeReviewItem {
	res, err := s.memory.PreviewMergeBranch(ctx, branchID, "partial")
	if err != nil {
		captureError(err)
		return nil
	}
	return mergeReviewItems(res, objects)
}

// resolveMergePreviewItems decorates a merge preview's left-for-review entries
// with their display labels by fetching the source branch's objects. It backs
// the merge preview modal, which — unlike the extraction detail — has no
// already-fetched object list to resolve against.
func (s *Server) resolveMergePreviewItems(ctx context.Context, branchID string, res *BranchMergeResult) []mergeReviewItem {
	if res == nil || len(res.LeftForReviewObjects()) == 0 {
		return nil
	}
	objects, err := s.memory.ListGraphObjects(ctx, branchID, "", nil)
	if err != nil {
		captureError(err)
		return nil
	}
	return mergeReviewItems(res, objects)
}

// uiDocumentMergePreview renders the merge preview modal for one extraction job:
// the server's predicted split (added / merged / left-for-review) before the
// user confirms. It is fetched lazily via HTMX and auto-opens once swapped in.
// A job without a resolvable branch, or a preview failure, renders the modal in
// its error state so the user still gets an actionable explanation.
func (s *Server) uiDocumentMergePreview(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	jobID := c.Param("jobId")
	actionURL := "/documents/" + url.PathEscape(docID) + "/extractions/" + url.PathEscape(jobID) + "/merge"

	job, err := s.findExtractionJob(ctx, docID, jobID)
	if err != nil {
		renderPartial(c.Response().Writer, c.Request(), mergePreviewModal(actionURL, nil, nil, userFacingError(err)))
		return nil
	}
	branchID, _, ok := s.extractionJobBranch(ctx, job)
	if !ok {
		renderPartial(c.Response().Writer, c.Request(), mergePreviewModal(actionURL, nil, nil, fmt.Errorf("this extraction has no staging branch to merge")))
		return nil
	}
	res, err := s.memory.PreviewMergeBranch(ctx, branchID, "partial")
	if err != nil {
		renderPartial(c.Response().Writer, c.Request(), mergePreviewModal(actionURL, nil, nil, userFacingError(err)))
		return nil
	}
	renderPartial(c.Response().Writer, c.Request(), mergePreviewModal(actionURL, res, s.resolveMergePreviewItems(ctx, branchID, res), nil))
	return nil
}
