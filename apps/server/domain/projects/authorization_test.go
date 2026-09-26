package projects_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ProjectAuthorizationSuite is the regression suite for the surface→authority
// matrix finding on the projects domain CRUD routes (matrix §2 `projects`,
// mechanism 4 missing-membership-check and mechanism 3 client-supplied
// identity). It exercises every changed route across the caller-class matrix:
// unauthenticated, foreign (no membership), org member, project member/role,
// and org_admin.
type ProjectAuthorizationSuite struct {
	testutil.BaseSuite
}

func TestProjectAuthorizationSuite(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	suite.Run(t, new(ProjectAuthorizationSuite))
}

func (s *ProjectAuthorizationSuite) SetupSuite() {
	s.SetDBSuffix("project_authorization")
	s.BaseSuite.SetupSuite()
}

// Test user IDs and tokens (mirrors testutil.TestTokenConfigs).
const (
	adminToken = "e2e-test-user" // AdminUser: org_admin + project_admin of the default org/project
	allScopes  = "all-scopes"    // AllScopesUser: configurable per-test memberships
	noScope    = "no-scope"      // NoScopeUser: no memberships (foreign)
	withScope  = "with-scope"    // WithScopeUser: configurable per-test memberships
)

// grant adds org and/or project memberships for a user to the default
// org/project. Each test runs in its own transaction, so membership setup is
// per-test and rolled back automatically.
func (s *ProjectAuthorizationSuite) grant(userID, orgRole, projectRole string) {
	if orgRole != "" {
		s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), s.OrgID, userID, orgRole))
	}
	if projectRole != "" {
		s.Require().NoError(testutil.CreateTestProjectMembership(s.Ctx, s.DB(), s.ProjectID, userID, projectRole))
	}
}

// ---------------------------------------------------------------------------
// GET /api/projects/:id — read requires project membership (any role) or any
// org membership of the owning org.
// ---------------------------------------------------------------------------

func (s *ProjectAuthorizationSuite) TestGet_Authority() {
	s.Run("unauthenticated", func() {
		resp := s.Client.GET("/api/projects/" + s.ProjectID)
		s.Equal(401, resp.StatusCode, resp.String())
	})
	s.Run("foreign", func() {
		resp := s.Client.GET("/api/projects/"+s.ProjectID, testutil.WithAuth(noScope))
		s.Equal(404, resp.StatusCode, resp.String())
	})
	s.Run("org member", func() {
		s.grant(testutil.AllScopesUser.ID, "member", "")
		resp := s.Client.GET("/api/projects/"+s.ProjectID, testutil.WithAuth(allScopes))
		s.Equal(200, resp.StatusCode, resp.String())
	})
	s.Run("project member", func() {
		s.grant(testutil.AllScopesUser.ID, "", "project_viewer")
		resp := s.Client.GET("/api/projects/"+s.ProjectID, testutil.WithAuth(allScopes))
		s.Equal(200, resp.StatusCode, resp.String())
	})
	s.Run("org admin", func() {
		resp := s.Client.GET("/api/projects/"+s.ProjectID, testutil.WithAuth(adminToken))
		s.Equal(200, resp.StatusCode, resp.String())
	})
}

// ---------------------------------------------------------------------------
// PATCH /api/projects/:id — update requires project_admin or owning-org
// org_admin.
// ---------------------------------------------------------------------------

func (s *ProjectAuthorizationSuite) TestUpdate_Authority() {
	updateBody := testutil.WithJSONBody(map[string]any{"name": "Renamed Project"})

	s.Run("unauthenticated", func() {
		resp := s.Client.PATCH("/api/projects/"+s.ProjectID, updateBody)
		s.Equal(401, resp.StatusCode, resp.String())
	})
	s.Run("foreign", func() {
		resp := s.Client.PATCH("/api/projects/"+s.ProjectID, testutil.WithAuth(noScope), updateBody)
		s.Equal(404, resp.StatusCode, resp.String())
	})
	s.Run("project viewer forbidden", func() {
		s.grant(testutil.AllScopesUser.ID, "", "project_viewer")
		resp := s.Client.PATCH("/api/projects/"+s.ProjectID, testutil.WithAuth(allScopes), updateBody)
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("org member forbidden", func() {
		s.grant(testutil.AllScopesUser.ID, "member", "")
		resp := s.Client.PATCH("/api/projects/"+s.ProjectID, testutil.WithAuth(allScopes), updateBody)
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("project admin", func() {
		s.grant(testutil.AllScopesUser.ID, "member", "project_admin")
		resp := s.Client.PATCH("/api/projects/"+s.ProjectID, testutil.WithAuth(allScopes), updateBody)
		s.Equal(200, resp.StatusCode, resp.String())
	})
	s.Run("org admin", func() {
		resp := s.Client.PATCH("/api/projects/"+s.ProjectID, testutil.WithAuth(adminToken), updateBody)
		s.Equal(200, resp.StatusCode, resp.String())
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/projects/:id — destructive; requires owning-org org_admin.
// ---------------------------------------------------------------------------

func (s *ProjectAuthorizationSuite) TestDelete_Authority() {
	s.Run("unauthenticated", func() {
		resp := s.Client.DELETE("/api/projects/" + s.ProjectID)
		s.Equal(401, resp.StatusCode, resp.String())
	})
	s.Run("foreign", func() {
		resp := s.Client.DELETE("/api/projects/"+s.ProjectID, testutil.WithAuth(noScope))
		s.Equal(404, resp.StatusCode, resp.String())
	})
	s.Run("project admin forbidden", func() {
		s.grant(testutil.AllScopesUser.ID, "member", "project_admin")
		resp := s.Client.DELETE("/api/projects/"+s.ProjectID, testutil.WithAuth(allScopes))
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("org admin", func() {
		resp := s.Client.DELETE("/api/projects/"+s.ProjectID, testutil.WithAuth(adminToken))
		s.Equal(202, resp.StatusCode, resp.String())
	})
}

// ---------------------------------------------------------------------------
// POST /api/projects/:id/restore — cancels a pending deletion; same bar as
// delete (owning-org org_admin).
// ---------------------------------------------------------------------------

func (s *ProjectAuthorizationSuite) TestRestore_Authority() {
	// Mark the project pending deletion first (as org_admin).
	resp := s.Client.DELETE("/api/projects/"+s.ProjectID, testutil.WithAuth(adminToken))
	s.Require().Equal(202, resp.StatusCode, resp.String())

	s.Run("unauthenticated", func() {
		resp := s.Client.POST("/api/projects/" + s.ProjectID + "/restore")
		s.Equal(401, resp.StatusCode, resp.String())
	})
	s.Run("foreign", func() {
		resp := s.Client.POST("/api/projects/"+s.ProjectID+"/restore", testutil.WithAuth(noScope))
		s.Equal(404, resp.StatusCode, resp.String())
	})
	s.Run("project admin forbidden", func() {
		s.grant(testutil.AllScopesUser.ID, "member", "project_admin")
		resp := s.Client.POST("/api/projects/"+s.ProjectID+"/restore", testutil.WithAuth(allScopes))
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("org admin", func() {
		resp := s.Client.POST("/api/projects/"+s.ProjectID+"/restore", testutil.WithAuth(adminToken))
		s.Equal(200, resp.StatusCode, resp.String())
	})
}

// ---------------------------------------------------------------------------
// GET /api/projects/:id/members — read requires project membership or org
// membership.
// ---------------------------------------------------------------------------

func (s *ProjectAuthorizationSuite) TestListMembers_Authority() {
	s.Run("unauthenticated", func() {
		resp := s.Client.GET("/api/projects/" + s.ProjectID + "/members")
		s.Equal(401, resp.StatusCode, resp.String())
	})
	s.Run("foreign", func() {
		resp := s.Client.GET("/api/projects/"+s.ProjectID+"/members", testutil.WithAuth(noScope))
		s.Equal(404, resp.StatusCode, resp.String())
	})
	s.Run("org member forbidden", func() {
		// Member PII: a plain org member (non-admin, not a project member) is
		// not entitled to enumerate member emails/names/roles (aligned with the
		// org member-list bar, #1015).
		s.grant(testutil.AllScopesUser.ID, "member", "")
		resp := s.Client.GET("/api/projects/"+s.ProjectID+"/members", testutil.WithAuth(allScopes))
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("project member", func() {
		s.grant(testutil.AllScopesUser.ID, "", "project_viewer")
		resp := s.Client.GET("/api/projects/"+s.ProjectID+"/members", testutil.WithAuth(allScopes))
		s.Equal(200, resp.StatusCode, resp.String())
	})
	s.Run("org admin", func() {
		resp := s.Client.GET("/api/projects/"+s.ProjectID+"/members", testutil.WithAuth(adminToken))
		s.Equal(200, resp.StatusCode, resp.String())
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/projects/:id/members/:userId — requires project_admin or
// owning-org org_admin.
// ---------------------------------------------------------------------------

func (s *ProjectAuthorizationSuite) TestRemoveMember_Authority() {
	// Victim: a project_user membership we will attempt to remove.
	s.grant(testutil.AllScopesUser.ID, "", "project_user")

	removePath := "/api/projects/" + s.ProjectID + "/members/" + testutil.AllScopesUser.ID

	s.Run("unauthenticated", func() {
		resp := s.Client.DELETE(removePath)
		s.Equal(401, resp.StatusCode, resp.String())
	})
	s.Run("foreign", func() {
		resp := s.Client.DELETE(removePath, testutil.WithAuth(noScope))
		s.Equal(404, resp.StatusCode, resp.String())
	})
	s.Run("project user forbidden", func() {
		s.grant(testutil.WithScopeUser.ID, "member", "project_user")
		resp := s.Client.DELETE(removePath, testutil.WithAuth(withScope))
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("project admin", func() {
		// AdminUser is project_admin of the default project (and org_admin).
		resp := s.Client.DELETE(removePath, testutil.WithAuth(adminToken))
		s.Equal(200, resp.StatusCode, resp.String())
	})
}

// ---------------------------------------------------------------------------
// POST /api/projects — create requires org_admin of the addressed (client
// supplied) org. Mechanism 3: the org id is caller-supplied and must not be
// trusted as authorization truth.
// ---------------------------------------------------------------------------

func (s *ProjectAuthorizationSuite) TestCreate_Authority() {
	newBody := func() testutil.RequestOption {
		return testutil.WithJSONBody(map[string]any{
			"name":  "New Project " + uuid.New().String()[:8],
			"orgId": s.OrgID,
		})
	}

	s.Run("unauthenticated", func() {
		resp := s.Client.POST("/api/projects", newBody())
		s.Equal(401, resp.StatusCode, resp.String())
	})
	s.Run("foreign forbidden", func() {
		// No membership in the org at all → 403 (insufficient authority).
		resp := s.Client.POST("/api/projects", testutil.WithAuth(noScope), newBody())
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("org member forbidden", func() {
		s.grant(testutil.AllScopesUser.ID, "member", "")
		resp := s.Client.POST("/api/projects", testutil.WithAuth(allScopes), newBody())
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("org admin", func() {
		resp := s.Client.POST("/api/projects", testutil.WithAuth(adminToken), newBody())
		s.Equal(201, resp.StatusCode, resp.String())
	})
}

// ---------------------------------------------------------------------------
// Invalid UUID — every authorizeProject-guarded route must return 400
// invalid-uuid (not 500) before any query touches the uuid column.
// ---------------------------------------------------------------------------

func (s *ProjectAuthorizationSuite) TestInvalidUUID_Returns400() {
	body := testutil.WithJSONBody(map[string]any{"name": "x"})

	s.Run("get", func() {
		resp := s.Client.GET("/api/projects/invalid-uuid", testutil.WithAuth(adminToken))
		s.Equal(400, resp.StatusCode, resp.String())
	})
	s.Run("update", func() {
		resp := s.Client.PATCH("/api/projects/invalid-uuid", testutil.WithAuth(adminToken), body)
		s.Equal(400, resp.StatusCode, resp.String())
	})
	s.Run("delete", func() {
		resp := s.Client.DELETE("/api/projects/invalid-uuid", testutil.WithAuth(adminToken))
		s.Equal(400, resp.StatusCode, resp.String())
	})
	s.Run("list members", func() {
		resp := s.Client.GET("/api/projects/invalid-uuid/members", testutil.WithAuth(adminToken))
		s.Equal(400, resp.StatusCode, resp.String())
	})
	s.Run("remove member", func() {
		resp := s.Client.DELETE("/api/projects/invalid-uuid/members/"+testutil.AdminUser.ID, testutil.WithAuth(adminToken))
		s.Equal(400, resp.StatusCode, resp.String())
	})
	s.Run("restore", func() {
		resp := s.Client.POST("/api/projects/invalid-uuid/restore", testutil.WithAuth(adminToken))
		s.Equal(400, resp.StatusCode, resp.String())
	})
}
