package projects

import (
	"os"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/skills"
)

// Module provides the projects domain
var Module = fx.Module("projects",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(provideMCPProjectOrgAdminAuthorizer),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(configureDeletionGracePeriod),
	fx.Invoke(configureSkillCapture),
)

// configureSkillCapture installs the projects domain as the project-policy
// reader for the skill-capture loop and the curator. The wiring lives here
// rather than in skills because the projects package imports skills; the
// reverse would be a cycle.
func configureSkillCapture(svc *Service, capture *skills.CaptureService, curator *skills.Curator, executor *agents.AgentExecutor) {
	if svc == nil {
		return
	}
	if capture != nil {
		capture.SetProjectConfigReader(svc)
	}
	if curator != nil {
		curator.SetProjectConfigReader(svc)
	}
	if executor != nil {
		executor.SetProjectCaptureConfigReader(svc)
	}
}

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
