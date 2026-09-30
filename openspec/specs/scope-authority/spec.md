# scope-authority

## Purpose

Defines the single-authority posture for Memory's fine-grained authorization: the application is the sole authority for fine-grained Memory scopes, and the identity provider authenticates only. It specifies the two mutually exclusive authentication planes, the OIDC session scope resolution tiers and the exact grant of each tier, the removal of the duplicate token-scope authority (token-carried scopes are never a grant and the permissive userinfo all-grant is gone), the organization-scoped entitlement check, the canonical organization membership role, the sequenced rollout, and the observability of the authority posture.

## Requirements

### Requirement: Single fine-grained scope authority

The system SHALL treat the application as the sole authority for fine-grained Memory scopes. The identity provider SHALL be used for authentication only: it establishes the subject identity and MAY carry at most one coarse, app-defined signal. An OIDC token SHALL NOT be able to grant a fine-grained Memory scope: the token-scope grant path SHALL be absent entirely, so no token-carried scope, role, or claim other than the one coarse superadmin signal can produce a fine-grained grant. No configuration flag SHALL reintroduce a token-carried fine-grained grant. No other third party SHALL be an authority for Memory scopes. Where an identity-provider-side coarse signal is configured, it SHALL map to at most one application-side superadmin grant and SHALL NOT populate any other entitlement tier. The coarse signal is delivered only through RFC 7662 token introspection: the userinfo fallback carries no role claims, so a role-derived superadmin grant is introspection-only and re-resolved on every request. The signal is a Zitadel project role carried in the token's `urn:zitadel:iam:org:project:{projectID}:roles` claim, whose value is `{role: {orgID: orgDomain}}` — organization IDs are the KEYS of the inner map (org domains are the values); a claim of any other shape SHALL yield no grant (fail closed).

#### Scenario: A token-carried Memory scope is never a grant
- **GIVEN** a validated OIDC token carries the Memory scope `data:write`
- **WHEN** the session scopes are resolved
- **THEN** the token-carried `data:write` is not granted

#### Scenario: The identity provider is not consulted for fine-grained grants
- **WHEN** the session scopes are resolved for an OIDC user
- **THEN** every granted scope originates from application-owned state (`core.api_tokens`, `core.superadmins`, `kb.organization_memberships`, `kb.project_memberships`, or the app-owned default set)

#### Scenario: A coarse signal maps to one superadmin grant
- **GIVEN** an operator has configured a single coarse identity-provider signal mapped to a superadmin grant
- **WHEN** an identity carrying that signal authenticates
- **THEN** the identity receives the superadmin entitlement and no fine-grained scope is taken from the token

### Requirement: Target scope resolution order

The system SHALL resolve effective scopes from exactly one of two mutually exclusive authentication planes. For an `emt_*` API token, the effective scopes SHALL be the token's own scopes. For an OIDC session, the system SHALL resolve scopes in the order: a superadmin grant, then an `org_admin` organization membership, then the project membership role for the project declared by `X-Project-ID`, then the app-owned default scope set, then an empty set. Resolution SHALL fail closed to an empty set when no tier grants a scope. The system SHALL NOT union token-carried scopes with application-derived scopes.

#### Scenario: API-token scopes are the effective scopes for machine callers
- **GIVEN** a request authenticated with an `emt_*` API token carrying `projects:read`
- **WHEN** the session scopes are resolved
- **THEN** the effective scopes are the API token's scopes and no OIDC tier is consulted

#### Scenario: Application entitlements are evaluated for OIDC sessions
- **GIVEN** an OIDC-authenticated request with no API token
- **AND** the user holds `project_viewer` in the project declared by `X-Project-ID`
- **WHEN** the session scopes are resolved
- **THEN** the session receives exactly the viewer read-only scopes

#### Scenario: No tier grants a scope fails closed
- **GIVEN** an OIDC-authenticated request
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

### Requirement: App-owned configuration vocabulary

The system SHALL name its remaining scope-policy configuration with an application-owned variable name: `MEMORY_OIDC_DEFAULT_SCOPES`. The other two scope-policy knobs — `MEMORY_USERINFO_GRANT_ALL_SCOPES` and `MEMORY_OIDC_TRUST_TOKEN_SCOPES` — SHALL be absent together with their grant paths and SHALL NOT be read. The previously shipped `ZITADEL_*` aliases SHALL also be absent. No environment variable SHALL enable a token-carried Memory scope grant or a permissive userinfo all-grant.

#### Scenario: Default scope set is configured by its application-owned name
- **GIVEN** `MEMORY_OIDC_DEFAULT_SCOPES=data:read` is set
- **WHEN** the configuration is loaded
- **THEN** the effective default scope set is `data:read`

#### Scenario: Removed knobs are not read
- **WHEN** the server starts
- **THEN** `MEMORY_USERINFO_GRANT_ALL_SCOPES`, `MEMORY_OIDC_TRUST_TOKEN_SCOPES`, and the `ZITADEL_*` aliases are not read, and no environment variable enables a token-carried Memory scope grant or a permissive userinfo all-grant

### Requirement: Token-carried Memory scopes are never a grant

The system SHALL ignore every Memory scope name carried on a validated OIDC token. It SHALL resolve scopes through application entitlements, then the app-owned default set, then an empty set, and SHALL NOT honour, filter, or union token-carried Memory scope names under any configuration. The token-scope grant path and the `MEMORY_OIDC_TRUST_TOKEN_SCOPES` flag SHALL be absent.

#### Scenario: Token scopes are ignored
- **GIVEN** a validated token carries the Memory scope `schema:write`
- **AND** the user holds `project_viewer` in the declared project
- **WHEN** the session scopes are resolved
- **THEN** the session receives exactly the viewer read-only scopes and never `schema:write`

#### Scenario: No configuration restores token-scope grants
- **WHEN** the server starts
- **THEN** no environment variable enables token-carried Memory scopes

### Requirement: Userinfo fallback uses fail-closed resolution

The permissive userinfo all-or-nothing grant SHALL be removed: the grant flag and its grant path SHALL be absent, and the userinfo fallback SHALL use the standard fail-closed resolution like every other path. A userinfo-authenticated user SHALL receive exactly the application-derived scopes for their membership (or the app-owned default, or nothing), and SHALL never receive the full scope catalogue without an explicit entitlement.

#### Scenario: Userinfo user receives exactly their entitlement
- **GIVEN** an OIDC user is authenticated via the userinfo fallback
- **AND** the user holds `project_viewer` in the declared project
- **WHEN** the session scopes are resolved
- **THEN** the session receives exactly the viewer read-only scopes

#### Scenario: Userinfo user with no entitlement receives nothing
- **GIVEN** an OIDC user is authenticated via the userinfo fallback
- **AND** the user has no membership and no app-owned default is configured
- **WHEN** the session scopes are resolved
- **THEN** the session receives no scopes and never the full scope catalogue

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

The system SHALL land the app-owned vocabulary, the entitlement tiers, and the token-scope trust flag before changing any default, SHALL NOT remove a grant path in the same release that removes its replacement, and SHALL then remove the flag and the token-scope grant path once the app-owned entitlement tiers exist. The rollout SHALL proceed in order: introduce the flag enabled (no behaviour change), flip its default off (opt-in), then remove the flag and the token-scope grant path entirely. The `ZITADEL_*` aliases SHALL be removed in the release following the deprecation.

#### Scenario: The rollout preserved grants at every step
- **GIVEN** the app-owned entitlement tiers exist
- **WHEN** the token-scope grant path is removed
- **THEN** every principal reaches their grants through application-owned state

#### Scenario: The removed flag cannot be re-enabled
- **WHEN** the server starts
- **THEN** `MEMORY_OIDC_TRUST_TOKEN_SCOPES` is not read and token-carried Memory scopes remain ignored

### Requirement: Scope-authority observability

The system SHALL expose the scope-authority posture through a dedicated `scope_authority` object in the health response, served on an authenticated endpoint and distinct from the generic `checks` map, with the boolean field `introspection_configured`. The object SHALL be additive and SHALL NOT change the status of the existing checks. The `token_scopes_trusted` and `permissive_all_grant` fields SHALL be removed with their grant paths.

#### Scenario: Health reports the authority posture
- **WHEN** the health endpoint is queried
- **THEN** the response contains a `scope_authority` object with the field `introspection_configured`

#### Scenario: Posture fields reflect configuration
- **GIVEN** introspection is configured
- **WHEN** the health endpoint is queried
- **THEN** `introspection_configured` is true

### Requirement: Route-derived organization context

The system SHALL resolve a request's organization context from a server-derived source only: the owning organization of the project declared by `X-Project-ID` (`kb.projects.organization_id`), or an organization context already validated by the authentication middleware. A client-supplied `X-Org-ID` header SHALL NOT be a trust source, in any posture, including a variant that performs a membership check on the header value: org context is route/project-derived only. When no server-derived organization context is available the request organization SHALL be empty. When a `X-Org-ID` header is present and conflicts with the owning organization of the declared project, the request SHALL be rejected with `403`, so a forged header can never widen access. A `X-Org-ID` header SHALL NOT influence scope resolution, and therefore SHALL NOT produce an `org_admin` entitlement.

#### Scenario: A bare X-Org-ID is not trusted
- **GIVEN** an authenticated request with an `X-Org-ID` header and no declared project
- **WHEN** the request organization is resolved
- **THEN** the request organization is empty and the header is discarded as a trust source

#### Scenario: A header conflicting with the declared project's owning organization is rejected
- **GIVEN** a request declaring project P, owned by organization A
- **AND** the request carries `X-Org-ID: B`
- **WHEN** the request organization is resolved
- **THEN** the request is rejected with `403`

#### Scenario: The declared project's owning organization is authoritative
- **GIVEN** an authenticated request declaring project P, owned by organization A
- **WHEN** the request organization is resolved
- **THEN** the request organization is A, whether or not an `X-Org-ID` header is present

#### Scenario: A spoofed X-Org-ID cannot produce an org-admin entitlement
- **GIVEN** an authenticated request that carries `X-Org-ID: B`
- **AND** the declared project is owned by organization A
- **AND** the user is not an `org_admin` of organization A
- **WHEN** the session scopes are resolved
- **THEN** the session does not receive the organization-administration set

### Requirement: Shared organization-administration decision

The system SHALL authorize every organization-scoped decision through a single app-side check, `CanAdministerOrg`, that requires an `org_admin` membership in the **addressed** organization (`kb.organization_memberships`, `role = 'org_admin'`). The check SHALL fail closed: no membership, a plain member, an empty user or org, a lookup error, or an absent database SHALL all yield false. The check SHALL NOT consult the project membership role and SHALL NOT admit a platform superadmin grant, so routing a decision point that previously refused a platform superadmin who was not an `org_admin` through this check cannot widen it. The organization-scoped decision points that consume it SHALL include organization settings mutations, organization member invitation, and project create/delete/transfer within an organization; each such decision point SHALL NOT re-derive the role locally or run a bespoke membership query.

The two organization-invitation decision points that already admitted an active `superadmin_full` before this change (org-admin invitation creation and its acceptance-time re-check, plus invitation revocation) SHALL consume `CanAdministerOrgOrPlatform`, which is `CanAdministerOrg` composed with the shared `superadmin_full` decision. A `superadmin_readonly` grant SHALL NOT satisfy either check.

#### Scenario: An org_admin of the addressed organization is authorized
- **GIVEN** a user holds `org_admin` in organization A
- **WHEN** an organization-scoped decision for organization A is evaluated
- **THEN** the decision is authorized

#### Scenario: An org_admin of a different organization is refused
- **GIVEN** a user holds `org_admin` in organization B only
- **WHEN** an organization-scoped decision for organization A is evaluated
- **THEN** the decision is refused

#### Scenario: A platform superadmin does not widen an org-admin-only decision
- **GIVEN** a user holds a `superadmin_full` grant but no `org_admin` membership in organization A
- **WHEN** an organization-scoped decision that is not the invitation surface is evaluated for organization A
- **THEN** the decision is refused

#### Scenario: A read-only superadmin is refused organization administration
- **GIVEN** a user holds a `superadmin_readonly` grant and no `org_admin` membership
- **WHEN** an organization-scoped decision is evaluated
- **THEN** the decision is refused

#### Scenario: Invitation administration preserves the platform override
- **GIVEN** a user holds an active `superadmin_full` grant and no `org_admin` membership
- **WHEN** an organization invitation is created or revoked
- **THEN** the decision is authorized

#### Scenario: A project administrator alone is refused organization administration
- **GIVEN** a user holds `project_admin` in the declared project and no `org_admin` membership
- **WHEN** an organization-scoped decision is evaluated
- **THEN** the decision is refused

### Requirement: Platform-scope token minting decision
The system SHALL authorize token minting of the platform tier — both bare `admin` and the `admin:all` umbrella — through a single decision check that requires an active **full** superadmin grant (`superadmin_full`). A `superadmin_readonly` grant SHALL NOT satisfy this check, nor SHALL an `org_admin` membership, and the check SHALL NOT consult the project membership role: org-scoped authority SHALL NOT buy platform-scoped power. Bare `admin` is folded into the same platform authority as `admin:all` (`admin:all` implies `admin` via `ScopeImplies`), so a caller who may not grant `admin:all` may not grant bare `admin` either. This supersedes the earlier decision in #812 §4.3 that admitted any-org `org_admin`. The check SHALL be defined once and consumed by every platform-scoped mint decision; existing bespoke membership queries SHALL become consumers of it.

#### Scenario: A full superadmin is authorized to mint an admin:all token
- **GIVEN** a user holds an active `superadmin_full` grant
- **WHEN** the user requests an `admin:all` token
- **THEN** the request is authorized

#### Scenario: A full superadmin is authorized to mint a bare admin token
- **GIVEN** a user holds an active `superadmin_full` grant
- **WHEN** the user requests a bare `admin` token
- **THEN** the request is authorized

#### Scenario: Organization administrator is refused an admin:all token
- **GIVEN** a user holds `org_admin` in an organization and no superadmin grant
- **WHEN** the user requests an `admin:all` token
- **THEN** the request is refused

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

#### Scenario: An unprivileged member is refused a bare admin token
- **GIVEN** a user holds no superadmin grant
- **WHEN** the user requests a bare `admin` token
- **THEN** the request is refused

#### Scenario: Ephemeral sandbox tokens never carry a platform scope
- **GIVEN** an ephemeral sandbox token is minted on behalf of a project member (the chat sandbox or a background agent run)
- **WHEN** the ephemeral token's scope set is produced
- **THEN** it SHALL NOT include `admin` or `admin:all` — the sandbox runs as ordinary project members and must never obtain platform authority through the ephemeral mint
