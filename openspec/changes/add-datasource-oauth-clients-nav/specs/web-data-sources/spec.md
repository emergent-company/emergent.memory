## ADDED Requirements

### Requirement: Data source OAuth clients is discoverable from the project settings rail

The project settings rail SHALL expose a "Data source OAuth clients" entry linking to
`/settings/oauth-clients`. The entry SHALL be shown to `project_admin` of the active project only and
SHALL be hidden (not disabled) for all other callers. The entry SHALL sit between "Connected
devices" and "Budget".

#### Scenario: Project admin sees the rail entry

- **WHEN** a `project_admin` of the active project opens any project settings page
- **THEN** the settings rail shows a "Data source OAuth clients" entry linking to
  `/settings/oauth-clients`

#### Scenario: Non-admin does not see the rail entry

- **WHEN** a user who is not `project_admin` of the active project opens any project settings page
- **THEN** the "Data source OAuth clients" entry is absent from the settings rail (hidden, not
  disabled)

### Requirement: Data source OAuth clients is discoverable from the org settings rail

The org settings rail SHALL expose a "Data source OAuth clients" entry linking to
`/orgs/:id/settings/oauth-clients`. The entry SHALL be shown to `org_admin` only and SHALL be hidden
(not disabled) for all other callers.

#### Scenario: Org admin sees the rail entry

- **WHEN** an `org_admin` opens the org settings hub
- **THEN** the rail shows a "Data source OAuth clients" entry linking to
  `/orgs/:id/settings/oauth-clients`

#### Scenario: Org member does not see the rail entry

- **WHEN** an org member who is not `org_admin` opens the org settings hub
- **THEN** the "Data source OAuth clients" entry is absent from the rail

### Requirement: OAuth client pages render inside their settings rail

The project and org "Data source OAuth clients" pages SHALL render inside their respective settings
rail with the "Data source OAuth clients" entry marked active (`aria-current="page"`), matching the
layout of every other settings screen.

#### Scenario: Project page renders in-rail with the entry active

- **WHEN** a `project_admin` opens `/settings/oauth-clients`
- **THEN** the page renders inside the project settings rail with the "Data source OAuth clients"
  entry active

#### Scenario: Org page renders in-rail with the entry active

- **WHEN** an `org_admin` opens `/orgs/:id/settings/oauth-clients`
- **THEN** the page renders inside the org settings rail with the "Data source OAuth clients" entry
  active

### Requirement: Data Sources page links to OAuth client settings

The Data Sources page header SHALL show an "OAuth clients" shortcut linking to
`/settings/oauth-clients`. The shortcut SHALL be shown to `project_admin` of the active project only.

#### Scenario: Project admin sees the shortcut

- **WHEN** a `project_admin` of the active project opens the Data Sources page
- **THEN** the header shows an "OAuth clients" link to `/settings/oauth-clients`

#### Scenario: Non-admin does not see the shortcut

- **WHEN** a user who is not `project_admin` of the active project opens the Data Sources page
- **THEN** the "OAuth clients" shortcut is absent from the header
