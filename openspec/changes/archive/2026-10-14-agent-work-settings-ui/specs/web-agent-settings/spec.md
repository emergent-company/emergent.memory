## ADDED Requirements

### Requirement: Work settings subpage

The agent settings surface SHALL expose a **Work** subpage at
`/agents/:id/settings/work`, reachable from the settings rail between Graph and
Model, that edits the agent definition's object-driven work configuration through
the existing definition update path. The subpage SHALL use the PRG form pattern
(shared save button, redirect to `?updated=1` on success), not the General
subpage's auto-save split.

#### Scenario: Work subpage is reachable from the settings rail

- **WHEN** the agent settings rail renders
- **THEN** it contains a Work entry linking to `/agents/:id/settings/work`, active only when the Work section is current

#### Scenario: Work subpage saves via the shared PRG path

- **WHEN** the operator submits the Work form
- **THEN** the gateway POSTs to `/agents/:id/settings/work` and redirects to `/agents/:id/settings/work?updated=1` on success

### Requirement: Status phase mapping fields

The Work subpage SHALL expose six text fields for the work-status phase names —
`ready`, `inProgress`, `review`, `revision`, `blocked`, `done` — bound to the
definition's `workConfig.status`. An empty value SHALL inherit the platform
default for that phase, shown as the input placeholder (`ready`, `in_progress`,
`review`, `revision`, `blocked`, `done`).

#### Scenario: Status names persist

- **WHEN** the operator enters a custom status name and saves
- **THEN** the saved agent definition's `workConfig.status.<phase>` holds that value

#### Scenario: Empty status inherits the default

- **WHEN** a status field is empty
- **THEN** it renders with the platform-default placeholder and the saved config leaves the phase empty (the server applies the default)

### Requirement: Review gate and budgets

The Work subpage SHALL expose a **Require review** toggle bound to
`workConfig.requiresReview`, and numeric fields for **failure limit** and
**revision limit** bound to `workConfig.failureLimit` / `workConfig.revisionLimit`.
Both numeric fields SHALL enforce a minimum of 1 and render the platform default
(3) as their placeholder. An empty numeric field SHALL read as the platform
default (0 sentinel).

#### Scenario: Review gate persists

- **WHEN** the operator checks the Require review toggle and saves
- **THEN** the saved agent definition's `workConfig.requiresReview` is true

#### Scenario: Budgets persist

- **WHEN** the operator sets the failure limit and revision limit and saves
- **THEN** the saved agent definition carries those values in `workConfig.failureLimit` / `workConfig.revisionLimit`

#### Scenario: Below-minimum budget is rejected

- **WHEN** the operator submits a failure limit or revision limit below 1
- **THEN** the submission is rejected and no budget change reaches the backend

### Requirement: Retry policy fields

The Work subpage SHALL expose four numeric fields for the retry backoff schedule
— max attempts (minimum 1), initial interval (ms, minimum 0), backoff coefficient
(minimum 1), and max interval (ms, minimum 0) — bound to `workConfig.retryPolicy`.

#### Scenario: Retry policy persists

- **WHEN** the operator sets the retry policy fields and saves
- **THEN** the saved agent definition's `workConfig.retryPolicy` holds the submitted values

### Requirement: Work contract fields

The Work subpage SHALL expose a **Require artifacts** toggle bound to
`workConfig.workContract.requireArtifacts`, and a comma-separated **Required
deliverable types** field bound to `workConfig.workContract.requiredDeliverableTypes`.

#### Scenario: Deliverable contract persists

- **WHEN** the operator checks Require artifacts, enters deliverable types, and saves
- **THEN** the saved agent definition carries `requireArtifacts: true` and the submitted `requiredDeliverableTypes`

### Requirement: Trigger statuses field

The Work subpage SHALL expose a comma-separated **Trigger statuses** field bound
to `workConfig.triggerStatuses`, with a read-only note stating that the agent
picks up work when an object enters one of these statuses. An empty value SHALL
leave the list empty (the server applies the default `["ready"]`).

#### Scenario: Trigger statuses persist

- **WHEN** the operator enters comma-separated trigger statuses and saves
- **THEN** the saved agent definition's `workConfig.triggerStatuses` holds the submitted values

### Requirement: Blueprint-derived edit gate

When the agent definition reports a `sourceBlueprintId`, the Work subpage SHALL
gate the save behind a confirmation dialog ("Save anyway") warning that the agent
was created by a blueprint and the edit may be overwritten. A definition without
a `sourceBlueprintId` SHALL save without the dialog.

#### Scenario: Blueprint-derived agent warns before save

- **WHEN** the Work form renders for an agent with a `sourceBlueprintId` and the operator submits
- **THEN** a confirmation dialog opens and the form posts only after the operator confirms "Save anyway"

#### Scenario: Manual agent saves directly

- **WHEN** the Work form renders for an agent with no `sourceBlueprintId` and the operator submits
- **THEN** no confirmation dialog opens and the form posts directly

### Requirement: Lossless section save

Saving any other settings subpage SHALL NOT drop the agent's `workConfig`: the
gateway write path SHALL preserve `workConfig` (only the server-computed
`toolGroups` field is stripped before serialization).

#### Scenario: Non-work save preserves work config

- **WHEN** the operator saves a different settings subpage for an agent with a configured `workConfig`
- **THEN** the persisted `workConfig` is unchanged
