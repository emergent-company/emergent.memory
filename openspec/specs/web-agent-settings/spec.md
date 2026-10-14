# web-agent-settings Specification

## Purpose
The agent General settings form exposes a Visibility control (a custom Alpine listbox, since a native `<select>` cannot render a two-line option description) with three levels — `project`, `external`, `internal` — their descriptions, conditional warning/note alerts, and server-side validation and persistence through the general-settings path.

## Requirements

### Requirement: Visibility control on the agent General settings form

The agent General settings form SHALL expose a **Visibility** control — a custom Alpine listbox (`role="listbox"` with three `role="option"` entries, one per visibility level) rendered as a two-line `Name` + `description`. A native `<select>`/`<option>` cannot render the second, dimmed description line per option, so a hidden input carries the submitted `visibility` value:

| Value | Name | Description |
|---|---|---|
| `project` | Project | Visible in this project's UI and chat. Not advertised in the project's A2A agent card. |
| `external` | External | Advertised in the project's A2A agent card, so external A2A clients can discover it. The agent's name becomes its public skill id. |
| `internal` | Internal | Hidden from the agents list. For system agents that other agents call — open it by direct link. |

The control SHALL default to `project`, and a server-rendered helper line SHALL repeat the saved option's description (rendered from the stored value; it does not update live on selection).

#### Scenario: Visibility listbox lists the three levels

- **WHEN** the General settings form renders
- **THEN** the Visibility listbox contains exactly three `role="option"` entries — `project`, `external`, `internal` — each rendering a two-line `Name` + `description` in that order

#### Scenario: Visibility defaults to project

- **WHEN** an agent has no visibility set (empty/missing)
- **THEN** the Visibility control preselects `project`

#### Scenario: Helper line reflects the saved value

- **WHEN** the General settings form renders for an agent with a stored visibility
- **THEN** the server-rendered helper line under the control shows that stored option's one-line description

### Requirement: External visibility warning

Selecting `external` SHALL render a warning that the agent is listed in the project's A2A agent card and that anyone with a project API token can discover it (`agents:read`) and call it (`agents:write`), advising the user to review the system prompt and tools before exposing it.

#### Scenario: External selection shows the warning

- **WHEN** the agent's visibility is `external`
- **THEN** the form renders a warning alert stating the agent is listed in the A2A agent card and is discoverable/callable by anyone with a project API token

#### Scenario: Non-external selection hides the warning

- **WHEN** the agent's visibility is `project` or `internal` (or empty/unknown)
- **THEN** the external warning is not rendered

### Requirement: Internal visibility note

Selecting `internal` SHALL render a note that the agent is hidden from the agents list, still reachable by direct link, and still callable by other agents.

#### Scenario: Internal selection shows the note

- **WHEN** the agent's visibility is `internal`
- **THEN** the form renders an informational note stating the agent is hidden from the list, reachable by direct link, and callable by other agents

#### Scenario: Non-internal selection hides the note

- **WHEN** the agent's visibility is `project` or `external` (or empty/unknown)
- **THEN** the internal note is not rendered

### Requirement: Visibility server-side validation and persistence

The general-settings applier SHALL validate the submitted visibility and persist it through the existing general-settings update path. An empty or missing value SHALL normalise to `project` (the server default); a value that is not one of `project`, `external`, or `internal` SHALL be rejected, and the backend SHALL NOT be called.

#### Scenario: Valid value persists

- **WHEN** the user submits a valid visibility value (`project`, `external`, or `internal`) with the General form
- **THEN** the agent definition's visibility is updated to that value and the change persists through the general-settings update path

#### Scenario: Empty value normalises to project

- **WHEN** the user submits the General form with no visibility field (or an empty value)
- **THEN** the agent definition's visibility is set to `project`

#### Scenario: Invalid value is rejected

- **WHEN** the user submits a visibility value outside `project`/`external`/`internal`
- **THEN** the submission is rejected with an error and no visibility change reaches the backend

#### Scenario: Saved visibility round-trips

- **WHEN** a visibility value is saved
- **THEN** the form re-renders with that value preselected in the Visibility control

### Requirement: Short description field on the agent General settings form

The agent General settings form SHALL expose an editable **Short description**
field bound to the definition's existing `description`, saved through the
general-settings path. The value SHALL be trimmed, and an empty value SHALL
clear the stored description. The field SHALL carry a hint that it is shown
under the agent's name in chat.

#### Scenario: Save a short description

- **WHEN** the owner enters a description on the General settings form and saves
- **THEN** the agent definition's `description` is persisted and shown under the agent's name in chat

#### Scenario: Clear the description

- **WHEN** the owner clears the field and saves
- **THEN** the stored `description` becomes empty and chat shows no description line

### Requirement: Debounced auto-save on the agent General settings form

The agent General settings form SHALL auto-save its fields as the user edits,
without an explicit submit step. Text fields SHALL save after a short debounce
(≈700 ms) once typing stops, not on every keystroke; selects, the icon/colour
pickers, and the Visibility listbox SHALL save once their value changes. A save
SHALL be sent to a JSON endpoint that applies the same General section applier
and answers `200 {"ok":true}` on success or `422 {"ok":false,"error":…}` on
validation failure, so errors surface inline without a page navigation. An
unchanged value SHALL NOT post, and the form SHALL keep its PRG POST route as a
no-JavaScript fallback. The General form SHALL NOT render the shared explicit
save button; every other settings subpage SHALL keep it.

#### Scenario: Typing saves once after the debounce

- **WHEN** the owner edits the name, description, or system prompt
- **THEN** one save is sent after typing stops — not one per keystroke — and a status indicator reports saving then saved

#### Scenario: Non-text changes save promptly

- **WHEN** the owner changes the language select, the icon/colour picker, or the visibility listbox
- **THEN** the change is saved without waiting for the text debounce

#### Scenario: Unchanged value does not post

- **WHEN** a field is re-entered with the value it already holds
- **THEN** no save request is sent

#### Scenario: Validation failure surfaces inline and preserves the edit

- **WHEN** the server rejects a save (for example an empty required name)
- **THEN** the status indicator shows the error with a retry affordance and the edited values remain in the form

#### Scenario: Pending edit is not lost on in-app navigation

- **WHEN** the owner edits a text field and navigates to another settings subpage before the debounce elapses
- **THEN** the pending change is flushed before the navigation

#### Scenario: Pending debounce is cancelled on unload

- **WHEN** the page is unloaded before the debounce elapses
- **THEN** the pending timer is cancelled rather than firing against a torn-down document

### Requirement: Project-context opt-in control on the agent General form

The agent **General** settings form SHALL expose a boolean control for the
agent definition's project-context opt-in (`includeProjectInfo`). Checking it
SHALL persist `Config["includeProjectInfo"] = true`; leaving it unchecked SHALL
remove the key. The control SHALL be lossless across partial section saves, like
the form's other General fields.

#### Scenario: Checking the control enables injection

- **WHEN** an operator checks the project-context control and saves the General settings form
- **THEN** the saved agent definition's `config.includeProjectInfo` is `true`

#### Scenario: Unchecking the control disables injection

- **WHEN** an operator unchecks the project-context control and saves the General settings form
- **THEN** the saved agent definition's `config` has no `includeProjectInfo` key

#### Scenario: Auto-save preserves the control

- **WHEN** the General form auto-saves with the control in a given state
- **THEN** the resulting `config.includeProjectInfo` matches that state and no other config key is affected

### Requirement: Type-scope control on the agent Graph subpage

The agent **Graph** settings subpage (`/agents/:id/settings/graph`) SHALL expose
a type-scope control that lets an operator set the agent definition's
object-type and relationship-type allowlists. Saving a non-empty selection SHALL
persist `Config["objectTypes"]` / `Config["relationshipTypes"]` to the
corresponding lists; leaving the selection empty (present but blank) SHALL remove
the keys, meaning "all types". The control SHALL be lossless across partial
section saves: the rendered form SHALL submit each allowlist field via a hidden
companion input so that clearing every box ("allow all types") is
distinguishable from a partial body that omits the field ("leave the stored
scope unchanged"). The General form SHALL NOT own these fields.

#### Scenario: Selecting types persists the allowlists

- **WHEN** an operator selects object types and relationship types on the Graph subpage and saves
- **THEN** the saved agent definition's `config.objectTypes` and `config.relationshipTypes` hold the selected lists

#### Scenario: Clearing the selection removes the keys

- **WHEN** an operator unchecks every box in the type-scope control and saves the Graph subpage
- **THEN** the submitted body carries each field present-but-blank, and the saved agent definition's `config` has no `objectTypes` or `relationshipTypes` key, meaning "all types"

#### Scenario: Omitting the field preserves the stored scope

- **WHEN** a save reaches the Graph applier without carrying the `objectType` / `relationshipType` field
- **THEN** the stored allowlists are left unchanged (and every unrelated config key is untouched)

#### Scenario: General save leaves the graph scope intact

- **WHEN** an operator saves the General settings form
- **THEN** the agent's `config.objectTypes` and `config.relationshipTypes` are unaffected

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
