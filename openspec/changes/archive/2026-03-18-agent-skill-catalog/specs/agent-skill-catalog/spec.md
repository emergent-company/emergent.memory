## ADDED Requirements

### Requirement: AgentDefinition supports a skills field
An agent definition SHALL support a `skills` field containing a list of skill names the agent is permitted to load. When this field is non-empty, the skill tool is automatically added to the agent's pipeline.

#### Scenario: Skills field present — tool auto-injected
- **WHEN** an agent definition has `skills: ["code-review"]`
- **THEN** the agent's pipeline includes a callable `skill` tool without requiring `"skill"` in `tools:`

#### Scenario: Skills field absent — tool not injected
- **WHEN** an agent definition has no `skills` field (or empty list) and `"skill"` is not in `tools:`
- **THEN** no `skill` tool is added to the pipeline

#### Scenario: Skills wildcard includes all agent-visible skills
- **WHEN** an agent definition has `skills: ["*"]`
- **THEN** the skill catalog includes all project-scoped, org-scoped, and global skills visible to the agent

#### Scenario: No skills in project — tool omitted even if declared
- **WHEN** the project has zero skills and the agent definition has `skills: ["*"]`
- **THEN** no `skill` tool is added (nothing to expose)

#### Scenario: Legacy fallback — skill in tools whitelist still works
- **WHEN** an agent definition has `tools: ["skill"]` and no `skills:` field
- **THEN** the skill tool is injected with all agent-visible skills (backward-compatible behaviour)

### Requirement: Skill catalog filtered to declared skill names
When `skills:` contains explicit names (not `["*"]`), the `<available_skills>` catalog SHALL include only the skills whose names match the declared list.

#### Scenario: Explicit list filters catalog
- **WHEN** an agent definition has `skills: ["code-review", "deploy-checklist"]` and the project has 10 skills
- **THEN** the tool description's `<available_skills>` block contains exactly `code-review` and `deploy-checklist`

#### Scenario: Declared skill not found — warning logged, absent from catalog
- **WHEN** an agent definition declares `skills: ["nonexistent-skill"]` and no such skill exists
- **THEN** the catalog is empty (tool still injected but empty), and a warning is logged at session start

#### Scenario: Wildcard bypasses filtering
- **WHEN** `skills: ["*"]`
- **THEN** the catalog contains all agent-visible skills (no filtering applied)

### Requirement: Skill catalog injected into tool description
The `skill` tool's description SHALL contain an `<available_skills>` XML block listing each advertised skill's name and description.

#### Scenario: Catalog lists all in-scope skills when count is within threshold
- **WHEN** the total in-scope skill count is ≤ 50
- **THEN** the tool description includes an `<available_skills>` block with one `<skill>` entry per skill (name + description)

#### Scenario: Catalog lists semantic top-K when count exceeds threshold
- **WHEN** the total in-scope skill count is > 50
- **THEN** the tool description includes only the top semantically relevant skills (≤ 10) with a note showing how many were omitted

#### Scenario: Semantic retrieval falls back to full list on error
- **WHEN** the embeddings service returns an error during semantic retrieval
- **THEN** all in-scope skills are included in the catalog (no error propagated to the agent)

### Requirement: Agent calls skill tool by name to load full content
When the agent calls the `skill` tool with `{"name": "<skill-name>"}`, the server SHALL return the full skill content.

#### Scenario: Valid name returns content
- **WHEN** the agent calls `skill({"name": "code-review"})`
- **THEN** the response contains `{"content": "<skill_content name=\"code-review\">...</skill_content>"}` with the full skill body

#### Scenario: Unknown name returns error and available names
- **WHEN** the agent calls `skill({"name": "nonexistent"})`
- **THEN** the response contains `{"error": "skill \"nonexistent\" not found", "available_names": [...]}`

#### Scenario: Empty name returns error
- **WHEN** the agent calls `skill({"name": ""})`
- **THEN** the response contains `{"error": "name is required"}`

### Requirement: Agent-visible skill scope merges global, org, and project skills
Skills visible to an agent SHALL be the union of project-scoped, org-scoped, and global skills. Project takes precedence over org, org over global, when names collide.

#### Scenario: Project skill shadows global skill of same name
- **WHEN** a global skill `foo` and a project skill `foo` both exist
- **THEN** only the project-scoped `foo` appears in the catalog

### Requirement: DescriptionEmbedding populated at skill write time
When a skill is created or updated, the server SHALL generate and store a vector embedding of the skill's description so that semantic retrieval can operate.

#### Scenario: Embedding generated on create
- **WHEN** a skill is created with a non-empty description
- **THEN** the `description_embedding` column is populated

#### Scenario: Embedding regenerated on description update
- **WHEN** a skill's description is updated
- **THEN** the `description_embedding` column is updated to reflect the new description

### Requirement: Agent definition CLI and blueprint YAML support skills field
The `memory agent-definitions create` and `update` commands SHALL accept a `--skills` flag. Blueprint YAML agent definitions SHALL support a `skills:` key.

#### Scenario: CLI creates definition with skills
- **WHEN** `memory agent-definitions create --name foo --skills "code-review,deploy-checklist"` is run
- **THEN** the created definition has `skills: ["code-review", "deploy-checklist"]`

#### Scenario: Blueprint YAML skills field parsed
- **WHEN** an agent YAML file contains `skills: ["*"]`
- **THEN** the blueprint apply sets the `skills` field on the created agent definition
