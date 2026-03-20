## MODIFIED Requirements

### Requirement: batch creation uses subgraph format when relationships are needed
When creating objects that need relationships wired in the same operation, agents SHALL use the subgraph format with `create-batch` rather than two separate passes (objects then relationships). The subgraph format is the primary recommended pattern; the flat-array format is for objects-only populations.

#### Scenario: agent populates objects with relationships
- **WHEN** an agent needs to create objects and wire relationships between them
- **THEN** the agent writes a single subgraph JSON file and calls `create-batch` once, with no stdout ID capture required

#### Scenario: agent populates objects only
- **WHEN** an agent needs to create objects with no relationships
- **THEN** the agent may use either the flat-array format or the subgraph format (objects array, empty relationships)

## ADDED Requirements

### Requirement: skill documents subgraph file format
The skill SHALL document the subgraph JSON format with a complete worked example showing `_ref` usage, `key` for idempotency, and the chunking pattern for populations exceeding 100 objects.

#### Scenario: agent follows subgraph example
- **WHEN** an agent reads the skill and needs to populate >1 object type with relationships
- **THEN** the agent can produce a valid subgraph file and run `create-batch` without needing to look up additional documentation

### Requirement: skill documents chunking strategy for large populations
For populations exceeding 100 objects, the skill SHALL show a Python chunking pattern that splits a large object+relationship list into subgraph chunks of ≤100 objects each, preserving cross-chunk relationship wiring via stable `key` values.

#### Scenario: agent populates 200+ objects with relationships
- **WHEN** an agent needs to create more than 100 objects with relationships
- **THEN** the agent uses `key` on all objects and splits into multiple subgraph calls, referencing cross-chunk objects by their `key` via a lookup after the first chunk
