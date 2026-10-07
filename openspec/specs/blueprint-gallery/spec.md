# blueprint-gallery Specification

## Purpose
Provide a web UI page that lets users browse, install, toggle, and remove blueprint
packs to extend the graph database schema.

## Requirements

### Requirement: Blueprints navigation entry
The web shell SHALL expose a "Blueprints" entry in the sidebar navigation linking to `/blueprints`.

#### Scenario: Navigate to blueprints
- **WHEN** a user clicks the "Blueprints" sidebar item
- **THEN** the browser navigates to `/blueprints` and renders the blueprints page

### Requirement: Installed packs view
The blueprints page SHALL list all installed packs with their name, version, active state,
and an action to inspect their types.

#### Scenario: Installed list rendered
- **WHEN** a user opens the blueprints page and packs are installed
- **THEN** each installed pack is shown with its name, version, and active/inactive state

### Requirement: Available packs view
The blueprints page SHALL list packs that are not yet installed, with an install action for each.

#### Scenario: Available list rendered
- **WHEN** a user opens the blueprints page and uninstalled packs exist
- **THEN** each available pack is shown with its name, version, source, and an install action

### Requirement: Install a pack from the UI
The UI SHALL allow installing an available pack and reflect the result, including any conflicts.

#### Scenario: Successful install
- **WHEN** a user activates the install action on an available pack
- **THEN** the pack moves to the installed list and the UI reports the install result

#### Scenario: Install with conflicts
- **WHEN** an install produces conflicts
- **THEN** the UI displays the conflicts rather than silently proceeding

### Requirement: Toggle a pack active from the UI
The UI SHALL allow enabling or disabling an installed pack.

#### Scenario: Disable a pack
- **WHEN** a user activates the toggle on an installed active pack
- **THEN** the pack is shown as inactive

### Requirement: Remove a pack from the UI
The UI SHALL allow soft-removing an installed pack.

#### Scenario: Remove a pack
- **WHEN** a user activates the remove action on an installed pack
- **THEN** the pack is removed from the installed list and its data remains recoverable

### Requirement: Inspect compiled types from the UI
The UI SHALL allow a user to view the object and relationship types a pack contributes.

#### Scenario: Type inspection
- **WHEN** a user opens type inspection for an installed pack
- **THEN** the UI lists the pack's object and relationship types with their labels and descriptions

### Requirement: Create a blueprint from the current schema

The UI SHALL let a user create a blueprint draft from the project's current object types, either as a new blueprint or as a new version of an existing blueprint, and show the resulting draft in the drafts list.

#### Scenario: Save current schema as a new blueprint

- **WHEN** a user invokes "Save as blueprint" from the Schema area or blueprints gallery, provides a name, and confirms
- **THEN** a draft blueprint is created from the current object types and appears in the blueprints drafts list

#### Scenario: Save current schema as a new version

- **WHEN** a user invokes "Save as new version" on an existing blueprint and provides a version label
- **THEN** a new draft version is created from the current object types and appears in that blueprint's version list

#### Scenario: Draft is installable

- **WHEN** a user installs a blueprint draft derived from the current schema
- **THEN** the install succeeds through the existing install path and reports the applied types

#### Scenario: Derivation fails validation

- **WHEN** the derivation request is rejected (for example, duplicate name and version)
- **THEN** the UI shows the error and does not add a draft to the list

### Requirement: Version history display

The blueprint detail view SHALL list a blueprint record's versions newest-first, with each
version linking to that exact version's detail view, and SHALL present the version inline
with the blueprint title and in the breadcrumb trail.

#### Scenario: Versions sorted newest-first

- **WHEN** a user opens a blueprint record with multiple versions
- **THEN** the versions list orders them newest-first by semantic version (not lexicographically)

#### Scenario: Navigate to a specific version

- **WHEN** a user selects a version from the versions list
- **THEN** the browser navigates to that exact version's detail view

#### Scenario: Version shown in the header and breadcrumb

- **WHEN** a user opens a blueprint version
- **THEN** the version badge is shown directly after the blueprint title and the version
  appears as the final breadcrumb step

### Requirement: Blueprint lifecycle and install indication

The UI SHALL indicate a blueprint's lifecycle and install state as two independent
dimensions: a Release state (Draft or Published) and an Install state (Installed when the
exact version is applied to the project). The version currently being viewed SHALL be
identified separately from these state badges.

#### Scenario: Release and install badges shown per version

- **WHEN** a user views a blueprint record's version list
- **THEN** each version shows a Release badge (Draft or Published) and, when that exact
  version is applied to the project, an Installed badge

#### Scenario: Current version identified

- **WHEN** a user views a blueprint version
- **THEN** that version's row is highlighted as the one currently being viewed

### Requirement: Suggested next version

The "Save as new version" flow SHALL suggest the next semantic version by bumping the patch
of the highest existing version of the blueprint.

#### Scenario: Next version suggested

- **WHEN** a user opens "Save as new version" for a blueprint whose highest version is 1.4.2
- **THEN** the version field is prefilled with 1.4.3

### Requirement: Compare draft versions

The detail view of a draft version SHALL allow the user to choose another version to compare
against, defaulting to the next-lower version, and SHALL highlight field-level differences:
additions in green, removals in red, and modifications in orange.

#### Scenario: Compare a draft against another version

- **WHEN** a user views a draft version and selects a comparison version
- **THEN** the UI shows the changed object types, relationship types, and agents with
  field-level additions, removals, and modifications highlighted by color and text labels

#### Scenario: Default comparison base

- **WHEN** a user opens a draft version without selecting a comparison version
- **THEN** the comparison defaults to the highest version strictly lower than the draft

### Requirement: Show blueprint provenance in the gallery

The gallery and blueprint detail views SHALL identify blueprint-derived object types and link them to the owning blueprint.

#### Scenario: Detail lists derived types

- **WHEN** a user opens a blueprint's detail view
- **THEN** its object types are listed with the owning blueprint identified

#### Scenario: Navigate from type to blueprint

- **WHEN** a user follows the owning-blueprint reference shown for a derived type
- **THEN** the browser navigates to that blueprint's detail view

### Requirement: Import a blueprint from a GitHub URL in the gallery

The gallery install surface SHALL gain a GitHub URL import form with a URL
field, an optional ref field, and an optional token field. Submitting the form
posts to the import flow; on success the imported blueprint is installed
(applied) and the UI shows success, and on failure the UI shows the error. The
existing registry-pack and bundled-pack install paths and their
ids/`data-testid`/htmx hooks SHALL be preserved.

Before installing, the flow SHALL let the user map the imported manifest's agent
models to valid `provider/model` references (previewing the manifest without
installing anything), and the install SHALL submit those mappings atomically so
a failed install leaves no partial blueprint behind and can be retried cleanly.

#### Scenario: Successful GitHub URL import

- **WHEN** a user submits a valid GitHub repository URL (with optional ref and
  token) in the gallery import form
- **THEN** the blueprint is imported, installed (applied), and the UI shows
  success

#### Scenario: Model mapping step before install

- **WHEN** the imported manifest's agents carry model references the server
  would reject (for example a bare name missing the `provider/` prefix)
- **THEN** the UI lets the user map each such agent to a valid reference before
  installing, and the submitted import applies those mappings

#### Scenario: No partial install on failure

- **WHEN** the install fails after the import (for example the apply is
  rejected)
- **THEN** the UI shows the error and leaves no partial blueprint installed, so
  the user can retry with the model mapped

#### Scenario: Import error shown

- **WHEN** the import fails (invalid URL, oversize archive, fetch failure, or
  authorization failure)
- **THEN** the UI shows the mapped error and installs nothing

#### Scenario: Existing install paths unchanged

- **WHEN** a user installs from the registry or a bundled pack as before
- **THEN** those install actions behave exactly as they did before this change,
  with their existing ids, `data-testid`, and htmx hooks intact

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

### Requirement: Apply an available update from the installed list

The blueprints page SHALL offer a live Update action on each installed,
repo-imported blueprint for which an update is available. Activating it SHALL
trigger the asynchronous apply and SHALL NOT require the user to leave the page.

#### Scenario: Trigger an update

- **WHEN** a user activates Update on an installed repo-imported blueprint with
  an available update
- **THEN** the gateway enqueues the update job and returns the page showing the
  update as queued/running

#### Scenario: No update action when up to date

- **WHEN** an installed repo-imported blueprint has no available update
- **THEN** no live Update action is shown for it

### Requirement: Update progress reflects job status

The installed-row Update control SHALL reflect the latest job's status —
queued/pending, running, succeeded, or failed — and SHALL remain accurate across
page reloads by reading the latest-job status from the update-status endpoint.

#### Scenario: Running is visible

- **WHEN** an update job is pending or running
- **THEN** the row shows a pending/running state on the Update control

#### Scenario: Success and failure are visible

- **WHEN** the update job reaches a terminal state
- **THEN** the row shows success or the error, and an available-update badge is
  cleared on success
