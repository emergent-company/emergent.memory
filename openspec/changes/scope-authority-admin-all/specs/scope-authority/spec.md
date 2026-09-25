## MODIFIED Requirements

### Requirement: Organization-scoped entitlement decisions

The system SHALL authorize organization-scoped decisions with a check of an active **full** superadmin grant (`superadmin_full`). A `superadmin_readonly` grant SHALL NOT satisfy this check, nor SHALL an `org_admin` membership. The check SHALL NOT consult the project membership role. `admin:all` token minting SHALL be authorized through this check and SHALL require `superadmin_full` only: an `org_admin` SHALL NOT be able to mint or set `admin:all`, because org-scoped authority SHALL NOT buy platform-scoped power. This supersedes the earlier decision in #812 §4.3 that admitted any-org `org_admin`. The check SHALL be defined once and consumed by every org-scoped decision; existing bespoke membership queries SHALL become consumers of it.

#### Scenario: A full superadmin is authorized to mint an admin:all token
- **GIVEN** a user holds an active `superadmin_full` grant
- **WHEN** the user requests an `admin:all` token
- **THEN** the request is authorized

#### Scenario: Organization administrator is authorized to mint an admin:all token
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
