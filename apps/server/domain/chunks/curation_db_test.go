package chunks_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/chunks"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// =============================================================================
// Shared response DTOs
// =============================================================================

type curationEditResp struct {
	ChunkID  string `json:"chunkId"`
	Revision int    `json:"revision"`
}

type curationRevisionDTO struct {
	ID        string `json:"id"`
	ChunkID   string `json:"chunkId"`
	Seq       int    `json:"seq"`
	Text      string `json:"text"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

type curationListResp struct {
	Data       []curationRevisionDTO `json:"data"`
	TotalCount int                   `json:"totalCount"`
}

type curationDiffLine struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type curationDiffResp struct {
	From  int                `json:"from"`
	To    int                `json:"to"`
	Lines []curationDiffLine `json:"lines"`
}

// =============================================================================
// HTTP endpoint suite (full in-process server, per-test tx isolation)
// =============================================================================

// ChunkCurationHTTPSuite exercises the real HTTP surface for chunk curation
// (PATCH /api/chunks/:id, GET .../revisions, GET .../revisions/diff,
// POST .../rollback) through the full in-process server with auth + membership
// middleware. It closes the coverage gap flagged in review: the curation
// handlers had unit tests but no DB-backed endpoint tests.
type ChunkCurationHTTPSuite struct {
	testutil.BaseSuite
}

func TestChunkCurationHTTPSuite(t *testing.T) {
	suite.Run(t, new(ChunkCurationHTTPSuite))
}

func (s *ChunkCurationHTTPSuite) SetupSuite() {
	s.SetDBSuffix("chunk_curation_http")
	s.BaseSuite.SetupSuite()
}

func (s *ChunkCurationHTTPSuite) auth() (testutil.RequestOption, testutil.RequestOption) {
	return testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID)
}

// seedDocChunk inserts a document + chunk owned by the current project, inside
// the per-test transaction. The chunk's revision 1 is seeded by the 00217
// trigger, so revision listing always has at least the original.
func (s *ChunkCurationHTTPSuite) seedDocChunk(text string) (docID, chunkID string) {
	s.T().Helper()
	docID = uuid.NewString()
	chunkID = uuid.NewString()
	s.Require().NoError(testutil.CreateTestDocument(s.Ctx, s.DB(), testutil.TestDocument{
		ID:        docID,
		ProjectID: s.ProjectID,
		Filename:  testutil.StringPtr("curation-http.txt"),
		Content:   testutil.StringPtr("curation content"),
	}))
	s.Require().NoError(testutil.CreateTestChunk(s.Ctx, s.DB(), testutil.TestChunk{
		ID:         chunkID,
		DocumentID: docID,
		ChunkIndex: 0,
		Text:       text,
	}))
	return docID, chunkID
}

func (s *ChunkCurationHTTPSuite) editChunk(chunkID, text string) *testutil.HTTPResponse {
	auth, project := s.auth()
	return s.Client.PATCH("/api/chunks/"+chunkID, auth, project, testutil.WithJSONBody(map[string]any{"text": text}))
}

func (s *ChunkCurationHTTPSuite) listRevisions(chunkID string) *testutil.HTTPResponse {
	auth, project := s.auth()
	return s.Client.GET("/api/chunks/"+chunkID+"/revisions", auth, project)
}

// ---------------------------------------------------------------------------
// Edit
// ---------------------------------------------------------------------------

func (s *ChunkCurationHTTPSuite) TestEditChunkRecordsRevisionAndReEmbed() {
	_, chunkID := s.seedDocChunk("version one")

	resp := s.editChunk(chunkID, "version two")
	s.Require().Equal(http.StatusOK, resp.StatusCode, "edit failed: %s", resp.String())

	var edited curationEditResp
	s.Require().NoError(resp.JSON(&edited))
	s.Equal(chunkID, edited.ChunkID)
	s.Equal(2, edited.Revision, "first edit must produce revision 2")

	// The chunk text is updated and flagged edited.
	var row struct {
		Text            string `bun:"text"`
		Edited          bool   `bun:"edited"`
		CurrentRevision int    `bun:"current_revision"`
	}
	s.Require().NoError(s.DB().NewRaw(
		`SELECT text, edited, current_revision FROM kb.chunks WHERE id = ?`, chunkID).Scan(s.Ctx, &row))
	s.Equal("version two", row.Text)
	s.True(row.Edited)
	s.Equal(2, row.CurrentRevision)

	// Re-embedding is scheduled.
	var jobStatus string
	s.Require().NoError(s.DB().NewRaw(
		`SELECT status FROM kb.chunk_embedding_jobs WHERE chunk_id = ?`, chunkID).Scan(s.Ctx, &jobStatus))
	s.Equal("pending", jobStatus)

	// Revision history is ascending with both texts.
	lresp := s.listRevisions(chunkID)
	s.Require().Equal(http.StatusOK, lresp.StatusCode, "list failed: %s", lresp.String())
	var list curationListResp
	s.Require().NoError(lresp.JSON(&list))
	s.Equal(2, list.TotalCount)
	s.Require().Len(list.Data, 2)
	s.Equal([]int{1, 2}, []int{list.Data[0].Seq, list.Data[1].Seq})
	s.Equal("version one", list.Data[0].Text)
	s.Equal("version two", list.Data[1].Text)
	s.NotEmpty(list.Data[0].CreatedAt)
}

func (s *ChunkCurationHTTPSuite) TestEditChunkRejectsEmptyText() {
	_, chunkID := s.seedDocChunk("original")

	for _, body := range []string{"", "   ", "\n\t "} {
		auth, project := s.auth()
		resp := s.Client.PATCH("/api/chunks/"+chunkID, auth, project,
			testutil.WithJSONBody(map[string]any{"text": body}))
		s.Equal(http.StatusBadRequest, resp.StatusCode, "empty text %q must be rejected: %s", body, resp.String())
	}

	// No revision or text change resulted.
	lresp := s.listRevisions(chunkID)
	var list curationListResp
	s.Require().NoError(lresp.JSON(&list))
	s.Equal(1, list.TotalCount, "rejected edits must not record revisions")
}

func (s *ChunkCurationHTTPSuite) TestEditChunkUnknownIDNotFound() {
	resp := s.editChunk(uuid.NewString(), "new text")
	s.Equal(http.StatusNotFound, resp.StatusCode, "unknown chunk must be 404: %s", resp.String())
}

func (s *ChunkCurationHTTPSuite) TestEditChunkCrossProjectForbidden() {
	foreignProject, _, foreignChunk := s.newForeignDocChunk("foreign secret")

	auth, _ := s.auth()
	resp := s.Client.PATCH("/api/chunks/"+foreignChunk, auth, testutil.WithProjectID(foreignProject),
		testutil.WithJSONBody(map[string]any{"text": "pwned"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project chunk edit must be forbidden: %s", resp.String())

	// The foreign chunk is untouched.
	var text string
	s.Require().NoError(s.DB().NewRaw(`SELECT text FROM kb.chunks WHERE id = ?`, foreignChunk).Scan(s.Ctx, &text))
	s.Equal("foreign secret", text)
}

// ---------------------------------------------------------------------------
// Revisions listing
// ---------------------------------------------------------------------------

func (s *ChunkCurationHTTPSuite) TestListRevisionsUneditedChunkReturnsOriginal() {
	_, chunkID := s.seedDocChunk("only version")

	resp := s.listRevisions(chunkID)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "list failed: %s", resp.String())
	var list curationListResp
	s.Require().NoError(resp.JSON(&list))
	s.Equal(1, list.TotalCount)
	s.Require().Len(list.Data, 1)
	s.Equal(1, list.Data[0].Seq)
	s.Equal("only version", list.Data[0].Text)
}

func (s *ChunkCurationHTTPSuite) TestListRevisionsUnknownIDNotFound() {
	auth, project := s.auth()
	resp := s.Client.GET("/api/chunks/"+uuid.NewString()+"/revisions", auth, project)
	s.Equal(http.StatusNotFound, resp.StatusCode, "unknown chunk revisions must be 404: %s", resp.String())
}

func (s *ChunkCurationHTTPSuite) TestListRevisionsCrossProjectForbidden() {
	foreignProject, _, foreignChunk := s.newForeignDocChunk("foreign secret")
	auth, _ := s.auth()
	resp := s.Client.GET("/api/chunks/"+foreignChunk+"/revisions",
		auth, testutil.WithProjectID(foreignProject))
	s.Equal(http.StatusForbidden, resp.StatusCode, "cross-project revisions must be 403: %s", resp.String())
}

// ---------------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------------

func (s *ChunkCurationHTTPSuite) TestDiffNonAdjacentRevisions() {
	_, chunkID := s.seedDocChunk("line a\nline b")
	s.Require().Equal(http.StatusOK, s.editChunk(chunkID, "line a\nline b\nline c").StatusCode)
	s.Require().Equal(http.StatusOK, s.editChunk(chunkID, "line a\nline c\nline d").StatusCode)

	auth, project := s.auth()
	resp := s.Client.GET(fmt.Sprintf("/api/chunks/%s/revisions/diff?from=1&to=3", chunkID), auth, project)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "diff failed: %s", resp.String())
	var diff curationDiffResp
	s.Require().NoError(resp.JSON(&diff))
	s.Equal(1, diff.From)
	s.Equal(3, diff.To)
	s.NotEmpty(diff.Lines)

	// "line b" is present only in revision 1 → must be a removal.
	seenRemoveB := false
	for _, l := range diff.Lines {
		if l.Text == "line b" {
			s.Equal("remove", l.Type, "line b should be removed when diffing 1→3")
			seenRemoveB = true
		}
	}
	s.True(seenRemoveB, "diff must include the removed intermediate line: %+v", diff.Lines)
}

func (s *ChunkCurationHTTPSuite) TestDiffIdenticalRevisionsAllEqual() {
	_, chunkID := s.seedDocChunk("same\ntext")
	s.Require().Equal(http.StatusOK, s.editChunk(chunkID, "same\ntext").StatusCode)

	auth, project := s.auth()
	resp := s.Client.GET(fmt.Sprintf("/api/chunks/%s/revisions/diff?from=1&to=2", chunkID), auth, project)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "identical diff failed: %s", resp.String())
	var diff curationDiffResp
	s.Require().NoError(resp.JSON(&diff))
	s.NotEmpty(diff.Lines)
	for _, l := range diff.Lines {
		s.Equal("equal", l.Type, "identical revisions must produce only equal lines: %+v", l)
	}
}

func (s *ChunkCurationHTTPSuite) TestDiffRejectsBadParams() {
	_, chunkID := s.seedDocChunk("x")
	auth, project := s.auth()

	for _, q := range []string{"", "?from=0&to=1", "?from=1&to=0", "?from=x&to=1", "?from=1", "?from=1&to=99"} {
		resp := s.Client.GET("/api/chunks/"+chunkID+"/revisions/diff"+q, auth, project)
		s.Contains([]int{http.StatusBadRequest, http.StatusNotFound}, resp.StatusCode,
			"query %q must be rejected, got %d: %s", q, resp.StatusCode, resp.String())
	}
}

// ---------------------------------------------------------------------------
// Rollback
// ---------------------------------------------------------------------------

func (s *ChunkCurationHTTPSuite) rollback(chunkID string, seq int) *testutil.HTTPResponse {
	auth, project := s.auth()
	return s.Client.POST("/api/chunks/"+chunkID+"/rollback", auth, project,
		testutil.WithJSONBody(map[string]any{"seq": seq}))
}

func (s *ChunkCurationHTTPSuite) TestRollbackRestoresPriorTextAsNewRevision() {
	_, chunkID := s.seedDocChunk("v1")
	s.Require().Equal(http.StatusOK, s.editChunk(chunkID, "v2").StatusCode)
	s.Require().Equal(http.StatusOK, s.editChunk(chunkID, "v3").StatusCode)

	resp := s.rollback(chunkID, 1)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "rollback failed: %s", resp.String())
	var rb curationEditResp
	s.Require().NoError(resp.JSON(&rb))
	s.Equal(4, rb.Revision, "rollback must append a new revision")

	var text string
	s.Require().NoError(s.DB().NewRaw(`SELECT text FROM kb.chunks WHERE id = ?`, chunkID).Scan(s.Ctx, &text))
	s.Equal("v1", text, "chunk text must be restored to the target revision")

	var list curationListResp
	s.Require().NoError(s.listRevisions(chunkID).JSON(&list))
	s.Require().Len(list.Data, 4)
	s.Equal(4, list.Data[3].Seq)
	s.Equal("v1", list.Data[3].Text, "the rollback itself is recorded as a new revision")
}

func (s *ChunkCurationHTTPSuite) TestRollbackToCurrentIsNoOp() {
	_, chunkID := s.seedDocChunk("current")

	resp := s.rollback(chunkID, 1)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "rollback-to-current failed: %s", resp.String())
	var rb curationEditResp
	s.Require().NoError(resp.JSON(&rb))
	s.Equal(1, rb.Revision, "rollback to the current revision reports the current revision")

	var list curationListResp
	s.Require().NoError(s.listRevisions(chunkID).JSON(&list))
	s.Equal(1, list.TotalCount, "rollback to current must not record a new revision")
}

func (s *ChunkCurationHTTPSuite) TestRollbackUnknownRevisionNotFound() {
	_, chunkID := s.seedDocChunk("v1")
	resp := s.rollback(chunkID, 99)
	s.Equal(http.StatusNotFound, resp.StatusCode, "unknown revision rollback must be 404: %s", resp.String())
}

func (s *ChunkCurationHTTPSuite) TestRollbackRejectsZeroSeq() {
	_, chunkID := s.seedDocChunk("v1")
	resp := s.rollback(chunkID, 0)
	s.Equal(http.StatusBadRequest, resp.StatusCode, "seq 0 must be rejected: %s", resp.String())
}

// ---------------------------------------------------------------------------
// Cross-project fixture
// ---------------------------------------------------------------------------

// newForeignDocChunk creates an org the admin user is NOT a member of, a project
// owned by it, and a document + chunk in it. Returns (projectID, docID, chunkID).
func (s *ChunkCurationHTTPSuite) newForeignDocChunk(text string) (string, string, string) {
	s.T().Helper()
	orgB := uuid.NewString()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	projectB := uuid.NewString()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))

	docID := uuid.NewString()
	chunkID := uuid.NewString()
	s.Require().NoError(testutil.CreateTestDocument(s.Ctx, s.DB(), testutil.TestDocument{
		ID:        docID,
		ProjectID: projectB,
		Filename:  testutil.StringPtr("foreign.txt"),
		Content:   testutil.StringPtr("foreign content"),
	}))
	s.Require().NoError(testutil.CreateTestChunk(s.Ctx, s.DB(), testutil.TestChunk{
		ID:         chunkID,
		DocumentID: docID,
		ChunkIndex: 0,
		Text:       text,
	}))
	return projectB, docID, chunkID
}

// =============================================================================
// Service suite (real pool connections: genuine row-lock concurrency)
// =============================================================================

// ChunkCurationServiceSuite exercises chunks.Service directly over a real
// connection pool so row locking and revision-sequence uniqueness are genuinely
// concurrent (unlike the per-test-transaction HTTP suite). It is the adversarial
// lane for optimistic-concurrency and bounded-output behaviour.
type ChunkCurationServiceSuite struct {
	suite.Suite
	testDB  *testutil.TestDB
	ctx     context.Context
	log     *slog.Logger
	svc     *chunks.Service
	repo    *chunks.Repository
	orgID   string
	project string
	docID   string
}

func TestChunkCurationServiceSuite(t *testing.T) {
	suite.Run(t, new(ChunkCurationServiceSuite))
}

func (s *ChunkCurationServiceSuite) SetupSuite() {
	s.ctx = context.Background()
	s.log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	s.testDB = testutil.SetupTestDBOrFail(s.T(), s.ctx, "chunk_curation_svc")
	s.repo = chunks.NewRepository(s.testDB.DB, s.log)
	s.svc = chunks.NewService(s.repo, s.log)
}

func (s *ChunkCurationServiceSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *ChunkCurationServiceSuite) SetupTest() {
	s.Require().NoError(testutil.TruncateTables(s.ctx, s.testDB.DB))
	s.Require().NoError(testutil.SetupTestFixtures(s.ctx, s.testDB.DB))

	s.orgID = uuid.NewString()
	s.project = uuid.NewString()
	s.Require().NoError(testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.project))

	s.docID = s.seedDoc(uuid.NewString(), s.project)
}

func (s *ChunkCurationServiceSuite) seedDoc(docID, projectID string) string {
	s.T().Helper()
	s.Require().NoError(testutil.CreateTestDocument(s.ctx, s.testDB.DB, testutil.TestDocument{
		ID:        docID,
		ProjectID: projectID,
		Filename:  testutil.StringPtr("svc.txt"),
		Content:   testutil.StringPtr("svc content"),
	}))
	return docID
}

func (s *ChunkCurationServiceSuite) seedChunk(text string) (uuid.UUID, uuid.UUID) {
	s.T().Helper()
	chunkID := uuid.NewString()
	s.Require().NoError(testutil.CreateTestChunk(s.ctx, s.testDB.DB, testutil.TestChunk{
		ID:         chunkID,
		DocumentID: s.docID,
		ChunkIndex: 0,
		Text:       text,
	}))
	return uuid.MustParse(s.project), uuid.MustParse(chunkID)
}

func (s *ChunkCurationServiceSuite) revisionTexts(chunkID uuid.UUID) []int {
	s.T().Helper()
	var seqs []int
	s.Require().NoError(s.testDB.DB.NewRaw(
		`SELECT seq FROM kb.chunk_revisions WHERE chunk_id = ? ORDER BY seq ASC`, chunkID).Scan(s.ctx, &seqs))
	return seqs
}

func (s *ChunkCurationServiceSuite) revisionCount(chunkID uuid.UUID) int {
	s.T().Helper()
	n, err := s.testDB.DB.NewSelect().
		TableExpr("kb.chunk_revisions").
		Where("chunk_id = ?", chunkID).
		Count(s.ctx)
	s.Require().NoError(err)
	return n
}

// TestConcurrentEditsSerializeAndNumberContiguously is the adversarial
// optimistic-concurrency probe: N goroutines edit the same chunk at once. The
// FOR UPDATE row lock must serialize them so every edit gets a distinct,
// contiguous revision and the unique (chunk_id, seq) constraint is never
// violated.
func (s *ChunkCurationServiceSuite) TestConcurrentEditsSerializeAndNumberContiguously() {
	const n = 8
	projectID, chunkID := s.seedChunk("original")

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		revs []int
		errs []error
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := s.svc.EditChunk(s.ctx, projectID, chunkID, fmt.Sprintf("edit-%02d", i))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			revs = append(revs, resp.Revision)
		}(i)
	}
	wg.Wait()

	s.Empty(errs, "concurrent edits must not fail: %v", errs)
	s.Len(revs, n)

	seen := map[int]bool{}
	for _, r := range revs {
		s.False(seen[r], "duplicate revision number %d returned by concurrent edits", r)
		seen[r] = true
	}
	for r := 2; r <= n+1; r++ {
		s.True(seen[r], "missing revision %d; got %v", r, revs)
	}

	seqs := s.revisionTexts(chunkID)
	s.Len(seqs, n+1, "original + one revision per edit")
	for i, seq := range seqs {
		s.Equal(i+1, seq, "revision seqs must be contiguous from 1: %v", seqs)
	}

	var current int
	s.Require().NoError(s.testDB.DB.NewRaw(
		`SELECT current_revision FROM kb.chunks WHERE id = ?`, chunkID).Scan(s.ctx, &current))
	s.Equal(n+1, current, "chunk current_revision must match the last edit")
}

// TestEditChunkCrossProjectForbiddenService pins authz at the repository layer:
// a chunk in another project must not be editable/readable through the target
// project id.
func (s *ChunkCurationServiceSuite) TestEditChunkCrossProjectForbiddenService() {
	// Foreign project + doc + chunk.
	foreignProject := uuid.NewString()
	foreignOrg := uuid.NewString()
	s.Require().NoError(testutil.SetupFullTestProject(s.ctx, s.testDB.DB, foreignOrg, foreignProject))
	foreignDoc := uuid.NewString()
	s.Require().NoError(testutil.CreateTestDocument(s.ctx, s.testDB.DB, testutil.TestDocument{
		ID: foreignDoc, ProjectID: foreignProject, Filename: testutil.StringPtr("f.txt"),
		Content: testutil.StringPtr("f"),
	}))
	foreignChunk := uuid.NewString()
	s.Require().NoError(testutil.CreateTestChunk(s.ctx, s.testDB.DB, testutil.TestChunk{
		ID: foreignChunk, DocumentID: foreignDoc, ChunkIndex: 0, Text: "foreign",
	}))

	ownProject := uuid.MustParse(s.project)
	fchunk := uuid.MustParse(foreignChunk)

	_, err := s.svc.EditChunk(s.ctx, ownProject, fchunk, "pwned")
	s.Error(err, "editing a foreign project's chunk must fail")

	_, err = s.svc.ListRevisions(s.ctx, ownProject, fchunk)
	s.Error(err, "listing a foreign project's revisions must fail")

	_, err = s.svc.RollbackChunk(s.ctx, ownProject, fchunk, 1)
	s.Error(err, "rolling back a foreign project's chunk must fail")

	_, err = s.svc.DiffRevisions(s.ctx, ownProject, fchunk, 1, 1)
	s.Error(err, "diffing a foreign project's revisions must fail")

	// The foreign chunk is unchanged.
	var text string
	s.Require().NoError(s.testDB.DB.NewRaw(`SELECT text FROM kb.chunks WHERE id = ?`, foreignChunk).Scan(s.ctx, &text))
	s.Equal("foreign", text)
}

// TestRollbackAfterFurtherEdits verifies a rollback target survives later edits
// and that the rollback is recorded as a new revision.
func (s *ChunkCurationServiceSuite) TestRollbackAfterFurtherEdits() {
	projectID, chunkID := s.seedChunk("rev1")
	_, err := s.svc.EditChunk(s.ctx, projectID, chunkID, "rev2")
	s.Require().NoError(err)
	_, err = s.svc.EditChunk(s.ctx, projectID, chunkID, "rev3")
	s.Require().NoError(err)
	_, err = s.svc.EditChunk(s.ctx, projectID, chunkID, "rev4")
	s.Require().NoError(err)

	rb, err := s.svc.RollbackChunk(s.ctx, projectID, chunkID, 2)
	s.Require().NoError(err)
	s.Equal(5, rb.Revision, "rollback after three edits must append revision 5")

	var text string
	s.Require().NoError(s.testDB.DB.NewRaw(`SELECT text FROM kb.chunks WHERE id = ?`, chunkID).Scan(s.ctx, &text))
	s.Equal("rev2", text)

	seqs := s.revisionTexts(chunkID)
	s.Equal([]int{1, 2, 3, 4, 5}, seqs)

	// A later edit continues the sequence.
	ed, err := s.svc.EditChunk(s.ctx, projectID, chunkID, "rev6")
	s.Require().NoError(err)
	s.Equal(6, ed.Revision)
}

// TestListRevisionsBoundedAtCap inserts more revisions than the cap and asserts
// the response is the newest cap, strictly ascending — never the oldest, never
// unordered. Diff against an old revision outside the window still works.
//
//nolint:dupl // shares seed/bulk-insert shape with the diff-too-large test.
func (s *ChunkCurationServiceSuite) TestListRevisionsBoundedAtCap() {
	const total = 506
	projectID, chunkID := s.seedChunk("rev-1")

	// Insert revisions 2..506 directly (fast, deterministic).
	_, err := s.testDB.DB.NewRaw(`
		INSERT INTO kb.chunk_revisions (chunk_id, seq, text)
		SELECT ?, g, 'rev-' || g FROM generate_series(2, ?) g`, chunkID, total).Exec(s.ctx)
	s.Require().NoError(err)
	_, err = s.testDB.DB.NewRaw(
		`UPDATE kb.chunks SET text = ?, current_revision = ? WHERE id = ?`, "rev-506", total, chunkID).Exec(s.ctx)
	s.Require().NoError(err)

	resp, err := s.svc.ListRevisions(s.ctx, projectID, chunkID)
	s.Require().NoError(err)
	s.Len(resp.Data, 500, "listing must be capped at 500")
	s.Equal(500, resp.TotalCount)

	// Newest 500 of 506 = seq 7..506, ascending.
	s.Equal(7, resp.Data[0].Seq)
	s.Equal(total, resp.Data[len(resp.Data)-1].Seq)
	for i := 1; i < len(resp.Data); i++ {
		s.Greater(resp.Data[i].Seq, resp.Data[i-1].Seq, "revisions must be strictly ascending")
	}

	// A revision outside the listing window is still diffable by seq.
	diff, err := s.svc.DiffRevisions(s.ctx, projectID, chunkID, 1, total)
	s.Require().NoError(err)
	s.Equal(1, diff.From)
	s.Equal(total, diff.To)
}

// TestEditChunkOversizedRejectedNoMutation pins the server-side size cap: an
// over-limit edit is rejected and records no revision.
func (s *ChunkCurationServiceSuite) TestEditChunkOversizedRejectedNoMutation() {
	projectID, chunkID := s.seedChunk("small")

	oversized := strings.Repeat("a", chunks.MaxChunkTextBytes+1)
	_, err := s.svc.EditChunk(s.ctx, projectID, chunkID, oversized)
	s.Require().Error(err)
	s.Contains(err.Error(), "exceeds maximum", "want the size-cap error, got %v", err)

	s.Equal(1, s.revisionCount(chunkID), "rejected edit must not record a revision")
	var text string
	s.Require().NoError(s.testDB.DB.NewRaw(`SELECT text FROM kb.chunks WHERE id = ?`, chunkID).Scan(s.ctx, &text))
	s.Equal("small", text)
}

// TestDiffTooLargeRejected pins the OOM guard: two large revisions are refused
// with a validation error instead of allocating an unbounded LCS matrix.
//
//nolint:dupl // intentionally mirrors the bounded-list shape.
func (s *ChunkCurationServiceSuite) TestDiffTooLargeRejected() {
	big1 := strings.Repeat("alpha line\n", 2100) // 2101 diff lines
	big2 := strings.Repeat("beta line\n", 2100)
	projectID, chunkID := s.seedChunk(big1)

	_, err := s.testDB.DB.NewRaw(
		`INSERT INTO kb.chunk_revisions (chunk_id, seq, text) VALUES (?, 2, ?)`, chunkID, big2).Exec(s.ctx)
	s.Require().NoError(err)

	_, err = s.svc.DiffRevisions(s.ctx, projectID, chunkID, 1, 2)
	s.Require().Error(err, "diff over the cell cap must be rejected")
	s.Contains(err.Error(), "too large", "want the diff-size error, got %v", err)
}

// TestEditChunkEnqueuesSingleActiveJob pins the idempotent enqueue: repeated
// edits of the same chunk do not accumulate duplicate active embedding jobs.
func (s *ChunkCurationServiceSuite) TestEditChunkEnqueuesSingleActiveJob() {
	projectID, chunkID := s.seedChunk("v1")
	for i := 0; i < 5; i++ {
		_, err := s.svc.EditChunk(s.ctx, projectID, chunkID, fmt.Sprintf("v%d", i+2))
		s.Require().NoError(err)
	}

	var active int
	s.Require().NoError(s.testDB.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.chunk_embedding_jobs WHERE chunk_id = ? AND status IN ('pending','processing')`,
		chunkID).Scan(s.ctx, &active))
	s.Equal(1, active, "edits must not accumulate duplicate active embedding jobs")
}
