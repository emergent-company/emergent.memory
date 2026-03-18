## Why

Agents traversing the knowledge graph have no way to access project skills — the `skill_tool.go` implementation exists but is undocumented, has no spec, and is not opted into by any blueprint agent definition. Without a spec there is no authoritative contract to test against, and without blueprint opt-in the feature is effectively invisible to users.

## What Changes

- Add canonical spec for the agent skill catalog capability (how the `skill` tool is built, how catalog is injected, how semantic retrieval falls back)
- Update the `workspace-memory-blueprint-v3` agent definitions to include `"skill"` in their `tools:` whitelist where appropriate
- Add e2e test coverage: an agent session where the agent calls the `skill` tool and gets skill content back
- Verify `DescriptionEmbedding` is populated on skill create/update (needed for semantic retrieval path)

## Capabilities

### New Capabilities

- `agent-skill-catalog`: Spec covering how the server builds and injects a skill catalog into an agent's tool pipeline, how agents call the `skill` tool to load full content, and the semantic retrieval fallback when skill count exceeds threshold

### Modified Capabilities

_(none — no existing specs change)_

## Impact

- `apps/server/domain/skills/skill_tool.go` — may need minor hardening against spec
- `apps/server/domain/agents/executor.go` — verify `buildSkillTool` wiring matches spec
- `workspace-memory-blueprint-v3/agents/*.yaml` — add `skill` to relevant agents' tools lists
- `emergent.memory.e2e/tests/cli/adk_sessions_test.go` — new skill session test
