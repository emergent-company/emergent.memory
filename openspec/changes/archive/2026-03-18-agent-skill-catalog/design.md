## Context

`skill_tool.go` already implements the core logic: on agent session start, the server fetches all agent-visible skills (merged global + org + project), builds an `<available_skills>` XML catalog in the tool description, and returns an ADK `functiontool` that does in-memory exact-name lookup to return full `<skill_content>`. Above 50 skills it falls back to pgvector semantic retrieval using the trigger message as query. Currently the tool is added only when `"skill"` appears as a magic string in `AgentDefinition.Tools` — there is no way to declare which specific skills an agent should use, and the mechanism is not discoverable. No spec exists, and no e2e tests cover the path.

## Goals / Non-Goals

**Goals:**
- Replace the `"skill"` magic string in `tools:` with a dedicated `skills:` field on `AgentDefinition`
- Auto-inject the skill tool when `skills:` is non-empty — no explicit `tools: ["skill"]` needed
- Filter the skill catalog to the declared skill names (or all agent-visible skills when `["*"]`)
- Update agent-definitions CLI and YAML blueprint parsing to support the new field
- Add e2e test: agent session where the agent calls the skill tool and gets content

**Non-Goals:**
- Changing the core `BuildSkillTool` / `selectRelevantSkills` algorithm
- Exposing the skill catalog via a standalone REST/MCP endpoint
- Per-skill access control beyond project/org scoping

## Decisions

### Decision: `skills:` field replaces `"skill"` in `tools:` whitelist

**Options considered:**
- A. Keep `"skill"` as a magic string in `tools:` (current approach)
- B. Add a separate `skills:` field; skill tool auto-injected when non-empty; `"skill"` removed from tools whitelist

**Choice: B.** The `tools:` whitelist is for MCP execution tools (things agents call to act on the world). Skills are a different concept — on-demand instructions. Mixing them in the same list is confusing. A dedicated field also enables per-agent skill filtering, which is impossible with the current approach.

**Migration:** `"skill"` in `tools:` will continue to work as a fallback during the transition period so existing agent definitions don't break. New definitions should use `skills:`.

### Decision: Catalog filtered to declared skill names when list is explicit

When `skills: ["code-review", "research-workflow"]`, only those two skills appear in the `<available_skills>` catalog. When `skills: ["*"]`, all agent-visible skills are included (current behaviour). Filtering is done in-memory after `FindForAgent` — no new DB query needed.

### Decision: Semantic retrieval threshold applies to post-filter count

The 50-skill threshold and top-K retrieval apply to however many skills are in scope after filtering. An agent with `skills: ["*"]` and 200 project skills gets semantic retrieval; one with `skills: ["foo", "bar"]` always gets both in the catalog.

### Decision: `DescriptionEmbedding` populated by the embeddings service at create/update time

Already implemented in `handler.go` — verified, no changes needed.

## Risks / Trade-offs

- **Breaking existing `"skill"` in tools definitions** → mitigated by fallback: if `skills:` is absent but `"skill"` is in `tools:`, existing behaviour is preserved.
- **Skill name typos in `skills:` list** → a skill name that doesn't match any DB skill is silently absent from the catalog. Mitigation: log a warning at session start for each declared skill name not found.
- **Blueprint YAML parsing** → the `skills:` key must be added to the blueprint agent definition parser.

## Open Questions

_(none — design is settled)_
