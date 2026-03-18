## 1. Verify existing implementation matches spec

- [x] 1.1 Read `skill_tool.go` and confirm `BuildSkillTool` returns `nil, nil` (not an error) when skill count is zero
- [x] 1.2 Verify `buildSkillTool` in `executor.go` checks `"skill"` in tools whitelist before calling `BuildSkillTool`
- [x] 1.3 Confirm `handler.go` `generateEmbedding` is called on both create and update (including PATCH when description changes)
- [x] 1.4 Confirm `store.go` Update sets `description_embedding = NULL` when embedding arg is nil (description unchanged path)

## 2. Add Skills field to AgentDefinition

- [x] 2.1 Add `Skills []string` field to `AgentDefinition` struct in `apps/server/domain/agents/entity.go`
- [x] 2.2 Add `skills` column to agent definitions DB schema (migration) — store as JSONB or text array
- [x] 2.3 Update `repository.go` create/update/get/list queries to include the `skills` field
- [x] 2.4 Update `buildSkillTool` in `executor.go` to check `agentDef.Skills` non-empty (in addition to legacy `"skill"` in tools check)
- [x] 2.5 In `skill_tool.go` / `BuildSkillTool`, add filtering: when skills list is explicit (not `["*"]`), filter `all` slice to only declared names before building catalog; log warning for declared names not found

## 3. CLI support

- [x] 3.1 Add `--skills` flag (comma-separated) to `memory agent-definitions create`
- [x] 3.2 Add `--skills` flag to `memory agent-definitions update`
- [x] 3.3 Include `skills` field in `memory agent-definitions get` output

## 4. Blueprint YAML support

- [x] 4.1 Add `skills` key parsing to the blueprint agent definition YAML reader
- [x] 4.2 Pass parsed skills list to the agent-definitions create/update API call during blueprint apply

## 5. Update v3 blueprint agents

- [x] 5.1 Revert `- skill` from `tools:` in `workspace-memory-blueprint-v3/agents/orchestrator.yaml`; add `skills: ["*"]`
- [x] 5.2 Revert `- skill` from `tools:` in `workspace-memory-blueprint-v3/agents/coding-manager.yaml`; add `skills: ["*"]`

## 6. Update e2e tests

- [x] 6.1 Update `TestADKSession_AgentUsesSkillTool` in `adk_sessions_test.go` to use `--skills` flag instead of `--tools skill`
- [x] 6.2 Update `TestADKSession_AgentWithoutSkillToolRuns` — confirm agent with no `skills:` field has no skill tool (no change needed if test already uses `--tools query_entities` only)

## 7. Sync spec to main specs

- [ ] 7.1 Run `/opsx:archive` to promote `specs/agent-skill-catalog/spec.md` to `openspec/specs/agent-skill-catalog/spec.md`
