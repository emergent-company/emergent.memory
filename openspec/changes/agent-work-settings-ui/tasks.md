## 1. Server — expose sourceBlueprintId on the definition DTO

- [x] 1.1 `apps/server/domain/agents/dto.go`: add `SourceBlueprintID *string json:"sourceBlueprintId,omitempty"` to `AgentDefinitionDTO` and map it in `ToDTO`.
- [x] 1.2 `apps/server/domain/agents/dto_test.go`: DTO test asserting the wire name `sourceBlueprintId` round-trips and is omitted when nil.

## 2. Server — full workConfig round-trip

- [x] 2.1 New `apps/server/domain/agents/work_config_roundtrip_db_test.go`: drive `h.UpdateDefinition` with a full `workConfig` body (status, requiresReview, failureLimit, revisionLimit, retryPolicy, workContract, triggerStatuses) and assert every field survives the write/read cycle and the response DTO.

## 3. Gateway — struct shape + write path

- [x] 3.1 `apps/web-ui/gateway/memory.go`: expand `AgentWorkConfig` to the full shape; add `AgentRetryPolicy` + `AgentWorkContract` mirrors; add `WorkConfig *AgentWorkConfig` and `SourceBlueprintID *string` to the local `AgentDefinition`.
- [x] 3.2 `apps/web-ui/gateway/memory.go`: document in `sanitizeAgentWrite` that WorkConfig is preserved (only toolGroups is dropped).

## 4. Gateway — section + applier

- [x] 4.1 `apps/web-ui/gateway/agent.go`: add `sectionWork`, register it in `agentSettingsSections`, and add `uiAgentUpdateWork` (PRG → `applyAgentSettingsSection`).
- [x] 4.2 `apps/web-ui/gateway/agent.go`: add `applyAgentWorkSection` mapping the seven field groups (status phases, `workRequiresReview`, `failureLimit`/`revisionLimit` (min 1), `maxAttempts` (min 1), `initialIntervalMs`/`maxIntervalMs` (min 0), `backoffCoefficient` (min 1), `workRequireArtifacts`, `requiredDeliverableType`, `triggerStatus`).
- [x] 4.3 `apps/web-ui/gateway/agent.go`: add form-value helpers (`agentWorkConfig`, `workIntStr`, `workFloatStr`, `workListValue`, `lastFormBool`, `workIntField`, `workFloatField`).
- [x] 4.4 `apps/web-ui/gateway/main.go`: register `POST /agents/:id/settings/work`.

## 5. Gateway — UI

- [x] 5.1 `apps/web-ui/gateway/agent.templ`: add the Work nav item and `case "work"` panel entry.
- [x] 5.2 `apps/web-ui/gateway/agent.templ`: add `agentWorkSettingsForm` rendering all seven field groups with numeric min/step bounds and default placeholders, plus a read-only work-pickup precondition note.
- [x] 5.3 `apps/web-ui/gateway/agent.templ`: add `agentWorkBlueprintWarning` ("Save anyway") gated on `SourceBlueprintID`, wired via `data-requires-confirm`.
- [x] 5.4 Run `templ generate` — never hand-edit `agent_templ.go`.

## 6. Tests

- [x] 6.1 New `apps/web-ui/gateway/agent_work_settings_test.go`: applier mapping + validation, sanitize preservation, form rendering (all field groups + bounds + blueprint dialog), and the PRG route test (no WorkConfig drop).
- [x] 6.2 Extend `apps/web-ui/gateway/agent_ui_test.go`: add the Work nav href and section to the render/sub-nav tests.
- [x] 6.3 New `apps/web-ui/tests/e2e/specs/agents/agent-work-settings-ui.spec.ts` modeled on `agent-tool-groups-ui.spec.ts`.

## 7. Spec + verify

- [x] 7.1 Server: `cd apps/server && go build ./... && go test ./domain/agents/ -short`.
- [x] 7.2 Gateway: `templ generate` + `go generate` + `go build ./... && go test ./... -short`.
- [x] 7.3 `openspec validate agent-work-settings-ui`.
- [x] 7.4 Open one PR containing the OpenSpec change, server, gateway, and e2e edits; push the branch.
