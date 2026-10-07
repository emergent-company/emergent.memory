## Purpose

Defines the iOS client's multi-project model: a device can onboard several Memory projects, each through its own QR code, and switch the active project globally so every screen — voice session, agent picker, object browser, session log — follows the selection.

## ADDED Requirements

### Requirement: Project-scoped onboarding

Each QR onboarding SHALL connect the device to exactly one project. The setup QR payload SHALL carry the project identity (`orgID`, `projectID`, `projectName`) it was minted under, and the app SHALL persist that identity alongside the returned per-device configuration. Onboarding SHALL NOT wipe existing connections.

#### Scenario: Onboarding records the project

- **WHEN** the user scans a setup QR minted on project A's devices page
- **THEN** the app stores a connection bound to project A (its URLs, device credential, and project identity) and makes it the active project

#### Scenario: Onboarding a second project keeps the first

- **WHEN** the user later scans a setup QR minted on project B
- **THEN** the app adds a project B connection and leaves the project A connection intact

#### Scenario: Re-onboarding the same project updates in place

- **WHEN** the user scans a QR for a project already stored
- **THEN** the existing connection is updated (not duplicated) and becomes active

### Requirement: Persist and list project connections

The app SHALL persist the set of onboarded project connections and the active project id across launches. Each connection SHALL retain its own server URL, token endpoint, API base URL, device credential, TTS strategy, and project identity.

#### Scenario: Connections survive relaunch

- **WHEN** the app is relaunched after onboarding one or more projects
- **THEN** the same connections and the same active project are restored

#### Scenario: Remove a connection

- **WHEN** the user removes a project connection
- **THEN** it is dropped from the stored set and, if it was active, another stored connection becomes active (or none, when the set becomes empty)

### Requirement: Global active-project switch

The app SHALL provide a global project switcher, reachable without leaving the agent picker, and SHALL rebuild its network/session state from the active connection when the selection changes.

#### Scenario: Switch re-scopes every screen

- **WHEN** the user switches the active project
- **THEN** the agent picker reloads, and subsequent voice, object-browser, and session-log calls use the newly active project's URLs and credential

#### Scenario: Adding a project from the switcher

- **WHEN** the user chooses "Add project" from the switcher
- **THEN** the QR scanner is presented, and a successful scan adds and activates that project

### Requirement: Per-project agent selection

The selected agent SHALL be remembered per project, so switching projects selects the agent previously chosen for that project, falling back to the default when none is stored.

#### Scenario: Agent selection follows the project

- **WHEN** the user selects agent X in project A, switches to project B, and switches back to project A
- **THEN** agent X is selected again in project A

### Requirement: Legacy single-connection migration

On first launch after upgrading from a single-connection install, the app SHALL preserve the existing connection rather than appearing unconfigured.

#### Scenario: Existing configuration is not lost

- **WHEN** stored project connections are absent but a legacy connection credential is present
- **THEN** the app synthesizes and activates a single connection from the legacy keys, with empty project identity until the next onboarding
