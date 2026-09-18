## MODIFIED Requirements

### Requirement: Require sign-in and connect a project

The app SHALL require connector CLI sign-in before the connector can run and
SHALL NOT accept a manually entered project token. When the signed-in user
connects a project, the connector CLI mints or reuses that project's token and
writes the engine config file (0600); the app persists only non-secret defaults
and per-project profiles.

#### Scenario: Signed out

- **WHEN** no account is signed in
- **THEN** the Connection page shows a single prominent sign-in action for the
  Production environment and offers no token entry and no environment choice

#### Scenario: Connect a project

- **WHEN** the signed-in user connects a project
- **THEN** the connector CLI mints or reuses the project token, writes the engine config file, and the engine (re)starts with the new configuration

#### Scenario: Token never written in plaintext config by the app

- **WHEN** the app manages non-secret settings
- **THEN** the project token is owned by the connector CLI, and any engine config file on disk is written by the CLI with restrictive permissions

### Requirement: Account control in the window header

The window header SHALL show an account control at the top right: a properly padded "Sign in" button when not effectively signed in, and — when effectively signed in — a control that opens the account switcher (see "Accounts are listed by email and switching uses the effective signed-in state"). The signed-out "Sign in" button SHALL start sign-in directly for the Production environment and SHALL NOT present an environment choice.

#### Scenario: Signed out

- **WHEN** the user is not effectively signed in
- **THEN** the top-right shows a padded, right-aligned "Sign in" control that starts Production sign-in in one click, with no Prod/Dev menu or picker

#### Scenario: Signed in

- **WHEN** the user is effectively signed in
- **THEN** the top-right control opens the account switcher, listing the signed-in accounts by email with the active account checkmarked

## ADDED Requirements

### Requirement: Single Production sign-in on every primary signed-out surface

Every primary signed-out surface SHALL offer exactly one prominent sign-in action
for starting a new sign-in, and that action SHALL target the Production
environment. The primary surfaces are the window header, the menu-bar popover,
the Connection page, and the Project & Account page; none of these SHALL render
two sign-in buttons, an environment picker, or an environment menu. The About
page is the deliberate exception — its non-prominent Development control is
governed by "Development sign-in is available only from the About page" and is
not a primary sign-in action. The environment that any surface offers SHALL be
resolved from one shared per-surface policy rather than enumerated ad hoc in each
view.

#### Scenario: Menu-bar popover while signed out

- **WHEN** the user left-clicks the menu-bar item with no account signed in
- **THEN** the popover shows one prominent sign-in button and no second sign-in
  button and no environment choice

#### Scenario: Connection page while signed out

- **WHEN** the Connection page renders with no account signed in
- **THEN** it shows one prominent sign-in button, no segmented Prod/Dev picker,
  and no environment menu

#### Scenario: Adding an account

- **WHEN** the user asks to add an account from an account menu while already
  signed in
- **THEN** the action signs in to Production directly and offers no environment
  choice

#### Scenario: A surface cannot opt itself into Development

- **WHEN** the per-surface sign-in policy is queried for any surface other than
  About
- **THEN** it returns Production only

### Requirement: Development sign-in is available only from the About page

The app SHALL offer sign-in to the Development environment only from the About
page, and only as a non-prominent, secondary control accompanied by text naming
the environment it targets. No other surface — the window header, the menu-bar
popover, the Connection page, the Project & Account page, the Dashboard, or the
MCP/permissions pages — SHALL offer a Development sign-in action. Development
remains a first-class environment for accounts that are already signed in:
existing Dev accounts SHALL still appear in the account switcher with their
`Dev` badge, remain selectable, and keep their connector session.

The About-only rule governs *selecting* an environment for a new sign-in.
Re-authentication of an existing account — including an expired Dev account —
SHALL target that account's own environment and is not an environment choice;
the Project & Account page and the window project switcher SHALL re-auth against
the active account's environment regardless of whether it is Production or
Development.

#### Scenario: About page offers Development

- **WHEN** the user opens the About page
- **THEN** a non-prominent control offers sign-in to the Development
  environment, labelled so the target environment is unambiguous, and it is the
  only place in the app that offers it

#### Scenario: Other surfaces do not offer Development

- **WHEN** the user is signed out and looks at the window header, the menu-bar
  popover, the Connection page, or the Project & Account page
- **THEN** none of them offers a Development sign-in action

#### Scenario: A signed-in Dev account keeps working

- **WHEN** an account signed in to the Development environment exists
- **THEN** it is still listed by email with its `Dev` badge, remains switchable,
  and its session is untouched by this change
