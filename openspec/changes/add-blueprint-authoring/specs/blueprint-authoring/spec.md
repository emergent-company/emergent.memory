## Purpose

Lets a user author and revise a project blueprint directly in the web UI: edit
its draft definition, release it as an immutable version, and install a chosen
version.

## ADDED Requirements

### Requirement: Edit a blueprint draft

The UI SHALL let a user edit the definition of a project blueprint draft — its
description and its object types (description, type-level ui accent, and typed
properties including adding, removing, and widget hints) — and persist the change
to that draft. A published version SHALL NOT be editable in place.

#### Scenario: Edit a draft object type

- **WHEN** a user edits an object type of a blueprint draft and saves
- **THEN** the change is persisted to the draft and the blueprint detail reflects it

#### Scenario: Add and remove object types

- **WHEN** a user adds a new object type or removes an existing one on a draft and saves
- **THEN** the draft's manifest contains exactly the object types shown in the editor

#### Scenario: Opening a published version forks a draft

- **WHEN** a user opens the editor for a published blueprint version
- **THEN** the UI forks a new draft version first and edits that draft, leaving the published version unchanged

#### Scenario: Edit rejected by the service

- **WHEN** the save request fails validation or the memory service rejects it
- **THEN** the UI shows an error and preserves the edited values for retry

### Requirement: Release a blueprint draft

The UI SHALL let a user release (publish) a blueprint draft after explicit
confirmation, and a released version SHALL be immutable.

#### Scenario: Release a draft

- **WHEN** a user releases a blueprint draft and confirms
- **THEN** the draft becomes a released version and is shown as released

#### Scenario: Released version is immutable

- **WHEN** a blueprint version is released
- **THEN** in-place editing is no longer offered and any subsequent change forks a new draft version

#### Scenario: Cancelled release aborts

- **WHEN** a user dismisses the release confirmation
- **THEN** the draft stays unpublished

### Requirement: Install a chosen blueprint version

The UI SHALL let a user install a specific blueprint version and report the
result. Installing a version SHALL leave the project schema unchanged until the
install is confirmed.

#### Scenario: Install from the version list

- **WHEN** a user activates install on a version in the blueprint version list
- **THEN** that version is applied to the project and the result is reported

#### Scenario: Install a still-draft version

- **WHEN** a user activates install on a version that is still a draft
- **THEN** the UI requires releasing it first and installs only after release

#### Scenario: Install is not implicit

- **WHEN** a user releases a blueprint version
- **THEN** the project schema is unchanged until the user separately activates install

#### Scenario: Installing an older version is blocked

- **WHEN** a user activates install on a version older than the version currently applied for that blueprint
- **THEN** the install is rejected with a clear error and the applied version is unchanged

### Requirement: Authorize blueprint authoring mutations

Editing, releasing, and installing blueprint versions SHALL require the schema
write capability and SHALL be rejected when the caller lacks it.

#### Scenario: Mutation without write capability

- **WHEN** a caller without the schema write capability attempts to edit, release, or install a blueprint version
- **THEN** the request is rejected with an authorization error and no blueprint or schema change is persisted
