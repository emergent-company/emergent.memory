package schemas_test

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// SchemaMembershipSuite exercises the credential-addressed /api/schemas routes
// (POST "", GET/PUT/DELETE /:packId) through the full in-process server (auth
// middleware + route table + handler) to prove that a caller's project is
// resolved server-side from membership/token binding, never from a
// client-supplied X-Project-ID header (issue #1023).
//
// The admin user (e2e-test-user token) is set up by BaseSuite.SetupTest as an
// org_admin member of s.OrgID (org A). A second org B is created on demand with
// no membership for the admin user, and the caller sends org B's project id via
// the X-Project-ID header to reach B's schema packs.
type SchemaMembershipSuite struct {
	testutil.BaseSuite
}

func TestSchemaMembershipSuite(t *testing.T) {
	suite.Run(t, new(SchemaMembershipSuite))
}

func (s *SchemaMembershipSuite) SetupSuite() {
	s.SetDBSuffix("schemas_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *SchemaMembershipSuite) newForeignProject() string {
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

// seedPack inserts a pack owned by projectID directly in the DB (the caller
// cannot create one via the API once the membership guard lands).
func (s *SchemaMembershipSuite) seedPack(projectID string) string {
	repo := schemas.NewRepository(s.DB(), slog.Default())
	pack, err := repo.CreatePack(s.Ctx, projectID, &schemas.CreatePackRequest{
		Name:    "foreign-secret",
		Version: "1.0.0",
	})
	s.Require().NoError(err)
	return pack.ID
}

func packBody(name string) map[string]any {
	return map[string]any{
		"name":                name,
		"version":             "1.0.0",
		"object_type_schemas": []map[string]any{{"name": "Person", "properties": map[string]any{}}},
	}
}

// TestCrossProjectSchemaCreateForbidden is the write reproducer: a member of
// org A who sends org B's project id via X-Project-ID must not create a schema
// pack in org B's project. Before the fix this returned 201 because the handler
// scoped the pack by the header-derived project ID without a membership check.
func (s *SchemaMembershipSuite) TestCrossProjectSchemaCreateForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/schemas",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(packBody("stolen-pack")))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project schema create must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossProjectSchemaReadForbidden is the read reproducer: a member of org A
// must not read a pack owned by org B's project via X-Project-ID.
func (s *SchemaMembershipSuite) TestCrossProjectSchemaReadForbidden() {
	projectB := s.newForeignProject()
	packB := s.seedPack(projectB)

	resp := s.Client.GET("/api/schemas/"+packB,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project schema read must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestUnknownProjectSchemaNotFound proves a session caller addressing a
// non-existent project receives 404 (no existence oracle) rather than a
// different error.
func (s *SchemaMembershipSuite) TestUnknownProjectSchemaNotFound() {
	resp := s.Client.POST("/api/schemas",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(uuid.New().String()),
		testutil.WithJSONBody(packBody("ghost-pack")))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"unknown-project schema create must be 404, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectSchemaCreateOK proves the server-side resolution still admits
// the caller's own project: the admin user is a member of s.OrgID, so creating
// a pack in their own project must succeed.
func (s *SchemaMembershipSuite) TestOwnProjectSchemaCreateOK() {
	resp := s.Client.POST("/api/schemas",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(packBody("own-pack")))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"own-project schema create must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestSchemaTokenProjectBindingForbidden proves a project-bound emt_* token that
// presents a different project's id via X-Project-ID is rejected 403 by
// RequireProjectTokenScope before any membership resolution.
func (s *SchemaMembershipSuite) TestSchemaTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()

	token := "emt_test_1023_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"schema:write"}, s.ProjectID))

	resp := s.Client.POST("/api/schemas",
		testutil.WithAuth(token), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(packBody("token-stolen-pack")))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project via X-Project-ID must be 403, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestSchemaTokenProjectBindingOK proves a project-bound emt_* token presenting
// its own project via X-Project-ID is still admitted (token binding passes,
// then the API-token caller passes the membership middleware through).
func (s *SchemaMembershipSuite) TestSchemaTokenProjectBindingOK() {
	token := "emt_test_1023_binding_ok"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"schema:write"}, s.ProjectID))

	resp := s.Client.POST("/api/schemas",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(packBody("token-own-pack")))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"project token addressing its own project must be 201, got %d: %s",
		resp.StatusCode, resp.String())
}
