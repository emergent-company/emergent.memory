# agent-include-project-info

## 1. Spec artifacts

- [x] 1.1 Write `openspec/changes/agent-include-project-info/proposal.md`
- [x] 1.2 Write `openspec/changes/agent-include-project-info/tasks.md`
- [x] 1.3 Write `openspec/changes/agent-include-project-info/.openspec.yaml`
- [x] 1.4 Write delta specs for `agent-context-injection` (new), `agent-definitions` (modified), `web-agent-settings` (modified)
- [x] 1.5 Validate with `openspec validate agent-include-project-info --strict`

## 2. Per-agent opt-in flag (`agents` domain)

- [x] 2.1 Add canonical config key `includeProjectInfo` and `AgentDefinition.IncludeProjectInfo()` reader (default `false`, nil-safe, non-bool-safe) in `domain/agents/entity.go`
- [x] 2.2 Expose `includeProjectInfo` on `AgentDefinitionDTO` (read), `CreateAgentDefinitionDTO`, and `UpdateAgentDefinitionDTO` (write) in `domain/agents/dto.go`
- [x] 2.3 Map the flag into the stored `config` blob on create/update in `domain/agents/handler.go`; delete the key when `false` so stored config stays minimal
- [x] 2.4 Populate `includeProjectInfo` in `AgentDefinition.ToDTO()`

## 3. Cross-surface injection (`agents` executor)

- [x] 3.1 Add `ExecuteRequest.ProjectInfo` (pre-fetched KB context)
- [x] 3.2 Fetch `project_info` once per run in `runPipeline` when the definition opts in (`AgentExecutor.resolveProjectInfo`, nil-repo-safe)
- [x] 3.3 Inject the `## Knowledge base context` block in `resolveInstruction`, gated on the flag, at one fixed position (after skills, before the caller appendix)
- [x] 3.4 Keep the composed system prompt byte-stable for a given agent/flag across surfaces

## 4. Remove the one-surface unconditional injection (`agentcompat`)

- [x] 4.1 Drop the `project_info` fetch + parameter from `buildSystemAppendix` and both fetch sites in `domain/agentcompat/service.go`
- [x] 4.2 Preserve `SystemPromptAppendix` behaviour for other callers (never written to the DB, still appended by the executor)

## 5. Web UI (`web-agent-settings`)

- [x] 5.1 Add an `includeProjectInfo` checkbox to the agent General settings form (`agent.templ`)
- [x] 5.2 Map the checkbox onto `Config["includeProjectInfo"]` in `applyAgentGeneralSection` (`agent.go`)
- [x] 5.3 Unit-test the reader/mapping

## 6. Tests

- [x] 6.1 `resolveInstruction`: flag `false` (default) injects nothing even when project info is available
- [x] 6.2 `resolveInstruction`: flag `true` injects the `## Knowledge base context` block exactly once, after skills, before the appendix
- [x] 6.3 Cross-surface parity: the composed instruction is byte-identical for the same agent/flag across trigger / A2A / chat-completions request shapes, and `project_info` is absent from all when off
- [x] 6.4 `IncludeProjectInfo()` + DTO round-trip (read/create/update) for both flag states
- [x] 6.5 `agentcompat.buildSystemAppendix` no longer carries project context

## 7. Verification

- [x] 7.1 `go build ./...` + `go vet ./...`
- [x] 7.2 `go test ./domain/agents/... ./domain/agentcompat/... -count=1`
- [x] 7.3 `golangci-lint run --new-from-rev=origin/main` on touched packages → 0 new issues
- [x] 7.4 `openspec validate agent-include-project-info --strict`
- [x] 7.5 No migration added (config-blob storage) — migration guard not applicable
