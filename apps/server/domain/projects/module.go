package projects

import (
	"os"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// Module provides the projects domain
var Module = fx.Module("projects",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(provideMCPProjectOrgAdminAuthorizer),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(configureDeletionGracePeriod),
)

// provideMCPProjectOrgAdminAuthorizer exposes projects.Service.AuthorizeOrgAdmin
// as mcp.ProjectOrgAdminAuthorizer so the MCP project-create tool enforces the
// same org_admin authority as the REST Create path (issue #1041). projects
// imports mcp here; mcp does not import projects, so there is no import cycle.
func provideMCPProjectOrgAdminAuthorizer(svc *Service) mcp.ProjectOrgAdminAuthorizer {
	return svc.AuthorizeOrgAdmin
}

// configureDeletionGracePeriod wires PROJECT_DELETION_GRACE_PERIOD (a Go
// duration string, e.g. "1h") into the projects service. Invalid/absent values
// keep the service default.
func configureDeletionGracePeriod(svc *Service) {
	if svc == nil {
		return
	}
	if raw := os.Getenv("PROJECT_DELETION_GRACE_PERIOD"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			svc.ConfigureDeletionGracePeriod(d)
		}
	}
}
