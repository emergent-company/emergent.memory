## ADDED Requirements

### Requirement: Default voice to disabled for a fresh project

The system SHALL resolve voice as disabled when a project has no stored voice
`enabled` setting, so a freshly created project starts with voice off and the
chat page omits its voice controls until the user opts in. A stored
`enabled=true` SHALL resolve voice as enabled, and a stored `enabled=false`
SHALL resolve voice as disabled.

#### Scenario: Fresh project defaults to disabled

- **WHEN** a project has no stored voice `enabled` setting
- **THEN** voice resolves as disabled and the chat page omits its voice controls

#### Scenario: Stored enabled=true overrides the default

- **WHEN** a project has a stored voice `enabled=true`
- **THEN** voice resolves as enabled regardless of the fresh-project default

#### Scenario: Stored enabled=false stays disabled

- **WHEN** a project has a stored voice `enabled=false`
- **THEN** voice resolves as disabled
