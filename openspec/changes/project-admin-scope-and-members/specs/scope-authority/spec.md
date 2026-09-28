## ADDED Requirements

### Requirement: Project-tier project:admin token scope

The system SHALL recognise a project-level token scope `project:admin` — a project-scoped admin umbrella — that the project tier MAY mint on a project-bound token only. `project:admin` SHALL expand, through the single `ScopeImplies` relation, to the full set of project scopes — data, schema, agents, graph, branches, journal, skills, documents, search, and chat — and SHALL NOT imply any platform scope (`admin`, `admin:read`, `admin:write`, `admin:all`), `mcp:admin`, or any `org:*` scope. The platform tier is unchanged: bare `admin` and `admin:all` SHALL remain `superadmin_full`-only, and a project or org entitlement SHALL NOT buy platform scope. Minting `project:admin` SHALL be authorized by a decision that requires the caller to be a `project_admin` of the addressed project OR an `org_admin` of the project's owning organization, on a project-bound token; an account-level token (`project_id IS NULL`) SHALL NEVER carry it.

#### Scenario: Project admin mints project:admin on a project-bound token
- **GIVEN** a user holds `project_admin` in project P
- **WHEN** the user requests a project-bound token for P carrying `project:admin`
- **THEN** the request is authorized

#### Scenario: Org admin of the owning org mints project:admin
- **GIVEN** a user holds `org_admin` in the organization that owns project P
- **WHEN** the user requests a project-bound token for P carrying `project:admin`
- **THEN** the request is authorized

#### Scenario: A project user or viewer is refused
- **GIVEN** a user holds `project_user` or `project_viewer` in project P and no `org_admin` membership in P's owning org
- **WHEN** the user requests a project-bound token for P carrying `project:admin`
- **THEN** the request is refused

#### Scenario: Account tokens never carry project:admin
- **GIVEN** a user requests an account-level token (no project binding) carrying `project:admin`
- **WHEN** the token is minted
- **THEN** the request is refused

#### Scenario: project:admin expands to the project tier only
- **GIVEN** a token carries `project:admin`
- **WHEN** the token's effective scopes are expanded
- **THEN** the expansion covers the project data/schema/agents/graph/branches/journal/skills/documents/search/chat scopes and contains no `admin`, `admin:read`, `admin:write`, `admin:all`, `mcp:admin`, or `org:*` scope

#### Scenario: Platform tier is unchanged
- **GIVEN** a user holds `project_admin` in a project and no `superadmin_full` grant
- **WHEN** the user requests a token carrying `admin` or `admin:all`
- **THEN** the request is refused (platform minting remains `superadmin_full`-only)
