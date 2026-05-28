package graph_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// DeleteReasonSuite tests the delete_reason field on soft-deleted graph objects
// and relationships — both via HTTP handler and MCP entity-delete tool.
type DeleteReasonSuite struct {
	testutil.BaseSuite
}

func TestDeleteReasonSuite(t *testing.T) {
	suite.Run(t, new(DeleteReasonSuite))
}

func (s *DeleteReasonSuite) SetupSuite() {
	s.SetDBSuffix("delete_reason")
	s.BaseSuite.SetupSuite()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *DeleteReasonSuite) createTestObject() string {
	resp := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{
			"type":       "TestEntity",
			"properties": map[string]any{"name": "test-object"},
		}),
	)
	s.Require().Equal(201, resp.StatusCode, "create object failed: %s", resp.String())
	var obj map[string]any
	s.Require().NoError(resp.JSON(&obj))
	return obj["id"].(string)
}

func (s *DeleteReasonSuite) createTestRelationship(srcID, dstID string) string {
	resp := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/graph/relationships",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{
			"type":   "related_to",
			"src_id": srcID,
			"dst_id": dstID,
		}),
	)
	s.Require().Equal(201, resp.StatusCode, "create relationship failed: %s", resp.String())
	var rel map[string]any
	s.Require().NoError(resp.JSON(&rel))
	return rel["id"].(string)
}

// getObjectHistoryTombstone fetches history and returns the first (latest) version.
func (s *DeleteReasonSuite) getObjectHistoryTombstone(id string) map[string]any {
	resp := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/graph/objects/"+id+"/history",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
	s.Require().Equal(200, resp.StatusCode, "get history failed: %s", resp.String())
	var result map[string]any
	s.Require().NoError(resp.JSON(&result))
	versions := result["versions"].([]any)
	s.Require().NotEmpty(versions, "expected at least one version in history")
	// Latest version is first
	return versions[0].(map[string]any)
}

// getRelationshipHistoryTombstone returns the latest relationship version.
func (s *DeleteReasonSuite) getRelationshipHistoryTombstone(id string) map[string]any {
	resp := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/graph/relationships/"+id+"/history",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
	s.Require().Equal(200, resp.StatusCode, "get rel history failed: %s", resp.String())
	var versions []any
	s.Require().NoError(resp.JSON(&versions))
	s.Require().NotEmpty(versions)
	return versions[0].(map[string]any)
}

// ---------------------------------------------------------------------------
// HTTP handler — entity delete_reason
// ---------------------------------------------------------------------------

func (s *DeleteReasonSuite) TestDeleteWithReason_SetsField() {
	id := s.createTestObject()

	resp := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/graph/objects/"+id,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{"reason": "no longer needed"}),
	)
	s.Equal(200, resp.StatusCode, resp.String())

	tombstone := s.getObjectHistoryTombstone(id)
	s.Equal("no longer needed", tombstone["delete_reason"], "delete_reason must be set on tombstone")
	s.NotNil(tombstone["deleted_at"], "deleted_at must be set on tombstone")
}

func (s *DeleteReasonSuite) TestDeleteWithoutReason_NullField() {
	id := s.createTestObject()

	resp := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/graph/objects/"+id,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
	s.Equal(200, resp.StatusCode, resp.String())

	tombstone := s.getObjectHistoryTombstone(id)
	s.Nil(tombstone["delete_reason"], "delete_reason must be nil when not provided")
	s.NotNil(tombstone["deleted_at"])
}

func (s *DeleteReasonSuite) TestDeleteReason_LongText() {
	id := s.createTestObject()
	longReason := strings.Repeat("x", 1000)

	resp := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/graph/objects/"+id,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{"reason": longReason}),
	)
	s.Equal(200, resp.StatusCode, resp.String())

	tombstone := s.getObjectHistoryTombstone(id)
	s.Equal(longReason, tombstone["delete_reason"])
}

func (s *DeleteReasonSuite) TestDeleteReason_RestoredObjectHasNullReason() {
	id := s.createTestObject()

	// Delete with reason
	resp := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/graph/objects/"+id,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{"reason": "forgotten"}),
	)
	s.Require().Equal(200, resp.StatusCode, resp.String())

	// Restore
	restoreResp := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/graph/objects/"+id+"/restore",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
	s.Require().Equal(200, restoreResp.StatusCode, restoreResp.String())

	var restored map[string]any
	s.Require().NoError(restoreResp.JSON(&restored))
	s.Nil(restored["delete_reason"], "restored object must not carry delete_reason")
	s.Nil(restored["deleted_at"], "restored object must not have deleted_at")

	// Tombstone in history still has reason
	tombstone := s.getObjectHistoryTombstone(id)
	// Tombstone is no longer the latest after restore — find it in history
	resp2 := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/graph/objects/"+id+"/history",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
	s.Require().Equal(200, resp2.StatusCode)
	var result map[string]any
	s.Require().NoError(resp2.JSON(&result))
	versions := result["versions"].([]any)
	// Find the tombstone version (the one with deleted_at set)
	found := false
	for _, v := range versions {
		ver := v.(map[string]any)
		if ver["deleted_at"] != nil {
			s.Equal("forgotten", ver["delete_reason"])
			found = true
			break
		}
	}
	s.True(found, "tombstone version with delete_reason not found in history")
	_ = tombstone // used above
}

// ---------------------------------------------------------------------------
// HTTP handler — relationship delete_reason
// ---------------------------------------------------------------------------

func (s *DeleteReasonSuite) TestDeleteReason_RelationshipDelete() {
	srcID := s.createTestObject()
	dstID := s.createTestObject()
	relID := s.createTestRelationship(srcID, dstID)

	resp := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/graph/relationships/"+relID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{"reason": "relationship forgotten"}),
	)
	s.Equal(200, resp.StatusCode, resp.String())

	tombstone := s.getRelationshipHistoryTombstone(relID)
	s.Equal("relationship forgotten", tombstone["delete_reason"])
	s.NotNil(tombstone["deleted_at"])
}

func (s *DeleteReasonSuite) TestDeleteRelationship_WithoutReason_NullField() {
	srcID := s.createTestObject()
	dstID := s.createTestObject()
	relID := s.createTestRelationship(srcID, dstID)

	resp := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/graph/relationships/"+relID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
	s.Equal(200, resp.StatusCode, resp.String())

	tombstone := s.getRelationshipHistoryTombstone(relID)
	s.Nil(tombstone["delete_reason"])
}

// ---------------------------------------------------------------------------
// MCP entity-delete tool — delete_reason via tool param
// ---------------------------------------------------------------------------

func (s *DeleteReasonSuite) TestMCPEntityDelete_WithReason() {
	s.SkipIfExternalServer("MCP tool test requires direct DB inspection")
	id := s.createTestObject()

	resp := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/mcp/tools/execute",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{
			"tool": "entity-delete",
			"args": map[string]any{
				"entity_id": id,
				"reason":    "forgotten via agent",
			},
		}),
	)
	s.Require().Equal(200, resp.StatusCode, resp.String())

	tombstone := s.getObjectHistoryTombstone(id)
	s.Equal("forgotten via agent", tombstone["delete_reason"])
	s.NotNil(tombstone["deleted_at"])
}

func (s *DeleteReasonSuite) TestMCPEntityDelete_WithoutReason() {
	s.SkipIfExternalServer("MCP tool test requires direct DB inspection")
	id := s.createTestObject()

	resp := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/mcp/tools/execute",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{
			"tool": "entity-delete",
			"args": map[string]any{
				"entity_id": id,
			},
		}),
	)
	s.Require().Equal(200, resp.StatusCode, resp.String())

	tombstone := s.getObjectHistoryTombstone(id)
	s.Nil(tombstone["delete_reason"])
	s.NotNil(tombstone["deleted_at"])
}

func (s *DeleteReasonSuite) TestMCPRelationshipDelete_WithReason() {
	s.SkipIfExternalServer("MCP tool test requires direct DB inspection")
	srcID := s.createTestObject()
	dstID := s.createTestObject()
	relID := s.createTestRelationship(srcID, dstID)

	resp := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/mcp/tools/execute",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]any{
			"tool": "relationship-delete",
			"args": map[string]any{
				"relationship_id": relID,
				"reason":          "forgotten via agent",
			},
		}),
	)
	s.Require().Equal(200, resp.StatusCode, resp.String())

	tombstone := s.getRelationshipHistoryTombstone(relID)
	s.Equal("forgotten via agent", tombstone["delete_reason"])
	s.NotNil(tombstone["deleted_at"])
}
