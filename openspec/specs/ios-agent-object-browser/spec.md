# ios-agent-object-browser Specification

## Purpose
TBD - created by archiving change agent-object-provenance. Update Purpose after archive.

## Requirements

### Requirement: Browse an agent's objects

The iOS app SHALL provide a native SwiftUI object browser scoped to a selected agent's provenance, listing the objects created by or updated by that agent. No object browser exists in iOS today; this is a net-new surface replacing the retired memories browser.

#### Scenario: List shows agent objects

- **WHEN** the user opens the object browser for an agent
- **THEN** the app shows a list of objects created by or updated by that agent

#### Scenario: No objects yet

- **WHEN** the selected agent has no objects in its provenance
- **THEN** the app shows an empty state indicating no objects yet

### Requirement: Search an agent's objects

The app SHALL allow the user to filter the agent's objects by entering search text.

#### Scenario: Search filters objects

- **WHEN** the user enters a search query
- **THEN** the list updates to show only objects matching the query

### Requirement: View an object's detail

The app SHALL allow the user to open an object to view its detail.

#### Scenario: Open object detail

- **WHEN** the user taps an object in the list
- **THEN** the app navigates to a detail view showing the object's content

### Requirement: Gate on agent availability

The app SHALL present the object browser only when an agent is selected and available for provenance browsing.

#### Scenario: Agent selected

- **WHEN** an agent is selected
- **THEN** the app shows an entry point to browse that agent's objects

#### Scenario: No agent selected

- **WHEN** no agent is selected
- **THEN** the app hides the object browser entry point

### Requirement: Fresh objects from the backend

The app SHALL fetch objects from the backend so the list reflects the agent's current provenance.

#### Scenario: Refresh on open

- **WHEN** the user opens the object browser
- **THEN** the app fetches the latest objects from the backend
