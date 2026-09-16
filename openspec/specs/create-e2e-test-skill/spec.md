## ADDED Requirements

### Requirement: Skill directory created at .opencode/skills/create-e2e-test/
The repo SHALL contain a skill at `.opencode/skills/create-e2e-test/SKILL.md` following standard OpenCode skill format. The skill directory SHALL also contain a `reference/` sub-directory with supporting docs.

#### Scenario: Skill file exists and is loadable
- **WHEN** `.opencode/skills/create-e2e-test/SKILL.md` is read
- **THEN** the file exists and contains valid skill content with a description, trigger conditions, and step-by-step workflow

### Requirement: SKILL.md documents the create-e2e-test workflow
The `SKILL.md` SHALL include:
- **When to use**: trigger conditions (contributor wants to add a new e2e test file)
- **Step-by-step workflow**: how to name the file, which framework functions to import, how to structure `TestMain` (if needed), how to set up/teardown a project, how to call agents
- **Reference to framework API**: pointer to `reference/framework-api.md`
- **Reference to patterns**: pointer to `reference/patterns.md`
- **Example skeleton**: a minimal but complete test file template

#### Scenario: Skill covers project setup pattern
- **WHEN** the skill is read by an agent
- **THEN** it explicitly covers calling `framework.CreateProject` and `framework.DeleteProjectOnCleanup` for test isolation

#### Scenario: Skill covers agent polling pattern
- **WHEN** the skill is read by an agent
- **THEN** it explicitly covers calling `framework.TriggerAgent` followed by `framework.PollUntilSuccess`

### Requirement: reference/framework-api.md lists all exported framework functions
The `reference/framework-api.md` file SHALL list every exported function in the `e2eframework` package with its signature, a one-line description, and which `framework/*.go` file it lives in.

#### Scenario: API reference covers all framework files
- **WHEN** `reference/framework-api.md` is read
- **THEN** it contains entries for functions from all 9 framework files (client, server, project, agents, graph, cli, runlog, env, parse)

### Requirement: reference/patterns.md documents common test patterns
The `reference/patterns.md` file SHALL document at minimum:
- Project isolation pattern (create + cleanup)
- Agent trigger + poll pattern
- Graph assertion pattern
- CLI invocation pattern

#### Scenario: Patterns file documents project isolation
- **WHEN** `reference/patterns.md` is read
- **THEN** it contains a section on how to create and auto-delete a project per test

#### Scenario: Patterns file contains code examples
- **WHEN** `reference/patterns.md` is read
- **THEN** each pattern section includes a Go code snippet showing actual usage
