package mcp

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Session project control (issue #1163).
//
// The project MCP transports resolve the session's active project at initialize
// time, from X-Project-ID or the initialize params. Clients that can do neither
// — claude.ai custom connectors among them — had to be pinned to one project
// through a per-project connector/proxy route. The `project` tool's list and use
// actions expose the same resolution machinery as a tool:
//
//   - action=list returns the projects the signed-in user belongs to;
//   - action=use switches this session's active project to project_id.
//
// A switch is authorized by the same check initialize performs
// (authorizeProjectClaim), and every later tools/call re-runs that check, so
// losing membership revokes access even for an already-switched session.
const (
	// projectToolName is the consolidated tool that carries the session project
	// actions alongside the project-info reads.
	projectToolName = "project"
	// projectActionList lists the projects the caller's user belongs to.
	projectActionList = "list"
	// projectActionUse switches the session's active project.
	projectActionUse = "use"

	// projectListHandler and projectUseHandler are the per-verb handler names the
	// consolidated actions dispatch to. They stay callable as deprecated aliases
	// (see consolidated_catalog.go), but the session mutation is performed by the
	// transports: only they hold the session.
	projectListHandler = "project-list"
	projectUseHandler  = "project-use"
)

// ProjectSummary is one project the caller's user belongs to, as returned by
// `project` action=list.
type ProjectSummary struct {
	ID               string `json:"id" bun:"id"`
	Name             string `json:"name" bun:"name"`
	OrganizationID   string `json:"organizationId" bun:"organization_id"`
	OrganizationName string `json:"organizationName,omitempty" bun:"organization_name"`
	Role             string `json:"role,omitempty" bun:"role"`
	// Active marks the session's current project.
	Active bool `json:"active"`
}

// projectSessionAction reports whether a tools/call targets the session project
// control actions, returning the action ("list" or "use"). It accepts the
// consolidated spelling (`project` with action=list|use) and the deprecated
// per-verb aliases (`project-list`, `project-use`).
func projectSessionAction(toolName string, args map[string]any) (string, bool) {
	// A deprecated per-verb name carries its action implicitly and cannot be a
	// family name, so resolve it through the alias table first.
	if alias, ok := toolAliases[toolName]; ok {
		if alias.Tool != projectToolName {
			return "", false
		}
		if alias.Action == projectActionList || alias.Action == projectActionUse {
			return alias.Action, true
		}
		return "", false
	}
	if toolName != projectToolName {
		return "", false
	}
	action, _ := actionArg(args)
	if action == projectActionList || action == projectActionUse {
		return action, true
	}
	return "", false
}

// projectUseProjectID reads and validates the project_id argument of a
// `project` action=use call.
func projectUseProjectID(args map[string]any) (string, error) {
	raw, ok := args["project_id"]
	if !ok {
		return "", toolArgErrorf("%s: missing required parameter: project_id", projectUseHandler)
	}
	id, _ := raw.(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", toolArgErrorf("%s: missing required parameter: project_id", projectUseHandler)
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", toolArgErrorf("%s: project_id must be a UUID, got %q", projectUseHandler, id)
	}
	return id, nil
}

// RequiresProjectSwitch reports whether this deployment requires an explicit
// `project` action=use before write tools may run on a session.
func (s *Service) RequiresProjectSwitch() bool {
	return s != nil && s.requireProjectSwitch
}

// isWriteScope reports whether an MCP RequiredScope authorizes a mutation. The
// scope naming convention is "resource:write" for mutations; schema:migrate
// applies migrations and the admin scopes are write-capable. Read scopes
// (graph:read, search, documents:read, …) are never blocked by the
// require-switch gate.
func isWriteScope(scope string) bool {
	switch scope {
	case "":
		return false
	case "schema:migrate", "admin", "admin:all", "admin:write", "project:admin":
		return true
	}
	return strings.HasSuffix(scope, ":write")
}

// ProjectSwitchRequired reports whether a tools/call must be refused because the
// deployment requires an explicit project switch and this session has not done
// one. It is false for the project control actions themselves, for read tools,
// and for unknown tools (dispatch reports those).
func (s *Service) ProjectSwitchRequired(ctx context.Context, projectID string, projectSwitched bool, toolName string, args map[string]any) bool {
	if !s.RequiresProjectSwitch() || projectSwitched {
		return false
	}
	if _, ok := projectSessionAction(toolName, args); ok {
		return false
	}
	// Creating a project does not write into the session's current project, so it
	// is not the accident the gate exists to prevent.
	if legacyDispatchName(toolName, args) == "project-create" {
		return false
	}
	// Resolve through the project-scoped catalog so handler-supplied dynamic
	// tools (source-backed and relay sessions) are classified too; the global
	// index cannot see them.
	def := s.GetToolByNameForProject(ctx, projectID, toolName)
	if def == nil {
		return false
	}
	return isWriteScope(def.RequiredScope)
}

// ProjectSwitchRequiredMessage is the actionable refusal returned by a transport
// when ProjectSwitchRequired is true.
func ProjectSwitchRequiredMessage() string {
	return "Project switch required: call project action=use with a project_id before running write tools on this session. Use project action=list to see the projects you belong to."
}

// ListMemberProjects returns the projects the caller's user may address through
// this MCP credential.
//
// Membership is the org-level grant the initialize check uses: a user belongs to
// every project owned by an organization they are a member of. Project-bound API
// tokens are limited to their bound project (switching to another project would
// be refused by authorizeProjectClaim) unless they were minted by the OAuth
// authorization server, whose grants represent the signed-in user rather than a
// single integration project.
func (s *Service) ListMemberProjects(ctx context.Context, user *auth.AuthUser, activeProjectID string) ([]ProjectSummary, error) {
	if user == nil {
		return nil, apperror.ErrUnauthorized
	}
	// A project-bound token without an OAuth grant can only ever address its own
	// project, so there is no membership query to make.
	boundOnly := false
	if user.APITokenProjectID != "" {
		oauthGranted, err := s.tokenHasActiveOAuthGrant(ctx, user.APITokenID)
		if err != nil {
			return nil, err
		}
		boundOnly = !oauthGranted
	}
	if user.ID == "" && !boundOnly {
		return nil, apperror.ErrUnauthorized
	}

	var projects []ProjectSummary
	if s.db != nil && (user.ID != "" || !boundOnly) {
		var rows []ProjectSummary
		err := s.db.NewSelect().
			TableExpr("kb.projects AS p").
			ColumnExpr("p.id AS id").
			ColumnExpr("p.name AS name").
			ColumnExpr("p.organization_id AS organization_id").
			ColumnExpr("o.name AS organization_name").
			ColumnExpr("om.role AS role").
			Join("JOIN kb.orgs AS o ON o.id = p.organization_id").
			Join("JOIN kb.organization_memberships AS om ON om.organization_id = p.organization_id").
			Where("om.user_id = ?", user.ID).
			Where("p.deleted_at IS NULL").
			Where("o.deleted_at IS NULL").
			OrderExpr("o.name ASC, p.name ASC").
			Scan(ctx, &rows)
		if err != nil {
			return nil, apperror.NewInternal("failed to list member projects", err)
		}
		projects = rows
	}
	if projects == nil {
		projects = []ProjectSummary{}
	}

	if boundOnly {
		filtered := make([]ProjectSummary, 0, 1)
		for _, p := range projects {
			if p.ID == user.APITokenProjectID {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			// The token is authoritative for its bound project even if the owning
			// user's current org membership no longer covers it (shared tokens
			// outlive role changes). Surface it so the agent can see the project
			// it is pinned to.
			summary, err := s.GetProjectSummary(ctx, user.APITokenProjectID, activeProjectID)
			if err != nil {
				return nil, err
			}
			if summary != nil {
				filtered = append(filtered, *summary)
			}
		}
		projects = filtered
	}

	for i := range projects {
		projects[i].Active = activeProjectID != "" && projects[i].ID == activeProjectID
	}
	return projects, nil
}

// GetProjectSummary returns one project's display metadata, or nil when it does
// not exist (or when the service has no database, the unit-test posture).
func (s *Service) GetProjectSummary(ctx context.Context, projectID, activeProjectID string) (*ProjectSummary, error) {
	if projectID == "" || s.db == nil {
		return nil, nil
	}
	var row ProjectSummary
	err := s.db.NewSelect().
		TableExpr("kb.projects AS p").
		ColumnExpr("p.id AS id").
		ColumnExpr("p.name AS name").
		ColumnExpr("p.organization_id AS organization_id").
		ColumnExpr("o.name AS organization_name").
		Join("LEFT JOIN kb.orgs AS o ON o.id = p.organization_id").
		Where("p.id = ?", projectID).
		Where("p.deleted_at IS NULL").
		Scan(ctx, &row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewInternal("failed to resolve project", err)
	}
	row.Active = activeProjectID != "" && row.ID == activeProjectID
	return &row, nil
}

// tokenHasActiveOAuthGrant reports whether tokenID was minted by the built-in
// OAuth authorization server and its grant is still active. Such a token is a
// user-scoped connector credential: its consent picks an initial project, but
// the session may switch to any project the owning user belongs to.
func (s *Service) tokenHasActiveOAuthGrant(ctx context.Context, tokenID string) (bool, error) {
	if tokenID == "" || s.db == nil {
		return false, nil
	}
	exists, err := s.db.NewSelect().
		TableExpr("core.oauth_grants").
		Where("token_id = ?", tokenID).
		Where("revoked_at IS NULL").
		Exists(ctx)
	if err != nil {
		return false, apperror.NewInternal("failed to resolve oauth grant", err)
	}
	return exists, nil
}

// executeListMemberProjects implements `project` action=list.
func (s *Service) executeListMemberProjects(ctx context.Context, projectID string) (*ToolResult, error) {
	user := auth.UserFromContext(ctx)
	projects, err := s.ListMemberProjects(ctx, user, projectID)
	if err != nil {
		return nil, err
	}
	return envelopeResult(true, map[string]any{
		"projects":        projects,
		"activeProjectId": projectID,
		"count":           len(projects),
	}, nil, "")
}

// executeProjectUse is the dispatch entry for `project` action=use. The HTTP
// transports intercept the action and mutate their session; reaching dispatch
// means an in-process caller (an agent run, which is already bound to its
// project) named it, so explain instead of silently doing nothing.
func (s *Service) executeProjectUse(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, toolArgErrorf("%s: switching projects requires an MCP session over HTTP (/api/mcp); an agent run is already bound to its project", projectUseHandler)
}

// projectUseResult builds the success result of a `project` action=use call.
func (s *Service) projectUseResult(ctx context.Context, projectID string) (*ToolResult, error) {
	summary, err := s.GetProjectSummary(ctx, projectID, projectID)
	if err != nil {
		return nil, err
	}
	data := map[string]any{
		"activeProjectId": projectID,
		"message":         "This MCP session now targets the selected project. All later tool calls use it.",
	}
	if summary != nil {
		data["activeProject"] = summary
	}
	return envelopeResult(true, data, nil, "")
}
