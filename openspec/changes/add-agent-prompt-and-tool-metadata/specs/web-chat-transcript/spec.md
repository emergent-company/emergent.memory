## Purpose

How the live web chat surface renders a conversation transcript: the agent's composed prompt as a card at the start of the conversation, and the execution duration and call id shown with each tool call.

## ADDED Requirements

### Requirement: Show the agent prompt at the start of a conversation

When a conversation transcript contains a `system` instruction record, the live chat SHALL render the instruction as a collapsible card at the very start of the transcript, presented like a tool-call card, and SHALL render only the first such record.

#### Scenario: Prompt present

- **WHEN** a conversation transcript loaded into the live chat contains a `system` instruction record
- **THEN** the chat shows an agent-prompt card above the first turn, with the instruction text in a collapsed, expandable body

#### Scenario: Prompt absent

- **WHEN** the transcript has no `system` instruction record
- **THEN** the chat renders the transcript with no agent-prompt card

#### Scenario: Duplicate instruction records

- **WHEN** the transcript contains more than one `system` instruction record
- **THEN** the chat renders the prompt card once, for the first record, and skips the rest

### Requirement: Show tool-call duration and id

For each historical tool call in a conversation transcript, the live chat's tool-call detail SHALL show the tool-call id and the execution duration when the record carries them.

#### Scenario: Duration and id present

- **WHEN** a historical tool-call record carries an id or an execution duration
- **THEN** expanding the tool call's detail shows the id and the duration

#### Scenario: Duration and id absent

- **WHEN** a historical tool-call record carries neither
- **THEN** the tool-call detail renders without them
