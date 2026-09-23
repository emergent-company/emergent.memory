## MODIFIED Requirements

### Requirement: Viewer role definition
The system SHALL define a `project_viewer` role for project memberships. The viewer role SHALL be stored in `kb.project_memberships.role` alongside the existing `project_admin` and `project_user` roles. The viewer role SHALL be immutable — a viewer cannot promote themselves. The set of valid `kb.project_memberships.role` values SHALL be exactly `project_admin`, `project_user`, and `project_viewer`; other strings SHALL NOT be written by any server code path. The canonical role for `kb.organization_memberships` SHALL be `org_admin`; `owner` SHALL NOT be written to `kb.organization_memberships` by any server code path and existing rows with `role = 'owner'` SHALL be normalised to `org_admin`.

#### Scenario: Viewer role stored in membership
- **WHEN** a user accepts a viewer invitation
- **THEN** a `kb.project_memberships` record is created with `role = 'project_viewer'`

#### Scenario: Bootstrapped project membership uses a canonical role
- **WHEN** standalone bootstrap creates the default project and its membership
- **THEN** the membership is written with `role = 'project_admin'`

#### Scenario: Legacy owner role is normalised
- **GIVEN** a `kb.project_memberships` row exists with `role = 'owner'`
- **WHEN** the normalisation migration runs
- **THEN** the row's role becomes `project_admin`

#### Scenario: Organization membership uses the canonical role
- **WHEN** any server code path creates an organization membership
- **THEN** the membership is written with `role = 'org_admin'` and never `owner`

#### Scenario: Legacy owner organization role is normalised
- **GIVEN** a `kb.organization_memberships` row exists with `role = 'owner'`
- **WHEN** the organization-role normalisation migration runs
- **THEN** the row's role becomes `org_admin`
