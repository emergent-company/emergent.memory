## Purpose

Playwright coverage of the agent-owned MCP endpoint UI: the "MCP endpoint"
section on an agent's own Settings page, which creates and revokes the endpoint,
mints labeled keys with a one-time secret reveal, rotates and revokes individual
keys, and lists the external client sessions those keys produced. The capability
also guards the superseded model — the project shares page must offer no agent
picker — and keeps the credentials it creates out of the shared bootstrap
tenant by cleaning up after every run.

## ADDED Requirements

### Requirement: The agent Settings page exposes the MCP endpoint section

The agent's Settings page SHALL render an MCP endpoint section that shows the
create-the-endpoint first step when the agent has no endpoint, and the live
endpoint's status, client URL, and revoke action once one exists.

#### Scenario: No endpoint yet

- **WHEN** a freshly created agent's Settings page is opened
- **THEN** the MCP section is visible and renders the create-the-endpoint action, with no live endpoint block

#### Scenario: Endpoint created with a client URL

- **WHEN** the analyst creates the endpoint from the section
- **THEN** the endpoint block renders with an active status, and the endpoint URL shown matches the URL the backend reports for that endpoint

#### Scenario: Endpoint URL is copyable

- **WHEN** the copy affordance beside the endpoint URL is used
- **THEN** the endpoint URL is what lands in the clipboard

### Requirement: Key secrets are revealed exactly once

A key's raw secret SHALL be visible only in the create/rotate response's reveal
panel and SHALL NOT be recoverable from any later render of the page or the key
list.

#### Scenario: Create shows the secret once

- **WHEN** a labeled key is created
- **THEN** the reveal panel shows a non-empty raw secret, and the key's list row appears with its label and an active status

#### Scenario: Secret is gone after leaving the reveal

- **WHEN** the settings page is loaded again through a normal navigation (and reloaded) after a key was created
- **THEN** the reveal panel is absent and the raw secret appears nowhere on the page, while the key's row still exists

#### Scenario: Key lists never carry a secret

- **WHEN** the endpoint's keys are read through the JSON list surface
- **THEN** the created key appears without any token value

### Requirement: Labeled keys are independent credentials

Keys on one endpoint SHALL have unique labels, SHALL be rotatable without losing
their identity, and SHALL be revocable individually without affecting the
endpoint or its other keys.

#### Scenario: Duplicate label is rejected readably

- **WHEN** a key is created with a label already used on the endpoint
- **THEN** a readable inline error is shown, no second key is created, and the existing key remains the only one with that label

#### Scenario: Rotation issues a new secret for the same key

- **WHEN** a key is rotated
- **THEN** a new secret is shown once that differs from the previous secret, and the same key (same identity) remains in the list

#### Scenario: One key revoke leaves the others working

- **WHEN** one of two keys on an endpoint is revoked
- **THEN** the revoked key is listed as revoked with no rotate/revoke actions, and the other key is still active and actionable

#### Scenario: Endpoint revoke returns to the not-enabled state

- **WHEN** the endpoint is revoked
- **THEN** the section returns to its create-the-endpoint state and the endpoint and keys blocks are gone

### Requirement: The sessions panel renders its empty state without a live run

The sessions panel SHALL render an explicit empty state for an endpoint that has
no external client sessions, so coverage does not depend on a live agent run.

#### Scenario: Empty sessions panel

- **WHEN** the Settings page of an endpoint with no sessions is opened
- **THEN** the sessions panel and its empty state are visible alongside the session status filter

### Requirement: The project shares page offers no agent picker

The project-share surface SHALL NOT expose an agent picker; agent sharing is
configured on the agent's own endpoint section.

#### Scenario: No agent field on share creation

- **WHEN** the project share create page is opened
- **THEN** its form contains no select and no control named for an agent

#### Scenario: Agent sharing is linked out, not selected

- **WHEN** the project shares list is opened
- **THEN** its agent affordance is a link to the agents surface rather than an inline agent selector

### Requirement: The spec confines and cleans up its tenant state

The spec SHALL create a dedicated agent and SHALL remove every credential and
entity it created, so repeated runs are idempotent and never race the parallel
read surface.

#### Scenario: Dedicated agent per run

- **WHEN** the spec runs
- **THEN** it creates and uses one dedicated agent rather than a shared bootstrap agent

#### Scenario: Credentials are revoked on completion

- **WHEN** the spec finishes, whether it passed or failed
- **THEN** every key it created is revoked, the endpoint is revoked, and the dedicated agent is deleted

#### Scenario: Writer spec is serial and self-cleaning

- **WHEN** the spec is discovered by the Playwright configuration
- **THEN** it runs in the serial `mutations` project, which depends on `setup` only
