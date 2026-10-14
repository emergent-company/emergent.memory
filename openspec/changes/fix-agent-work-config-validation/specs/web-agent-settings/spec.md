## Purpose

Keep the agent **Work** settings subpage, the gateway write path, and the server
resolver honest and in agreement about default budgets, the retry-policy gap, and
comma-separated list input.

## MODIFIED Requirements

### Requirement: Review gate and budgets

The Work subpage SHALL expose a **Require review** toggle bound to
`workConfig.requiresReview`, and numeric fields for **failure limit** and
**revision limit** bound to `workConfig.failureLimit` / `workConfig.revisionLimit`.
Both numeric fields SHALL accept `0` — and the empty field — as the platform
default (the server resolver treats `≤ 0` / empty as "use the built-in default")
and SHALL render the platform default (3) as their placeholder. A negative value
SHALL be rejected. The gateway form and the server resolver SHALL agree: a stored
`0` (or any value `≤ 0`) resolves to the built-in default.

#### Scenario: Review gate persists

- **WHEN** the operator checks the Require review toggle and saves
- **THEN** the saved agent definition's `workConfig.requiresReview` is true

#### Scenario: Budgets persist

- **WHEN** the operator sets the failure limit and revision limit and saves
- **THEN** the saved agent definition carries those values in `workConfig.failureLimit` / `workConfig.revisionLimit`

#### Scenario: Zero budget inherits the default

- **WHEN** the operator types `0` into the failure limit or revision limit field and saves
- **THEN** the submission is accepted, the saved value is `0` (the "use default" sentinel), and the server resolves it to the built-in default

#### Scenario: Below-minimum budget is rejected

- **WHEN** the operator submits a failure limit or revision limit below 0 (a negative value)
- **THEN** the submission is rejected and no budget change reaches the backend

### Requirement: Retry policy fields

The Work subpage SHALL expose four numeric fields for the retry backoff schedule
— max attempts (minimum 1), initial interval (ms, minimum 0), backoff coefficient
(minimum 1, rendered with `step="any"` so a non-decimal-step value set through the
API or a blueprint does not fail native step validation on re-save), and max
interval (ms, minimum 0) — bound to `workConfig.retryPolicy`. Only **max attempts**
is consumed by the work scheduler today; the initial interval, backoff
coefficient, and max interval are persisted on the definition but are not yet
applied, and the Retry policy section SHALL disclose that gap rather than present
those fields as shipped behaviour.

#### Scenario: Retry policy persists

- **WHEN** the operator sets the retry policy fields and saves
- **THEN** the saved agent definition's `workConfig.retryPolicy` holds the submitted values

#### Scenario: Backoff coefficient accepts arbitrary precision

- **WHEN** a definition carries a backoff coefficient that is not a multiple of `0.1` (set through the API or a blueprint) and the Work form re-renders
- **THEN** the field uses `step="any"`, so the stored value passes native constraint validation and can be re-saved unchanged

#### Scenario: Unapplied backoff fields are disclosed

- **WHEN** the Work subpage renders the Retry policy section
- **THEN** it states that only max attempts is applied and the initial interval, backoff coefficient, and max interval fields are saved but not yet used when scheduling retries

## ADDED Requirements

### Requirement: Duplicate-free comma-separated Work list fields

The Work subpage's comma-separated **Required deliverable types** and **Trigger
statuses** fields SHALL split on commas, trim whitespace, drop empty entries, and
collapse duplicate entries while preserving first-occurrence order before binding
them to `workConfig.workContract.requiredDeliverableTypes` /
`workConfig.triggerStatuses`.

#### Scenario: Duplicate entries collapse

- **WHEN** the operator enters the same value twice in either list field and saves
- **THEN** the saved list contains each distinct value exactly once, in first-occurrence order
