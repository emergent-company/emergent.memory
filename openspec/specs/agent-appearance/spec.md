# agent-appearance Specification

## Purpose
Lets an owner give an agent definition a user-chosen icon and color, persisted as `ui_config` on `kb.agent_definitions`, and have that appearance render everywhere the agent is visible in the Web UI — reusing the same storage shape, pickers, and rendering primitives object types already use.

## Requirements

### Requirement: Store agent appearance

The system SHALL store an agent definition's user-chosen icon and color in a `ui_config` JSONB column on `kb.agent_definitions`, shaped `{"icon":"<bare-kebab-lucide-name>","color":"<#RRGGBB>"}`. Both fields SHALL be optional, and an empty object `{}` SHALL mean "use defaults".

#### Scenario: Appearance defaults when unset

- **WHEN** an agent definition has no icon or color chosen
- **THEN** its stored `ui_config` is `{}` and the UI renders the neutral `lucide--bot` tile

#### Scenario: Appearance set

- **WHEN** the owner sets an icon (`bot`) and color (`#4F46E5`) on an agent definition
- **THEN** `ui_config` stores `{"icon":"bot","color":"#4F46E5"}`

### Requirement: Expose appearance in the API

The system SHALL expose the agent's appearance as a camelCase `uiConfig` field on `AgentDefinitionDTO` and `AgentDefinitionSummaryDTO`, and SHALL accept it on `CreateAgentDefinitionDTO` and `UpdateAgentDefinitionDTO`. The summary DTO MUST carry `uiConfig` so lists and pickers can render appearance without a full fetch.

#### Scenario: Full definition returns appearance

- **WHEN** the client fetches a single agent definition
- **THEN** the response `AgentDefinitionDTO` includes `uiConfig` reflecting the stored icon and color

#### Scenario: Summary returns appearance

- **WHEN** the client lists agent definitions
- **THEN** each `AgentDefinitionSummaryDTO` includes `uiConfig`

#### Scenario: Create accepts appearance

- **WHEN** the client creates an agent definition with `uiConfig: {"icon":"bot","color":"#4F46E5"}`
- **THEN** the definition is created with that appearance persisted

#### Scenario: Update accepts appearance

- **WHEN** the client updates an agent definition's `uiConfig`
- **THEN** the stored appearance changes to the new icon and color

### Requirement: Edit appearance in the Web UI

The Web UI agent editor (settings form and create/edit modal) SHALL use the same pickers object types use — `ui.IconPicker` and `ui.ColorPicker`, backed by `supportedIconPickerOptions()` and `schemaColorPresets`. The icon catalog SHALL include `lucide--bot`.

#### Scenario: Pick icon and color

- **WHEN** the owner opens the agent editor and selects an icon and color
- **THEN** the selection is saved to `ui_config` and rendered on the agent

#### Scenario: Clear appearance to defaults

- **WHEN** the owner clears the icon and color
- **THEN** `ui_config` becomes `{}` and the agent renders the neutral `lucide--bot` tile

### Requirement: Render appearance everywhere

The system SHALL render the agent's icon and color on every agent-visible surface — agent list cards/rows, the agent dashboard header and summary card, session/chat author bubbles, the session row and detail header, schedules rows/detail, the ⌘K spotlight, blueprint agent rows, and client-side chat-stream bubbles — via the existing `typeGlyph` / `typeIconTile` / `typeNameChip` / `typeColorStyle` primitives.

#### Scenario: Configured agent renders its appearance

- **WHEN** an agent has an icon and color set
- **THEN** each agent-visible surface renders that icon and color instead of the neutral tile

#### Scenario: Unset agent renders the neutral tile

- **WHEN** an agent has no icon or color set
- **THEN** each agent-visible surface renders the neutral `lucide--bot` tile, unchanged from today

### Requirement: Blueprint declares appearance

Agent manifests in blueprints SHALL be able to declare `ui: {icon, color}`, and that appearance SHALL be applied when the blueprint creates or updates the agent definition — parity with object types declaring `ui` in `object_type_schemas`.

#### Scenario: Blueprint apply sets appearance

- **WHEN** a blueprint manifest declares an agent with `ui: {icon: "bot", color: "#4F46E5"}` and is applied
- **THEN** the resulting agent definition stores that appearance
