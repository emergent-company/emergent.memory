package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/emergent-company/go-daisy/render"
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
	Err           error
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

// extractionJobBranchID derives the staging branch a job wrote to from its
// first created object. ok is false when the job produced nothing or the object
// can no longer be resolved.
func (s *Server) extractionJobBranchID(ctx context.Context, job *ExtractionJob) (string, bool) {
	if len(job.CreatedObjects) == 0 {
		return "", false
	}
	obj, err := s.memory.GetGraphObject(ctx, job.CreatedObjects[0])
	if err != nil || obj == nil || obj.BranchID == "" {
		captureError(err)
		return "", false
	}
	return obj.BranchID, true
}

// loadExtractionJobDetail assembles the per-job detail. Every step degrades
// gracefully: a failed object/relationship/readiness fetch keeps the counts
// already gathered rather than failing the partial.
func (s *Server) loadExtractionJobDetail(ctx context.Context, documentID, jobID string) extractionJobDetail {
	var d extractionJobDetail
	job, err := s.findExtractionJob(ctx, documentID, jobID)
	if err != nil {
		d.Err = err
		return d
	}
	d.Job = *job

	branchID, ok := s.extractionJobBranchID(ctx, job)
	if !ok {
		d.NoBranch = true
		return d
	}
	d.BranchID = branchID

	objects, err := s.memory.ListGraphObjects(ctx, branchID, "", job.CreatedObjects)
	if err != nil {
		d.Err = err
		return d
	}
	d.Objects = objects
	d.Relationships = s.collectExtractionRelationships(ctx, branchID, objects)

	if branches, err := s.memory.ListBranches(ctx); err != nil {
		captureError(err)
	} else if i := slices.IndexFunc(branches, func(b Branch) bool { return b.ID == branchID }); i >= 0 {
		d.Branch = &branches[i]
	}
	if readiness, err := s.memory.GetMergeReadiness(ctx, branchID); err != nil {
		captureError(err)
	} else {
		d.Readiness = readiness
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

// --- handlers ---

// uiDocumentChunks renders the document Chunks sub-page: the shared detail
// header, the sub-navigation rail, and the existing chunks section.
func (s *Server) uiDocumentChunks(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	doc, err := s.memory.GetDocument(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Document"), DocumentChunksPage(nil, nil, err))
	}
	chunks, err := s.memory.ListChunks(ctx, id)
	captureError(err)
	return s.page(c, pageTitle(documentName(*doc), "Chunks"), DocumentChunksPage(doc, chunks, nil))
}

// uiDocumentExtractions renders the Extraction history sub-page: every
// extraction job for the document, newest first, each lazily loading its
// detail. ?merged=1 carries the merge-success flash.
func (s *Server) uiDocumentExtractions(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	doc, err := s.memory.GetDocument(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Document"), DocumentExtractionsPage(nil, nil, err, "", nil))
	}
	jobs, err := s.memory.ListExtractionJobs(ctx, id)
	if err != nil {
		return s.page(c, pageTitle(documentName(*doc), "Extraction history"), DocumentExtractionsPage(doc, nil, err, "", nil))
	}
	sortExtractionJobsNewestFirst(jobs)
	flashMsg := flashFromQuery(c, []flashParam{{key: "merged", msg: "Branch merged into main."}})
	flashErr := flashError(c)
	return s.page(c, pageTitle(documentName(*doc), "Extraction history"), DocumentExtractionsPage(doc, jobs, nil, flashMsg, flashErr))
}

// uiDocumentExtractionDetail renders the HTMX partial for one extraction job:
// the objects and relationships that run produced on its staging branch, its
// branch name and merge readiness, and the merge action when available.
func (s *Server) uiDocumentExtractionDetail(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	detail := s.loadExtractionJobDetail(ctx, docID, c.Param("jobId"))
	render.RenderPartial(c.Response().Writer, c.Request(), ExtractionJobDetail(docID, detail))
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
// a ?merged=1 flash. A job without a resolvable branch, or a merge failure,
// redirects with the real error.
func (s *Server) uiDocumentMerge(c echo.Context) error {
	ctx := c.Request().Context()
	docID := c.Param("id")
	historyPath := documentExtractionsPath(docID)

	job, err := s.findExtractionJob(ctx, docID, c.Param("jobId"))
	if err != nil {
		return redirectWithError(c, historyPath, err)
	}
	branchID, ok := s.extractionJobBranchID(ctx, job)
	if !ok {
		return redirectWithError(c, historyPath, fmt.Errorf("merge: this extraction has no staging branch to merge"))
	}
	if _, err := s.memory.MergeBranch(ctx, branchID, "partial"); err != nil {
		return redirectWithError(c, historyPath, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), historyPath+"?merged=1")
	return nil
}
