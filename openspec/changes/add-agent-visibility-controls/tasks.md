## 1. Server — tighten A2A skill resolvability

- [x] 1.1 Extract the visibility decision into a pure helper `a2aPickResolvableDefinition(external, fallback *AgentDefinition) *AgentDefinition`: external wins; fallback accepted unless `internal`; `internal`-only returns nil. Verify with `go build ./...`.
- [x] 1.2 Rewrite `resolveA2AAgentBySkillID` to resolve `external` first, fall back to `FindAgentDefinitionBySlug`, and route through the helper so an `internal` match resolves as not-found. Verify with `go build ./...`.
- [x] 1.3 Update the routing-contract doc comment above `resolveA2AAgent`/`resolveA2AAgentBySkillID` to state external preferred, project fallback, internal never resolvable. Verify with `go build ./...`.
- [x] 1.4 Unit-test (TDD) `a2aPickResolvableDefinition`: external preferred over project; project fallback resolves; internal not resolvable; no match. Verify with `go test ./domain/agents/...`.
- [x] 1.5 Fix stale ACP wording in the `AgentVisibility` doc comments (`entity.go`): external → "advertised in the project's A2A agent card"; project → "not advertised"; internal → "never via A2A". Comment-only. Verify with `go build ./...`.

## 2. Web UI — Visibility control on the General settings form

- [x] 2.1 Add visibility option/warning/note helpers (`agentVisibilityOptions`, `agentVisibilityNormalize`, `agentVisibilityValue`, `agentVisibilityDescription`, warning/note consts) in `gateway/ui.go`. Verify with `go build ./...`.
- [x] 2.2 Add a `ui.Section("Visibility")` to `agentGeneralSettingsForm` in `gateway/agent.templ` with a native daisyUI `<select>` (options labelled `Name — description`), a server-rendered helper line, and conditional `external` warning / `internal` note alerts. Verify with `templ generate` + `go build ./...`.
- [x] 2.3 Validate and persist visibility in `applyAgentGeneralSection` (`gateway/agent.go`): empty→`project`, unknown rejected. Verify with `go build ./...`.
- [x] 2.4 Unit-test (TDD) in `agent_visibility_test.go`: dropdown renders three options + labels; stored value preselects; warning/note conditional; empty/unknown normalise to project; valid persists, empty→project, invalid rejected without backend call. Verify with `go test ./...`.

## 3. Specs

- [x] 3.1 Write `specs/web-agent-settings/spec.md` (`## ADDED Requirements`) covering the three options + descriptions, default `project`, external warning, internal note, server-side validation, and persistence via the general-settings path.
- [x] 3.2 Write `specs/a2a-message-flow/spec.md` (`## MODIFIED Requirements`) restating the skill-routing contract: external preferred, project resolvable by slug (not advertised), internal never resolvable via A2A, with scenarios.
- [x] 3.3 Write `proposal.md`, `design.md`, `tasks.md`, and `.openspec.yaml`. Verify with `openspec validate add-agent-visibility-controls --strict`.

## 4. Verify

- [x] 4.1 `go build ./... && go vet ./domain/agents/... && go test ./domain/agents/...` in `apps/server/`. All pass.
- [x] 4.2 `openspec validate add-agent-visibility-controls --strict` from the repo root. Valid.
