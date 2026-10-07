package integration

// DocumentSourceVersionsSuite covers the data-layer foundation of OpenSpec change
// `add-google-drive-ingestion`: the `kb.sources` / `kb.source_sync_jobs` tables
// and the `kb.documents` versioning + attribution columns added by migration
// `00226_document_sources_versions.sql`, plus the version-aware repository
// methods on `domain/documents`.
//
// It runs against the in-process test server backed by a real Postgres test
// database (testutil.BaseSuite). If Postgres is unreachable the suite skips via
// testdb.SkipOrFatal; the package still compiles.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

func TestDocumentSourceVersionsSuite(t *testing.T) {
	suite.Run(t, new(DocumentSourceVersionsSuite))
}

type DocumentSourceVersionsSuite struct {
	testutil.BaseSuite
}

func (s *DocumentSourceVersionsSuite) SetupSuite() {
	s.SetDBSuffix("docsrcver")
	s.BaseSuite.SetupSuite()
}

// insertSource creates a kb.sources row and returns its id.
func (s *DocumentSourceVersionsSuite) insertSource(provider string) string {
	s.T().Helper()
	id := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.sources (id, project_id, provider)
		VALUES (uuid(?), uuid(?), ?)
	`, id, s.ProjectID, provider).Exec(s.Ctx)
	s.Require().NoError(err, "insert source")
	return id
}

// insertSourceJob creates a kb.source_sync_jobs row and returns its id.
func (s *DocumentSourceVersionsSuite) insertSourceJob(sourceID, status string) string {
	s.T().Helper()
	id := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.source_sync_jobs (id, project_id, source_id, status)
		VALUES (uuid(?), uuid(?), uuid(?), ?)
	`, id, s.ProjectID, sourceID, status).Exec(s.Ctx)
	s.Require().NoError(err, "insert source sync job")
	return id
}

// insertDocumentRaw inserts a kb.documents row with explicit versioning /
// attribution columns, mirroring what the ingestion path writes.
func (s *DocumentSourceVersionsSuite) insertDocumentRaw(db bun.IDB, id, canonicalID string, version int, supersedesID, sourceID, externalID *string) error {
	s.T().Helper()
	_, err := db.NewRaw(`
		INSERT INTO kb.documents
			(id, project_id, canonical_id, version, supersedes_id, source_id, source_external_id)
		VALUES (uuid(?), uuid(?), uuid(?), ?, uuid(?), uuid(?), ?)
	`, id, s.ProjectID, canonicalID, version, supersedesID, sourceID, externalID).Exec(s.Ctx)
	return err
}

// TestBackfill_CanonicalIDAndVersionPopulated verifies that a document created
// without an explicit canonical_id / version gets a non-null canonical_id and
// version 1 from the migration's defaults/backfill.
func (s *DocumentSourceVersionsSuite) TestBackfill_CanonicalIDAndVersionPopulated() {
	docID := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.documents (id, project_id)
		VALUES (uuid(?), uuid(?))
	`, docID, s.ProjectID).Exec(s.Ctx)
	s.Require().NoError(err)

	var canonicalID string
	var version int
	err = s.DB().NewRaw(`
		SELECT canonical_id::text, version FROM kb.documents WHERE id = uuid(?)
	`, docID).Scan(s.Ctx, &canonicalID, &version)
	s.Require().NoError(err)
	s.NotEmpty(canonicalID, "canonical_id must be backfilled/populated")
	s.Equal(1, version, "version must default to 1")
}

// TestHeadPartialUniqueIndex verifies exactly one HEAD (supersedes_id IS NULL)
// per (source_id, source_external_id), while a version row (supersedes_id set)
// may share the external id.
func (s *DocumentSourceVersionsSuite) TestHeadPartialUniqueIndex() {
	sourceID := s.insertSource("google_drive")
	externalID := "drive-item-1"
	canonicalID := uuid.New().String()

	headID := uuid.New().String()
	err := s.insertDocumentRaw(s.DB(), headID, canonicalID, 1, nil, &sourceID, &externalID)
	s.Require().NoError(err, "first HEAD insert")

	// A second HEAD for the same (source_id, source_external_id) must violate
	// the partial unique index. Run inside a savepoint so the constraint
	// violation does not abort the enclosing test transaction.
	secondHeadID := uuid.New().String()
	err = s.DB().RunInTx(s.Ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return s.insertDocumentRaw(tx, secondHeadID, canonicalID, 2, nil, &sourceID, &externalID)
	})
	s.Require().Error(err, "second HEAD must violate the partial unique index")
	s.True(pgutils.IsUniqueViolation(err), "expected unique violation, got: %v", err)

	// A version row with supersedes_id set (pointing at the prior HEAD) succeeds.
	versionID := uuid.New().String()
	err = s.insertDocumentRaw(s.DB(), versionID, canonicalID, 2, &headID, &sourceID, &externalID)
	s.Require().NoError(err, "version row with supersedes_id set must succeed")
}

// TestAdmissionPartialUniqueIndex verifies at most one active
// (pending/processing) sync job per source, and that a terminal status frees the
// slot.
func (s *DocumentSourceVersionsSuite) TestAdmissionPartialUniqueIndex() {
	sourceID := s.insertSource("google_drive")

	job1 := s.insertSourceJob(sourceID, "pending")

	// A second pending job for the same source is rejected (inside a savepoint
	// so the violation does not abort the test transaction).
	err := s.DB().RunInTx(s.Ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewRaw(`
			INSERT INTO kb.source_sync_jobs (id, project_id, source_id, status)
			VALUES (uuid(?), uuid(?), uuid(?), 'pending')
		`, uuid.New().String(), s.ProjectID, sourceID).Exec(ctx)
		return err
	})
	s.Require().Error(err, "second pending job must violate the admission index")
	s.True(pgutils.IsUniqueViolation(err), "expected unique violation, got: %v", err)

	// A terminal status frees the slot.
	_, err = s.DB().NewRaw(`
		UPDATE kb.source_sync_jobs SET status = 'completed' WHERE id = uuid(?)
	`, job1).Exec(s.Ctx)
	s.Require().NoError(err, "mark job terminal")

	job3 := s.insertSourceJob(sourceID, "pending")
	s.NotEmpty(job3, "a new pending job must be admitted after terminal status")
}

// TestFK_SourceDeleteSetsNull verifies deleting a kb.sources row keeps its
// documents and SET NULLs documents.source_id.
func (s *DocumentSourceVersionsSuite) TestFK_SourceDeleteSetsNull() {
	sourceID := s.insertSource("google_drive")
	externalID := "drive-item-fk"
	canonicalID := uuid.New().String()
	docID := uuid.New().String()

	err := s.insertDocumentRaw(s.DB(), docID, canonicalID, 1, nil, &sourceID, &externalID)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`DELETE FROM kb.sources WHERE id = uuid(?)`, sourceID).Exec(s.Ctx)
	s.Require().NoError(err, "delete source")

	// Document survives.
	var count int
	err = s.DB().NewRaw(`SELECT count(*) FROM kb.documents WHERE id = uuid(?)`, docID).Scan(s.Ctx, &count)
	s.Require().NoError(err)
	s.Equal(1, count, "document must survive source deletion")

	// source_id is SET NULL.
	var sourceIsNull bool
	err = s.DB().NewRaw(`SELECT source_id IS NULL FROM kb.documents WHERE id = uuid(?)`, docID).Scan(s.Ctx, &sourceIsNull)
	s.Require().NoError(err)
	s.True(sourceIsNull, "documents.source_id must be SET NULL after source delete")
}

// TestRepository_VersioningAndDeletedFromSource exercises the version-aware
// repository methods end-to-end.
func (s *DocumentSourceVersionsSuite) TestRepository_VersioningAndDeletedFromSource() {
	repo := documents.NewRepository(s.DB(), slog.Default())

	sourceID := s.insertSource("google_drive")
	externalID := "drive-item-repo"
	canonicalID := uuid.New().String()
	now := time.Now().UTC()

	// v1 (first HEAD).
	v1 := &documents.Document{
		ID:               uuid.New().String(),
		ProjectID:        s.ProjectID,
		CreatedAt:        now,
		UpdatedAt:        now,
		CanonicalID:      canonicalID,
		Version:          1,
		SupersedesID:     nil,
		SourceID:         &sourceID,
		SourceExternalID: &externalID,
		SourceStatus:     documents.SourceStatusActive,
	}
	s.Require().NoError(repo.InsertDocument(s.Ctx, v1))

	head, err := repo.GetHeadBySource(s.Ctx, s.ProjectID, sourceID, externalID)
	s.Require().NoError(err)
	s.Require().NotNil(head, "GetHeadBySource must resolve the inserted HEAD")
	s.Equal(v1.ID, head.ID, "HEAD should be v1")

	// v2 (new HEAD, sharing canonical_id, incremented version).
	v2 := &documents.Document{
		ID:               uuid.New().String(),
		ProjectID:        s.ProjectID,
		CreatedAt:        now,
		UpdatedAt:        now,
		CanonicalID:      canonicalID,
		Version:          2,
		SupersedesID:     nil,
		SourceID:         &sourceID,
		SourceExternalID: &externalID,
		SourceStatus:     documents.SourceStatusActive,
	}
	s.Require().NoError(repo.CreateVersion(s.Ctx, v2))

	head, err = repo.GetHeadBySource(s.Ctx, s.ProjectID, sourceID, externalID)
	s.Require().NoError(err)
	s.Require().NotNil(head, "GetHeadBySource must resolve the new HEAD")
	s.Equal(v2.ID, head.ID, "HEAD should advance to v2")

	// History returns both versions, ordered by version, retaining v1.
	versions, err := repo.ListVersionsByCanonical(s.Ctx, s.ProjectID, canonicalID)
	s.Require().NoError(err)
	s.Require().Len(versions, 2, "both versions must be retained")
	s.Equal(1, versions[0].Version, "history must be ordered by version ASC")
	s.Equal(v1.ID, versions[0].ID)
	s.Equal(2, versions[1].Version)
	s.Equal(v2.ID, versions[1].ID)

	// UpdateSourceState mutates the revision pointer/hash without creating a
	// version.
	revID := "rev-2"
	textHash := "hash-2"
	s.Require().NoError(repo.UpdateSourceState(s.Ctx, v2.ID, &revID, &textHash, now))

	// Mark the head deleted-from-source. Default List is HEAD-only and excludes
	// deleted-from-source rows, so it now returns nothing: v2 is the only HEAD
	// and it is deleted, while v1 is superseded history.
	s.Require().NoError(repo.MarkSourceDeleted(s.Ctx, v2.ID, now))

	res, err := repo.List(s.Ctx, documents.ListParams{ProjectID: s.ProjectID})
	s.Require().NoError(err)
	s.Require().Len(res.Documents, 0, "deleted HEAD and its superseded version must both be excluded from default List")

	versions, err = repo.ListVersionsByCanonical(s.Ctx, s.ProjectID, canonicalID)
	s.Require().NoError(err)
	s.Require().Len(versions, 2, "history must still return both versions after deletion")

	// MarkSourceActive re-activates the HEAD; default List returns exactly the
	// HEAD (v2), and the superseded v1 stays excluded.
	s.Require().NoError(repo.MarkSourceActive(s.Ctx, v2.ID))
	res, err = repo.List(s.Ctx, documents.ListParams{ProjectID: s.ProjectID})
	s.Require().NoError(err)
	s.Require().Len(res.Documents, 1, "default List must return only the HEAD version after re-activation")
	s.Equal(v2.ID, res.Documents[0].ID, "HEAD must be the re-activated v2")
	for _, d := range res.Documents {
		s.NotEqual(v1.ID, d.ID, "superseded version must not appear in default List")
	}
}
