## ADDED Requirements

### Requirement: Skill tool opt-in via AgentDefinition.Tools
The `skill` tool SHALL be added to an agent's tool set if and only if `"skill"` appears in `AgentDefinition.Tools`. Agents without `"skill"` in their Tools list SHALL NOT receive the skill tool and SHALL be unaffected by this feature.

#### Scenario: Agent with "skill" in Tools receives the skill tool
- **WHEN** `runPipeline()` assembles tools for an agent whose `AgentDefinition.Tools` contains `"skill"`
- **THEN** `BuildSkillTool` is called and the resulting tool is appended to the tool list

#### Scenario: Agent without "skill" in Tools is unaffected
- **WHEN** `runPipeline()` assembles tools for an agent whose `AgentDefinition.Tools` does not contain `"skill"`
- **THEN** no skill tool is added and no skill-related code runs

### Requirement: Semantic skill retrieval at run start
When the skill tool is built for a run, the system SHALL retrieve the top-K most relevant skills using semantic similarity against the run's trigger message.

Retrieval logic:
1. Call `Repository.FindForAgent(ctx, projectID)` to get the merged skill set (global + project-scoped).
2. If total accessible skill count ≤ `SkillListThreshold` (50), use all skills (no embedding call).
3. If total count > 50:
   a. Determine query text: use `run.TriggerMessage` if non-empty; otherwise fall back to `agentDefinition.Name + " " + agentDefinition.Description`.
   b. Call `embeddingsSvc.EmbedQuery(ctx, queryText)` to get a 768-dim query vector.
   c. Call `Repository.FindRelevant(ctx, projectID, queryVector, topK)` which executes a cosine similarity query (`<=>` operator) with `SET LOCAL ivfflat.probes = 10`, returning the top `SkillTopK` (10) skills ordered by ascending distance.
   d. Only skills with `description_embedding IS NOT NULL` are eligible for semantic retrieval.
4. If the embedding call fails, fall back to listing all accessible skills (log a warning).

Constants:
- `SkillListThreshold = 50`
- `SkillTopK = 10`

#### Scenario: Small library — all skills listed
- **WHEN** the project has 30 accessible skills (global + project-scoped) and "skill" is in AgentDefinition.Tools
- **THEN** all 30 skills appear in the tool description without any embedding call

#### Scenario: Large library — semantic top-K retrieved
- **WHEN** the project has 200 accessible skills and "skill" is in AgentDefinition.Tools
- **THEN** `EmbedQuery` is called once with the trigger message, and the 10 most relevant skills (by cosine similarity on description_embedding) are surfaced in the tool description

#### Scenario: Fallback when embedding service unavailable
- **WHEN** the project has 200 accessible skills and `EmbedQuery` returns an error
- **THEN** all 200 accessible skills are listed in the tool description, a warning is logged, and the run continues normally

#### Scenario: Fallback when no trigger message
- **WHEN** the run has no trigger message (e.g. scheduled trigger)
- **THEN** the query text for embedding is `agentDefinition.Name + " " + agentDefinition.Description`

### Requirement: Skill tool description lists available skills as XML
The `skill` tool's `Description` field SHALL contain an `<available_skills>` XML block enumerating each surfaced skill with its `name`, `description`, and a human-readable `content_preview` (first 100 chars of content).

Format:
```
Load detailed workflow instructions for a named skill.

<available_skills>
  <skill>
    <name>emergent-onboard</name>
    <description>Onboard a project into Memory...</description>
  </skill>
  ...
</available_skills>
```

If no skills are accessible (empty library), the tool description SHALL omit the `<available_skills>` block and note that no skills are available.

#### Scenario: Tool description contains available skills
- **WHEN** `BuildSkillTool` resolves 5 skills for a run
- **THEN** the tool `Description` contains an `<available_skills>` block with 5 `<skill>` entries

#### Scenario: Empty library
- **WHEN** no skills are accessible for the project
- **THEN** the tool is still registered but its description indicates no skills are available

### Requirement: Skill tool invocation returns full content
When the agent calls `skill({name: "..."})`, the tool SHALL:
1. Look up the skill by exact `name` from the set resolved at tool build time (in-memory map, no additional DB call).
2. Return a `<skill_content>` XML block containing the skill's full `content` field (the Markdown body).
3. If the name is not found, return an error message listing the available skill names.

The tool input schema SHALL accept a single string parameter `name`.

#### Scenario: Successful skill load
- **WHEN** the agent calls `skill({name: "emergent-onboard"})`
- **THEN** the tool returns `<skill_content name="emergent-onboard">\n{full content}\n</skill_content>`

#### Scenario: Unknown skill name
- **WHEN** the agent calls `skill({name: "nonexistent-skill"})`
- **THEN** the tool returns an error message: `Skill "nonexistent-skill" not found. Available skills: emergent-onboard, emergent-agents, ...`

#### Scenario: Exact name match required
- **WHEN** the agent calls `skill({name: "EmergentOnboard"})` (wrong casing)
- **THEN** the tool returns a not-found error (no fuzzy matching)

### Requirement: Skill tool errors are non-fatal to run assembly
If `BuildSkillTool` fails for any reason (e.g. DB error fetching skills), the error SHALL be logged as a warning and the run SHALL proceed without the skill tool. The run SHALL NOT be aborted.

#### Scenario: DB error during skill tool build
- **WHEN** `BuildSkillTool` is called and `Repository.FindForAgent` returns a database error
- **THEN** the error is logged at WARN level, no skill tool is added to the run, and `runPipeline` continues normally
