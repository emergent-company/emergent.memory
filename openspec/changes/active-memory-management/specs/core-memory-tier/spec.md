## ADDED Requirements

### Requirement: Memory objects support a tier property
The `Memory` object type in the `agent-memory` schema pack SHALL support a `tier` string property with allowed values `archival` (default) and `core`. Existing memories without an explicit tier SHALL be treated as `archival`.

#### Scenario: New memory defaults to archival tier
- **WHEN** `save_memory` is called without specifying a tier
- **THEN** the created Memory object SHALL have `tier = archival`
- **THEN** the Memory object SHALL have label `archival` in addition to its category and scope labels

#### Scenario: Memory promoted to core has correct label
- **WHEN** `promote_to_core` is called for a memory
- **THEN** the Memory object SHALL have `tier = core`
- **THEN** the Memory object SHALL have label `core` replacing `archival`

### Requirement: promote_to_core MCP tool promotes a memory to the core tier
The system SHALL expose a `promote_to_core` MCP tool that sets `tier = core` on a specified Memory object for the current user.

#### Scenario: Successful promotion to core
- **WHEN** `promote_to_core` is called with a valid `memory_id` belonging to the current user
- **THEN** the system SHALL update the Memory object's `tier` property to `core`
- **THEN** the system SHALL update the object's labels to replace `archival` with `core`
- **THEN** the system SHALL return success with the memory ID and its content

#### Scenario: Promotion rejected for other user's memory
- **WHEN** `promote_to_core` is called with a `memory_id` belonging to a different user
- **THEN** the system SHALL return an error indicating the memory was not found
- **THEN** the system SHALL NOT modify any memory

#### Scenario: Core memory count at cap triggers warning
- **WHEN** `promote_to_core` is called and the user already has core memories at or above the configured cap (default: 10)
- **THEN** the system SHALL still promote the memory
- **THEN** the system SHALL include a warning in the response: "Core memory cap reached (N/10). Consider demoting lower-priority core memories."

### Requirement: Core memories are auto-injected into every chat session system prompt
At the start of each chat message processing, the system SHALL query all `tier = core` Memory objects for the current user and prepend them to the session's system prompt, without requiring the LLM to call `recall_memories`.

#### Scenario: Core memories present — injected into system prompt
- **WHEN** a chat message is processed for a user who has core memories
- **AND** the `agent-memory` schema pack is installed in the current project
- **THEN** the system SHALL query `type=Memory, tier=core, actor_id=<current_user>` from the graph
- **THEN** the system SHALL prepend a formatted `## Core Memory` section to the system prompt containing all core memories
- **THEN** the LLM SHALL receive the core memories in context without making any tool calls

#### Scenario: No core memories — no injection
- **WHEN** a chat message is processed for a user who has no core memories
- **THEN** the system SHALL skip the core memory injection step
- **THEN** the system prompt SHALL NOT contain a `## Core Memory` section

#### Scenario: agent-memory schema pack not installed — no injection
- **WHEN** a chat message is processed in a project that does not have the `agent-memory` schema pack installed
- **THEN** the system SHALL skip core memory injection entirely

#### Scenario: Core memory injection capped at configured limit
- **WHEN** a user has more core memories than the configured cap (default: 10)
- **THEN** the system SHALL inject only the top-N core memories by `use_count` descending, then `created_at` descending
- **THEN** the injected section SHALL note the total count vs injected count if truncated

### Requirement: manage_memory supports demoting core memories to archival
The existing `manage_memory` tool's `update` action SHALL support setting `tier = archival` to demote a core memory back to the archival tier.

#### Scenario: Successful demotion from core to archival
- **WHEN** `manage_memory` is called with `action: update`, `memory_id`, and `updates: {tier: "archival"}`
- **THEN** the system SHALL update the Memory object's `tier` to `archival`
- **THEN** the system SHALL update labels to replace `core` with `archival`
- **THEN** the memory SHALL no longer appear in core memory injection
