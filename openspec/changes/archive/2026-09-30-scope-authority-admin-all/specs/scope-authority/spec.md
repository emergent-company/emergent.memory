## REMOVED Requirements

### Requirement: Organization-scoped entitlement decisions
**Reason**: `admin:all` token minting no longer admits an `org_admin` membership (issue #949), so the scenario asserting an `org_admin` is authorized to mint an `admin:all` token is reversed. The requirement is re-stated under a name that reflects the check's now platform-scoped, `superadmin_full`-only decision.
**Migration**: `admin:all` minting is `superadmin_full`-only; see the added requirement below.

## ADDED Requirements

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

