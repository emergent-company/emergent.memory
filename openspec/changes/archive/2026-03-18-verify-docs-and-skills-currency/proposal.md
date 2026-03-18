## Why

Documentation and skills have accumulated drift relative to the current platform: the MCP README still says "18 tools" and references tool names from an earlier schema (e.g. `hybrid_search`, `batch_create_entities`) that no longer match the actual implementation (`search-hybrid`, `entity-create`); the memory-onboard skill is ahead of the embedded CLI skill on the `set-info` workflow step; and several new CLI commands (`memory init`, `memory ask`, `memory adk-sessions`, `memory mcp-guide`, `memory install-memory-skills`) have no coverage in the site docs at all. Catching these now prevents AI agents and new users from hitting friction with stale instructions.

## What Changes

- **MCP README** (`apps/server/domain/mcp/README.md`): update tool count (was "18 → 29", now 30+ real tools across all files), replace legacy tool names (`hybrid_search` → `search-hybrid`, `semantic_search` → `search-semantic`, etc.) with current hyphenated names, correct the `X-API-Key` / `Authorization: Bearer` discrepancy between README and actual handler
- **Developer guide MCP page** (`docs/site/developer-guide/mcp-servers.md`): add note about `memory mcp-guide` CLI command for generating client config snippets
- **memory-onboard skill** (`.agents/skills/memory-onboard/SKILL.md`): update env var guidance to acknowledge `MEMORY_PROJECT_ID`/`MEMORY_PROJECT_TOKEN` written by `memory init` in addition to `MEMORY_PROJECT`
- **memory-blueprints skill** (`.agents/skills/memory-blueprints/SKILL.md`): add the standard `## Rules` block that all other skills have
- **Developer guide**: add a new CLI reference page covering `memory init`, `memory ask`, `memory adk-sessions`, `memory mcp-guide`, `memory install-memory-skills` — these exist in the CLI but have zero site documentation

## Capabilities

### New Capabilities
- `cli-new-commands-docs`: Document the undocumented CLI commands (`init`, `ask`, `adk-sessions`, `mcp-guide`, `install-memory-skills`) in the developer guide

### Modified Capabilities
- `mcp-tool-naming-convention`: Update MCP README to reflect current tool names, counts, and auth method

## Impact

- `apps/server/domain/mcp/README.md` — tool names, count, and auth examples
- `docs/site/developer-guide/mcp-servers.md` — minor addition (mcp-guide reference)
- `docs/site/developer-guide/` — new page for undocumented CLI commands
- `.agents/skills/memory-onboard/SKILL.md` — env var section update
- `.agents/skills/memory-blueprints/SKILL.md` — add Rules block
- No API, database, or backend code changes
