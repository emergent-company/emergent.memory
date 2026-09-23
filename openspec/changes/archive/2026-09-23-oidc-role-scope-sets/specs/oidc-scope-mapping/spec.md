## MODIFIED Requirements

### Requirement: Request-scoped role derivation
When an OIDC session is authenticated and the request declares a project via `X-Project-ID`, the system SHALL derive Memory scopes from the user's `kb.project_memberships` role for that project. The three canonical roles SHALL map to explicit scope sets: `project_viewer` = `data:read`, `schema:read`, `agents:read`, `projects:read`; `project_user` = the viewer set plus `data:write`; `project_admin` = the user set plus `agents:write` and `schema:write`. The sets SHALL satisfy `project_admin ⊇ project_user ⊇ project_viewer`. The system SHALL NOT combine roles across the user's other projects into the effective scope set. The role scope sets SHALL NOT contain `admin`, `admin:read`, `admin:write`, `admin:all`, `mcp:admin`, any `org:*` scope, `project:invite:create`, or any `account:*` scope.

#### Scenario: Viewer restricted to read-only in the declared project
- **GIVEN** an OIDC user holds `role = 'project_viewer'` in project P
- **AND** the request declares `X-Project-ID: P`
- **WHEN** the token carries no explicit Memory scope
- **THEN** the session's scopes are exactly `data:read`, `schema:read`, `agents:read`, `projects:read`

#### Scenario: User receives the viewer set plus data:write
- **GIVEN** an OIDC user holds `role = 'project_user'` in project P
- **AND** the request declares `X-Project-ID: P`
- **WHEN** the token carries no explicit Memory scope
- **THEN** the session's scopes are exactly `data:read`, `schema:read`, `agents:read`, `projects:read`, `data:write`

#### Scenario: Admin receives the write-inclusive set
- **GIVEN** an OIDC user holds `role = 'project_admin'` in project P
- **AND** the request declares `X-Project-ID: P`
- **WHEN** the token carries no explicit Memory scope
- **THEN** the session's scopes are exactly `data:read`, `schema:read`, `agents:read`, `projects:read`, `data:write`, `agents:write`, `schema:write`

#### Scenario: Role sets are nested
- **WHEN** the three canonical role scope sets are compared
- **THEN** `project_admin` contains every scope in `project_user`, and `project_user` contains every scope in `project_viewer`

#### Scenario: Project roles never grant admin or org administration
- **WHEN** any canonical role scope set is inspected
- **THEN** it contains no `admin*` scope, no `mcp:admin`, no `org:*` scope, no `project:invite:create`, and no `account:*` scope

#### Scenario: No cross-project widening
- **GIVEN** an OIDC user holds `project_admin` in project A and `project_viewer` in project B
- **AND** the request declares `X-Project-ID: B`
- **WHEN** the token carries no explicit Memory scope
- **THEN** the session receives exactly the viewer read-only scopes and never any admin scope

### Requirement: Configurable default scope set
The system SHALL support a configurable default scope set for authenticated OIDC users who have no explicit Memory grant. The default SHALL be read from the `ZITADEL_OIDC_DEFAULT_SCOPES` environment variable as a comma-separated list and SHALL be empty when unset. The default SHALL apply only to an OIDC user who has no project membership in the declared project; a user whose membership carries an unrecognised role SHALL NOT receive the default set.

#### Scenario: Default scope set applied when the user has no membership
- **GIVEN** `ZITADEL_OIDC_DEFAULT_SCOPES=data:read,search` is configured
- **AND** an OIDC user has no explicit Memory scope and no project membership in the declared project
- **WHEN** the token is validated
- **THEN** the session receives `data:read` and `search`

#### Scenario: Default scope set is not applied to an unrecognised membership role
- **GIVEN** `ZITADEL_OIDC_DEFAULT_SCOPES=data:write` is configured
- **AND** an OIDC user holds a project membership whose role is `owner`
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

#### Scenario: Empty default preserves fail-closed behaviour
- **GIVEN** `ZITADEL_OIDC_DEFAULT_SCOPES` is unset
- **AND** an OIDC user has no explicit Memory scope and no project membership
- **WHEN** the token is validated through introspection
- **THEN** the session receives no Memory scopes

### Requirement: Fail-closed resolution
The system SHALL resolve OIDC scopes in the order: explicit Memory scopes from the token, then a mapped canonical project role, then the configured default set for a user with no project membership, then an empty set. A project-role lookup error, an unrecognised project role, an unrecognised issuer, or a failed token validation SHALL yield an empty scope set and SHALL NOT yield the default set or the full scope catalogue.

#### Scenario: Unmapped role yields no scopes even with a default configured
- **GIVEN** `ZITADEL_OIDC_DEFAULT_SCOPES` is configured
- **AND** an OIDC user holds a project membership with an unrecognised role string (e.g. `owner` or a typo)
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

#### Scenario: Role lookup failure fails closed
- **GIVEN** the project-role lookup returns an error
- **AND** `ZITADEL_OIDC_DEFAULT_SCOPES` is configured
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

## ADDED Requirements

### Requirement: Bounded umbrella expansion of role scope sets
When evaluating authorization the system SHALL expand a role's scope set through the umbrella scope map, so a role-derived write scope also satisfies the fine-grained scopes it implies: `schema:write` additionally grants `schema:migrate`, `agents:write` additionally grants `chat:admin`, and `data:write` additionally grants the related write scopes and `journal:write`. This expansion is deliberate and accepted. The system SHALL pin the exact expanded result for each canonical role with an explicit set-equality test, and the expansion SHALL NOT reach any scope excluded from the role sets (`admin*`, `mcp:admin`, `org:*`, `project:invite:create`, `account:*`).

#### Scenario: Expanded viewer set is pinned
- **WHEN** the `project_viewer` role scopes are expanded
- **THEN** the result is exactly the viewer read-only scopes plus the read scopes they imply (`search`, `journal:read`, `chat:use`, `skills:read`, and the implied `*:read` scopes), with no write scope

#### Scenario: Expanded user set is pinned
- **WHEN** the `project_user` role scopes are expanded
- **THEN** the result is exactly the expanded viewer set plus `data:write` and the write scopes it implies, including `schema:write` and `journal:write`

#### Scenario: Expanded admin set is pinned and reaches no excluded scope
- **WHEN** the `project_admin` role scopes are expanded
- **THEN** the result is exactly the expanded user set plus `agents:write`, `chat:admin`, `skills:write`, and `schema:migrate`, and contains no `admin*`, `mcp:admin`, `org:*`, `project:invite:create`, or `account:*` scope
