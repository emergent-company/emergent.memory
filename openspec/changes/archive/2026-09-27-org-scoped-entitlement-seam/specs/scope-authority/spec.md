## ADDED Requirements

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
