## ADDED Requirements

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
