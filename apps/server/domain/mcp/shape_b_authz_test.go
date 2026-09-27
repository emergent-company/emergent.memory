package mcp_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/graph"
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
	// A real graph.Service is required: AssignPack / UpdateAssignmentBySchemaID /
	// DeleteAssignmentBySchemaID invalidate the schema cache on a write, so a
	// nil graph service would panic. The schema provider is a no-op — cache
	// invalidation is the only surface these schema writes touch.
	graphRepo := graph.NewRepository(s.DB(), log, s.TestDB.Config)
	graphSvc := graph.NewService(graphRepo, log, noopSchemaProvider{}, nil, nil, nil, nil, nil, nil, nil)
	schemasSvc := schemas.NewService(schemas.NewRepository(s.DB(), log), graphSvc, log, s.TestDB.Config)

	s.mcpSvc = mcp.NewService(mcp.ServiceParams{
		DB:                        s.DB(),
		Cfg:                       s.TestDB.Config,
		Log:                       log,
		ProjectOrgAdminAuthorizer: projectsSvc.AuthorizeOrgAdmin,
		SchemasSvc:                schemasSvc,
	})
	_ = s.mcpSvc.GetToolDefinitions()
}

// noopSchemaProvider satisfies graph.SchemaProvider for the schema-write paths
// exercised here; only InvalidateProjectCache is called (by the schema service's
// cache invalidation), never GetProjectSchemas.
type noopSchemaProvider struct{}

func (noopSchemaProvider) GetProjectSchemas(context.Context, string) (*graph.ExtractionSchemas, error) {
	return nil, nil
}

func (noopSchemaProvider) InvalidateProjectCache(string) {}

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
//
// These schema tools declare RequiredScope "schema:write". The in-process
// ExecuteTool gate refuses an untrusted run on that scope, so these Shape B
// (service-layer) tests mark the context transport-enforced — exactly as the
// HTTP transport does after its own scope check — to reach the service seam
// being asserted here.

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

	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))
	_, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-delete", map[string]any{"schema_id": foreignSchema})
	s.assertAppErrorStatus(err, 404)
}

func (s *ShapeBAuthzSuite) TestSchemaDeleteOwnProjectSucceeds() {
	ownSchema := s.seedSchema(s.ProjectID, "own-schema")

	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))
	result, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-delete", map[string]any{"schema_id": ownSchema})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	var n int
	err = s.DB().NewRaw(`SELECT COUNT(*) FROM kb.graph_schemas WHERE id = ?`, ownSchema).Scan(s.Ctx, &n)
	s.Require().NoError(err)
	s.Require().Equal(0, n)
}

// seedForeignProject creates a second org+project owned by the admin user and
// returns the project id, so a caller addressing it is a cross-project caller.
func (s *ShapeBAuthzSuite) seedForeignProject() string {
	s.T().Helper()
	foreignOrg := uuid.New().String()
	foreignProject := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), foreignOrg, "Foreign Org"))
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    foreignProject,
		OrgID: foreignOrg,
		Name:  "Foreign Project",
	}, testutil.AdminUser.ID))
	return foreignProject
}

// --- schema-assign (executeAssignSchema → schemas.AssignVisiblePack) ---

// TestSchemaAssignForeignProjectRefused is the fail-closed guard for #1114: the
// schema-assign tool delegates to schemas.AssignVisiblePack, which must refuse a
// schema the caller's project cannot see (foreign project_id, non-builtin) with 404.
func (s *ShapeBAuthzSuite) TestSchemaAssignForeignProjectRefused() {
	foreignProject := s.seedForeignProject()
	foreignSchema := s.seedSchema(foreignProject, "foreign-secret-schema")

	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))
	_, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-assign", map[string]any{"schema_id": foreignSchema})
	s.assertAppErrorStatus(err, 404)
}

func (s *ShapeBAuthzSuite) TestSchemaAssignOwnProjectSucceeds() {
	ownSchema := s.seedSchema(s.ProjectID, "own-schema")

	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))
	result, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-assign", map[string]any{"schema_id": ownSchema})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	var n int
	err = s.DB().NewRaw(
		`SELECT COUNT(*) FROM kb.project_schemas WHERE project_id = ? AND schema_id = ? AND removed_at IS NULL`,
		s.ProjectID, ownSchema,
	).Scan(s.Ctx, &n)
	s.Require().NoError(err)
	s.Require().Equal(1, n)
}

// --- schema-assignment-update (executeUpdateTemplateAssignment) ---

func (s *ShapeBAuthzSuite) TestSchemaAssignmentUpdateForeignProjectRefused() {
	foreignProject := s.seedForeignProject()
	foreignSchema := s.seedSchema(foreignProject, "foreign-schema")

	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))
	_, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-assignment-update", map[string]any{
		"schema_id": foreignSchema,
		"active":    false,
	})
	s.assertAppErrorStatus(err, 404)
}

// --- schema-uninstall (executeUninstallSchema) ---

func (s *ShapeBAuthzSuite) TestSchemaUninstallForeignProjectRefused() {
	foreignProject := s.seedForeignProject()
	foreignSchema := s.seedSchema(foreignProject, "foreign-schema")

	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))
	_, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-uninstall", map[string]any{"schema_id": foreignSchema})
	s.assertAppErrorStatus(err, 404)
}

// TestSchemaAssignmentUpdateAndUninstallOwnSucceeds exercises the happy path for
// the two assignment tools: assign → deactivate → uninstall, all through the
// shared schemas service seam.
func (s *ShapeBAuthzSuite) TestSchemaAssignmentUpdateAndUninstallOwnSucceeds() {
	ownSchema := s.seedSchema(s.ProjectID, "own-schema")

	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))

	_, err := s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-assign", map[string]any{"schema_id": ownSchema})
	s.Require().NoError(err)

	_, err = s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-assignment-update", map[string]any{
		"schema_id": ownSchema,
		"active":    false,
	})
	s.Require().NoError(err)

	var active bool
	err = s.DB().NewRaw(
		`SELECT active FROM kb.project_schemas WHERE project_id = ? AND schema_id = ? AND removed_at IS NULL`,
		s.ProjectID, ownSchema,
	).Scan(s.Ctx, &active)
	s.Require().NoError(err)
	s.Require().False(active)

	_, err = s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-uninstall", map[string]any{"schema_id": ownSchema})
	s.Require().NoError(err)

	var n int
	err = s.DB().NewRaw(
		`SELECT COUNT(*) FROM kb.project_schemas WHERE project_id = ? AND schema_id = ? AND removed_at IS NULL`,
		s.ProjectID, ownSchema,
	).Scan(s.Ctx, &n)
	s.Require().NoError(err)
	s.Require().Equal(0, n)
}

// TestSchemaDeleteForeignAssignedSchemaRefused is the fail-closed guard for
// #1116: a foreign schema that is assigned to another project must yield 404
// (the existence oracle), not the 400 "assigned to projects" branch, which the
// pre-fix ordering leaked because the assignment check ran before ownership.
func (s *ShapeBAuthzSuite) TestSchemaDeleteForeignAssignedSchemaRefused() {
	foreignProject := s.seedForeignProject()
	foreignSchema := s.seedSchema(foreignProject, "foreign-assigned-schema")

	// Install the foreign schema into its own project so DeletePack's
	// assignment-count branch would fire if it ran before the ownership oracle.
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
		VALUES (?, ?, true, NOW())
	`, foreignProject, foreignSchema).Exec(s.Ctx)
	s.Require().NoError(err)

	ctx := mcp.ContextWithTransportEnforced(auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID}))
	_, err = s.mcpSvc.ExecuteTool(ctx, s.ProjectID, "schema-delete", map[string]any{"schema_id": foreignSchema})
	s.assertAppErrorStatus(err, 404)
}
