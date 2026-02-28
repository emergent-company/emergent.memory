## Context

Agent skills in `.agents/skills/` are simple `SKILL.md` files read at runtime by the agent runtime. Each skill is a directory containing a single `SKILL.md` with `<instructions>` tags. The existing `agent-db-migrations` skill covers only `task migrate:up/down/status` — it does not teach an agent how to create a new migration file, write correct Goose SQL, or handle edge cases (NO TRANSACTION, existing databases, rollback testing).

## Goals / Non-Goals

**Goals:**
- Produce a single `SKILL.md` that gives an agent everything it needs to complete a migration task end-to-end without further guidance
- Cover: file generation, SQL authoring conventions, applying, verifying, rollback, troubleshooting

**Non-Goals:**
- Modifying or replacing the existing `agent-db-migrations` skill
- Changing the migration tooling, Taskfile, or any Go code
- Covering programmatic migration usage (that belongs in developer docs, not agent skills)

## Decisions

**Use a new directory rather than updating `agent-db-migrations`**
The existing skill is intentionally minimal (Taskfile-only). Replacing it risks breaking agents that rely on its current scope. A separate skill with a focused name (`agent-go-migrations`) allows both to coexist.

**Inline all reference material in the skill**
Agent runtimes only receive the SKILL.md content. External links or references to other files aren't accessible at runtime, so all conventions, directives, and troubleshooting steps must be self-contained.

## Risks / Trade-offs

- [Risk] Skill duplicates some content from `apps/server-go/migrations/README.md` → Acceptable: the README is for humans, the skill is formatted for agent consumption. They can drift independently.
- [Risk] Migration tool flags change in the future → Mitigation: the skill references Taskfile commands first (stable interface); direct `go run` commands are secondary and clearly labelled as advanced.
