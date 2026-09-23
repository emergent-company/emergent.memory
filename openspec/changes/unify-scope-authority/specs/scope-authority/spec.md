# scope-authority

## Purpose

Defines the single-authority posture for Memory's fine-grained authorization: the application is the sole authority for fine-grained Memory scopes, and the identity provider authenticates only. It specifies the two mutually exclusive authentication planes, the OIDC session scope resolution tiers and the exact grant of each tier, the opt-in trust model for scopes carried on OIDC tokens, the time-boxed permissive userinfo grant, the organization-scoped entitlement check, the canonical organization membership role, the sequenced rollout, and the observability of the authority posture.

## ADDED Requirements

### Requirement: Single fine-grained scope authority

The system SHALL treat the application as the sole authority for fine-grained Memory scopes. The identity provider SHALL be used for authentication only: it establishes the subject identity and MAY carry at most one coarse, app-defined signal. An OIDC token SHALL NOT be able to grant a fine-grained Memory scope in the target state: the standing default of the token-scope trust flag SHALL be disabled. During the sequenced rollout the flag is introduced enabled and flips to disabled in the following release (see 'Sequenced rollout preserves existing grants'). No other third party SHALL be an authority for Memory scopes. Where an identity-provider-side coarse signal is configured, it SHALL map to at most one application-side superadmin grant and SHALL NOT populate any other entitlement tier. The coarse signal is delivered only through RFC 7662 token introspection: the userinfo fallback carries no role claims, so a role-derived superadmin grant is introspection-only and re-resolved on every request.

#### Scenario: A token-carried Memory scope is not a grant by default
- **GIVEN** the token-scope trust flag is disabled
- **AND** a validated OIDC token carries the Memory scope `data:write`
- **WHEN** the session scopes are resolved
- **THEN** the token-carried `data:write` is not granted

#### Scenario: The identity provider is not consulted for fine-grained grants
- **GIVEN** the token-scope trust flag is disabled
- **WHEN** the session scopes are resolved for an OIDC user
- **THEN** every granted scope originates from application-owned state (`core.api_tokens`, `core.superadmins`, `kb.organization_memberships`, `kb.project_memberships`, or the app-owned default set)

#### Scenario: A coarse signal maps to one superadmin grant
- **GIVEN** an operator has configured a single coarse identity-provider signal mapped to a superadmin grant
- **WHEN** an identity carrying that signal authenticates
- **THEN** the identity receives the superadmin entitlement and no fine-grained scope is taken from the token

### Requirement: Target scope resolution order

The system SHALL resolve effective scopes from exactly one of two mutually exclusive authentication planes. For an `emt_*` API token, the effective scopes SHALL be the token's own scopes. For an OIDC session, the system SHALL resolve scopes in the order: token-carried Memory scopes (only while the token-scope trust flag is enabled, and terminal when it applies), then a superadmin grant, then an `org_admin` organization membership, then the project membership role for the project declared by `X-Project-ID`, then the app-owned default scope set, then an empty set. Resolution SHALL fail closed to an empty set when no tier grants a scope. The system SHALL NOT union token-carried scopes with application-derived scopes.

#### Scenario: API-token scopes are the effective scopes for machine callers
- **GIVEN** a request authenticated with an `emt_*` API token carrying `projects:read`
- **WHEN** the session scopes are resolved
- **THEN** the effective scopes are the API token's scopes and no OIDC tier is consulted

#### Scenario: Application entitlements are evaluated for OIDC sessions
- **GIVEN** an OIDC-authenticated request with no API token
- **AND** the token-scope trust flag is disabled
- **AND** the user holds `project_viewer` in the project declared by `X-Project-ID`
- **WHEN** the session scopes are resolved
- **THEN** the session receives exactly the viewer read-only scopes

#### Scenario: No tier grants a scope fails closed
- **GIVEN** an OIDC-authenticated request
- **AND** the token-scope trust flag is disabled
- **AND** the user has no superadmin grant, no organization membership, no project membership, and no app-owned default is configured
- **WHEN** the session scopes are resolved
- **THEN** the session receives no Memory scopes

### Requirement: Entitlement tier grants

The system SHALL grant, for an OIDC session: a **full** superadmin grant (`superadmin_full`) the full scope catalogue, terminally; a **read-only** superadmin grant (`superadmin_readonly`) SHALL NOT receive the full scope catalogue and SHALL receive at most a bounded read-only set; an `org_admin` membership the organization-administration scope set, comprising `org:read`, `org:invite:create`, `org:project:create`, and `org:project:delete`, and containing no `project:*` scope and no data, schema, or agent scope; and a project membership the role scope set for the declared project. The organization-administration set and the project role set SHALL be combined, because they govern disjoint resource families. A full superadmin grant SHALL be terminal and SHALL NOT be combined with lower tiers. The request's organization SHALL be resolved from the declared project's owning organization, or else from an org context validated by the authentication middleware; where neither is available the `org_admin` tier SHALL NOT be granted. An `org_admin` membership SHALL NOT widen the project-scope tier: the organization-administration set SHALL contain no `project:*` scope, so it cannot add a project scope a pure project member would not have.

#### Scenario: Superadmin receives the full catalogue
- **GIVEN** a user holds an active `superadmin_full` grant
- **WHEN** the session scopes are resolved
- **THEN** the session receives the full scope catalogue

#### Scenario: A read-only superadmin does not receive the full catalogue
- **GIVEN** a user holds a `superadmin_readonly` grant
- **WHEN** the session scopes are resolved
- **THEN** the session does not receive the full scope catalogue and receives no write or admin scope

#### Scenario: Organization administrator receives the organization-administration set
- **GIVEN** a user holds `org_admin` in the request's organization and no project membership
- **WHEN** the session scopes are resolved
- **THEN** the session receives exactly `org:read`, `org:invite:create`, `org:project:create`, and `org:project:delete`, and no `project:*`, data, schema, or agent scope

#### Scenario: Organization and project tiers combine
- **GIVEN** a user holds `org_admin` in the request's organization
- **AND** the user holds `project_viewer` in the project declared by `X-Project-ID`
- **WHEN** the session scopes are resolved
- **THEN** the session receives the union of the organization-administration set and the viewer read-only set

#### Scenario: Organization administration never widens project scopes
- **GIVEN** a user holds `org_admin` in the request's organization
- **AND** the user holds `project_viewer` in the project declared by `X-Project-ID`
- **WHEN** the session scopes are resolved
- **THEN** the session contains no project write scope and no data, schema, or agent write scope

#### Scenario: Organization administration is scoped to the request's organization
- **GIVEN** a user holds `org_admin` in organization A but not in organization B
- **AND** the request declares a project owned by organization B
- **WHEN** the session scopes are resolved
- **THEN** the session does not receive the organization-administration set from the organization A membership

### Requirement: Organization-scoped entitlement decisions

The system SHALL authorize organization-scoped decisions with a check of: an active **full** superadmin grant (`superadmin_full`), or an `org_admin` membership. A `superadmin_readonly` grant SHALL NOT satisfy this check. The check SHALL NOT consult the project membership role. `admin:all` token minting SHALL be authorized through this check. The check SHALL preserve the existing any-organization semantics of `org_admin` eligibility (an `org_admin` in any organization qualifies), so replacing the existing bespoke query does not silently narrow cross-organization behaviour. The check SHALL be defined once and consumed by every org-scoped decision; existing bespoke membership queries SHALL become consumers of it.

#### Scenario: Organization administrator is authorized to mint an admin:all token
- **GIVEN** a user holds `org_admin` in an organization and no superadmin grant
- **WHEN** the user requests an `admin:all` token
- **THEN** the request is authorized

#### Scenario: A project administrator alone is refused
- **GIVEN** a user holds `project_admin` in the declared project and neither a superadmin grant nor an `org_admin` membership
- **WHEN** the user requests an `admin:all` token
- **THEN** the request is refused

#### Scenario: A user with neither entitlement is refused
- **GIVEN** a user holds neither a superadmin grant nor an `org_admin` membership
- **WHEN** the user requests an `admin:all` token
- **THEN** the request is refused

#### Scenario: A read-only superadmin is refused admin:all minting
- **GIVEN** a user holds a `superadmin_readonly` grant and no `org_admin` membership
- **WHEN** the user requests an `admin:all` token
- **THEN** the request is refused

### Requirement: App-owned configuration vocabulary

The system SHALL name its scope-policy configuration with application-owned variable names: `MEMORY_OIDC_DEFAULT_SCOPES`, `MEMORY_USERINFO_GRANT_ALL_SCOPES`, and `MEMORY_OIDC_TRUST_TOKEN_SCOPES`. Each previously shipped `ZITADEL_*` name SHALL be accepted as a deprecated alias for one release and SHALL emit a startup warning when used. When both the canonical name and its alias are set, the canonical name SHALL win and the alias SHALL be ignored with a warning. The aliases SHALL be removed in the release following the deprecation.

#### Scenario: Canonical name is preferred over the deprecated alias
- **GIVEN** both `MEMORY_OIDC_DEFAULT_SCOPES=data:read` and `ZITADEL_OIDC_DEFAULT_SCOPES=data:write` are set
- **WHEN** the configuration is loaded
- **THEN** the effective default scope set is `data:read` and a warning is emitted

#### Scenario: Deprecated alias is honoured with a warning
- **GIVEN** only `ZITADEL_OIDC_DEFAULT_SCOPES=data:read` is set
- **WHEN** the configuration is loaded
- **THEN** the effective default scope set is `data:read` and a deprecation warning naming both variables is emitted

### Requirement: Token-carried Memory scopes are opt-in

The system SHALL honour Memory scope names carried on a validated OIDC token only while the `MEMORY_OIDC_TRUST_TOKEN_SCOPES` flag is enabled. When enabled, the token's Memory scopes SHALL be used verbatim and terminally, and SHALL NOT be combined with application-derived scopes. When the flag is disabled, the system SHALL ignore all Memory scope names on the token and SHALL resolve scopes through application entitlements, then the app-owned default set, then an empty set. The final state of the system SHALL be the flag disabled, with the token-scope grant path removed entirely.

#### Scenario: Explicit token scopes are honoured while trust is enabled
- **GIVEN** `MEMORY_OIDC_TRUST_TOKEN_SCOPES=true`
- **AND** a validated token carries the Memory scope `schema:write`
- **WHEN** the session scopes are resolved
- **THEN** the session receives `schema:write` verbatim and no role-derived or default scope is added

#### Scenario: Token scopes are ignored once trust is disabled
- **GIVEN** `MEMORY_OIDC_TRUST_TOKEN_SCOPES=false`
- **AND** a validated token carries the Memory scope `schema:write`
- **AND** the user holds `project_viewer` in the declared project
- **WHEN** the session scopes are resolved
- **THEN** the session receives exactly the viewer read-only scopes and never `schema:write`

### Requirement: Time-boxed permissive userinfo grant

While the permissive userinfo all-or-nothing grant is active (the grant flag is enabled and token introspection is not configured), the system SHALL emit a startup warning naming the flag and the introspection precondition, and SHALL expose the state through a health field. The system SHALL remove the grant flag and its grant path once introspection is the norm, after which the userinfo fallback SHALL use the standard fail-closed resolution. The entitlement tiers SHALL exist before the grant path is removed.

#### Scenario: Permissive grant is visible while active
- **GIVEN** the userinfo grant-all flag is enabled and introspection is not configured
- **WHEN** the server starts and the health endpoint is queried
- **THEN** a startup warning is emitted and the health response reports the permissive grant as active

#### Scenario: Enabling introspection disables the permissive grant
- **GIVEN** the userinfo grant-all flag is enabled and introspection client credentials are configured
- **WHEN** a user is authenticated via the userinfo fallback
- **THEN** the session receives scopes from the standard resolution and never the full scope catalogue, and the health field reports the permissive grant as inactive

### Requirement: Canonical organization membership role

The canonical role stored in `kb.organization_memberships` SHALL be `org_admin`. No server code path SHALL write `owner` as an organization membership role. A migration SHALL normalise existing `kb.organization_memberships` rows whose role is `owner` to `org_admin`, idempotently, and SHALL be documented as a one-way normalisation. The spec text of any capability that describes `owner` as a valid `kb.organization_memberships` role SHALL be corrected.

#### Scenario: Organization creation writes the canonical role
- **WHEN** an organization is created
- **THEN** the creator's `kb.organization_memberships` row is written with `role = 'org_admin'`

#### Scenario: Standalone bootstrap writes the canonical role
- **WHEN** standalone bootstrap creates the default organization and its membership
- **THEN** the membership is written with `role = 'org_admin'`

#### Scenario: Legacy owner organization rows are normalised
- **GIVEN** a `kb.organization_memberships` row exists with `role = 'owner'`
- **WHEN** the normalisation migration runs
- **THEN** the row's role becomes `org_admin`

### Requirement: Sequenced rollout preserves existing grants

The system SHALL introduce the app-owned vocabulary, the entitlement tiers, and the token-scope trust flag before changing any default, and SHALL NOT remove a grant path in the same release that removes its replacement. The token-scope trust flag SHALL default to enabled when introduced and to disabled in the following release. The `ZITADEL_*` aliases SHALL be removed in the release following the deprecation. A default change introduced by the rollout SHALL be reversible by explicit configuration in the release in which it takes effect.

#### Scenario: Introduction of the trust flag does not change effective grants
- **GIVEN** the token-scope trust flag is introduced with its default enabled
- **AND** a token carries a Memory scope
- **WHEN** the session scopes are resolved
- **THEN** the effective scopes are unchanged from before the flag was introduced

#### Scenario: The default flip is announced before it happens
- **GIVEN** the token-scope trust flag is enabled by default in the current release
- **WHEN** the server starts
- **THEN** a deprecation warning states that token-carried Memory scopes will stop being honoured in the next release and names the flag

#### Scenario: A flipped default is reversible without a downgrade
- **GIVEN** the token-scope trust flag has flipped to disabled by default
- **WHEN** an operator sets the flag to enabled
- **THEN** token-carried Memory scopes are honoured again in that deployment

### Requirement: Scope-authority observability

The system SHALL expose the scope-authority posture through a dedicated `scope_authority` object in the health response, distinct from the generic `checks` map, with the boolean fields `token_scopes_trusted`, `permissive_all_grant`, and `introspection_configured`. The object SHALL be additive and SHALL NOT change the status of the existing checks.

#### Scenario: Health reports the authority posture
- **WHEN** the health endpoint is queried
- **THEN** the response contains a `scope_authority` object with the fields `token_scopes_trusted`, `permissive_all_grant`, and `introspection_configured`

#### Scenario: Posture fields reflect configuration
- **GIVEN** the token-scope trust flag is disabled and introspection is configured
- **WHEN** the health endpoint is queried
- **THEN** `token_scopes_trusted` is false, `introspection_configured` is true, and `permissive_all_grant` is false
