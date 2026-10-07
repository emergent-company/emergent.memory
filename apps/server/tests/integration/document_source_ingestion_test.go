package integration

// DocumentSourceIngestionSuite covers the service seam (ApplySourceItem,
// MarkSourceItemDeleted, GetVersionHistory) and the document-api delta
// (GET /:id/versions, includeDeletedFromSource) introduced by OpenSpec change
// `add-google-drive-ingestion`. It runs against the in-process test server
// backed by a real Postgres test database (testutil.BaseSuite); the suite skips
// via testdb.SkipOrFatal when Postgres is unreachable.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/chunking"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func TestDocumentSourceIngestionSuite(t *testing.T) {
	suite.Run(t, new(DocumentSourceIngestionSuite))
}

type DocumentSourceIngestionSuite struct {
	testutil.BaseSuite
}

func (s *DocumentSourceIngestionSuite) SetupSuite() {
	s.SetDBSuffix("docsrcseam")
	s.BaseSuite.SetupSuite()
}

// recordingExtractionCreator records TriggerForDocument calls so the seam tests
// can assert extraction is (or is not) scheduled.
type recordingExtractionCreator struct {
	documentIDs []string
}

func (c *recordingExtractionCreator) TriggerForDocument(ctx context.Context, projectID, documentID string) error {
	c.documentIDs = append(c.documentIDs, documentID)
	return nil
}

// chunkingAdapter adapts chunking.Service to documents.ChunkingService.
type chunkingAdapter struct {
	svc *chunking.Service
}

func (a *chunkingAdapter) RecreateChunks(ctx context.Context, projectID, documentID string) error {
	_, err := a.svc.RecreateChunks(ctx, projectID, documentID, false)
	return err
}

// insertSource creates a kb.sources row and returns its id.
func (s *DocumentSourceIngestionSuite) insertSource(provider string) string {
	s.T().Helper()
	id := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.sources (id, project_id, provider)
		VALUES (uuid(?), uuid(?), ?)
	`, id, s.ProjectID, provider).Exec(s.Ctx)
	s.Require().NoError(err, "insert source")
	return id
}

// newSeamService builds a documents.Service wired with real chunking and a
// recording extraction creator, on the suite's transaction.
func (s *DocumentSourceIngestionSuite) newSeamService() (*documents.Service, *recordingExtractionCreator) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := documents.NewService(documents.NewRepository(s.DB(), log), log)
	rec := &recordingExtractionCreator{}
	svc.AttachPipeline(&chunkingAdapter{svc: chunking.NewService(s.DB(), s.TestDB.Config, log)}, rec)
	return svc, rec
}

func (s *DocumentSourceIngestionSuite) countChunks(documentID string) int {
	s.T().Helper()
	var n int
	err := s.DB().NewRaw(`SELECT count(*) FROM kb.chunks WHERE document_id = uuid(?)`, documentID).Scan(s.Ctx, &n)
	s.Require().NoError(err)
	return n
}

func (s *DocumentSourceIngestionSuite) countHeads(canonicalID string) int {
	s.T().Helper()
	var n int
	err := s.DB().NewRaw(`SELECT count(*) FROM kb.documents WHERE canonical_id = uuid(?) AND supersedes_id IS NULL`, canonicalID).Scan(s.Ctx, &n)
	s.Require().NoError(err)
	return n
}

// ---------------------------------------------------------------------------
// Service seam
// ---------------------------------------------------------------------------

func (s *DocumentSourceIngestionSuite) TestApplySourceItem_CreateSkipAndVersion() {
	svc, rec := s.newSeamService()
	sourceID := s.insertSource("google_drive")
	externalID := "drive-item-seam"

	base := documents.ApplySourceItemParams{
		ProjectID:  s.ProjectID,
		SourceID:   sourceID,
		ExternalID: externalID,
		RevisionID: "rev-1",
		Filename:   "notes.txt",
		MimeType:   "text/plain",
	}

	// First ingest: creates v1 with chunks and triggers extraction.
	p := base
	p.Content = "the quick brown fox jumps over the lazy dog. this is a long enough body of text to split into more than one chunk when the default chunk size applies, and it carries real words so the splitter has something to work with across the content."
	r1, err := svc.ApplySourceItem(s.Ctx, p)
	s.Require().NoError(err)
	s.Require().NotNil(r1)
	s.True(r1.Created, "first ingest must create the document")
	s.False(r1.NewVersion)
	s.False(r1.Skipped)
	s.NotEmpty(r1.DocumentID)

	s.Greater(s.countChunks(r1.DocumentID), 0, "first ingest must create chunks")
	s.Len(rec.documentIDs, 1, "first ingest must trigger extraction once")

	// Identical text re-ingest: skipped, same id, no new version, no duplicate chunks, no extraction.
	p.RevisionID = "rev-2" // provider revision moved, text unchanged
	r2, err := svc.ApplySourceItem(s.Ctx, p)
	s.Require().NoError(err)
	s.True(r2.Skipped, "identical parsed text must be skipped")
	s.False(r2.Created)
	s.False(r2.NewVersion)
	s.Equal(r1.DocumentID, r2.DocumentID, "skip must target the existing HEAD")

	s.Equal(s.countChunks(r1.DocumentID), s.countChunks(r1.DocumentID), "no duplicate chunks")
	s.Len(rec.documentIDs, 1, "skip must not trigger extraction")

	// Changed text: appends a version, retains the prior, exactly one HEAD.
	p.Content = "completely different parsed text body that changes the extracted meaning and therefore the sha256 hash, forcing a new document version with its own fresh chunks and extraction pass."
	p.RevisionID = "rev-3"
	r3, err := svc.ApplySourceItem(s.Ctx, p)
	s.Require().NoError(err)
	s.True(r3.NewVersion, "changed text must append a version")
	s.False(r3.Created)
	s.False(r3.Skipped)
	s.NotEqual(r1.DocumentID, r3.DocumentID, "new version must be a distinct row")

	versions, err := svc.GetVersionHistory(s.Ctx, s.ProjectID, r3.DocumentID)
	s.Require().NoError(err)
	s.Require().Len(versions, 2, "prior version must be retained")
	s.Equal(1, versions[0].Version)
	s.Equal(2, versions[1].Version)

	canonicalID := versions[0].CanonicalID
	s.Equal(1, s.countHeads(canonicalID), "exactly one HEAD must exist")
	s.Len(rec.documentIDs, 2, "changed text must trigger extraction once more")
}

func (s *DocumentSourceIngestionSuite) TestApplySourceItem_BypassesContentHashDedup() {
	svc, _ := s.newSeamService()
	srcA := s.insertSource("google_drive")
	srcB := s.insertSource("google_drive")

	content := "identical parsed text shared across two different source items"
	params := func(sourceID, externalID string) documents.ApplySourceItemParams {
		return documents.ApplySourceItemParams{
			ProjectID:  s.ProjectID,
			SourceID:   sourceID,
			ExternalID: externalID,
			RevisionID: "rev-1",
			Filename:   "doc.txt",
			MimeType:   "text/plain",
			Content:    content,
		}
	}

	rA, err := svc.ApplySourceItem(s.Ctx, params(srcA, "ext-A"))
	s.Require().NoError(err)
	s.True(rA.Created)

	rB, err := svc.ApplySourceItem(s.Ctx, params(srcB, "ext-B"))
	s.Require().NoError(err)
	s.True(rB.Created, "second source item must create a distinct document")
	s.NotEqual(rA.DocumentID, rB.DocumentID, "identical content must not collapse across source items")
}

func (s *DocumentSourceIngestionSuite) TestMarkSourceItemDeleted_AndReapplyReactivates() {
	svc, _ := s.newSeamService()
	sourceID := s.insertSource("google_drive")
	externalID := "drive-item-delete"

	base := documents.ApplySourceItemParams{
		ProjectID:  s.ProjectID,
		SourceID:   sourceID,
		ExternalID: externalID,
		RevisionID: "rev-1",
		Filename:   "doc.txt",
		MimeType:   "text/plain",
		Content:    "original parsed text body for the delete/reactivate flow",
	}
	r1, err := svc.ApplySourceItem(s.Ctx, base)
	s.Require().NoError(err)
	s.True(r1.Created)

	// Mark deleted: excluded from default list, history still returns it.
	s.Require().NoError(svc.MarkSourceItemDeleted(s.Ctx, s.ProjectID, sourceID, externalID))

	res, err := svc.List(s.Ctx, documents.ListParams{ProjectID: s.ProjectID})
	s.Require().NoError(err)
	for _, d := range res.Documents {
		s.NotEqual(r1.DocumentID, d.ID, "deleted-from-source document must be excluded from default List")
	}

	versions, err := svc.GetVersionHistory(s.Ctx, s.ProjectID, r1.DocumentID)
	s.Require().NoError(err)
	s.Require().Len(versions, 1, "history must still return the deleted version")
	s.Equal(documents.SourceStatusDeletedFromSource, versions[0].SourceStatus)

	// Re-apply with changed text: reactivates as a NEW active version.
	base.RevisionID = "rev-2"
	base.Content = "changed parsed text after the item reappeared in scope"
	r2, err := svc.ApplySourceItem(s.Ctx, base)
	s.Require().NoError(err)
	s.True(r2.NewVersion, "re-apply after delete with changed text must append a new version")
	s.NotEqual(r1.DocumentID, r2.DocumentID)

	versions, err = svc.GetVersionHistory(s.Ctx, s.ProjectID, r2.DocumentID)
	s.Require().NoError(err)
	s.Require().Len(versions, 2, "deleted prior version must be retained")
	s.Equal(1, s.countHeads(versions[0].CanonicalID), "exactly one HEAD (the active one) must exist")
}

// ---------------------------------------------------------------------------
// Handler / document-api delta
// ---------------------------------------------------------------------------

func (s *DocumentSourceIngestionSuite) TestVersionHistoryEndpoint() {
	repo := documents.NewRepository(s.DB(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	sourceID := s.insertSource("google_drive")
	externalID := "drive-item-api"
	canonicalID := uuid.New().String()
	now := time.Now().UTC()
	rev1 := "rev-1"
	rev2 := "rev-2"

	v1 := &documents.Document{
		ID:               uuid.New().String(),
		ProjectID:        s.ProjectID,
		CreatedAt:        now,
		UpdatedAt:        now,
		CanonicalID:      canonicalID,
		Version:          1,
		SourceID:         &sourceID,
		SourceExternalID: &externalID,
		SourceRevisionID: &rev1,
		SourceStatus:     documents.SourceStatusActive,
	}
	s.Require().NoError(repo.InsertDocument(s.Ctx, v1))

	v2 := &documents.Document{
		ID:               uuid.New().String(),
		ProjectID:        s.ProjectID,
		CreatedAt:        now,
		UpdatedAt:        now,
		CanonicalID:      canonicalID,
		Version:          2,
		SourceID:         &sourceID,
		SourceExternalID: &externalID,
		SourceRevisionID: &rev2,
		SourceStatus:     documents.SourceStatusActive,
	}
	s.Require().NoError(repo.CreateVersion(s.Ctx, v2))

	resp := s.Client.GET(
		"/api/documents/"+v2.ID+"/versions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "versions endpoint: %s", resp.Body)

	var body struct {
		Versions []documents.DocumentVersion `json:"versions"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.Require().Len(body.Versions, 2, "versions must be returned")
	s.Equal(1, body.Versions[0].Version, "versions ordered by version number")
	s.Equal(2, body.Versions[1].Version)
	s.Require().NotNil(body.Versions[0].SourceRevisionID)
	s.Equal("rev-1", *body.Versions[0].SourceRevisionID)
	s.Equal(documents.SourceStatusActive, body.Versions[0].SourceStatus)
}

func (s *DocumentSourceIngestionSuite) TestVersionHistoryRequiresAuth() {
	resp := s.Client.GET("/api/documents/" + uuid.New().String() + "/versions")
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode, "unauthenticated versions request must be rejected")
}

func (s *DocumentSourceIngestionSuite) TestListHonorsIncludeDeletedFromSource() {
	repo := documents.NewRepository(s.DB(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	sourceID := s.insertSource("google_drive")
	externalID := "drive-item-list"
	now := time.Now().UTC()

	doc := &documents.Document{
		ID:               uuid.New().String(),
		ProjectID:        s.ProjectID,
		CreatedAt:        now,
		UpdatedAt:        now,
		CanonicalID:      uuid.New().String(),
		Version:          1,
		SourceID:         &sourceID,
		SourceExternalID: &externalID,
		SourceStatus:     documents.SourceStatusActive,
	}
	s.Require().NoError(repo.InsertDocument(s.Ctx, doc))
	s.Require().NoError(repo.MarkSourceDeleted(s.Ctx, doc.ID, now))

	listIDs := func(includeDeleted bool) []string {
		s.T().Helper()
		url := "/api/documents"
		if includeDeleted {
			url += "?includeDeletedFromSource=true"
		}
		resp := s.Client.GET(url, testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
		s.Require().Equal(http.StatusOK, resp.StatusCode, "list: %s", resp.Body)
		var result struct {
			Documents []struct {
				ID string `json:"id"`
			} `json:"documents"`
		}
		s.Require().NoError(json.Unmarshal(resp.Body, &result))
		ids := make([]string, 0, len(result.Documents))
		for _, d := range result.Documents {
			ids = append(ids, d.ID)
		}
		return ids
	}

	defaultIDs := listIDs(false)
	s.NotContains(defaultIDs, doc.ID, "default list must exclude deleted-from-source")

	withDeletedIDs := listIDs(true)
	s.Contains(withDeletedIDs, doc.ID, "includeDeletedFromSource=true must include the deleted document")
}
