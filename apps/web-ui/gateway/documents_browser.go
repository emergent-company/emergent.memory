package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

// documentPageSize is the number of documents one page of the browser renders
// (and the API returns). Smaller than the old hardcoded 100 because every row
// now renders twice — once in the table, once in the mobile card list.
const documentPageSize = 50

// The users' status vocabulary. The API derives six states from conversion and
// extraction progress; the page offers these four buckets over them (plus All).
const (
	documentStatusAll        = ""
	documentStatusProcessing = "processing"
	documentStatusReady      = "ready"
	documentStatusExtracted  = "extracted"
	documentStatusFailed     = "failed"
)

// documentSortCreated/documentSortUpdated are the API's sort keys.
const (
	documentSortCreated = "created"
	documentSortUpdated = "updated"
	documentOrderAsc    = "asc"
	documentOrderDesc   = "desc"
)

// normalizeDocumentStatus coerces a raw `status` query value to the bucket
// vocabulary. Anything unrecognised falls back to "all" rather than being
// forwarded — the API rejects unknown values with a 400, and a bookmarked typo
// should not turn the page into an error.
func normalizeDocumentStatus(raw string) string {
	switch s := strings.ToLower(strings.TrimSpace(raw)); s {
	case documentStatusProcessing, documentStatusReady, documentStatusExtracted, documentStatusFailed:
		return s
	default:
		return documentStatusAll
	}
}

// normalizeDocumentSort coerces a raw `sort` query value to the API vocabulary.
func normalizeDocumentSort(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), documentSortUpdated) {
		return documentSortUpdated
	}
	return documentSortCreated
}

// normalizeDocumentOrder coerces a raw `order` query value. Empty means the API
// default (descending).
func normalizeDocumentOrder(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), documentOrderAsc) {
		return documentOrderAsc
	}
	return documentOrderDesc
}

// documentSourceNone is the source-filter value for documents with no data
// source (direct uploads). Source ids are UUIDs, so it cannot collide.
const documentSourceNone = "none"

// documentSourceTypeLabel is the human label for a document source type. The
// source-type vocabulary is small and stable ("upload", "drive", "repo",
// "email"); anything else is prettified rather than shown raw.
func documentSourceTypeLabel(sourceType string) string {
	switch s := strings.ToLower(strings.TrimSpace(sourceType)); s {
	case "":
		return ""
	case "upload":
		return "Uploaded"
	case "drive":
		return "Google Drive"
	case "repo":
		return "Repository"
	case "email":
		return "Email"
	default:
		return strings.ToUpper(s[:1]) + strings.ReplaceAll(s[1:], "_", " ")
	}
}

// documentProcessingLabel is the user-facing name of a derived processing
// status. Unknown values fall back to the raw value so a new state is visible
// instead of blank.
func documentProcessingLabel(status string) string {
	switch status {
	case "converting":
		return "Converting"
	case "conversion_failed":
		return "Conversion failed"
	case "extracting":
		return "Extracting"
	case "extraction_failed":
		return "Extraction failed"
	case "completed":
		return "Extracted"
	case "ready_for_extraction":
		return "Ready"
	case "":
		return ""
	default:
		return status
	}
}

// documentProcessingIntent colours the status badge of a derived processing
// status. "Ready" is deliberately neutral: it is a resting state, not a
// completed one, and it should not compete with "Extracted".
func documentProcessingIntent(status string) ui.BadgeIntent {
	switch status {
	case "conversion_failed", "extraction_failed":
		return ui.BadgeError
	case "converting", "extracting":
		return ui.BadgeInfo
	case "completed":
		return ui.BadgeSuccess
	default:
		return ui.BadgeNeutral
	}
}

// documentHeaderStatus picks the derived processing status for the document
// header, falling back to the raw extraction status for an older API. A
// dead-lettered extraction folds into extraction_failed so the header never
// renders the internal "dead_letter" token.
func documentHeaderStatus(doc *Document) string {
	if doc == nil {
		return ""
	}
	if doc.ProcessingStatus != "" {
		return doc.ProcessingStatus
	}
	if doc.ExtractionStatus == "dead_letter" {
		return "extraction_failed"
	}
	return doc.ExtractionStatus
}

// documentRow is one rendered document. The table and the mobile card share it
// so both surfaces cannot drift apart.
type documentRow struct {
	ID         string
	Name       string
	Chunks     int
	Status     string // derived processing status value ("" when unknown)
	Versions   int
	Source     string // resolved provenance label ("Uploaded", a data source's name, or a source type)
	SourceID   string // data-source integration id; set only when the row can link to it
	Uploader   string // display name of the uploader; empty for source-synced and pre-#2260 rows
	UpdatedAt  string
	CreatedAt  string
	SourceType string
}

// documentSourceOption is one selectable value in the source filter.
type documentSourceOption struct {
	Value string // the `sourceId` API value, or documentSourceNone for uploads
	Label string
	Count int
}

// documentPageData is everything the documents page renders: the active
// query/filter/sort state (so controls and links can echo it), the page's rows,
// and the counts.
type documentPageData struct {
	Query    string
	Status   string
	SourceID string
	Sort     string
	Order    string
	Cursor   string
	// Attention is the one needs-attention area whose problems are shown in
	// full, instead of the panel's clipped eight. Empty means clipped.
	Attention string

	Rows       []documentRow
	Total      int
	Counts     DocumentStatusCounts
	NextCursor string
	HasMore    bool

	Sources []documentSourceOption

	// Problems is the project's terminal-failure report. Nil means none, a
	// failed fetch, or a non-Documents page state; the panel renders nothing.
	Problems *ProcessingProblemsReport
	// CanManage gates admin-only actions in the panel, such as the project-wide
	// embedding retrigger.
	CanManage bool

	LoadErr  error
	FlashMsg string
	FlashErr error
}

// filtered reports whether any narrowing control is active. It selects the
// empty state: "no documents yet" is a lie when a filter hid them.
func (d documentPageData) filtered() bool {
	return d.Query != "" || d.Status != documentStatusAll || d.SourceID != ""
}

// listParams builds the API request for this page at one cursor position.
func (d documentPageData) listParams(cursor string) DocumentListParams {
	return DocumentListParams{
		Query:    d.Query,
		Status:   d.Status,
		SourceID: d.SourceID,
		Sort:     d.Sort,
		Order:    d.Order,
		Cursor:   cursor,
		Limit:    documentPageSize,
	}
}

// requestURL is the canonical page URL for this state with `overrides` applied
// on top. Empty overrides delete their key, so a control can both set and clear
// a filter. The cursor is always dropped unless explicitly passed: changing a
// filter restarts pagination.
func (d documentPageData) requestURL(overrides map[string]string) string {
	q := url.Values{}
	if d.Query != "" {
		q.Set("q", d.Query)
	}
	if d.Status != documentStatusAll {
		q.Set("status", d.Status)
	}
	if d.SourceID != "" {
		q.Set("sourceId", d.SourceID)
	}
	if d.Attention != "" {
		q.Set("attention", d.Attention)
	}
	if d.Sort != "" && d.Sort != documentSortCreated {
		q.Set("sort", d.Sort)
	}
	if d.Order != "" && d.Order != documentOrderDesc {
		q.Set("order", d.Order)
	}
	for k, v := range overrides {
		if v == "" {
			q.Del(k)
			continue
		}
		q.Set(k, v)
	}
	// Drop values that restate the defaults, so a link to "created, descending"
	// is /documents and not /documents?sort=created&order=desc.
	if q.Get("sort") == documentSortCreated {
		q.Del("sort")
	}
	if q.Get("order") == documentOrderDesc {
		q.Del("order")
	}
	if len(q) == 0 {
		return "/documents"
	}
	return "/documents?" + q.Encode()
}

// documentsPartialURL is the "Load more" target: the next cursor plus the active
// state, so the appended rows belong to the same view.
func documentsPartialURL(d documentPageData) string {
	q := url.Values{}
	q.Set("cursor", d.NextCursor)
	if d.Query != "" {
		q.Set("q", d.Query)
	}
	if d.Status != documentStatusAll {
		q.Set("status", d.Status)
	}
	if d.SourceID != "" {
		q.Set("sourceId", d.SourceID)
	}
	if d.Sort != "" {
		q.Set("sort", d.Sort)
	}
	if d.Order != "" {
		q.Set("order", d.Order)
	}
	return "/documents/partial?" + q.Encode()
}

// documentsSortHref is the href of one sortable column header: sorting by a new
// key starts descending (newest first); sorting by the active key flips the
// direction. Pagination restarts either way.
func documentsSortHref(d documentPageData, key string) string {
	dir := documentOrderDesc
	if d.Sort == key {
		if d.Order == documentOrderDesc {
			dir = documentOrderAsc
		}
	} else if d.Order == documentOrderAsc {
		// Keep the reader's chosen direction when they switch the key.
		dir = documentOrderAsc
	}
	return d.requestURL(map[string]string{"sort": key, "order": dir})
}

// documentsSortDir is the direction of a column, or "" when the column is not
// the active sort.
func documentsSortDir(d documentPageData, key string) string {
	if d.Sort != key {
		return ""
	}
	return d.Order
}

// documentsSortLabel names the active order for the footer note. It states the
// order in words the reader can check against the rows, rather than naming the
// sort key and drawing an arrow.
func documentsSortLabel(d documentPageData) string {
	if d.Sort == documentSortUpdated {
		if d.Order == documentOrderAsc {
			return "Least recently updated first"
		}
		return "Recently updated first"
	}
	if d.Order == documentOrderAsc {
		return "Oldest first"
	}
	return "Newest first"
}

// documentStatusOption is one option of the status filter select.
type documentStatusOption struct {
	Value string
	Label string
	Count int
}

// documentSelectOption is one option of a filter <select>.
type documentSelectOption struct {
	Value string
	Label string
}

// documentStatusOptions is the status filter: the four buckets over the
// derived status, plus All. The counts come from the API's status breakdown.
func documentStatusOptions(d documentPageData) []documentStatusOption {
	return []documentStatusOption{
		{Value: documentStatusAll, Label: "All", Count: d.Counts.All},
		{Value: documentStatusProcessing, Label: "Processing", Count: d.Counts.Processing},
		{Value: documentStatusReady, Label: "Ready", Count: d.Counts.Ready},
		{Value: documentStatusExtracted, Label: "Extracted", Count: d.Counts.Extracted},
		{Value: documentStatusFailed, Label: "Failed", Count: d.Counts.Failed},
	}
}

// documentStatusOptionLabel is the select label for one status bucket: the
// user-facing name with the bucket's document count.
func documentStatusOptionLabel(opt documentStatusOption) string {
	return opt.Label + " (" + groupDigits(int64(opt.Count)) + ")"
}

// documentSortSelectOptions lists the sort keys offered in the toolbar.
func documentSortSelectOptions() []documentSelectOption {
	return []documentSelectOption{
		{Value: documentSortCreated, Label: "Created"},
		{Value: documentSortUpdated, Label: "Updated"},
	}
}

// documentOrderSelectOptions lists the direction options offered in the
// toolbar. The labels name the time, not the direction word, so the control
// reads the same whichever sort key is active.
func documentOrderSelectOptions() []documentSelectOption {
	return []documentSelectOption{
		{Value: documentOrderDesc, Label: "Newest first"},
		{Value: documentOrderAsc, Label: "Oldest first"},
	}
}

// documentSourceSelectOptions lists the provenance filter's options with their
// document counts.
func documentSourceSelectOptions(d documentPageData) []documentSelectOption {
	out := make([]documentSelectOption, 0, len(d.Sources)+1)
	out = append(out, documentSelectOption{Value: "", Label: "All sources"})
	for _, src := range d.Sources {
		label := src.Label
		if src.Count > 0 {
			label = label + " (" + strconv.Itoa(src.Count) + ")"
		}
		out = append(out, documentSelectOption{Value: src.Value, Label: label})
	}
	return out
}

// loadDocumentsPage fills data from the query/filter state already parsed into
// it, at cursor. Source labels are resolved best-effort: a failed integrations
// fetch degrades the Source column to the raw source type, never fails the page.
// The filter *options* are a page concern, not a row concern: uiDocuments loads
// them after this, so a "Load more" request does not fetch a select it never
// renders.
func (s *Server) loadDocumentsPage(ctx context.Context, data *documentPageData, cursor string) error {
	if data.Sort == "" {
		data.Sort = documentSortCreated
	}
	if data.Order == "" {
		data.Order = documentOrderDesc
	}

	page, err := s.memory.ListDocuments(ctx, data.listParams(cursor))
	if err != nil {
		return err
	}

	names := s.documentSourceNames(ctx)

	rows := make([]documentRow, 0, len(page.Documents))
	for _, doc := range page.Documents {
		source := ""
		if doc.SourceID != "" {
			source = names[doc.SourceID]
		}
		if source == "" {
			source = documentSourceTypeLabel(doc.SourceType)
		}
		rows = append(rows, documentRow{
			ID:         doc.ID,
			Name:       documentName(doc),
			Chunks:     doc.Chunks,
			Status:     doc.ProcessingStatus,
			Versions:   doc.Version,
			Source:     source,
			SourceID:   doc.SourceID,
			Uploader:   doc.CreatedByDisplayName,
			UpdatedAt:  doc.UpdatedAt,
			CreatedAt:  doc.CreatedAt,
			SourceType: doc.SourceType,
		})
	}

	data.Rows = rows
	data.Total = page.Total
	data.Counts = page.StatusCounts
	data.NextCursor = page.NextCursor
	data.HasMore = page.NextCursor != ""
	return nil
}

// documentSourceNames indexes the project's data sources by id for per-row
// provenance. A failed fetch yields an empty map: rows then show the source
// type instead of a name, which is a degradation, not an error.
func (s *Server) documentSourceNames(ctx context.Context) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, optionalFetchTimeout)
	defer cancel()
	integrations, err := s.memory.ListDSIntegrations(ctx)
	if err != nil {
		return nil
	}
	names := make(map[string]string, len(integrations))
	for _, src := range integrations {
		if src.Name != "" {
			names[src.ID] = src.Name
		}
	}
	return names
}

// loadDocumentSourceOptions populates the source filter with the connected data
// sources that actually own documents, plus one "Uploaded" bucket for files
// with no source. Counts come from the dedicated endpoint and use the same
// population as the listing. A failed count fetch leaves the filter empty
// rather than failing the page; an active filter is still shown so the user can
// clear it.
func (s *Server) loadDocumentSourceOptions(ctx context.Context, data *documentPageData) {
	ctx, cancel := context.WithTimeout(ctx, optionalFetchTimeout)
	defer cancel()
	counts, err := s.memory.ListDocumentSourceCounts(ctx)
	names := s.documentSourceNames(ctx)
	namesLoaded := names != nil

	type aggregate struct {
		label string
		count int
	}
	byValue := make(map[string]*aggregate, len(counts))
	order := make([]string, 0, len(counts))
	if err == nil {
		for _, c := range counts {
			value := c.SourceID
			label := ""
			switch {
			case value == "":
				value = documentSourceNone
				label = "Uploaded"
			case names[value] != "":
				label = names[value]
			case namesLoaded:
				label = documentRemovedSourceLabel(c.SourceType)
			default:
				label = documentSourceTypeLabel(c.SourceType)
			}
			if agg, ok := byValue[value]; ok {
				agg.count += c.Count
				continue
			}
			byValue[value] = &aggregate{label: label, count: c.Count}
			order = append(order, value)
		}
	}
	options := make([]documentSourceOption, 0, len(order)+1)
	for _, value := range order {
		agg := byValue[value]
		options = append(options, documentSourceOption{Value: value, Label: agg.label, Count: agg.count})
	}
	// Keep an active filter selectable even when its documents are gone (the
	// user still needs a way to clear it).
	if data.SourceID != "" && byValue[data.SourceID] == nil {
		label := "Unknown source"
		switch {
		case data.SourceID == documentSourceNone:
			label = "Uploaded"
		case names[data.SourceID] != "":
			label = names[data.SourceID]
		}
		options = append([]documentSourceOption{{Value: data.SourceID, Label: label}}, options...)
	}
	data.Sources = options
}

// documentRemovedSourceLabel names a documents bucket whose data source no
// longer exists, without inventing a name for it.
func documentRemovedSourceLabel(sourceType string) string {
	if label := documentSourceTypeLabel(sourceType); label != "" {
		return label + " (removed source)"
	}
	return "Removed source"
}

// uiDocuments renders the documents page: stats, filters, and one page of the
// listing plus the upload form. ?uploaded=1 surfaces an upload success message,
// ?err=1 an upload failure.
func (s *Server) uiDocuments(c echo.Context) error {
	data := documentPageData{
		Query:     strings.TrimSpace(c.QueryParam("q")),
		Status:    normalizeDocumentStatus(c.QueryParam("status")),
		SourceID:  strings.TrimSpace(c.QueryParam("sourceId")),
		Sort:      normalizeDocumentSort(c.QueryParam("sort")),
		Order:     normalizeDocumentOrder(c.QueryParam("order")),
		Cursor:    c.QueryParam("cursor"),
		Attention: normalizeAttentionArea(c.QueryParam("attention")),
		FlashMsg: flashFromQuery(c, []flashParam{
			{key: "uploaded", msg: "Document uploaded."},
			{key: "duplicate", msg: "Document already exists (identical content)."},
			{key: "deleted", msg: "Document deleted."},
			{key: "extractions_retried", msg: "Retry started for failed extractions."},
			{key: "extractions_none", msg: "No failed extractions to retry."},
			{key: "conversions_retried", msg: "Failed conversions queued for retry."},
			{key: "conversions_none", msg: "No failed conversions to retry."},
			{key: "embeddings_retried", msg: "Failed embeddings queued for retry."},
			{key: "embeddings_none", msg: "No failed embeddings to retry."},
		}),
		FlashErr: flashError(c),
	}
	ctx := c.Request().Context()
	data.CanManage = s.projectAdminForCaller(c, s.currentProjectID(ctx))
	if err := s.loadDocumentsPage(ctx, &data, data.Cursor); err != nil {
		data.LoadErr = userFacingError(err)
	} else {
		s.loadDocumentSourceOptions(ctx, &data)
	}
	// Load the report before page() so the sidebar badge reuses the same cached
	// value. A panel fetch failure must not fail the document listing.
	if report, perr := s.cachedProcessingProblems(ctx); perr != nil {
		captureError(perr)
	} else {
		data.Problems = report
	}
	return s.page(c, pageTitle("Documents"), DocumentsPage(data))
}

// uiDocumentsPartial returns the next page of document rows (plus the same rows
// as mobile cards and a replacement "Load more" slot) as an HTMX fragment. It
// carries the search/filter/sort context forward so appended rows match the
// active view.
func (s *Server) uiDocumentsPartial(c echo.Context) error {
	data := documentPageData{
		Query:    strings.TrimSpace(c.QueryParam("q")),
		Status:   normalizeDocumentStatus(c.QueryParam("status")),
		SourceID: strings.TrimSpace(c.QueryParam("sourceId")),
		Sort:     normalizeDocumentSort(c.QueryParam("sort")),
		Order:    normalizeDocumentOrder(c.QueryParam("order")),
	}
	if err := s.loadDocumentsPage(c.Request().Context(), &data, c.QueryParam("cursor")); err != nil {
		captureError(err)
		return echo.NewHTTPError(http.StatusBadGateway, "failed to load more documents")
	}
	renderPartial(c.Response().Writer, c.Request(), documentsRowsPartial(data))
	return nil
}

// documentUpdatedLabel renders a document's last-update time, falling back to
// its creation time when the API omitted it.
func documentUpdatedLabel(r documentRow) string {
	if r.UpdatedAt != "" {
		return r.UpdatedAt
	}
	return r.CreatedAt
}

// documentsCountLabel is the header badge text: the true project total, not the
// number of rows on this page.
func documentsCountLabel(total int) string {
	return countLabel(total, "document", "documents")
}

// documentSortDirectionIcon is the glyph of the sort-direction toggle.
func documentSortDirectionIcon(d documentPageData) string {
	if d.Order == documentOrderAsc {
		return "lucide--arrow-up"
	}
	return "lucide--arrow-down"
}
