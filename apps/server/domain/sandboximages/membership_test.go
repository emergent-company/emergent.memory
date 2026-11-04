package sandboximages_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// SandboxImagesMembershipSuite proves the /api/admin/sandbox-images group is
// project-scoped (issue #968): it admits a project member (or project-bound
// token), never a bare `admin` scope or a non-member session, and it scopes the
// :id read/delete to the caller's project so a member cannot reach another
// project's image by ID.
type SandboxImagesMembershipSuite struct {
	testutil.BaseSuite
	foreignImageID string
}

func TestSandboxImagesMembershipSuite(t *testing.T) {
	suite.Run(t, new(SandboxImagesMembershipSuite))
}

func (s *SandboxImagesMembershipSuite) SetupSuite() {
	s.SetDBSuffix("sandboximages_membership")
	s.BaseSuite.SetupSuite()
}

// sandboxMemberAdminToken is a bare `admin` account token minted by a
// non-member user — the escalation path a scope-only gate admits (#948/#949).
const sandboxMemberAdminToken = "emt_test_968_sandbox_member_admin"

func (s *SandboxImagesMembershipSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	// A non-member user who mints a bare `admin` account token.
	memberID := uuid.New().String()
	s.Require().NoError(testutil.CreateTestUser(s.Ctx, s.DB(), testutil.TestUser{
		ID:            memberID,
		ZitadelUserID: "sandboximages-nonmember-" + memberID[:8],
		Email:         "sandboximages-nonmember@test.local",
	}))
	s.Require().NoError(testutil.CreateTestAccountAPIToken(s.Ctx, s.DB(), memberID, sandboxMemberAdminToken, []string{"admin"}))

	// A sandbox image owned by a foreign project (a different org).
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	projectB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))

	s.foreignImageID = uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.sandbox_images (id, name, type, provider, status, project_id, created_at, updated_at)
		VALUES (?, 'foreign-img', 'custom', 'gvisor', 'ready', ?, NOW(), NOW())
	`, s.foreignImageID, projectB).Exec(s.Ctx)
	s.Require().NoError(err)
}

// seedOwnImage inserts an image owned by s.ProjectID and returns its ID.
func (s *SandboxImagesMembershipSuite) seedOwnImage() string {
	id := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.sandbox_images (id, name, type, provider, status, project_id, created_at, updated_at)
		VALUES (?, 'own-img', 'custom', 'gvisor', 'ready', ?, NOW(), NOW())
	`, id, s.ProjectID).Exec(s.Ctx)
	s.Require().NoError(err)
	return id
}

// --- List ---

func (s *SandboxImagesMembershipSuite) TestListNonMemberForbidden() {
	resp := s.Client.GET("/api/admin/sandbox-images",
		testutil.WithAuth("with-scope"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"non-member list must be 403, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SandboxImagesMembershipSuite) TestListBareAdminTokenForbidden() {
	resp := s.Client.GET("/api/admin/sandbox-images",
		testutil.WithAuth(sandboxMemberAdminToken), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"bare admin token list must be 403, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SandboxImagesMembershipSuite) TestListOwnProjectOK() {
	resp := s.Client.GET("/api/admin/sandbox-images",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project list must be 200, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SandboxImagesMembershipSuite) TestListNoUserUnauthorized() {
	resp := s.Client.GET("/api/admin/sandbox-images", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

// --- Get ---

func (s *SandboxImagesMembershipSuite) TestGetOwnProjectOK() {
	id := s.seedOwnImage()
	resp := s.Client.GET("/api/admin/sandbox-images/"+id,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project get must be 200, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SandboxImagesMembershipSuite) TestGetForeignImageNotFound() {
	resp := s.Client.GET("/api/admin/sandbox-images/"+s.foreignImageID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"foreign-project get must be 404, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SandboxImagesMembershipSuite) TestGetNonMemberForbidden() {
	id := s.seedOwnImage()
	resp := s.Client.GET("/api/admin/sandbox-images/"+id,
		testutil.WithAuth("with-scope"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"non-member get must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// --- Create ---

func (s *SandboxImagesMembershipSuite) TestCreateOwnProjectCreated() {
	resp := s.Client.POST("/api/admin/sandbox-images",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"name": "py-ml", "docker_ref": "python:3.12-slim"}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"own-project create must be 201, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SandboxImagesMembershipSuite) TestCreateNonMemberForbidden() {
	resp := s.Client.POST("/api/admin/sandbox-images",
		testutil.WithAuth("with-scope"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"name": "py-ml"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"non-member create must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// --- Delete ---

func (s *SandboxImagesMembershipSuite) TestDeleteOwnProjectOK() {
	id := s.seedOwnImage()
	resp := s.Client.DELETE("/api/admin/sandbox-images/"+id,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNoContent, resp.StatusCode,
		"own-project delete must be 204, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SandboxImagesMembershipSuite) TestDeleteForeignImageNotFound() {
	resp := s.Client.DELETE("/api/admin/sandbox-images/"+s.foreignImageID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"foreign-project delete must be 404, got %d: %s", resp.StatusCode, resp.String())
}

func (s *SandboxImagesMembershipSuite) TestDeleteNonMemberForbidden() {
	id := s.seedOwnImage()
	resp := s.Client.DELETE("/api/admin/sandbox-images/"+id,
		testutil.WithAuth("with-scope"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"non-member delete must be 403, got %d: %s", resp.StatusCode, resp.String())
}
