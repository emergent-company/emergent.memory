## REMOVED Requirements

### Requirement: Link to the memories browser

**Reason**: The "memories browser" is retired; the agent dashboard now links to the project object browser filtered by provenance. See the added requirement below.
**Migration**: The memories subpage link is replaced by a link to objects created/updated by this agent.

The dashboard SHALL provide a link to the memories subpage.

#### Scenario: Memories link present

- **WHEN** the dashboard loads
- **THEN** a link to the memories subpage is shown

### Requirement: List memory objects

The memories subpage SHALL list memory objects, each showing its content, category, and confidence.

#### Scenario: Memories present

- **WHEN** the memories subpage loads and memory objects exist
- **THEN** the objects are listed, each showing content, category, and confidence

#### Scenario: No memories

- **WHEN** the memories subpage loads and no memory objects exist
- **THEN** a clear "no memories yet" state is shown

### Requirement: Search memory objects

The memories subpage SHALL let the user search memory objects and display the matching results.

#### Scenario: Search with matches

- **WHEN** the user enters a query that matches stored objects
- **THEN** the matching objects are listed

#### Scenario: Search with no matches

- **WHEN** the user enters a query with no matching objects
- **THEN** a clear "no matches" state is shown

### Requirement: Show a memory's full content

Selecting a memory SHALL show its full content.

#### Scenario: Open a memory detail

- **WHEN** the user selects a memory in the list
- **THEN** the memory's full content is displayed

## ADDED Requirements

### Requirement: Link to objects created/updated by this agent

The dashboard SHALL provide a link to a subpage that browses the project's objects filtered by provenance — objects created by or updated by this agent.

#### Scenario: Objects link present

- **WHEN** the dashboard loads
- **THEN** a link to the agent's created/updated objects subpage is shown
