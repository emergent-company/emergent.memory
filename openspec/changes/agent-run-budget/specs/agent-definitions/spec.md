## MODIFIED Requirements

### Requirement: Agent Definition Storage

The system SHALL store agent definitions from product manifests in the `kb.agent_definitions` table.

#### Scenario: Agent definition schema

- **WHEN** the database migration runs
- **THEN** `kb.agent_definitions` SHALL exist with columns: id, product_id, project_id, name, description, system_prompt, model (JSONB), tools (TEXT[]), trigger, flow_type, is_default, max_steps, default_timeout, visibility, acp_config (JSONB), config (JSONB), created_at, updated_at
- **AND** the `model` JSONB column SHALL support an optional `budget` sub-object with fields `maxCostUSD` (float, nullable) and `maxTotalTokens` (integer, nullable)

#### Scenario: Product manifest import

- **WHEN** a product with agent definitions is installed on a project
- **THEN** each agent in the product manifest's `agents` array SHALL be stored as a row in `kb.agent_definitions`
- **AND** `product_id` SHALL reference the installed product
- **AND** `project_id` SHALL reference the project
- **AND** if the agent manifest includes a `budget` block under `model`, those fields SHALL be stored in the `model` JSONB column

#### Scenario: Product update re-sync

- **WHEN** a product is updated (new version installed)
- **THEN** agent definitions from the new manifest SHALL replace the previous definitions
- **AND** existing AgentRun records SHALL remain intact (they reference agent_id, not definition content)

## ADDED Requirements

### Requirement: ModelConfig Budget Sub-Object
`AgentDefinition.ModelConfig` SHALL support an optional `Budget *AgentBudget` field. `AgentBudget` SHALL have two independently-optional fields: `MaxCostUSD *float64` and `MaxTotalTokens *int64`. A nil `Budget` pointer means no budget is configured.

#### Scenario: ModelConfig with budget
- **WHEN** an agent definition is loaded from the database with a `model` JSONB column containing a `budget` object
- **THEN** `ModelConfig.Budget` SHALL be non-nil
- **AND** `MaxCostUSD` and `MaxTotalTokens` SHALL reflect the stored values (nil if absent)

#### Scenario: ModelConfig without budget
- **WHEN** an agent definition is loaded from the database with a `model` JSONB column that has no `budget` key
- **THEN** `ModelConfig.Budget` SHALL be nil
- **AND** the agent SHALL execute without budget constraints
