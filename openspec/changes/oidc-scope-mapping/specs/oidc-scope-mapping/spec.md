## ADDED Requirements

### Requirement: Request-scoped role derivation
When an OIDC session is authenticated and the request declares a project via `X-Project-ID`, the system SHALL derive Memory scopes from the user's `kb.project_memberships` role for that project. The system SHALL NOT combine roles across the user's other projects into the effective scope set.

#### Scenario: Viewer restricted to read-only in the declared project
- **GIVEN** an OIDC user holds `role = 'project_viewer'` in project P
- **AND** the request declares `X-Project-ID: P`
- **WHEN** the token carries no explicit Memory scope
- **THEN** the session's scopes are exactly `data:read`, `schema:read`, `agents:read`, `projects:read`

#### Scenario: No cross-project widening
- **GIVEN** an OIDC user holds `project_admin` in project A and `project_viewer` in project B
- **AND** the request declares `X-Project-ID: B`
- **WHEN** the token carries no explicit Memory scope
- **THEN** the session receives only the viewer read-only scopes and never an admin scope set

### Requirement: Configurable default scope set
The system SHALL support a configurable default scope set for authenticated OIDC users who have no explicit Memory grant. The default SHALL be read from the `ZITADEL_OIDC_DEFAULT_SCOPES` environment variable as a comma-separated list and SHALL be empty when unset.

#### Scenario: Default scope set applied when no role mapping exists
- **GIVEN** `ZITADEL_OIDC_DEFAULT_SCOPES=data:read,search` is configured
- **AND** an OIDC user has no explicit Memory scope and no mapped project role
- **WHEN** the token is validated
- **THEN** the session receives `data:read` and `search`

#### Scenario: Empty default preserves fail-closed behaviour
- **GIVEN** `ZITADEL_OIDC_DEFAULT_SCOPES` is unset
- **AND** an OIDC user has no explicit Memory scope and no mapped project role
- **WHEN** the token is validated through introspection
- **THEN** the session receives no Memory scopes

### Requirement: Fail-closed resolution
The system SHALL resolve OIDC scopes in the order: explicit Memory scopes from the token, then a mapped project role, then the configured default set, then an empty set. A project-role lookup error, an unrecognised issuer, or a failed token validation SHALL yield an empty scope set and SHALL NOT yield the default set or the full scope catalogue.

#### Scenario: Unmapped role does not exceed today's introspection path
- **GIVEN** `ZITADEL_OIDC_DEFAULT_SCOPES` is unset
- **AND** an OIDC user holds a project membership with an unrecognised role string (e.g. `owner`)
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

#### Scenario: Role lookup failure fails closed
- **GIVEN** the project-role lookup returns an error
- **AND** `ZITADEL_OIDC_DEFAULT_SCOPES` is configured
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

### Requirement: Explicit Memory scopes are authoritative
When a validated token carries scopes that are part of the Memory scope vocabulary, the system SHALL use those scopes verbatim and SHALL NOT add role-derived or default scopes. Non-Memory OIDC scopes (such as `openid`, `profile`, `email`, `offline_access`) SHALL NOT be treated as an explicit grant.

#### Scenario: Explicit token scopes win over role derivation
- **GIVEN** an OIDC user holds `project_viewer` in the declared project
- **AND** the token carries the Memory scope `data:write`
- **WHEN** the token is validated
- **THEN** the session receives `data:write` and is not additionally restricted to the viewer read-only set

#### Scenario: Standard OIDC scopes are not an explicit grant
- **GIVEN** an introspected token carries `openid profile email offline_access`
- **AND** the user has no mapped project role and no configured default
- **WHEN** the token is validated
- **THEN** the session receives no Memory scopes

### Requirement: Gated all-or-nothing userinfo grant
The system SHALL grant `GetAllScopes()` to an OIDC user validated via the userinfo endpoint only when the `ZITADEL_USERINFO_GRANT_ALL_SCOPES` flag is enabled AND introspection is not configured. When introspection is configured, or the flag is disabled, the userinfo path SHALL resolve scopes using the standard fail-closed resolution and SHALL NOT substitute the full scope catalogue.

#### Scenario: Pilot all-grant preserved when introspection is unconfigured
- **GIVEN** `ZITADEL_USERINFO_GRANT_ALL_SCOPES=true` and no introspection client credentials are configured
- **WHEN** a user is authenticated via the userinfo endpoint
- **THEN** the session receives the full scope catalogue

#### Scenario: All-grant suppressed once introspection is configured
- **GIVEN** introspection client credentials are configured
- **AND** `ZITADEL_USERINFO_GRANT_ALL_SCOPES=true`
- **WHEN** a user is authenticated via the userinfo fallback
- **THEN** the session receives scopes from the standard resolution and never the full scope catalogue

### Requirement: Derived scopes are never cached
The system SHALL cache only raw OIDC claims and SHALL re-derive effective scopes on every request, so that a project-role change or a default-scope-set change takes effect on the next request.

#### Scenario: Demotion takes effect on the next request
- **GIVEN** an OIDC user's membership is changed from a write-capable state to `project_viewer`
- **WHEN** the next request is authenticated using a cached introspection entry
- **THEN** the session receives the viewer read-only scopes and no write scope
