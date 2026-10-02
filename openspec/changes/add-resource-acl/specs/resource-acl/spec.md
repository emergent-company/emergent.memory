## ADDED Requirements

### Requirement: ACL entries model per-resource permissions

An ACL entry SHALL identify a resource by `(resource_type, resource_id)`, a principal by `(principal_type, principal_id)`, and a `permission` (at minimum `read`). Resource types SHALL include document, graph object (canonical id), and source. Principal types SHALL include user and group.

#### Scenario: Grant read on a resource

- **WHEN** an ACL entry grants `read` to a principal on a resource
- **THEN** that principal SHALL be authorized to read that resource

#### Scenario: Entry types are constrained

- **WHEN** an ACL entry is written
- **THEN** its `resource_type` and `principal_type` SHALL be from the supported sets, and `permission` SHALL be a supported value (at minimum `read`)

### Requirement: Deny-by-default outside project membership

A principal who is not a member of the resource's project SHALL have no access to the resource unless an explicit ACL entry grants it. Within a project, a member SHALL have `read` on the project's resources unless an explicit ACL entry revokes it.

#### Scenario: Non-member denied by default

- **WHEN** a principal is not a member of a project and no ACL entry grants access
- **THEN** the principal SHALL be denied access to the project's resources

#### Scenario: Member read by default

- **WHEN** a project member has no explicit ACL entry on a resource
- **THEN** they SHALL be authorized to read it (default project-member read)

### Requirement: ACL grants and revokes

A principal SHALL be grantable and revocable for a resource. A revoke (or an explicit deny) SHALL override the default project-member read. Grants and revokes SHALL be idempotent and auditable.

#### Scenario: Revoke overrides default

- **WHEN** an explicit deny (or revoke) entry exists for a principal on a resource
- **THEN** the principal SHALL be denied that resource even though they are a project member

#### Scenario: Grant and revoke are idempotent

- **WHEN** the same grant (or revoke) is applied twice
- **THEN** no duplicate entry is created and the effective permission is unchanged

### Requirement: Group inheritance resolves recursively

A principal SHALL be authorized for a resource if any group they belong to (transitively) is granted that resource. Group resolution SHALL be recursive and cycle-safe.

#### Scenario: Transitive group membership grants access

- **WHEN** a user belongs to a group that belongs to a parent group granted `read` on a resource
- **THEN** the user SHALL be authorized to read that resource

#### Scenario: Cycles do not hang resolution

- **WHEN** group membership contains a cycle
- **THEN** resolution SHALL terminate and return the correct effective permission

### Requirement: Search never returns unauthorized resources

Authorization SHALL be enforced at every search and graph hybrid search entry point, so no read path can bypass the ACL. Enforcement SHALL be centralized in a single `authorizeResources()` helper used by all legs.

#### Scenario: All legs share one enforcement point

- **WHEN** graph, text, and relationship legs run
- **THEN** each SHALL apply the same `authorizeResources()` helper, so there is no divergent authorization logic

### Requirement: Admin bypass policy

An explicit admin (superadmin / org admin) SHALL have a documented, controlled bypass that is applied at a single point, not scattered across call sites.

#### Scenario: Admin bypasses resource ACL

- **WHEN** an org/super admin reads resources
- **THEN** they SHALL bypass the per-resource ACL (subject to project/org membership where applicable), applied through one documented path

### Requirement: Backfill preserves existing access

The migration that introduces ACL SHALL backfill entries such that every existing project member SHALL have `read` on their project's existing resources, preserving current behaviour with no observed change for existing users.

#### Scenario: Existing members unaffected by migration

- **WHEN** the ACL migration runs against existing data
- **THEN** existing project members SHALL retain read access to their project's resources exactly as before the migration

### Requirement: PermissionSource contract for future syncers

The system SHALL define a `PermissionSource` interface contract that a future connector ACL syncer SHALL implement, so external permission changes can be synced into ACL entries. This change SHALL define the contract and SHALL NOT implement any connector syncer.

#### Scenario: Contract defined, no implementations shipped

- **WHEN** this change lands
- **THEN** a `PermissionSource` interface contract SHALL exist and be documented, and no connector ACL syncer SHALL be implemented

### Requirement: Postgres RLS is not used

The per-resource authorization SHALL be implemented in the application layer, not via Postgres Row-Level Security. RLS SHALL NOT be the enforcement mechanism for resource ACL.

#### Scenario: No RLS dependency

- **WHEN** resource ACL is enforced
- **THEN** enforcement is application-side (via `authorizeResources()`), and the database is not relied upon for row-level ACL
