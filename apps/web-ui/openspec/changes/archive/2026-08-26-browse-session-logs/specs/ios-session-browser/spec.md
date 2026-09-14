## Purpose

Provides a native SwiftUI chat-style browser for reviewing recorded agent sessions, including the tool calls each turn made.

## ADDED Requirements

### Requirement: List sessions

The app SHALL list recorded sessions, most recent first.

#### Scenario: Sessions shown

- **WHEN** the user opens the sessions browser
- **THEN** the app shows the sessions ordered by recency

#### Scenario: No sessions

- **WHEN** no sessions have been recorded
- **THEN** the app shows an empty state indicating no sessions yet

### Requirement: View a session as a chat

The app SHALL render a session as a conversation, with user and assistant turns shown as distinct chat messages.

#### Scenario: Chat rendered

- **WHEN** the user opens a session
- **THEN** the app shows the conversation as a chat, distinguishing user from assistant messages

### Requirement: Show tool call details

The app SHALL display, for each turn, the tool calls it made, with the tool name, arguments, and result.

#### Scenario: Tool call visible

- **WHEN** a turn made a tool call
- **THEN** the app shows the tool call (name, arguments, result, error flag) expandable within that turn

### Requirement: Fresh sessions from the backend

The app SHALL fetch session data from the backend so the list reflects the recorded sessions.

#### Scenario: Refresh on open

- **WHEN** the user opens the sessions browser
- **THEN** the app fetches the latest sessions from the backend
