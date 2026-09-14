## MODIFIED Requirements

### Requirement: Show connected project and user identity

The app SHALL show the connected Memory project and organisation by NAME (not raw identifiers), and the user identity (display name, email, avatar or initials) — sourced from the signed-in OIDC session when present, or marked unavailable when only an API token is configured. Identifiers SHALL be hidden by default and revealed/copyable through a copy control (reveal on hover, copy on click). The app SHALL degrade gracefully when identity is unavailable (e.g. token-only setup or unreachable server).

#### Scenario: Project and user shown

- **WHEN** the app is signed in and the server is reachable
- **THEN** the Project & Account page shows the project name, the organisation name, and the signed-in user's display name, email, and avatar (or initials)

#### Scenario: Token-only setup

- **WHEN** no OIDC session exists and only a project token is configured
- **THEN** project/organisation still show, and the user card states that identity is unavailable (or that the app is using an API token)

#### Scenario: Identifier hidden but copyable

- **WHEN** the user hovers the copy control next to a project or organisation name
- **THEN** the raw identifier is revealed (tooltip/label) and clicking the control copies it to the clipboard

### Requirement: Sidebar navigation separating information from settings

The main window SHALL use a sidebar with two sections — information pages and settings pages — and a detail area that shows the selected page; the previously monolithic Settings view is split into those pages. A project switcher SHALL be reachable from the navigation (e.g. the Project & Account page and/or the sidebar).

#### Scenario: Navigate between pages

- **WHEN** the user selects a sidebar item
- **THEN** the detail area shows that page and the selection is remembered while the app runs

#### Scenario: Information and settings are distinct groups

- **WHEN** the sidebar renders
- **THEN** information pages (overview, project/account) and settings pages (tools, permissions, connection, about) appear in separate sections

#### Scenario: Switch project from navigation

- **WHEN** the user chooses a different project in the switcher
- **THEN** the window reflects the newly active project without a restart
