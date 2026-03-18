## Context

The Memory platform documentation spans three layers that must stay in sync:
1. **Site docs** (`docs/site/`) — user-facing and developer-facing guides
2. **Domain READMEs** (`apps/server/domain/*/README.md`) — implementation-level reference living next to the code
3. **Agent skills** (`.agents/skills/*/SKILL.md`) — operational playbooks for AI agents driving the CLI

Audit findings:
- `apps/server/domain/mcp/README.md` still uses legacy underscore tool names (`hybrid_search`, `semantic_search`, `find_similar`, `traverse_graph`, `list_relationships`, `batch_create_entities`, `batch_create_relationships`, `restore_entity`, `list_tags`) and reports "29 tools" (now ~50+ across all tool files). Auth examples show `X-API-Key` while the actual handler also accepts `Authorization: Bearer`; the developer guide (the canonical reference) correctly uses `Bearer`.
- Five CLI commands (`memory init`, `memory ask`, `memory adk-sessions`, `memory mcp-guide`, `memory install-memory-skills`) are fully implemented but absent from all site docs.
- `memory-onboard` skill uses `MEMORY_PROJECT` as the only env var; `memory init` and `memory projects set` write `MEMORY_PROJECT_ID` + `MEMORY_PROJECT_NAME` + `MEMORY_PROJECT_TOKEN` — agents using the skill may not recognise the env vars their init step produces.
- `memory-blueprints` skill is missing the standard `## Rules` block that every other skill includes.

## Goals / Non-Goals

**Goals:**
- Correct tool names and counts in `apps/server/domain/mcp/README.md`
- Standardise auth documentation to `Authorization: Bearer` throughout the MCP README
- Add developer guide coverage for undocumented CLI commands
- Update `memory-onboard` skill to mention `MEMORY_PROJECT_ID`/`MEMORY_PROJECT_TOKEN`
- Add `## Rules` block to `memory-blueprints` skill

**Non-Goals:**
- Rewriting the entire MCP README from scratch
- Adding user guide coverage for `memory ask` / `memory init` (CLI-first workflows belong in developer guide)
- Auditing the Go SDK reference pages
- Changing any Go implementation code

## Decisions

### Decision: Update MCP README in-place rather than regenerate
**Rationale**: The README has valuable context (architecture, Quick Start flows, SDK examples) that would be lost in a full regeneration. Targeted surgery on the tool table, auth section, and changelog is lower risk and faster.

**Alternative considered**: Auto-generate README from service code via a script. Rejected because it would remove narrative context and requires ongoing tooling maintenance.

### Decision: New developer guide page for undocumented commands
**Rationale**: These commands (`init`, `ask`, `adk-sessions`, `mcp-guide`, `install-memory-skills`) don't cleanly fit any existing page. A dedicated `cli-reference.md` page in `docs/site/developer-guide/` keeps the developer guide index clean and gives these commands a canonical home.

**Alternative considered**: Add each command to the most relevant existing page (e.g., `mcp-guide` on `mcp-servers.md`). Rejected because it scatters coverage and some commands (like `ask`, `init`) don't fit any existing page.

### Decision: memory-onboard skill mentions both env var styles
**Rationale**: `MEMORY_PROJECT` (name-based resolution) and `MEMORY_PROJECT_ID` (direct ID) both work. The skill should guide agents to check for either and understand that `memory init` produces `MEMORY_PROJECT_ID` + `MEMORY_PROJECT_TOKEN`. Rewriting the skill to only use `MEMORY_PROJECT_ID` would break workflows that pre-date `memory init`.

## Risks / Trade-offs

- [Risk] MCP README tool count may drift again as tools are added → Mitigation: note in the README that the count is approximate and link to the source files
- [Risk] New CLI reference page may go stale as commands evolve → Mitigation: keep the page thin (link to `memory <cmd> --help` as the authoritative reference) and add it to the AGENTS.md review checklist

## Migration Plan

All changes are documentation-only. No server restart, database migration, or client update required. Changes take effect when merged and the site is rebuilt.

## Open Questions

- Should `memory ask` get a dedicated user guide page (not just developer guide)? Defer — it's a power-user feature; developer guide is sufficient for now.
