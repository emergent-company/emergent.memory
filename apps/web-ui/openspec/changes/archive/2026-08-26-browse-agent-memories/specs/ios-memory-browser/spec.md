## Purpose

Provides a native SwiftUI browsing surface in the iOS app for reading memories created by the selected agent, using standard navigation and list components.

## ADDED Requirements

### Requirement: Browse agent memories

The app SHALL display a list of the selected agent's stored memories.

#### Scenario: List shows memories

- **WHEN** the user opens the memories view for a memory-capable agent
- **THEN** the app shows a list of that agent's memories

#### Scenario: No memories yet

- **WHEN** the selected agent has no stored memories
- **THEN** the app shows an empty state indicating no memories yet

### Requirement: Search memories

The app SHALL allow the user to filter the agent's memories by entering search text.

#### Scenario: Search filters memories

- **WHEN** the user enters a search query
- **THEN** the list updates to show only memories matching the query

### Requirement: View a memory's content

The app SHALL allow the user to open a memory to view its full content.

#### Scenario: Open memory detail

- **WHEN** the user taps a memory in the list
- **THEN** the app navigates to a detail view showing the memory's full content

### Requirement: Gate on memory capability

The app SHALL present the memories browsing surface only when the selected agent is memory-capable.

#### Scenario: Memory-capable agent

- **WHEN** the selected agent supports memory
- **THEN** the app shows an entry point to browse memories

#### Scenario: Non-memory agent

- **WHEN** the selected agent does not support memory
- **THEN** the app hides the memories entry point

### Requirement: Fresh memories from the backend

The app SHALL fetch memories from the backend so the list reflects the agent's current stored memories.

#### Scenario: Refresh on open

- **WHEN** the user opens the memories view
- **THEN** the app fetches the latest memories from the backend
