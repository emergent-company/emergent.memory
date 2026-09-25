package schemaregistry_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// SchemaRegistryMembershipSuite proves a caller's schema-registry access is
// derived from real organization membership, not from the client-supplied
// :projectId path param (issue #913).
type SchemaRegistryMembershipSuite struct {
	testutil.BaseSuite
}

func TestSchemaRegistryMembershipSuite(t *testing.T) {
	suite.Run(t, new(SchemaRegistryMembershipSuite))
}

func (s *SchemaRegistryMembershipSuite) SetupSuite() {
	s.SetDBSuffix("schemaregistry_membership")
	s.BaseSuite.SetupSuite()
}

func (s *SchemaRegistryMembershipSuite) newForeignProject() string {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	projectB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))
	return projectB
}

func (s *SchemaRegistryMembershipSuite) TestCrossProjectTypesForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/api/schema-registry/projects/"+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project types must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SchemaRegistryMembershipSuite) TestCrossProjectTypeWriteForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/schema-registry/projects/"+projectB+"/types",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"type_name":   "Stolen",
			"json_schema": map[string]any{"type": "object", "properties": map[string]any{}},
		}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project type create must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SchemaRegistryMembershipSuite) TestOwnProjectTypesOK() {
	resp := s.Client.GET("/api/schema-registry/projects/"+s.ProjectID,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project types must succeed, got %d: %s", resp.StatusCode, resp.String())
}
