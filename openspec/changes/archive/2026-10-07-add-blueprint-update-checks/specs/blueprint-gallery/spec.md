## ADDED Requirements

### Requirement: Repo-imported update status on the installed list

The blueprints page SHALL show the GitHub provenance and update status for each
installed blueprint that was imported from a repository: its upstream repo link,
the installed ref when recorded, and — once a check has run — the last-checked
time and, when an update is detected, a clear "Update available: vX → vY"
indicator. Blueprints with no repository source SHALL show none of this.

#### Scenario: Update available is shown

- **WHEN** an installed blueprint carries a `sourceUrl` and its latest check
  reports `updateAvailable` with a newer `latestVersion`
- **THEN** the row shows the repo link, an "Update available: vX → vY" badge,
  and the last-checked time

#### Scenario: No update shown when up to date

- **WHEN** an installed GitHub blueprint's latest check reports no update
- **THEN** the row shows its repo link and check action but no update badge

#### Scenario: Non-repo blueprints are unchanged

- **WHEN** an installed blueprint has no repo source (bundled or registry)
- **THEN** the row renders exactly as before, with no repo line or check action

### Requirement: On-demand update check from the UI

The blueprints page SHALL offer a "Check for updates" action on every
repo-imported installed blueprint that enqueues an update check, and SHALL show
a pending/disabled state on the control while the request is in flight.

#### Scenario: Trigger a check

- **WHEN** a user activates "Check for updates" on a repo-imported blueprint
- **THEN** the gateway enqueues a check and returns the page with a queued
  confirmation

#### Scenario: Pending state

- **WHEN** the check request is in flight
- **THEN** the action is disabled and shows a pending label until the response
  arrives

### Requirement: Project GitHub access token from the UI

The blueprints page SHALL let a user store a project GitHub personal access
token for private-repo update checks. The token SHALL be submitted as a password
field and never echoed back to the page or into the redirect target.

#### Scenario: Store a token

- **WHEN** a user submits the GitHub access token dialog with a non-empty token
- **THEN** the gateway stores it and returns the page with a saved confirmation

#### Scenario: Token never echoed

- **WHEN** the token is stored
- **THEN** the rendered page and the redirect URL do not contain the token
