package mcp_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ShapeBAuthzSuite proves the six remaining Shape B raw-SQL write sites tracked
// in #1041 now enforce the domain's authorization rather than a bare scope.
//
// Two sites are demonstrated here:
//
//  1. project-create (executeCreateProject): before the fix the tool ran a bare
//     INSERT INTO kb.projects with no org-membership check, so an admin-scoped
//     caller could create a project in any org it named (the org_id arg is
//     client-supplied). Now it enforces the shared
//     projects.Service.AuthorizeOrgAdmin helper — the same helper the REST
//     Create path calls — so a non-org_admin is refused 403 while an org_admin
//     still succeeds.
//
//  2. schema-delete (executeDeleteSchema): the tool now delegates to
//     schemas.Service.DeletePack (the REST DeletePack handler's service), so a
//     caller addressing another project's schema is refused 404 (no existence
//     oracle) while deleting its own schema still succeeds.
type ShapeBAuthzSuite struct {
	testutil.BaseSuite

	mcpSvc *mcp.Service
}

func TestShapeBAuthzSuite(t *testing.T) {
	suite.Run(t, new(ShapeBAuthzSuite))
}

func (s *ShapeBAuthzSuite) SetupSuite() {
	s.SetDBSuffix("mcp_shape_b_authz")
	s.BaseSuite.SetupSuite()
}

func (s *ShapeBAuthzSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	log := slog.Default()
	orgsRepo := orgs.NewRepository(s.DB(), log)
	projectsSvc := projects.NewService(projects.ServiceParams{
		Repo:                projects.NewRepository(s.DB(), log),
		Log:                 log,
		OrgMembershipReader: orgsRepo,
	})
	// graphSvc is nil: DeletePack (the only write this suite exercises) does not
	// invalidate the schema cache.
	schemasSvc := schemas.NewService(schemas.NewRepository(s.DB(), log), nil, log, s.TestDB.Config)

	s.mcpSvc = mcp.NewService(mcp.ServiceParams{
		DB:                        s.DB(),
		Cfg:                       s.TestDB.Config,
		Log:                       log,
		ProjectOrgAdminAuthorizer: projectsSvc.AuthorizeOrgAdmin,
		SchemasSvc:                schemasSvc,
	})
	_ = s.mcpSvc.GetToolDefinitions()
}

func (s *ShapeBAuthzSuite) seedSchema(projectID, name string) string {
	s.T().Helper()
	id := uuid.New()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, project_id, object_type_schemas, source)
		VALUES (?, ?, '1.0.0', ?, '{}'::jsonb, 'custom')
	`, id, name, projectID).Exec(s.Ctx)
	s.Require().NoError(err)
	return id.String()
}

func (s *ShapeBAuthzSuite) assertAppErrorStatus(err error, wantStatus int) {
	s.T().Helper()
	s.Require().Error(err)
	var appErr *apperror.Error
	s.Require().Truef(errors.As(err, &appErr), "expected *apperror.Error, got %T: %v", err, err)
	s.Require().Equalf(wantStatus, appErr.HTTPStatus, "got status %d: %v", appErr.HTTPStatus, err)
}

// --- project-create (executeCreateProject) ---

func (s *ShapeBAuthzSuite) TestProjectCreateNonOrgAdminRefused() {
	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.RegularUser.ID}))
	_, err := s.mcpSvc.ExecuteTool(ctx, "", "project-create", map[string]any{
		"name":   "evil-project",
		"org_id": s.OrgID,
	})
	s.assertAppErrorStatus(err, 403)
}

func (s *ShapeBAuthzSuite) TestProjectCreateOrgAdminSucceeds() {
	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))
	result, err := s.mcpSvc.ExecuteTool(ctx, "", "project-create", map[string]any{
		"name":   "legit-project",
		"org_id": s.OrgID,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	var n int
	err = s.DB().NewRaw(
		`SELECT COUNT(*) FROM kb.projects WHERE name = ? AND organization_id = ?`,
		"legit-project", s.OrgID,
	).Scan(s.Ctx, &n)
	s.Require().NoError(err)
	s.Require().Equal(1, n)
}

// --- schema-delete (executeDeleteSchema) ---

func (s *ShapeBAuthzSuite) TestSchemaDeleteForeignProjectRefused() {
	foreignOrg := uuid.New().String()
	foreignProject := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), foreignOrg, "Foreign Org"))
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    foreignProject,
		OrgID: foreignOrg,
		Name:  "Foreign Project",
	}, testutil.AdminUser.ID))

	foreignSchema := s.seedSchema(foreignProject, "foreign-secret-schema")

	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID})
	_, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-delete", map[string]any{"schema_id": foreignSchema})
	s.assertAppErrorStatus(err, 404)
}

func (s *ShapeBAuthzSuite) TestSchemaDeleteOwnProjectSucceeds() {
	ownSchema := s.seedSchema(s.ProjectID, "own-schema")

	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID})
	result, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-delete", map[string]any{"schema_id": ownSchema})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	var n int
	err = s.DB().NewRaw(`SELECT COUNT(*) FROM kb.graph_schemas WHERE id = ?`, ownSchema).Scan(s.Ctx, &n)
	s.Require().NoError(err)
	s.Require().Equal(0, n)
}
