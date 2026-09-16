## Purpose

Lets a user sign in to several Memory accounts (across prod and dev
environments) at the same time, switch between them from the profile icon, and
work with only that account's projects, organisations and connector state.

## ADDED Requirements

### Requirement: Environments

The app SHALL provide built-in environments (Prod and Dev), each with a server
URL, an OIDC issuer, and a native client id, and SHALL use the selected
environment's values for sign-in and API calls.

#### Scenario: Sign in to dev

- **WHEN** the user chooses the Dev environment and signs in
- **THEN** OIDC runs against the dev issuer with the dev client id and API calls use the dev server URL

#### Scenario: Environment badge

- **WHEN** accounts from different environments are listed
- **THEN** each account shows its environment so prod/dev are not confused

### Requirement: Multiple accounts with isolation

The app SHALL allow multiple accounts to be signed in simultaneously; each
account SHALL have its own session, projects, organisations, connected project,
local tool profiles and project tokens, with no data shared between accounts.

#### Scenario: Switching accounts

- **WHEN** the user switches to another account
- **THEN** the connector for the previous account is disconnected, and only the new account's projects/organisations/state are shown

#### Scenario: Adding an account

- **WHEN** the user adds an account (same or other environment)
- **THEN** a new sign-in runs for that environment and the new account is added to the list without disturbing existing accounts

#### Scenario: Signing out one account

- **WHEN** the user signs out one account
- **THEN** that account's session is removed (and the connector disconnected if it was active), while other accounts remain signed in

### Requirement: Account switcher from the profile icon

Clicking the profile icon SHALL show all signed-in accounts and allow switching,
adding an account, and reaching account management.

#### Scenario: Account list

- **WHEN** the profile icon is clicked
- **THEN** a menu lists each signed-in account (identity + environment) with the active one indicated

#### Scenario: Switch from the menu

- **WHEN** the user clicks another account in the menu
- **THEN** the app switches to that account (disconnect + reload) and the window reflects the new account

### Requirement: Migrate the existing session

On first launch after the feature lands, the existing single-account session and
per-project secrets SHALL be migrated into an account entry so the user stays
signed in.

#### Scenario: Existing session preserved

- **WHEN** the app starts with a legacy single session and no accounts
- **THEN** an account is created for it (prod), the session/tokens are moved into that account's storage, and the user remains signed in
