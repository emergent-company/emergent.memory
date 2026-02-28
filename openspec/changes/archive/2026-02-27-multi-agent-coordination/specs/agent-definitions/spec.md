## ADDED Requirements

### Requirement: Agent Definition Storage

The system SHALL store agent definitions from product manifests in the `kb.agent_definitions` table.

#### Scenario: Agent definition schema

- **WHEN** the database migration runs
- **THEN** `kb.agent_definitions` SHALL exist with columns: id, product_id, project_id, name, description, system_prompt, model (JSONB), tools (TEXT[]), trigger, flow_type, is_default, max_steps, default_timeout, visibility, acp_config (JSONB), config (JSONB), created_at, updated_at

#### Scenario: Product manifest import

- **WHEN** a product with agent definitions is installed on a project
- **THEN** each agent in the product manifest's `agents` array SHALL be stored as a row in `kb.agent_definitions`
- **AND** `product_id` SHALL reference the installed product
- **AND** `project_id` SHALL reference the project

#### Scenario: Product update re-sync

- **WHEN** a product is updated (new version installed)
- **THEN** agent definitions from the new manifest SHALL replace the previous definitions
- **AND** existing AgentRun records SHALL remain intact (they reference agent_id, not definition content)

### Requirement: Agent Visibility Levels

The system SHALL support three visibility levels for agent definitions: external, project, and internal.

#### Scenario: Default visibility

- **WHEN** an agent definition does not specify a visibility level
- **THEN** the system SHALL default to `project` visibility

#### Scenario: Admin UI filtering

- **WHEN** the admin UI requests the agent list endpoint
- **THEN** agents with visibility `external` and `project` SHALL be included
- **AND** agents with visibility `internal` SHALL be excluded by default
- **AND** an `include_internal=true` query parameter SHALL include internal agents

#### Scenario: Internal agents in coordination tools

- **WHEN** an agent calls `list_available_agents`
- **THEN** agents with visibility `internal` SHALL be included in the results
- **AND** `spawn_agents` SHALL accept internal agents without restriction

### Requirement: ACP Configuration

The system SHALL store ACP (Agent Card Protocol) metadata for externally-visible agents.

#### Scenario: ACP config on external agent

- **WHEN** an agent definition has `visibility: "external"` and an `acp` configuration block
- **THEN** the system SHALL store the ACP metadata (display_name, description, capabilities, input_modes, output_modes) in the `acp_config` JSONB column

#### Scenario: ACP config ignored for non-external

- **WHEN** an agent definition has `visibility: "project"` or `"internal"` with an `acp` block
- **THEN** the system SHALL ignore the ACP configuration (do not store it)

#### Scenario: External without ACP

- **WHEN** an agent definition has `visibility: "external"` but no `acp` block
- **THEN** the agent SHALL be externally invocable
- **AND** `acp_config` SHALL be NULL (invoke-only, not discoverable via agent card)

### Requirement: Interactive Agent Selection

The system SHALL support selecting a default agent for interactive chat sessions.

#### Scenario: Default agent for chat

- **WHEN** a chat session is initiated for a project
- **THEN** the system SHALL use the agent definition with `is_default: true` for that project
- **AND** if multiple defaults exist, the system SHALL use the one with `visibility: "external"` (or first match)

#### Scenario: No default agent fallback

- **WHEN** a chat session is initiated and no agent definition has `is_default: true`
- **THEN** the system SHALL fall back to direct LLM interaction without agent tools

### Requirement: Agent Trigger Configuration

The system SHALL support event-driven and schedule-driven triggers on agent definitions.

#### Scenario: Event trigger registration

- **WHEN** an agent definition has `trigger: "on_document_ingested"`
- **THEN** the system SHALL register a listener for document ingestion events
- **AND** when a document is ingested, the system SHALL execute that agent with the document context as input

#### Scenario: Schedule trigger registration

- **WHEN** an agent definition has a cron expression as its trigger (e.g., `"0 8 * * MON-FRI"`)
- **THEN** the system SHALL register a cron job in the existing scheduler
- **AND** the job SHALL execute the agent at the specified schedule

#### Scenario: Manual-only agent

- **WHEN** an agent definition has `trigger: null`
- **THEN** the agent SHALL only be executable via API trigger, chat interaction, or `spawn_agents`
