package main

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/labstack/echo/v4"
)

// dsSyncJobsPageSize is how many sync jobs the detail page renders inline. The
// rest load through the htmx partial, so a long-lived source cannot grow the
// page without bound.
const dsSyncJobsPageSize = 5

// dsSyncHistoryView is the sync-history page rendered inline: one page of jobs
// plus the state needed to ask for the next one. Offset is the position of the
// first job in Jobs within the source's full history; Total is the source's
// true job count (not the page size).
type dsSyncHistoryView struct {
	Jobs    []DSSyncJob
	Total   int
	Offset  int
	HasMore bool
}

// dsSyncHistoryTotal is the badge count for the history header. A nil view (a
// failed or skipped listing) reads as zero jobs.
func dsSyncHistoryTotal(page *dsSyncHistoryView) int {
	if page == nil {
		return 0
	}
	return page.Total
}

// dsSyncHistoryViewFor turns a sync-jobs response into a view of the page that
// starts at offset. HasMore compares the end of this page with the API's total.
func dsSyncHistoryViewFor(resp *DSListSyncJobsResponse, offset int) *dsSyncHistoryView {
	if resp == nil {
		return nil
	}
	loaded := offset + len(resp.Items)
	return &dsSyncHistoryView{
		Jobs:    resp.Items,
		Total:   resp.Total,
		Offset:  offset,
		HasMore: loaded < resp.Total,
	}
}

// dsSyncJobsPartialURL is the "Load more" target: the next page's offset for
// this source.
func dsSyncJobsPartialURL(sourceID string, offset int) string {
	return "/data-sources/" + url.PathEscape(sourceID) + "/sync-jobs/partial?offset=" + strconv.Itoa(offset)
}

// dsSyncJobsPageParams parses the API listing's limit/offset. Zero means "let
// the memory API apply its bounded default".
func dsSyncJobsPageParams(limitStr, offsetStr string) (limit, offset int) {
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
		limit = n
	}
	if n, err := strconv.Atoi(offsetStr); err == nil && n > 0 {
		offset = n
	}
	return limit, offset
}

// dsSyncJobsOffset parses the partial's offset; anything invalid means the
// first page.
func dsSyncJobsOffset(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// uiDataSourceSyncJobsPartial returns the next page of sync-job rows plus a
// replacement "Load more" slot (or the end note) as an HTMX fragment. It is the
// affordance that keeps the detail page from rendering the whole history.
func (s *Server) uiDataSourceSyncJobsPartial(c echo.Context) error {
	id := c.Param("id")
	offset := dsSyncJobsOffset(c.QueryParam("offset"))
	resp, err := s.memory.ListDSSyncJobs(c.Request().Context(), id, "", dsSyncJobsPageSize, offset)
	if err != nil {
		captureError(err)
		return echo.NewHTTPError(http.StatusBadGateway, "failed to load more sync jobs")
	}
	view := dsSyncHistoryViewFor(resp, offset)
	if view == nil {
		view = &dsSyncHistoryView{Offset: offset}
	}
	renderPartial(c.Response().Writer, c.Request(), dsSyncJobsPartial(id, *view))
	return nil
}
