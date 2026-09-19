# Tasks — A2A v1.0 Interop

Milestones M0–M2 plus the ACP disposition are the scope of this change. M3/M4 items are explicitly deferred and tracked here for completeness.

## 1. DTOs, mapping, and facade skeleton

- [ ] 1.1 Create `domain/agents/a2a_dto.go` with A2A v1.0 wire types (camelCase): `AgentCard`, `AgentInterface`, `AgentProvider`, `AgentCapabilities`, `AgentSkill`, `SecurityScheme`/`HTTPAuthSecurityScheme`, `SecurityRequirement`, `Task`, `TaskStatus`, `TaskState`, `Message`, `Role`, `Part`, `Artifact`, `StreamResponse`, `TaskStatusUpdateEvent`, `TaskArtifactUpdateEvent`, `SendMessageRequest`/`Response`, `ListTasksRequest`/`Response`
- [ ] 1.2 Unit-test DTO JSON round-trips assert camelCase keys and enum spelling (`TASK_STATE_CANCELED`, `ROLE_USER`, `ROLE_AGENT`)
- [ ] 1.3 Create `domain/agents/a2a_mapping.go` with `MapRunStatusToTaskState(status AgentRunStatus) TaskState` implementing the design's status table
- [ ] 1.4 Unit-test `MapRunStatusToTaskState` for every internal status, asserting `cancelling`→`WORKING`, `skipped`→`COMPLETED`, `cancelled`→`CANCELED`
- [ ] 1.5 Implement message → `Part`/`Message` translation (text → `text` part; tool trajectory → `data` part) with no `kind` field
- [ ] 1.6 Unit-test message translation: assistant text, tool call, and multi-part messages
- [ ] 1.7 Implement `RunToTask(run, messages, question, artifacts) Task` producing stable `Task.id`, mapped `contextId`, `status`, `artifacts`, `history`
- [ ] 1.8 Unit-test `RunToTask`: stable id across a resume chain, `resume_run_id` never exposed, context inference from taskId
- [ ] 1.9 Implement internal stream event → `StreamResponse` translation (`task`/`message`/`statusUpdate`/`artifactUpdate`)
- [ ] 1.10 Unit-test the stream event map for working/completed/error/awaiting/cancelled and text-delta accumulation

## 2. Discovery (M0)

- [ ] 2.1 Implement `GlobalAgentCard()` returning a static, config-driven card (platform identity, one `HTTP+JSON` interface, `protocolVersion: "1.0"`, `capabilities.streaming=true`, `capabilities.extendedAgentCard=true`, generic/empty skills)
- [ ] 2.2 Unit-test the global card contains no project ids, agent names, or org ids (tenant-leak invariant)
- [ ] 2.3 Implement `GET /.well-known/agent-card.json` (no auth) returning `application/a2a+json`
- [ ] 2.4 Implement `ExtendedAgentCard` handler: resolve project from bearer token, build `skills[]` from `visibility='external'` definitions
- [ ] 2.5 Implement `AgentDefinitionToSkill(def, cfg) AgentSkill` (slug `id`, `ACPConfig` description/input/output modes, non-nil `tags`)
- [ ] 2.6 Unit-test skill derivation: slug, description preference, non-nil tags, external-only filtering
- [ ] 2.7 Declare bearer `securityScheme` + `securityRequirements` on the extended card; assert no OAuth2/OIDC scheme is emitted
- [ ] 2.8 Register discovery routes on Echo at the root (outside `/api/`); wire handler in `module.go`
- [ ] 2.9 Integration-test discovery: 200 unauthenticated global card; 401/403 extended card; external-only skills

## 3. Message flow (M1)

- [ ] 3.1 Implement `POST /message:send` with `returnImmediately` semantics (sync block vs async create) and `agents:write`
- [ ] 3.2 Implement lazy `contextId` creation and use of the reused session table for context continuity
- [ ] 3.3 Implement `GET /tasks/{id}` (with `historyLength`) scoped to the token's project, `agents:read`
- [ ] 3.4 Implement `GET /tasks` with `contextId`/`status`/`pageSize`/`pageToken` filters and `totalSize`/`nextPageToken`
- [ ] 3.5 Implement `POST /tasks/{id}:cancel` mapping to the existing run cancellation, `agents:write`
- [ ] 3.6 Implement `POST /tasks/{id}:subscribe` streaming an existing task's events
- [ ] 3.7 Integration-test message flow: sync completion, async submit, get/list/cancel, cross-project invisibility
- [ ] 3.8 Integration-test scope enforcement: `agents:read` cannot send; `agents:write` cannot be omitted from cancel

## 4. Streaming (M2)

- [ ] 4.1 Implement `POST /message:stream` returning `text/event-stream` with ordered `StreamResponse` events
- [ ] 4.2 Bridge the executor `StreamCallback` to A2A stream events without reordering
- [ ] 4.3 Implement terminal-stream close at terminal A2A state
- [ ] 4.4 Integration-test streaming: wrapper members only, working→completed ordering, artifact accumulation, terminal close
- [ ] 4.5 Document the `INPUT_REQUIRED`-closes-stream deviation in `design.md` and the endpoint reference

## 5. Human-in-the-loop (M2)

- [ ] 5.1 Emit `TASK_STATE_INPUT_REQUIRED` with `status.message` for `ask_user` pauses
- [ ] 5.2 Emit `TASK_STATE_INPUT_REQUIRED` for tool-approval pauses with a distinguishable metadata discriminator
- [ ] 5.3 Implement resume: `message:send` with `message.taskId` claims the question and calls `executor.Resume`, preserving `Task.id`
- [ ] 5.4 Implement resume guard: reject non-`INPUT_REQUIRED` targets (no silent new task) and unknown task ids
- [ ] 5.5 Unit-test concurrent-resume serialization (at most one accepted)
- [ ] 5.6 Unit-test `contextId`/`taskId` consistency rejection and task-only context inference
- [ ] 5.7 Integration-test the full HITL loop: pause → follow-up message → completion with unchanged task id

## 6. Versioning and errors

- [ ] 6.1 Implement `A2A-Version` negotiation (header + query), default `1.0`, reject unsupported with `VERSION_NOT_SUPPORTED`
- [ ] 6.2 Implement the `google.rpc.Status` error envelope with `reason`/`domain: a2a-protocol.org`
- [ ] 6.3 Implement the HTTP status mapping (-32001→404, -32002/3/4/5/9→400, -32006→500)
- [ ] 6.4 Unit-test version negotiation and the error envelope for not-found, not-cancelable, and unsupported-version
- [ ] 6.5 Implement explicit `PUSH_NOTIFICATION_NOT_SUPPORTED` for unimplemented push-config endpoints

## 7. ACP deprecation (this change)

- [ ] 7.1 Add `Deprecation` + `Sunset` headers to all `/acp/v1/` and `/agent-chat/v1/` responses
- [ ] 7.2 Unit-test that legacy responses carry both headers and preserve their previous status/body
- [ ] 7.3 Note in `acp_routes.go` that the routes are deprecated and slated for removal

## 8. First-party consumers

- [ ] 8.1 Add `pkg/sdk/a2a` client covering extended card, send, stream, get/list/cancel
- [ ] 8.2 Unit-test the A2A SDK client against an `httptest` server (success + error envelopes)
- [ ] 8.3 Add `memory a2a` CLI command group (discover, send, stream, tasks get/list/cancel)
- [ ] 8.4 Mark `memory acp` deprecated in help text while keeping it functional
- [ ] 8.5 Decide and implement `acp-*` MCP tool disposition (repoint to `a2a-*` or retire as duplicates)
- [ ] 8.6 Record the MCP tool disposition decision in `design.md`

## 9. Conformance gates

- [ ] 9.1 Add golden-file tests for AgentCard (global + extended) and Task JSON shapes
- [ ] 9.2 Add a test asserting every emitted `TaskState` is a member of the A2A enum and no internal string leaks
- [ ] 9.3 Wire the official `a2a-tck` suite into CI as a non-blocking smoke job
- [ ] 9.4 Document the 0.3-wire TCK limitation and the golden-file v1.0 coverage in the change docs

## 10. Verification

- [ ] 10.1 Run `go build ./...` (from `apps/server` and `apps/cli`) with no errors
- [ ] 10.2 Run `task lint`
- [ ] 10.3 Run `task test` (unit) and `task test:integration`; no regressions
- [ ] 10.4 Manually exercise discovery + `message:send` (sync and stream) against a locally running server
- [ ] 10.5 Confirm the web-ui is unaffected (no `/agent-chat/v1` references were found pre-change)

## Deferred (out of scope for this change)

- [ ] 11.1 Push notifications / `pushNotificationConfigs` webhooks
- [ ] 11.2 JSON-RPC 2.0 binding (evaluate `a2a-go/v2` for this binding only)
- [ ] 11.3 gRPC binding
- [ ] 11.4 AgentCard `signatures` (JWS, RFC 8785 canonicalization)
- [ ] 11.5 `/tasks/{id}:subscribe` streaming held open across `INPUT_REQUIRED`
- [ ] 11.6 Delete ACP implementation and rename `kb.acp_*` tables (later release)
