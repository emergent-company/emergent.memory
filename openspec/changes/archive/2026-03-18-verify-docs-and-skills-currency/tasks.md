## 1. Fix MCP README (apps/server/domain/mcp/README.md)

- [x] 1.1 Update the Overview tool count ("18 tools" / "29 tools") to reflect current approximate count (~50) with a note that the count is approximate and to consult source files
- [x] 1.2 Replace legacy snake_case tool names in the New Tools section with current hyphenated names: `hybrid_search` → `search-hybrid`, `semantic_search` → `search-semantic`, `find_similar` → `search-similar`, `traverse_graph` → `graph-traverse`, `list_relationships` → `relationship-list`, `update_relationship` → `relationship-update`, `delete_relationship` → `relationship-delete`, `restore_entity` → `entity-restore`, `batch_create_entities` → `entity-create` (batched), `batch_create_relationships` → `relationship-create` (batched), `list_tags` → `tag-list`
- [x] 1.3 Update Quick Start HTTP examples to use `Authorization: Bearer <token>` instead of `X-API-Key: your-key`
- [x] 1.4 Update the Authentication section to document both accepted methods (`Authorization: Bearer` recommended, `X-API-Key` also accepted) and drop the `X-Project-ID` requirement note (project is now derived from the token/session)
- [x] 1.5 Update the Changelog section to note the tool name standardisation (kebab-case) and current tool count

## 2. Update Developer Guide MCP Servers Page (docs/site/developer-guide/mcp-servers.md)

- [x] 2.1 Add a note or callout in the "Connecting Claude Desktop" section pointing to `memory mcp-guide` as a CLI shortcut to generate the config snippet automatically

## 3. Create Developer Guide CLI Reference Page (docs/site/developer-guide/cli-reference.md)

- [x] 3.1 Create `docs/site/developer-guide/cli-reference.md` with sections for: `memory init`, `memory ask`, `memory adk-sessions`, `memory mcp-guide`, `memory install-memory-skills`
- [x] 3.2 For `memory init`: describe the 3-step wizard (project, provider, skills), the `.env.local` output (`MEMORY_PROJECT_ID`, `MEMORY_PROJECT_NAME`, `MEMORY_PROJECT_TOKEN`), and both flags (`--skip-provider`, `--skip-skills`)
- [x] 3.3 For `memory ask`: describe the 3 context modes (unauthenticated, auth-no-project, auth-with-project), add at least 3 example commands
- [x] 3.4 For `memory adk-sessions` (alias `sessions`): document `list` and `get` subcommands with examples
- [x] 3.5 For `memory mcp-guide`: one-line description plus a usage example showing the output format (JSON snippets for Claude Desktop / Cursor)
- [x] 3.6 For `memory install-memory-skills`: describe purpose, `--dir` and `--force` flags, and a typical usage example
- [x] 3.7 Add `cli-reference.md` link to `docs/site/developer-guide/index.md`

## 4. Update memory-onboard Skill (.agents/skills/memory-onboard/SKILL.md)

- [x] 4.1 In the `## Rules` section, update the "Always supply a project" note to mention both `MEMORY_PROJECT` and `MEMORY_PROJECT_ID` as accepted env vars
- [x] 4.2 In Step 2d (write project ID), add a note that `memory init` and `memory projects set` automatically write `MEMORY_PROJECT_ID`, `MEMORY_PROJECT_NAME`, and `MEMORY_PROJECT_TOKEN` to `.env.local` — agents should check for these vars when determining if a project is already configured

## 5. Update memory-blueprints Skill (.agents/skills/memory-blueprints/SKILL.md)

- [x] 5.1 Add the standard `## Rules` block (no `memory browse`, always `NO_PROMPT=1`, always supply `--project`) after the YAML frontmatter and the intro paragraph, matching the format used in all other memory skills
