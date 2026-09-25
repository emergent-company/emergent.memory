# authorization-enforcement

## Purpose

Defines the enforcement layer for Memory authorization: a typed authority model (authority tiers, principal kinds, resource kinds, and the invariant that a grant is scoped to a resource family and a specific org/project and may never be exercised beyond that scope, with identity never client-promoted), a single declarative surface registry with one enforcement point, a `RequireAuthority` combinator with child-ownership resolution built in, identity derived by construction with client-supplied `X-Project-ID` demoted to a validated hint, one transport-agnostic `Authorize` function used by HTTP routes, in-process dispatch, SSE, and public share alike, a conformance test kit that generates the caller-class matrix per surface, and a CI coverage guard that fails the build when a registered route lacks an authority declaration or a conformance test.

## ADDED Requirements

### Requirement: Authority tiers and principal kinds

The system SHALL model authority as four tiers — platform (`superadmin_full`), organization (`org_admin` of a specific organization), project (a membership role in a specific project), and child (ownership of a specific child resource) — where platform is a terminal superset and the organization and project tiers govern disjoint resource families. The system SHALL recognize the principal kinds: OAuth session, account API token, project-bound token, agent, and public share / anonymous. A principal SHALL carry grants, each grant being a tier plus a scope (an organization, a project, or a child resource); a platform grant SHALL have an empty scope and cover everything.

#### Scenario: A grant is scoped to one org or project
- **GIVEN** a principal holds `org_admin` in organization A and no membership in organization B
- **WHEN** the principal exercises an organization-scoped entrypoint addressed at organization B
- **THEN** the request is denied

#### Scenario: Platform is a terminal superset
- **GIVEN** a principal holds an active `superadmin_full` grant
- **WHEN** the principal exercises any entrypoint of any tier
- **THEN** the request is allowed without consulting lower tiers

### Requirement: The authority invariant

The system SHALL enforce that an authority grant is exercised only over a resource in the grant's resource family and within the grant's scope, that the required level for a resource equals the resource's tier, and that a client never promotes its own identity: the caller's organization and project SHALL be derived from the credential and the route, and any client-supplied project or organization identifier SHALL be a hint validated against the derived identity, never an authority input. An organization-tier grant SHALL NOT grant project-tier data access.

#### Scenario: A client-supplied project id cannot mint a grant
- **GIVEN** an OAuth session supplies an `X-Project-ID` header for a project it is not a member of
- **WHEN** identity is derived
- **THEN** the header is treated as a hint and no project grant is issued

#### Scenario: Organization administration does not grant project data access
- **GIVEN** a principal holds `org_admin` in the project's organization but no project membership
- **WHEN** the principal exercises a project-scoped data entrypoint
- **THEN** the request is denied

### Requirement: Declarative surface registry

The system SHALL provide a registry in which every entrypoint declares its resource kind, required level, and a resolver for the concrete resource. Registration SHALL fail when the declared level is below the kind's required level, when an entry id is duplicated, or when the resolver is absent. A single enforcement middleware SHALL apply the declaration through the one `Authorize` path, replacing hand-assembled guard chains.

#### Scenario: A declaration below the kind floor is rejected at registration
- **GIVEN** an entrypoint declares a project-kind resource at the child level
- **WHEN** the entry is registered
- **THEN** registration fails

#### Scenario: One enforcement point applies a declaration
- **GIVEN** an entrypoint is registered with kind project and level project
- **WHEN** a request exercises the entrypoint
- **THEN** a single enforcement middleware checks project-level authority over the resolved resource, without a hand-assembled guard chain

### Requirement: Child-ownership resolution

The system SHALL resolve a child resource's owning project before authorizing access to it, so a bare child identifier can never satisfy an ownership check. The resolver for each child kind SHALL map the child identifier to its owning project server-side.

#### Scenario: A bare child id is denied without a parent
- **GIVEN** a request addresses a child resource by id only
- **WHEN** authorization is checked
- **THEN** the child's owning project is resolved server-side and the check is against that project's membership, never against the bare id

### Requirement: Identity derived by construction

The system SHALL derive a principal from the already-authenticated identity and the route's declared resource in exactly one place. For a project-bound token the bound project SHALL win over any client-supplied project header; for an OAuth session or account token, membership in the derived project's owning organization SHALL be checked before a project grant is issued. A handler SHALL NOT construct a principal from client-supplied fields.

#### Scenario: A token-bound project wins over the header
- **GIVEN** a project-bound token whose bound project is P
- **AND** the request supplies an `X-Project-ID` header of a different project
- **WHEN** identity is derived
- **THEN** the bound project P is used and the mismatched header is rejected

### Requirement: Transport-agnostic enforcement

The system SHALL provide a single `Authorize` function that is the one enforcement point for every transport: HTTP routes, in-process dispatch, SSE, and public share. A tool's declared authority SHALL be defined once and enforced identically on every transport, including the in-process execution path.

#### Scenario: In-process dispatch enforces the same authority as HTTP
- **GIVEN** a tool declares a required scope or an agent-only flag
- **WHEN** the tool is executed in-process (not through an HTTP transport)
- **THEN** the same authority check is applied as on the HTTP path

### Requirement: Conformance test kit

The system SHALL provide a conformance kit that, given a registry entry, generates the caller-class matrix — unauthenticated, project member, non-member, foreign-project, bare-scope token, account token, superadmin, agent, and share — and executes each row against the real enforcement path, so every module receives the same caller-class test for free.

#### Scenario: Every surface gets the caller-class matrix
- **GIVEN** a registered entrypoint
- **WHEN** its conformance matrix is generated
- **THEN** a row exists for each caller class with an expected allow-or-deny decision derived from the entry's kind and level

### Requirement: CI coverage guard

The system SHALL fail the build when a registered route lacks an authority declaration or a conformance test, so an unguarded or untested route cannot be introduced.

#### Scenario: An untested declaration fails the build
- **GIVEN** a registry entry with no conformance matrix
- **WHEN** the CI coverage guard runs
- **THEN** the build fails

#### Scenario: An unregistered handler fails the build
- **GIVEN** a route handler not backed by a registry entry
- **WHEN** the CI coverage guard runs
- **THEN** the build fails
