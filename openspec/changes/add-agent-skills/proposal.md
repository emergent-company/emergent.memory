## Why

Agents currently have no mechanism to load specialized workflow instructions on demand — every agent must have all guidance baked into its system prompt at definition time, which bloats context windows and makes instructions hard to reuse across agents. A reusable, DB-managed skill library (modeled after OpenCode's skill system) lets agents pull structured workflow instructions exactly when needed, keeping prompts lean while enabling richer, more capable agentic behavior.

## What Changes

- New `kb.skills` database table stores skills with global or project-scoped visibility
- New `skills` domain (handler, store, module) exposes a REST API for CRUD on skills
- New `skill` native tool added to the agent executor — opted in via `AgentDefinition.Tools` whitelist — surfaces available skills and returns `<skill_content>` blocks on demand
- New `sdk/skills` package for programmatic access
- New `memory skills` CLI subcommand with `list`, `get`, `create`, `update`, `delete`, and `import` (parses SKILL.md frontmatter) commands
- Existing `.agents/skills/*.SKILL.md` files can be seeded into the DB via `memory skills import`

## Capabilities

### New Capabilities

- `skill-library`: DB-managed skill storage and REST API — CRUD for global and project-scoped skills, with project-level skills overriding global skills of the same name
- `agent-skill-tool`: Native `skill` tool for the agent executor — lists available skills in tool description, returns full skill content on demand when agent invokes the tool
- `skill-cli`: `memory skills` CLI subcommand with full CRUD plus `import` command that parses SKILL.md frontmatter

### Modified Capabilities

- `agent-definitions`: `AgentDefinition.Tools` whitelist gains a new first-class value `"skill"` to opt agents into skill tool access — no schema change required, this is purely additive behavior

## Impact

- **New files**: `apps/server/migrations/00052_create_skills.sql`, `apps/server/domain/skills/` (entity, store, handler, skill_tool, module), `apps/server/pkg/sdk/skills/client.go`, `tools/cli/internal/cmd/skills.go`
- **Modified files**: `apps/server/domain/agents/executor.go` (inject SkillRepository, wire skill tool in `runPipeline`), `apps/server/pkg/sdk/sdk.go` (register `Skills` client), `tools/cli/cmd/main.go` (import skills cmd)
- **Database**: new `kb.skills` table; no changes to existing tables
- **API**: new routes under `/api/skills` (global) and `/api/projects/:projectId/skills` (project-scoped)
- **No breaking changes** — all additions are additive; existing agents unaffected unless they opt in via `Tools: ["skill"]`
