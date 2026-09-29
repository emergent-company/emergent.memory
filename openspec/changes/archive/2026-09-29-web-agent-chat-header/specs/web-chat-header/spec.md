## ADDED Requirements

### Requirement: Chat header shows the active agent's identity

The chat pane header SHALL show the active agent's icon tile, name, and stored
`description` — the icon from the agent's `ui_config` (neutral `lucide--bot`
tile when unset), the name as the title, and the description as a second line.
When the active agent has no description, the description line SHALL be omitted
rather than rendered empty. When no agent is active (no agents and no
conversation), the header SHALL fall back to the existing generic
"Chat with Memory" copy.

#### Scenario: Header renders the agent's identity

- **WHEN** the chat page loads with an agent that has an icon, a name, and a description
- **THEN** the header shows that agent's icon tile, its name as the title, and its description as the second line

#### Scenario: Agent without a description

- **WHEN** the active agent has no description
- **THEN** the header shows the icon and name with no second line

#### Scenario: Agent without an appearance

- **WHEN** the active agent has no `ui_config` icon or color
- **THEN** the header renders the neutral `lucide--bot` tile

#### Scenario: No agent active

- **WHEN** the chat page loads with no agents and no conversation
- **THEN** the header shows the generic "Chat with Memory" copy and does not error

### Requirement: Chat header tracks the active agent live

The chat pane header SHALL update to the newly selected agent when the agent
picker changes, and SHALL reflect the conversation's agent when a session is
resumed. The description SHALL be written to the header as text content, never
as HTML.

#### Scenario: Picker changes the header

- **WHEN** the user selects a different agent in the agent picker
- **THEN** the header's title, description, and icon update to that agent

#### Scenario: Resuming a session updates the header

- **WHEN** the user resumes a conversation whose agent differs from the currently shown one
- **THEN** the header shows that conversation's agent

#### Scenario: Description is not injected as HTML

- **WHEN** an agent's description contains HTML-like markup
- **THEN** the header shows it as literal text
