package integration

// DocumentListFiltersSuite covers the documents listing's filter, search, sort,
// count and pagination contract:
//
//   - the four status buckets partition the derived processing status, and a
//     document whose conversion failed while an extraction job is pending lands
//     in exactly one of them;
//   - statusCounts ignores the status filter but honours search and source;
//   - metadata search matches filename and source URL, with LIKE metacharacters
//     treated literally;
//   - sorting by created/updated in both directions keeps the keyset cursor
//     walk free of gaps and duplicates;
//   - listings stay HEAD-only and exclude documents removed at the source.
//
// It runs against the in-process test server backed by a real Postgres test
// database (testutil.BaseSuite). If Postgres is unreachable the suite skips via
// testdb.SkipOrFatal; the package still compiles.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func TestDocumentListFiltersSuite(t *testing.T) {
	suite.Run(t, new(DocumentListFiltersSuite))
}

type DocumentListFiltersSuite struct {
	testutil.BaseSuite
}

func (s *DocumentListFiltersSuite) SetupSuite() {
	s.SetDBSuffix("doclist")
	s.BaseSuite.SetupSuite()
}

// docSpec describes one seeded document plus the job rows that decide its
// derived processing status.
type docSpec struct {
	id               string
	filename         *string
	sourceURL        *string
	sourceType       string
	sourceID         *string
	conversionStatus string
	createdAt        time.Time
	updatedAt        time.Time
	parsingStatus    string // "" = no parsing job
	extractionStatus string // "" = no extraction job
}

// insertDocument seeds one document and its job rows, returning its id.
func (s *DocumentListFiltersSuite) insertDocument(spec docSpec) string {
	s.T().Helper()
	id := spec.id
	if id == "" {
		id = uuid.New().String()
	}
	sourceType := spec.sourceType
	if sourceType == "" {
		sourceType = "upload"
	}
	conversionStatus := spec.conversionStatus
	if conversionStatus == "" {
		conversionStatus = "not_required"
	}

	var sourceID any
	if spec.sourceID != nil {
		sourceID = *spec.sourceID
	}
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.documents
			(id, project_id, filename, source_url, source_type, source_id, conversion_status, created_at, updated_at)
		VALUES (uuid(?), uuid(?), ?, ?, ?, ?::uuid, ?, ?, ?)
	`, id, s.ProjectID, spec.filename, spec.sourceURL, sourceType, sourceID, conversionStatus, spec.createdAt, spec.updatedAt).Exec(s.Ctx)
	s.Require().NoError(err, "insert document")

	if spec.parsingStatus != "" {
		_, err = s.DB().NewRaw(`
			INSERT INTO kb.document_parsing_jobs (id, organization_id, project_id, document_id, status, source_type, created_at, updated_at)
			VALUES (uuid(?), uuid(?), uuid(?), uuid(?), ?, 'upload', ?, ?)
		`, uuid.New().String(), s.OrgID, s.ProjectID, id, spec.parsingStatus, spec.createdAt, spec.createdAt).Exec(s.Ctx)
		s.Require().NoError(err, "insert parsing job")
	}
	if spec.extractionStatus != "" {
		_, err = s.DB().NewRaw(`
			INSERT INTO kb.object_extraction_jobs (id, project_id, document_id, status, created_at, updated_at)
			VALUES (uuid(?), uuid(?), uuid(?), ?, ?, ?)
		`, uuid.New().String(), s.ProjectID, id, spec.extractionStatus, spec.createdAt, spec.createdAt).Exec(s.Ctx)
		s.Require().NoError(err, "insert extraction job")
	}
	return id
}

// insertSource creates a kb.sources row and returns its id.
func (s *DocumentListFiltersSuite) insertSource(provider string) string {
	s.T().Helper()
	id := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.sources (id, project_id, provider)
		VALUES (uuid(?), uuid(?), ?)
	`, id, s.ProjectID, provider).Exec(s.Ctx)
	s.Require().NoError(err, "insert source")
	return id
}

// list calls the repository listing with the given params.
func (s *DocumentListFiltersSuite) list(params documents.ListParams) *documents.ListResult {
	s.T().Helper()
	params.ProjectID = s.ProjectID
	if params.Limit == 0 {
		params.Limit = 100
	}
	repo := documents.NewRepository(s.DB(), slog.Default())
	res, err := repo.List(s.Ctx, params)
	s.Require().NoError(err, "list documents")
	return res
}

// idsOf returns the ids of a result page in order.
func idsOf(res *documents.ListResult) []string {
	out := make([]string, 0, len(res.Documents))
	for _, d := range res.Documents {
		out = append(out, d.ID)
	}
	return out
}

// TestStatusBucketsPartitionThePopulation pins the bucket → row mapping,
// including the CASE precedence case where a failed conversion coexists with a
// pending extraction job.
func (s *DocumentListFiltersSuite) TestStatusBucketsPartitionThePopulation() {
	now := time.Now().UTC()
	converting := s.insertDocument(docSpec{filename: testutil.StringPtr("converting.md"), parsingStatus: "pending", createdAt: now, updatedAt: now})
	conversionFailed := s.insertDocument(docSpec{filename: testutil.StringPtr("conversion-failed.pdf"), conversionStatus: "failed", createdAt: now, updatedAt: now})
	// Both a failed conversion and a pending extraction: the CASE checks
	// conversion_failed before extracting, so this row is "failed", not
	// "processing".
	failedAndPending := s.insertDocument(docSpec{filename: testutil.StringPtr("both.pdf"), conversionStatus: "failed", extractionStatus: "pending", createdAt: now, updatedAt: now})
	extracting := s.insertDocument(docSpec{filename: testutil.StringPtr("extracting.md"), extractionStatus: "processing", createdAt: now, updatedAt: now})
	extractionFailed := s.insertDocument(docSpec{filename: testutil.StringPtr("extraction-failed.md"), extractionStatus: "failed", createdAt: now, updatedAt: now})
	completed := s.insertDocument(docSpec{filename: testutil.StringPtr("completed.md"), extractionStatus: "completed", createdAt: now, updatedAt: now})
	ready := s.insertDocument(docSpec{filename: testutil.StringPtr("ready.md"), createdAt: now, updatedAt: now})

	cases := map[string][]string{
		documents.StatusBucketProcessing: {converting, extracting},
		documents.StatusBucketFailed:     {conversionFailed, failedAndPending, extractionFailed},
		documents.StatusBucketExtracted:  {completed},
		documents.StatusBucketReady:      {ready},
	}
	for bucket, want := range cases {
		s.Run(bucket, func() {
			res := s.list(documents.ListParams{Status: bucket})
			s.ElementsMatch(want, idsOf(res))
			s.Equal(len(want), res.Total, "total should count only the filtered rows")
		})
	}

	// No bucket selected: everything, and the counts describe the whole
	// population regardless of the status filter.
	all := s.list(documents.ListParams{})
	s.Len(all.Documents, 7)
	s.Equal(documents.StatusCounts{All: 7, Processing: 2, Ready: 1, Extracted: 1, Failed: 3}, all.StatusCounts)

	// Counts stay stable while a status filter is active.
	filtered := s.list(documents.ListParams{Status: documents.StatusBucketFailed})
	s.Equal(3, filtered.Total)
	s.Equal(all.StatusCounts, filtered.StatusCounts, "status counts must ignore the status filter")
}

// TestSearchMatchesFilenameAndSourceURL covers the metadata search, including a
// literal LIKE metacharacter.
func (s *DocumentListFiltersSuite) TestSearchMatchesFilenameAndSourceURL() {
	now := time.Now().UTC()
	spec := s.insertDocument(docSpec{filename: testutil.StringPtr("Quarterly Spec.md"), createdAt: now, updatedAt: now})
	notes := s.insertDocument(docSpec{filename: testutil.StringPtr("notes.txt"), createdAt: now, updatedAt: now})
	percent := s.insertDocument(docSpec{filename: testutil.StringPtr("100% done.md"), createdAt: now, updatedAt: now})
	sourceDoc := s.insertDocument(docSpec{
		sourceURL:  testutil.StringPtr("https://drive.example/folders/Board-Pack.pdf"),
		sourceType: "drive",
		createdAt:  now,
		updatedAt:  now,
	})

	res := s.list(documents.ListParams{Query: "spec"})
	s.ElementsMatch([]string{spec}, idsOf(res))
	s.Equal(1, res.Total)
	s.Equal(1, res.StatusCounts.All, "counts honour the search term")

	res = s.list(documents.ListParams{Query: "board-pack"})
	s.ElementsMatch([]string{sourceDoc}, idsOf(res))

	res = s.list(documents.ListParams{Query: "notes"})
	s.ElementsMatch([]string{notes}, idsOf(res))

	// A literal % must not act as a wildcard: only the document whose name
	// contains a percent sign matches.
	res = s.list(documents.ListParams{Query: "%"})
	s.ElementsMatch([]string{percent}, idsOf(res))

	// Source filter combines with search.
	res = s.list(documents.ListParams{SourceType: testutil.StringPtr("drive")})
	s.ElementsMatch([]string{sourceDoc}, idsOf(res))
	s.Equal(1, res.Total)
}

// TestSortByUpdatedUsesTheUpdatedKeyset pins sorting by updated_at, which
// differs from created_at order and therefore needs its own keyset.
func (s *DocumentListFiltersSuite) TestSortByUpdatedUsesTheUpdatedKeyset() {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	// Older document, touched most recently.
	olderTouched := s.insertDocument(docSpec{filename: testutil.StringPtr("older-touched.md"), createdAt: base, updatedAt: base.Add(48 * time.Hour)})
	// Newer document, never touched since creation.
	newerStale := s.insertDocument(docSpec{filename: testutil.StringPtr("newer-stale.md"), createdAt: base.Add(24 * time.Hour), updatedAt: base.Add(24 * time.Hour)})

	// sorted by updated desc: the recently touched one comes first...
	res := s.list(documents.ListParams{Sort: documents.SortUpdated, Order: documents.OrderDesc})
	s.Equal([]string{olderTouched, newerStale}, idsOf(res))

	// ...which is the opposite of created desc.
	res = s.list(documents.ListParams{Sort: documents.SortCreated, Order: documents.OrderDesc})
	s.Equal([]string{newerStale, olderTouched}, idsOf(res))

	// Ascending flips both.
	res = s.list(documents.ListParams{Sort: documents.SortUpdated, Order: documents.OrderAsc})
	s.Equal([]string{newerStale, olderTouched}, idsOf(res))
}

// TestCursorPaginationWalksBothSorts walks every page of a listing under each
// sort and direction and asserts the walk is complete and duplicate-free.
func (s *DocumentListFiltersSuite) TestCursorPaginationWalksBothSorts() {
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	// Five documents, seeded a..e. `created_at` increases with the letter, so
	// `created asc` is a..e. `updated_at` decreases with the letter (a is the
	// most recently touched), so `updated desc` is also a..e while
	// `updated asc` is e..a.
	createdAsc := make([]string, 0, 5)
	for i := range 5 {
		id := s.insertDocument(docSpec{
			filename:  testutil.StringPtr("doc-" + string(rune('a'+i)) + ".md"),
			createdAt: base.Add(time.Duration(i) * time.Hour),
			updatedAt: base.Add(time.Duration(10-i) * time.Hour),
		})
		createdAsc = append(createdAsc, id)
	}
	createdDesc := reversed(createdAsc)

	for _, tc := range []struct {
		name  string
		sort  string
		order string
		want  []string
	}{
		{name: "created desc", sort: documents.SortCreated, order: documents.OrderDesc, want: createdDesc},
		{name: "created asc", sort: documents.SortCreated, order: documents.OrderAsc, want: createdAsc},
		{name: "updated desc", sort: documents.SortUpdated, order: documents.OrderDesc, want: createdAsc},
		{name: "updated asc", sort: documents.SortUpdated, order: documents.OrderAsc, want: createdDesc},
	} {
		s.Run(tc.name, func() {
			var (
				got    []string
				cursor *documents.Cursor
			)
			for page := 0; page < 10; page++ {
				res := s.list(documents.ListParams{Limit: 2, Sort: tc.sort, Order: tc.order, Cursor: cursor})
				got = append(got, idsOf(res)...)
				if res.NextCursor == nil {
					break
				}
				parsed, err := documents.ParseCursor(*res.NextCursor)
				s.Require().NoError(err)
				s.Require().Equal(tc.sort, parsed.Sort, "cursor must carry the active sort")
				cursor = parsed
			}
			s.Equal(tc.want, got, "keyset walk must be ordered, complete and duplicate-free")
		})
	}
}

// reversed returns a copy of ids in reverse order.
func reversed(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[len(ids)-1-i] = id
	}
	return out
}

// TestFilterBySourceID pins the per-data-source filter: a connected source id
// narrows to its documents, and "no source" narrows to direct uploads.
func (s *DocumentListFiltersSuite) TestFilterBySourceID() {
	now := time.Now().UTC()
	drive := s.insertSource("google_drive")
	web := s.insertSource("web")
	driveDoc := s.insertDocument(docSpec{filename: testutil.StringPtr("drive.md"), sourceType: "drive", sourceID: &drive, createdAt: now, updatedAt: now})
	webDoc := s.insertDocument(docSpec{filename: testutil.StringPtr("web.md"), sourceType: "web", sourceID: &web, createdAt: now.Add(-time.Minute), updatedAt: now})
	upload := s.insertDocument(docSpec{filename: testutil.StringPtr("upload.md"), sourceType: "upload", createdAt: now.Add(-2 * time.Minute), updatedAt: now})

	res := s.list(documents.ListParams{SourceID: &drive})
	s.ElementsMatch([]string{driveDoc}, idsOf(res))

	res = s.list(documents.ListParams{SourceUnassigned: true})
	s.ElementsMatch([]string{upload}, idsOf(res))

	res = s.list(documents.ListParams{SourceID: &web})
	s.ElementsMatch([]string{webDoc}, idsOf(res))

	// The status breakdown follows the source filter.
	res = s.list(documents.ListParams{SourceID: &drive})
	s.Equal(1, res.StatusCounts.All)
}

// TestSourceCountsEndpoint pins the source filter's counts: grouped by connected
// source id (empty for uploads), over the same HEAD-only population as a listing.
func (s *DocumentListFiltersSuite) TestSourceCountsEndpoint() {
	now := time.Now().UTC()
	drive := s.insertSource("google_drive")
	web := s.insertSource("web")
	s.insertDocument(docSpec{filename: testutil.StringPtr("drive-1.md"), sourceType: "drive", sourceID: &drive, createdAt: now, updatedAt: now})
	s.insertDocument(docSpec{filename: testutil.StringPtr("drive-2.md"), sourceType: "drive", sourceID: &drive, createdAt: now.Add(-time.Minute), updatedAt: now})
	s.insertDocument(docSpec{filename: testutil.StringPtr("web-1.md"), sourceType: "web", sourceID: &web, createdAt: now.Add(-2 * time.Minute), updatedAt: now})
	s.insertDocument(docSpec{filename: testutil.StringPtr("upload.md"), sourceType: "upload", createdAt: now.Add(-3 * time.Minute), updatedAt: now})
	head := s.insertDocument(docSpec{filename: testutil.StringPtr("head.md"), sourceType: "drive", sourceID: &drive, createdAt: now.Add(-4 * time.Minute), updatedAt: now})
	superseded := s.insertDocument(docSpec{filename: testutil.StringPtr("old.md"), sourceType: "drive", sourceID: &drive, createdAt: now.Add(-5 * time.Minute), updatedAt: now})
	removed := s.insertDocument(docSpec{filename: testutil.StringPtr("removed.md"), sourceType: "drive", sourceID: &drive, createdAt: now.Add(-6 * time.Minute), updatedAt: now})
	_, err := s.DB().NewRaw(`UPDATE kb.documents SET supersedes_id = uuid(?) WHERE id = uuid(?)`, head, superseded).Exec(s.Ctx)
	s.Require().NoError(err)
	_, err = s.DB().NewRaw(`UPDATE kb.documents SET source_status = 'deleted_from_source' WHERE id = uuid(?)`, head).Exec(s.Ctx)
	s.Require().NoError(err)
	_, err = s.DB().NewRaw(`UPDATE kb.documents SET source_status = 'deleted_from_source' WHERE id = uuid(?)`, removed).Exec(s.Ctx)
	s.Require().NoError(err)

	resp := s.Client.GET("/api/documents/source-counts",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "source counts: %s", resp.Body)
	var body struct {
		Sources []struct {
			SourceID   string `json:"sourceId"`
			SourceType string `json:"sourceType"`
			Count      int    `json:"count"`
		} `json:"sources"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &body), "decode source counts: %s", resp.Body)

	got := map[string]int{}
	for _, src := range body.Sources {
		got[src.SourceID] = src.Count
	}
	s.Equal(2, got[drive], "drive docs")
	s.Equal(1, got[web], "web docs")
	s.Equal(1, got[""], "uploads have no source id")
	for _, src := range body.Sources {
		if src.SourceID == "" && src.SourceType != "upload" {
			s.T().Errorf("unassigned bucket type = %q, want upload", src.SourceType)
		}
	}
}

// TestListingExcludesSupersededAndDeletedFromSource keeps the new filters inside
// the population contract the listing already had.
func (s *DocumentListFiltersSuite) TestListingExcludesSupersededAndDeletedFromSource() {
	now := time.Now().UTC()
	head := s.insertDocument(docSpec{filename: testutil.StringPtr("head.md"), createdAt: now, updatedAt: now})
	superseded := s.insertDocument(docSpec{filename: testutil.StringPtr("superseded.md"), createdAt: now, updatedAt: now})
	removed := s.insertDocument(docSpec{filename: testutil.StringPtr("removed.md"), createdAt: now, updatedAt: now})

	_, err := s.DB().NewRaw(`UPDATE kb.documents SET supersedes_id = uuid(?) WHERE id = uuid(?)`, head, superseded).Exec(s.Ctx)
	s.Require().NoError(err)
	_, err = s.DB().NewRaw(`UPDATE kb.documents SET source_status = 'deleted_from_source' WHERE id = uuid(?)`, removed).Exec(s.Ctx)
	s.Require().NoError(err)

	res := s.list(documents.ListParams{})
	s.ElementsMatch([]string{head}, idsOf(res))
	s.Equal(1, res.Total)
	s.Equal(1, res.StatusCounts.All, "counts use the same population as the rows")

	// History reads can still opt back in to documents removed at the source.
	res = s.list(documents.ListParams{IncludeDeletedFromSource: true})
	s.ElementsMatch([]string{head, removed}, idsOf(res))
}

// TestListEndpointContract drives GET /api/documents through the HTTP stack, not
// the repository, so the 400 contract the design promises (unknown
// vocabulary, an unusable cursor, a cursor from another sort) is pinned where a
// client actually meets it — and a valid request still returns the shape the
// gateway reads.
func (s *DocumentListFiltersSuite) TestListEndpointContract() {
	now := time.Now().UTC()
	failed := s.insertDocument(docSpec{filename: testutil.StringPtr("contract-failed.pdf"), conversionStatus: "failed", createdAt: now.Add(-2 * time.Hour), updatedAt: now})
	s.insertDocument(docSpec{filename: testutil.StringPtr("contract-failed-2.pdf"), conversionStatus: "failed", createdAt: now.Add(-3 * time.Hour), updatedAt: now})
	s.insertDocument(docSpec{filename: testutil.StringPtr("contract-ready.md"), createdAt: now.Add(-4 * time.Hour), updatedAt: now})

	opts := []testutil.RequestOption{
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
	}
	get := func(query string) *testutil.HTTPResponse {
		return s.Client.GET("/api/documents"+query, opts...)
	}

	// A valid page: the filters reach the API and the response carries the total,
	// the breakdown and a cursor for the next page.
	page := get("?status=failed&q=contract&sort=updated&order=asc&limit=1")
	s.Require().Equal(http.StatusOK, page.StatusCode, "valid listing: %s", page.Body)

	var body struct {
		Documents []struct {
			ID string `json:"id"`
		} `json:"documents"`
		Total        int `json:"total"`
		StatusCounts struct {
			All        int `json:"all"`
			Failed     int `json:"failed"`
			Ready      int `json:"ready"`
			Extracted  int `json:"extracted"`
			Processing int `json:"processing"`
		} `json:"statusCounts"`
		NextCursor string `json:"next_cursor"`
	}
	s.Require().NoError(json.Unmarshal(page.Body, &body), "decode listing: %s", page.Body)
	s.Require().Len(body.Documents, 1, "limit=1: %s", page.Body)
	s.Require().Equal(2, body.Total, "total honours every filter including status")
	s.Require().Equal(2, body.StatusCounts.Failed, "breakdown counts the matching population")
	s.Require().NotEmpty(body.NextCursor, "a second matching document means a next page exists")

	// The cursor narrows to the second page when it is used with the sort it was
	// minted for...
	next := get("?status=failed&q=contract&sort=updated&order=asc&limit=1&cursor=" + url.QueryEscape(body.NextCursor))
	s.Require().Equal(http.StatusOK, next.StatusCode, "second page: %s", next.Body)
	var second struct {
		Documents []struct {
			ID string `json:"id"`
		} `json:"documents"`
	}
	s.Require().NoError(json.Unmarshal(next.Body, &second), "decode second page: %s", next.Body)
	s.Require().Len(second.Documents, 1, "second page: %s", next.Body)
	s.Require().NotEqual(body.Documents[0].ID, second.Documents[0].ID, "pages must not repeat a row")

	// ...and is refused, not silently mis-paged, under the other sort.
	mismatch := get("?status=failed&sort=created&cursor=" + url.QueryEscape(body.NextCursor))
	s.Require().Equal(http.StatusBadRequest, mismatch.StatusCode, "cursor from another sort: %s", mismatch.Body)

	for _, tc := range []struct{ name, query string }{
		{"unknown status", "?status=done"},
		{"unknown sort", "?sort=filename"},
		{"unknown order", "?order=sideways"},
		{"invalid source id", "?sourceId=not-a-uuid"},
		{"limit below range", "?limit=0"},
		{"limit above range", "?limit=501"},
		{"unparseable cursor", "?cursor=not-a-cursor"},
	} {
		s.Run(tc.name, func() {
			resp := get(tc.query)
			s.Equal(http.StatusBadRequest, resp.StatusCode, "%s: %s", tc.name, resp.Body)
		})
	}

	// The row the failed-conversion seed produced is reachable by its own bucket,
	// so a status filter is not silently dropped on the way through the handler.
	inFailed := get("?status=failed&limit=100")
	s.Require().Equal(http.StatusOK, inFailed.StatusCode, "failed bucket: %s", inFailed.Body)
	s.Contains(string(inFailed.Body), failed, "the failed document is in the failed bucket")
}
