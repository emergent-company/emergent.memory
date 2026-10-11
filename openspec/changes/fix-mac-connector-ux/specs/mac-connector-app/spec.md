## MODIFIED Requirements

### Requirement: Signed-out state prompts re-authentication

When the app has an account but the connector CLI has no valid session (an
expired or cleared session), the app SHALL show a signed-out state with a clear
Sign in action instead of a raw "Couldn't load projects" error. Detecting the
failure SHALL prefer the connector CLI's own session status (`auth status`),
falling back to the failure text (`not signed in`, `401`, `invalid_token`). A
successful sign-in SHALL import that session into the connector CLI via
`auth import` and then load the user's projects. Non-auth failures SHALL still
show the error state. When a project is connected and the failure is an
auth/session failure, the app SHALL keep the connected intent and SHALL NOT stop
a running engine whose config still binds that project; only a non-auth failure
tears down the connection and stops the engine. An expected auth/session failure
SHALL NOT be surfaced as raw connector-CLI text (for example a
`run 'memory-connector auth login' first` hint) — the app SHALL present the
signed-out / re-authentication affordance instead. While the session is invalid,
the Project & Account page SHALL keep listing the stored accounts rather than
replacing them with a single generic sign-in prompt, and each account whose
session is invalid SHALL offer a "Sign in again" action that re-authenticates
against that account's own environment (a Development account stays Development).

#### Scenario: Auth failure shows the signed-out state

- **WHEN** loading projects fails because the connector CLI reports no valid
  session (or the failure is a `not signed in`, `401`, or `invalid_token`
  error)
- **THEN** the Project & Account page and the project switcher show a
  "Signed out" prompt with a Sign in action (and a Try again action), not a raw
  "Couldn't load projects" error

#### Scenario: Sign-in imports the session and loads projects

- **WHEN** the user signs in from the signed-out prompt
- **THEN** the app imports the session into the connector CLI via
  `auth import` and loads the user's projects for the signed-in environment

#### Scenario: Non-auth failure still shows the error state

- **WHEN** loading projects fails for a reason that is not an auth/session
  failure
- **THEN** the app shows the "Couldn't load projects" error state with the
  failure message, as before

#### Scenario: Session expires while a project is connected

- **WHEN** a project is connected and the connector CLI's session expires or is
  cleared, so a config re-materialisation (e.g. `projects use` during the launch
  scope-settle) fails with an auth/session error
- **THEN** the app keeps the connected intent and leaves the running engine
  running (its long-lived project token and on-disk config still bind that
  project), and surfaces the signed-out prompt for management actions — it does
  not tear down a healthy engine

#### Scenario: Stored accounts remain listed while signed out

- **WHEN** the connector session is invalid but the app still has one or more
  stored accounts
- **THEN** the Project & Account page lists those accounts (not a single generic
  sign-in prompt), so each can be re-authenticated

#### Scenario: Re-authentication targets the account's own environment

- **WHEN** a listed account needs re-authentication and the user chooses
  "Sign in again"
- **THEN** the app signs in to that account's own environment (a Development
  account re-authenticates against Development, not Production)

#### Scenario: Expected auth failure is not shown as a raw error

- **WHEN** an operation fails only because there is no valid connector session
  (for example the CLI reports `not signed in`)
- **THEN** the app does not display the raw connector-CLI message as an error
  banner; it shows the signed-out / re-authentication affordance instead

### Requirement: Sidebar navigation separating information from settings

The main window SHALL use a sidebar with two sections — information pages and
settings pages — and a detail area that shows the selected page; the previously
monolithic Settings view is split into those pages. A project switcher SHALL be
reachable from the navigation (e.g. the Project & Account page and/or the
sidebar). About SHALL NOT live in either section: it is not project/settings
context and SHALL be pinned to the bottom of the sidebar, visually separated
from the scrolled sections.

#### Scenario: Navigate between pages

- **WHEN** the user selects a sidebar item
- **THEN** the detail area shows that page and the selection is remembered while the app runs

#### Scenario: Information and settings are distinct groups

- **WHEN** the sidebar renders
- **THEN** information pages (overview, project/account) and settings pages
  (tools, hosted MCP servers, permissions, connection) appear in separate
  sections, and About is pinned at the bottom rather than inside a section

#### Scenario: Switch project from navigation

- **WHEN** the user chooses a different project in the switcher
- **THEN** the window reflects the newly active project without a restart

#### Scenario: About is pinned to the bottom

- **WHEN** the sidebar renders
- **THEN** the About item appears at the bottom of the sidebar, separated from
  the information and settings sections, and selecting it shows the About page

### Requirement: The bundled connector engine is a pinned, non-self-updating snapshot

The app SHALL bundle a connector engine binary whose version is determined by
the app release, and that binary SHALL be replaced only by installing a new
version of the app. When the engine is executed from inside the app bundle, an
engine-initiated self-upgrade SHALL be refused with a message stating that the
engine is managed by the app, and the bundle SHALL remain unmodified. The
bundled engine SHALL report the same version as the app release.

#### Scenario: Engine self-upgrade attempted inside the bundle

- **WHEN** the bundled engine is asked to upgrade itself while running from inside the app bundle
- **THEN** it refuses, reports that it is managed by the app, and does not modify any file in the bundle

#### Scenario: Engine self-upgrade from a standalone install

- **WHEN** the engine binary lives outside any app bundle and is asked to upgrade itself
- **THEN** it upgrades in place as before

#### Scenario: Engine version tracks the app release

- **WHEN** a new version of the app is installed
- **THEN** the bundled engine reports the same version as that app release

#### Scenario: Versions are reported

- **WHEN** the user inspects the app's version information
- **THEN** the app version is shown, and no separate bundled-engine version is shown

## ADDED Requirements

### Requirement: Window footer reports project-connection status

The main window's footer SHALL report project-connection status as a single
status dot plus one label, derived from the effective signed-in state together
with the connected project. It SHALL show a green dot with "1 project connected"
when the user is effectively signed in and a project is connected, and an orange
dot with "no projects connected" otherwise. The footer SHALL NOT show engine,
hub, or connector-internal detail as visible text; a persisted connected-project
identifier SHALL NOT read as connected while the user is signed out.

#### Scenario: Connected while signed in

- **WHEN** the user is effectively signed in and a project is connected
- **THEN** the footer shows a green dot and "1 project connected"

#### Scenario: Signed out with a leftover connection

- **WHEN** the user is signed out although a connected-project identifier is
  still persisted
- **THEN** the footer shows an orange dot and "no projects connected"

#### Scenario: No engine/hub text in the footer

- **WHEN** the footer renders
- **THEN** it shows only the status dot and the project-connection label;
  engine/hub health is available via the control's help tooltip and the
  menu-bar icon, not as footer text

### Requirement: About page reports app version and instance only

The About page SHALL report the app's version and build and the connector
instance identifier. It SHALL NOT show the bundled engine's version, and it
SHALL NOT show a single server URL (the app supports more than one
environment, so one URL is misleading).

#### Scenario: Details omit internal identifiers

- **WHEN** the user opens the About page
- **THEN** its details show the instance identifier, the app version/build, and
  no engine-version row and no server-URL row
