package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

// TemplatePacksTestSuite tests the template packs API endpoints
type TemplatePacksTestSuite struct {
	testutil.BaseSuite
}

func TestTemplatePacksSuite(t *testing.T) {
	suite.Run(t, new(TemplatePacksTestSuite))
}

func (s *TemplatePacksTestSuite) SetupSuite() {
	s.SetDBSuffix("templatepacks")
	s.BaseSuite.SetupSuite()
}

// createTemplatePackViaDB creates a template pack directly in the database for testing
func (s *TemplatePacksTestSuite) createTemplatePackViaDB(name, version string, description *string) string {
	s.Require().NotNil(s.DB(), "Database connection required for template pack creation")

	var packID string
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.graph_template_packs (name, version, description, object_type_schemas, relationship_type_schemas)
		VALUES (?, ?, ?, '[]'::jsonb, '[]'::jsonb)
		RETURNING id
	`, name, version, description).Exec(context.Background(), &packID)
	s.Require().NoError(err)

	return packID
}

// createTemplatePackWithSchemasViaDB creates a template pack with object and relationship type schemas
func (s *TemplatePacksTestSuite) createTemplatePackWithSchemasViaDB(name, version string, objectSchemas, relationshipSchemas string) string {
	s.Require().NotNil(s.DB(), "Database connection required for template pack creation")

	var packID string
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.graph_template_packs (name, version, object_type_schemas, relationship_type_schemas)
		VALUES (?, ?, ?::jsonb, ?::jsonb)
		RETURNING id
	`, name, version, objectSchemas, relationshipSchemas).Exec(context.Background(), &packID)
	s.Require().NoError(err)

	return packID
}

// assignPackToProjectViaDB assigns a template pack to a project directly in the database
func (s *TemplatePacksTestSuite) assignPackToProjectViaDB(projectID, packID string, active bool) string {
	s.Require().NotNil(s.DB(), "Database connection required for pack assignment")

	var assignmentID string
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.project_template_packs (project_id, template_pack_id, active, installed_at)
		VALUES (?, ?, ?, NOW())
		RETURNING id
	`, projectID, packID, active).Exec(context.Background(), &assignmentID)
	s.Require().NoError(err)

	return assignmentID
}

// =============================================================================
// Test: Get Available Packs (GET /api/template-packs/projects/:projectId/available)
// =============================================================================

func (s *TemplatePacksTestSuite) TestGetAvailablePacks_RequiresAuth() {
	resp := s.Client.GET("/api/v2/template-packs/projects/" + s.ProjectID + "/available")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestGetAvailablePacks_ReturnsEmptyArrayWhenNoPacks() {
	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/available",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.NotNil(result) // Should be array, not nil
}

func (s *TemplatePacksTestSuite) TestGetAvailablePacks_ReturnsPacks() {
	// Create template packs
	desc := "Test pack description"
	s.createTemplatePackViaDB("Test Pack 1", "1.0.0", &desc)
	s.createTemplatePackViaDB("Test Pack 2", "2.0.0", nil)

	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/available",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.GreaterOrEqual(len(result), 2)
}

func (s *TemplatePacksTestSuite) TestGetAvailablePacks_ExcludesAlreadyInstalledPacks() {
	// Create template packs
	pack1ID := s.createTemplatePackViaDB("Available Pack", "1.0.0", nil)
	pack2ID := s.createTemplatePackViaDB("Installed Pack", "1.0.0", nil)

	// Assign pack2 to the project
	s.assignPackToProjectViaDB(s.ProjectID, pack2ID, true)

	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/available",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Should only find pack1 (the available one)
	found := false
	for _, pack := range result {
		if pack["id"] == pack1ID {
			found = true
		}
		// Should not find pack2 since it's already installed
		s.NotEqual(pack2ID, pack["id"], "Installed pack should not be in available list")
	}
	s.True(found, "Available pack should be in the list")
}

// =============================================================================
// Test: Get Installed Packs (GET /api/template-packs/projects/:projectId/installed)
// =============================================================================

func (s *TemplatePacksTestSuite) TestGetInstalledPacks_RequiresAuth() {
	resp := s.Client.GET("/api/v2/template-packs/projects/" + s.ProjectID + "/installed")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestGetInstalledPacks_ReturnsEmptyArrayWhenNoPacks() {
	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/installed",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.NotNil(result)
	s.Equal(0, len(result))
}

func (s *TemplatePacksTestSuite) TestGetInstalledPacks_ReturnsInstalledPacks() {
	// Create and install template packs
	pack1ID := s.createTemplatePackViaDB("Installed Pack 1", "1.0.0", nil)
	pack2ID := s.createTemplatePackViaDB("Installed Pack 2", "2.0.0", nil)
	s.assignPackToProjectViaDB(s.ProjectID, pack1ID, true)
	s.assignPackToProjectViaDB(s.ProjectID, pack2ID, true)

	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/installed",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal(2, len(result))
}

func (s *TemplatePacksTestSuite) TestGetInstalledPacks_IncludesInactivePacks() {
	// Create and install template packs with different active states
	pack1ID := s.createTemplatePackViaDB("Active Pack", "1.0.0", nil)
	pack2ID := s.createTemplatePackViaDB("Inactive Pack", "1.0.0", nil)
	s.assignPackToProjectViaDB(s.ProjectID, pack1ID, true)
	s.assignPackToProjectViaDB(s.ProjectID, pack2ID, false)

	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/installed",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal(2, len(result))

	// Find both packs and verify their active states
	var activePack, inactivePack map[string]any
	for _, pack := range result {
		if pack["name"] == "Active Pack" {
			activePack = pack
		} else if pack["name"] == "Inactive Pack" {
			inactivePack = pack
		}
	}
	s.NotNil(activePack)
	s.NotNil(inactivePack)
	s.Equal(true, activePack["active"])
	s.Equal(false, inactivePack["active"])
}

// =============================================================================
// Test: Get Compiled Types (GET /api/template-packs/projects/:projectId/compiled-types)
// =============================================================================

func (s *TemplatePacksTestSuite) TestGetCompiledTypes_RequiresAuth() {
	resp := s.Client.GET("/api/v2/template-packs/projects/" + s.ProjectID + "/compiled-types")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestGetCompiledTypes_ReturnsEmptyTypesWhenNoPacks() {
	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/compiled-types",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.Contains(result, "objectTypes")
	s.Contains(result, "relationshipTypes")

	objectTypes := result["objectTypes"].([]any)
	relationshipTypes := result["relationshipTypes"].([]any)
	s.Equal(0, len(objectTypes))
	s.Equal(0, len(relationshipTypes))
}

func (s *TemplatePacksTestSuite) TestGetCompiledTypes_ReturnsTypesFromInstalledPacks() {
	// Create template pack with schemas
	objectSchemas := `[{"name": "Person", "label": "Person", "description": "A person entity"}]`
	relationshipSchemas := `[{"name": "knows", "label": "Knows", "sourceType": "Person", "targetType": "Person"}]`
	packID := s.createTemplatePackWithSchemasViaDB("Schema Pack", "1.0.0", objectSchemas, relationshipSchemas)

	// Install the pack
	s.assignPackToProjectViaDB(s.ProjectID, packID, true)

	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/compiled-types",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	objectTypes := result["objectTypes"].([]any)
	relationshipTypes := result["relationshipTypes"].([]any)
	s.GreaterOrEqual(len(objectTypes), 1)
	s.GreaterOrEqual(len(relationshipTypes), 1)

	// Verify the Person object type is present
	personType := objectTypes[0].(map[string]any)
	s.Equal("Person", personType["name"])
}

func (s *TemplatePacksTestSuite) TestGetCompiledTypes_ExcludesInactivePacks() {
	// Create template pack with schemas
	objectSchemas := `[{"name": "InactiveType", "label": "Inactive Type"}]`
	packID := s.createTemplatePackWithSchemasViaDB("Inactive Schema Pack", "1.0.0", objectSchemas, "[]")

	// Install the pack but set it to inactive
	s.assignPackToProjectViaDB(s.ProjectID, packID, false)

	resp := s.Client.GET("/api/v2/template-packs/projects/"+s.ProjectID+"/compiled-types",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	objectTypes := result["objectTypes"].([]any)
	// Should not contain types from inactive packs
	for _, objType := range objectTypes {
		t := objType.(map[string]any)
		s.NotEqual("InactiveType", t["name"], "Inactive pack types should not be in compiled types")
	}
}

// =============================================================================
// Test: Assign Pack (POST /api/template-packs/projects/:projectId/assign)
// =============================================================================

func (s *TemplatePacksTestSuite) TestAssignPack_RequiresAuth() {
	resp := s.Client.POST("/api/v2/template-packs/projects/"+s.ProjectID+"/assign",
		testutil.WithJSONBody(map[string]any{"template_pack_id": "some-id"}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestAssignPack_RequiresTemplatePackID() {
	resp := s.Client.POST("/api/v2/template-packs/projects/"+s.ProjectID+"/assign",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{}),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestAssignPack_AssignsPackToProject() {
	// Create a template pack
	packID := s.createTemplatePackViaDB("Pack To Assign", "1.0.0", nil)

	resp := s.Client.POST("/api/v2/template-packs/projects/"+s.ProjectID+"/assign",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"template_pack_id": packID,
		}),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Contains(result, "id")
	s.Equal(s.ProjectID, result["projectId"])
	s.Equal(packID, result["templatePackId"])
	s.Equal(true, result["active"])
}

func (s *TemplatePacksTestSuite) TestAssignPack_ReturnsNotFoundForInvalidPackID() {
	resp := s.Client.POST("/api/v2/template-packs/projects/"+s.ProjectID+"/assign",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"template_pack_id": "00000000-0000-0000-0000-000000000000",
		}),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// =============================================================================
// Test: Update Assignment (PATCH /api/template-packs/projects/:projectId/assignments/:assignmentId)
// =============================================================================

func (s *TemplatePacksTestSuite) TestUpdateAssignment_RequiresAuth() {
	resp := s.Client.PATCH("/api/v2/template-packs/projects/" + s.ProjectID + "/assignments/some-id")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestUpdateAssignment_ReturnsNotFoundForInvalidAssignmentID() {
	resp := s.Client.PATCH("/api/v2/template-packs/projects/"+s.ProjectID+"/assignments/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"active": false}),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestUpdateAssignment_TogglesActiveStatus() {
	// Create and assign a pack
	packID := s.createTemplatePackViaDB("Toggle Pack", "1.0.0", nil)
	assignmentID := s.assignPackToProjectViaDB(s.ProjectID, packID, true)

	// Deactivate the pack
	resp := s.Client.PATCH("/api/v2/template-packs/projects/"+s.ProjectID+"/assignments/"+assignmentID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"active": false}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal("updated", result["status"])

	// Verify in database
	var active bool
	err = s.DB().NewRaw(`SELECT active FROM kb.project_template_packs WHERE id = ?`, assignmentID).Scan(context.Background(), &active)
	s.NoError(err)
	s.False(active)
}

// =============================================================================
// Test: Delete Assignment (DELETE /api/template-packs/projects/:projectId/assignments/:assignmentId)
// =============================================================================

func (s *TemplatePacksTestSuite) TestDeleteAssignment_RequiresAuth() {
	resp := s.Client.DELETE("/api/v2/template-packs/projects/" + s.ProjectID + "/assignments/some-id")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestDeleteAssignment_ReturnsNotFoundForInvalidAssignmentID() {
	resp := s.Client.DELETE("/api/v2/template-packs/projects/"+s.ProjectID+"/assignments/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *TemplatePacksTestSuite) TestDeleteAssignment_DeletesAssignment() {
	// Create and assign a pack
	packID := s.createTemplatePackViaDB("Delete Pack", "1.0.0", nil)
	assignmentID := s.assignPackToProjectViaDB(s.ProjectID, packID, true)

	resp := s.Client.DELETE("/api/v2/template-packs/projects/"+s.ProjectID+"/assignments/"+assignmentID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNoContent, resp.StatusCode)

	// Verify deleted from database
	var count int
	err := s.DB().NewRaw(`SELECT COUNT(*) FROM kb.project_template_packs WHERE id = ?`, assignmentID).Scan(context.Background(), &count)
	s.NoError(err)
	s.Equal(0, count)
}

func (s *TemplatePacksTestSuite) TestDeleteAssignment_DoesNotAffectOtherProjects() {
	// Create a pack
	packID := s.createTemplatePackViaDB("Shared Pack", "1.0.0", nil)

	// Assign to current project
	assignment1ID := s.assignPackToProjectViaDB(s.ProjectID, packID, true)

	// Create another project and assign the same pack
	otherProjectID := s.createProjectViaAPI("Other Project")
	assignment2ID := s.assignPackToProjectViaDB(otherProjectID, packID, true)

	// Delete assignment from current project
	resp := s.Client.DELETE("/api/v2/template-packs/projects/"+s.ProjectID+"/assignments/"+assignment1ID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusNoContent, resp.StatusCode)

	// Verify other project's assignment still exists
	var count int
	err := s.DB().NewRaw(`SELECT COUNT(*) FROM kb.project_template_packs WHERE id = ?`, assignment2ID).Scan(context.Background(), &count)
	s.NoError(err)
	s.Equal(1, count, "Other project's assignment should still exist")
}

// createProjectViaAPI creates a project via API and returns its ID
func (s *TemplatePacksTestSuite) createProjectViaAPI(name string) string {
	resp := s.Client.POST("/api/v2/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":  name,
			"orgId": s.OrgID,
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create project: %s", resp.String())

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.Require().NoError(err)
	return project["id"].(string)
}
