## MODIFIED Requirements

### Requirement: Render appearance everywhere

The system SHALL render the agent's icon and color on every agent-visible surface — agent list cards/rows, the agent dashboard header and summary card, session/chat author bubbles, the session row and detail header, the chat pane header for the active agent, schedules rows/detail, the ⌘K spotlight, blueprint agent rows, and client-side chat-stream bubbles — via the existing `typeGlyph` / `typeIconTile` / `typeNameChip` / `typeColorStyle` primitives.

#### Scenario: Configured agent renders its appearance

- **WHEN** an agent has an icon and color set
- **THEN** each agent-visible surface renders that icon and color instead of the neutral tile

#### Scenario: Unset agent renders the neutral tile

- **WHEN** an agent has no icon or color set
- **THEN** each agent-visible surface renders the neutral `lucide--bot` tile, unchanged from today

#### Scenario: Chat pane header renders the active agent's tile

- **WHEN** the chat pane is showing an agent that has an icon and color set
- **THEN** the pane header renders that agent's icon tile (and the neutral `lucide--bot` tile when unset)
