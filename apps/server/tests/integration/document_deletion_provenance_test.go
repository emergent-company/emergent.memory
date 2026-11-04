package integration

// Tests for the document-deletion provenance contract introduced by the
// canonical-aware deletion cascade (kb.graph_objects.extraction_job_id).
//
// These tests run against a real Postgres database (no LLM). Derived graph
// objects are created through the real graph service Create/CreateOrUpdate path
// with ExtractionJobID set so provenance lands in kb.graph_objects.extraction_job_id
// (not only in properties), exactly as the extraction worker's persistResults
// does. Relationships are inserted directly so their src_id/dst_id reference the
// CANONICAL ids of derived objects, which is what the deletion resolver trusts.
//
// Test matrix:
//   TestCascadeAndImpactMatchReality      — GetDeletionImpact + DeleteWithCascade
//                                           counts equal rows actually removed
//   TestSurvivingVersionChainRepair       — surviving entity keeps exactly one HEAD
//                                           after its attributable version is deleted
//   TestProvenanceStampingOnGraphUpsert   — CreateOrUpdate stamps the extraction_job_id
//                                           column on created and updated versions

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// DocumentDeletionProvenanceSuite covers the database-backed deletion provenance
// contract for the canonical-aware document deletion cascade.
type DocumentDeletionProvenanceSuite struct {
	suite.Suite
	ctx       context.Context
	testDB    *testutil.TestDB
	log       *slog.Logger
	orgID     string
	projectID string

	graphSvc *graph.Service
	docsRepo *documents.Repository
}

func TestDocumentDeletionProvenanceSuite(t *testing.T) {
	suite.Run(t, new(DocumentDeletionProvenanceSuite))
}

func (s *DocumentDeletionProvenanceSuite) SetupSuite() {
	s.ctx = context.Background()
	testutil.LoadEnvFiles()
	s.log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	db, err := testutil.SetupTestDB(s.ctx, "doc_deletion_provenance")
	s.Require().NoError(err, "Failed to setup test database")
	s.testDB = db

	s.graphSvc = s.newGraphService()
	s.docsRepo = documents.NewRepository(s.testDB.DB, s.log)
}

func (s *DocumentDeletionProvenanceSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *DocumentDeletionProvenanceSuite) SetupTest() {
	s.Require().NoError(testutil.TruncateTables(s.ctx, s.testDB.DB))
	s.Require().NoError(testutil.SetupTestFixtures(s.ctx, s.testDB.DB))
	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()
	s.Require().NoError(testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID))
}

// newGraphService builds a graph service wired to the test DB, mirroring the
// merge_policy_test.go pattern. Embedding/enrichment/events are left nil so
// object creation only touches the graph tables.
func (s *DocumentDeletionProvenanceSuite) newGraphService() *graph.Service {
	cfg := &config.Config{}
	cfg.Graph.MaxBatchObjects = 500
	cfg.Graph.MaxBatchRelationships = 500
	cfg.Graph.MaxListLimit = 1000
	cfg.Graph.DefaultListLimit = 100
	repo := graph.NewRepository(s.testDB.DB, s.log, cfg)
	return graph.NewService(repo, s.log,
		graph.ProvideSchemaProvider(s.testDB.DB, s.log),
		graph.ProvideInverseTypeProvider(s.testDB.DB, s.log),
		nil, nil, nil, nil, nil, nil)
}

// createDocument inserts a document and returns its ID.
func (s *DocumentDeletionProvenanceSuite) createDocument(filename string) string {
	docID := uuid.New().String()
	s.Require().NoError(testutil.CreateTestDocument(s.ctx, s.testDB.DB, testutil.TestDocument{
		ID:         docID,
		ProjectID:  s.projectID,
		Filename:   &filename,
		Content:    testutil.StringPtr("test content for provenance deletion"),
		SourceType: testutil.StringPtr("upload"),
	}))
	return docID
}

// createExtractionJob inserts a completed object_extraction_jobs row and returns its ID.
func (s *DocumentDeletionProvenanceSuite) createExtractionJob(documentID string) string {
	jobID := uuid.New().String()
	_, err := s.testDB.DB.NewRaw(`
		INSERT INTO kb.object_extraction_jobs (id, project_id, document_id, status, created_at, updated_at)
		VALUES (?, ?, ?, 'completed', now(), now())
	`, jobID, s.projectID, documentID).Exec(s.ctx)
	s.Require().NoError(err)
	return jobID
}

// createObject creates a graph object through the real graph service Create path.
// When jobID is non-nil, the object's kb.graph_objects.extraction_job_id column is
// stamped (the provenance contract the deletion resolver relies on).
func (s *DocumentDeletionProvenanceSuite) createObject(typeName string, props map[string]any, jobID *uuid.UUID) *graph.GraphObjectResponse {
	s.T().Helper()
	pid, _ := uuid.Parse(s.projectID)
	obj, err := s.graphSvc.Create(s.ctx, pid, &graph.CreateGraphObjectRequest{
		Type:            typeName,
		Properties:      props,
		ExtractionJobID: jobID,
	}, nil)
	s.Require().NoError(err)
	return obj
}

// createRelationship inserts a relationship whose endpoints are CANONICAL object IDs,
// mirroring how the graph stores relationship endpoints.
func (s *DocumentDeletionProvenanceSuite) createRelationship(srcCanonical, dstCanonical uuid.UUID, relType string) uuid.UUID {
	s.T().Helper()
	relID := uuid.New()
	_, err := s.testDB.DB.NewRaw(`
		INSERT INTO kb.graph_relationships (id, project_id, type, src_id, dst_id, canonical_id, version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, now())
	`, relID, s.projectID, relType, srcCanonical, dstCanonical, relID).Exec(s.ctx)
	s.Require().NoError(err)
	return relID
}

// objectExists reports whether a graph object row still exists.
func (s *DocumentDeletionProvenanceSuite) objectExists(id uuid.UUID) bool {
	s.T().Helper()
	var exists bool
	s.Require().NoError(s.testDB.DB.NewRaw(
		"SELECT EXISTS(SELECT 1 FROM kb.graph_objects WHERE id = ?)", id).Scan(s.ctx, &exists))
	return exists
}

// relationshipExists reports whether a graph relationship row still exists.
func (s *DocumentDeletionProvenanceSuite) relationshipExists(id uuid.UUID) bool {
	s.T().Helper()
	var exists bool
	s.Require().NoError(s.testDB.DB.NewRaw(
		"SELECT EXISTS(SELECT 1 FROM kb.graph_relationships WHERE id = ?)", id).Scan(s.ctx, &exists))
	return exists
}

// extractionJobIDOf returns the extraction_job_id column value for an object row
// ("" when NULL).
func (s *DocumentDeletionProvenanceSuite) extractionJobIDOf(id uuid.UUID) string {
	s.T().Helper()
	var got string
	s.Require().NoError(s.testDB.DB.NewRaw(
		"SELECT COALESCE(extraction_job_id::text, '') FROM kb.graph_objects WHERE id = ?", id).Scan(s.ctx, &got))
	return got
}

// TestCascadeAndImpactMatchReality verifies that GetDeletionImpact and
// DeleteWithCascade agree on what is actually removed: derived objects and the
// relationship touching their canonical ids are deleted, and an unrelated
// (non-provenanced) object survives.
func (s *DocumentDeletionProvenanceSuite) TestCascadeAndImpactMatchReality() {
	docID := s.createDocument("provenance-cascade.txt")
	jobID := s.createExtractionJob(docID)
	jobUUID, _ := uuid.Parse(jobID)

	// Derived objects A and B, both attributable to this extraction job.
	a := s.createObject("Person", map[string]any{"name": "Alice"}, &jobUUID)
	b := s.createObject("Person", map[string]any{"name": "Bob"}, &jobUUID)

	// Relationship referencing the CANONICAL ids of the derived objects.
	relID := s.createRelationship(a.CanonicalID, b.CanonicalID, "knows")

	// Unrelated object with no provenance — must survive deletion.
	c := s.createObject("Person", map[string]any{"name": "Carol"}, nil)

	// Impact must report both graph objects and the relationship.
	impact, err := s.docsRepo.GetDeletionImpact(s.ctx, s.projectID, docID)
	s.Require().NoError(err)
	s.Require().NotNil(impact)
	s.Equal(2, impact.Impact.GraphObjects, "impact should count both derived objects")
	s.Equal(1, impact.Impact.GraphRelationships, "impact should count the relationship")
	s.Equal(1, impact.Impact.ExtractionJobs)

	// Delete with cascade; returned counts must equal rows actually removed.
	summary, err := s.docsRepo.DeleteWithCascade(s.ctx, s.projectID, docID)
	s.Require().NoError(err)
	s.Require().NotNil(summary)
	s.Equal(2, summary.GraphObjects)
	s.Equal(1, summary.GraphRelationships)
	s.Equal(1, summary.ExtractionJobs)

	// Derived objects and their relationship are gone.
	s.False(s.objectExists(a.ID), "derived object A should be deleted")
	s.False(s.objectExists(b.ID), "derived object B should be deleted")
	s.False(s.relationshipExists(relID), "relationship should be deleted")

	// Unrelated object survives.
	s.True(s.objectExists(c.ID), "unrelated object must survive")
}

// TestSurvivingVersionChainRepair verifies that when only the attributable
// (newer) version of an entity is deleted, the older non-derived version survives
// and its version chain is repaired so exactly one HEAD remains.
func (s *DocumentDeletionProvenanceSuite) TestSurvivingVersionChainRepair() {
	docID := s.createDocument("provenance-version-chain.txt")
	jobID := s.createExtractionJob(docID)
	jobUUID, _ := uuid.Parse(jobID)

	pid, _ := uuid.Parse(s.projectID)
	key := "alice-smith"

	// Older, non-derived version (version 1, NULL provenance).
	v1resp, err := s.graphSvc.Create(s.ctx, pid, &graph.CreateGraphObjectRequest{
		Type:       "Person",
		Key:        &key,
		Properties: map[string]any{"name": "Alice Smith"},
	}, nil)
	s.Require().NoError(err)

	// v2: newer attributable version (current HEAD).
	v2resp, created, err := s.graphSvc.CreateOrUpdate(s.ctx, pid, &graph.CreateGraphObjectRequest{
		Type:            "Person",
		Key:             &key,
		Properties:      map[string]any{"name": "Alice Smith", "occupation": "engineer"},
		ExtractionJobID: &jobUUID,
	}, nil)
	s.Require().NoError(err)
	s.False(created, "CreateOrUpdate should create a new version, not a fresh object")
	s.Equal(v1resp.CanonicalID, v2resp.CanonicalID, "versions must share a canonical entity")

	canonicalID := v1resp.CanonicalID

	// Delete the document: only the attributable version is removed.
	summary, err := s.docsRepo.DeleteWithCascade(s.ctx, s.projectID, docID)
	s.Require().NoError(err)
	s.Require().NotNil(summary)
	s.Equal(1, summary.GraphObjects, "only the attributable version is removed")

	// Entity retained: the non-derived version survives, the attributable one is gone.
	s.True(s.objectExists(v1resp.ID), "non-derived version must survive")
	s.False(s.objectExists(v2resp.ID), "attributable version must be deleted")

	// Surviving row's supersedes_id no longer points at the deleted row.
	var supersedesNull bool
	s.Require().NoError(s.testDB.DB.NewRaw(
		"SELECT (supersedes_id IS NULL) FROM kb.graph_objects WHERE id = ?", v1resp.ID).Scan(s.ctx, &supersedesNull))
	s.True(supersedesNull, "surviving row must not point at a deleted row")

	// Exactly one HEAD remains so HEAD queries still see the entity.
	var heads int
	s.Require().NoError(s.testDB.DB.NewRaw(
		"SELECT COUNT(*) FROM kb.graph_objects WHERE canonical_id = ? AND supersedes_id IS NULL AND deleted_at IS NULL",
		canonicalID).Scan(s.ctx, &heads))
	s.Equal(1, heads, "exactly one HEAD should remain")
}

// TestProvenanceStampingOnGraphUpsert covers the provenance contract at the
// graph upsert boundary — the same path the extraction worker's persistResults
// drives when it calls CreateOrUpdate with ExtractionJobID set. The worker entry
// point itself requires a live LLM, so it is not directly exercisable here.
func (s *DocumentDeletionProvenanceSuite) TestProvenanceStampingOnGraphUpsert() {
	jobUUID := uuid.New()

	pid, _ := uuid.Parse(s.projectID)
	key := "carol-danvers"

	// First write: new object, column stamped.
	first, created, err := s.graphSvc.CreateOrUpdate(s.ctx, pid, &graph.CreateGraphObjectRequest{
		Type:            "Person",
		Key:             &key,
		Properties:      map[string]any{"name": "Carol Danvers"},
		ExtractionJobID: &jobUUID,
	}, nil)
	s.Require().NoError(err)
	s.True(created)
	s.Equal(jobUUID.String(), s.extractionJobIDOf(first.ID),
		"created object must carry extraction_job_id in the column")

	// Second write: updated properties produce a new version, also stamped.
	second, created, err := s.graphSvc.CreateOrUpdate(s.ctx, pid, &graph.CreateGraphObjectRequest{
		Type:            "Person",
		Key:             &key,
		Properties:      map[string]any{"name": "Carol Danvers", "rank": "captain"},
		ExtractionJobID: &jobUUID,
	}, nil)
	s.Require().NoError(err)
	s.False(created, "second write must create a new version, not a fresh object")
	s.NotEqual(first.ID, second.ID)
	s.Equal(jobUUID.String(), s.extractionJobIDOf(second.ID),
		"updated version must carry extraction_job_id in the column")
}
