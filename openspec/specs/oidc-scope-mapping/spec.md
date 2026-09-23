# oidc-scope-mapping Specification

## Purpose
Defines how Memory derives the effective scope set for OIDC-authenticated requests: explicit Memory scopes carried on the token are authoritative; otherwise scopes are derived from the caller's role in the project declared by `X-Project-ID`; otherwise, for a caller with no project membership, a configurable default scope set applies; otherwise resolution fails closed to an empty set. Derived scopes are re-evaluated per request so role or configuration changes take effect immediately.

## Requirements

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
The system SHALL support a configurable default scope set for authenticated OIDC users who have no explicit Memory grant. The default SHALL be read from the application-owned `MEMORY_OIDC_DEFAULT_SCOPES` environment variable as a comma-separated list and SHALL be empty when unset. The previously shipped `ZITADEL_OIDC_DEFAULT_SCOPES` name SHALL be accepted as a deprecated alias for one release and SHALL emit a startup warning when used; when both names are set, `MEMORY_OIDC_DEFAULT_SCOPES` SHALL win. The default SHALL apply only to an OIDC user who has no project membership in the declared project and holds no application entitlement; a user whose membership carries an unrecognised role SHALL NOT receive the default set.

#### Scenario: Default scope set applied when the user has no membership
- **GIVEN** `MEMORY_OIDC_DEFAULT_SCOPES=data:read,search` is configured
- **AND** an OIDC user has no explicit Memory scope, no application entitlement, and no project membership in the declared project
- **WHEN** the token is validated
- **THEN** the session receives `data:read` and `search`

#### Scenario: Default scope set is not applied to an unrecognised membership role
- **GIVEN** `MEMORY_OIDC_DEFAULT_SCOPES=data:write` is configured
- **AND** an OIDC user holds a project membership whose role is `owner`
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

#### Scenario: Empty default preserves fail-closed behaviour
- **GIVEN** `MEMORY_OIDC_DEFAULT_SCOPES` is unset and its deprecated alias is unset
- **AND** an OIDC user has no explicit Memory scope, no application entitlement, and no project membership
- **WHEN** the token is validated through introspection
- **THEN** the session receives no Memory scopes

#### Scenario: Deprecated name still configures the default set with a warning
- **GIVEN** only `ZITADEL_OIDC_DEFAULT_SCOPES=data:read` is set
- **WHEN** the server starts
- **THEN** the effective default scope set is `data:read` and a deprecation warning naming both variables is emitted

### Requirement: Fail-closed resolution
The system SHALL resolve OIDC scopes in the order: explicit Memory scopes from the token (honoured only while the token-scope trust flag is enabled, and terminal when they apply), then application entitlements (an active superadmin grant, then an `org_admin` organization membership, then a mapped canonical project role), then the configured default set for a user with no project membership and no entitlement, then an empty set. The system SHALL NOT union token-carried scopes with application-derived scopes. The organization-administration set from the `org_admin` tier and the project role set from the project-membership tier SHALL be combined, because they govern disjoint resource families. A project-role lookup error, an unrecognised project role, an unrecognised issuer, a failed token validation, or a disabled token-scope trust flag with no application entitlement and no default configuration SHALL yield an empty scope set and SHALL NOT yield the default set or the full scope catalogue.

#### Scenario: Unmapped role yields no scopes even with a default configured
- **GIVEN** `MEMORY_OIDC_DEFAULT_SCOPES` is configured
- **AND** an OIDC user holds a project membership with an unrecognised role string (e.g. `owner` or a typo)
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

#### Scenario: Role lookup failure fails closed
- **GIVEN** the project-role lookup returns an error
- **AND** `MEMORY_OIDC_DEFAULT_SCOPES` is configured
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

#### Scenario: Token scopes are not consulted when trust is disabled
- **GIVEN** the token-scope trust flag is disabled
- **AND** the token carries a Memory scope
- **AND** the user has no application entitlement and no default is configured
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

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

### Requirement: Explicit Memory scopes are authoritative
When a validated token carries scopes that are part of the Memory scope vocabulary, the system SHALL use those scopes verbatim and terminally — adding no role-derived, entitlement, or default scopes — but only while the `MEMORY_OIDC_TRUST_TOKEN_SCOPES` flag is enabled. The flag SHALL be introduced enabled so that the introduction release changes no effective grant, and its standing default SHALL be disabled from the following release (see `scope-authority` 'Sequenced rollout preserves existing grants'). When that flag is disabled, the system SHALL ignore Memory scope names carried by the token and resolve scopes solely from application-owned state. Non-Memory OIDC scopes (such as `openid`, `profile`, `email`, `offline_access`) SHALL NOT be treated as an explicit grant in any configuration.

#### Scenario: Explicit token scopes win over role derivation
- **GIVEN** `MEMORY_OIDC_TRUST_TOKEN_SCOPES=true`
- **AND** an OIDC user holds `project_viewer` in the declared project
- **AND** the token carries the Memory scope `data:write`
- **WHEN** the token is validated
- **THEN** the session receives `data:write` and is not additionally restricted to the viewer read-only set

#### Scenario: Standard OIDC scopes are not an explicit grant
- **GIVEN** an introspected token carries `openid profile email offline_access`
- **AND** the user has no mapped project role, no application entitlement, and no configured default
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

#### Scenario: Explicit token scopes are ignored once trust is disabled
- **GIVEN** `MEMORY_OIDC_TRUST_TOKEN_SCOPES=false`
- **AND** an OIDC user holds `project_viewer` in the declared project
- **AND** the token carries the Memory scope `data:write`
- **WHEN** the token is validated
- **THEN** the session receives exactly the viewer read-only scopes

### Requirement: Gated all-or-nothing userinfo grant
The system SHALL grant `GetAllScopes()` to an OIDC user validated via the userinfo endpoint only when the app-owned `MEMORY_USERINFO_GRANT_ALL_SCOPES` flag is enabled AND introspection is not configured. The previously shipped `ZITADEL_USERINFO_GRANT_ALL_SCOPES` name SHALL be accepted as a deprecated alias for one release with a startup warning. While the grant is active the system SHALL emit a startup warning and report the active state through health. When introspection is configured, or the flag is disabled, the userinfo path SHALL resolve scopes using the standard fail-closed resolution and SHALL NOT substitute the full scope catalogue. The flag and its grant path SHALL be removed once introspection is the norm.

#### Scenario: Pilot all-grant preserved when introspection is unconfigured
- **GIVEN** `MEMORY_USERINFO_GRANT_ALL_SCOPES=true` and no introspection client credentials are configured
- **WHEN** a user is authenticated via the userinfo endpoint
- **THEN** the session receives the full scope catalogue, a startup warning is emitted, and health reports the grant as active

#### Scenario: All-grant suppressed once introspection is configured
- **GIVEN** introspection client credentials are configured
- **AND** `MEMORY_USERINFO_GRANT_ALL_SCOPES=true`
- **WHEN** a user is authenticated via the userinfo fallback
- **THEN** the session receives scopes from the standard resolution and never the full scope catalogue

#### Scenario: Deprecated name still enables the grant with a warning
- **GIVEN** only `ZITADEL_USERINFO_GRANT_ALL_SCOPES=true` is set and introspection is not configured
- **WHEN** the server starts
- **THEN** the grant behaviour applies and a deprecation warning naming both variables is emitted

### Requirement: Derived scopes are never cached
The system SHALL cache only raw OIDC claims and SHALL re-derive effective scopes on every request, so that a project-role change or a default-scope-set change takes effect on the next request.

#### Scenario: Demotion takes effect on the next request
- **GIVEN** an OIDC user's membership is changed from a write-capable state to `project_viewer`
- **WHEN** the next request is authenticated using a cached introspection entry
- **THEN** the session receives the viewer read-only scopes and no write scope
