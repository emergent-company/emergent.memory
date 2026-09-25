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
