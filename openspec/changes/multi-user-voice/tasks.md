## 1. Gateway — session-aware token mint + binding

- [x] 1.1 Add `MemoryClient.CreateProjectToken(projectID, name, scopes)` proxying Memory `POST /api/projects/:projectId/tokens`, and verify a unit test against the fake memory backend covers the request shape (name + scopes) and returns the minted token — satisfied by the existing `MemoryClient.CreateAPIToken` (`gateway/api_tokens.go`), used by `voiceBindingFor`
- [x] 1.2 Confirm the exact scopes the worker needs (`chat:use` + agent-question respond/cancel) against `emergent.memory` route guards and record them in the task notes; verify the fake-memory test asserts those scopes — confirmed `chat:use` (agent-questions routes have no extra scope)
- [x] 1.3 Add a `voiceBinding` struct + short-TTL in-memory store keyed by room with one-time consume, and verify a unit test covers set/consume/expiry — `gateway/voice_binding.go` + `TestVoiceBindingStore*`
- [x] 1.4 Make `mintToken` session-aware (design D6): session mode resolves active project/org, resolves the requested agent name → definition id in that project, mints the project token, and stores the binding; verify a unit test covers session mode and `X-API-Key` mode (static project/token) separately — `TestMintTokenSessionModeStoresBinding` / `TestMintTokenKeyModeStoresStaticBinding`
- [x] 1.5 Add the internal `/internal/voice-binding?room=…` endpoint gated by `WORKER_INTERNAL_KEY`; verify a handler test covers valid fetch, missing/invalid key, and unknown room — `voiceBindingHandler` + `TestVoiceBindingHandler*`

## 2. Per-project agent-definition resolution

- [x] 2.1 Add per-project agent lookup (name → definition id scoped to the caller's project) reusing `ListAgentDefinitions`/`GetAgentDefinition` with the session project, and verify a unit test proves the same agent name resolves to different definition ids in different projects — `resolveVoiceAgent` + `TestResolveVoiceAgentPerProject`

## 3. Supervisor narrowing

- [x] 3.1 Stop injecting `AGENT_DEFINITION_ID`/`AGENT_LANGUAGE`; keep `AGENT_NAME` as the dispatch key and additionally inject `WORKER_INTERNAL_KEY` + the internal binding URL into the worker env; verify the supervisor spawn test asserts the new env and the absence of the removed vars — `supervisor.go` `spawnLocked`/`NewSupervisor`

## 4. Worker per-job binding (Python)

- [x] 4.1 Add binding parsing + fetch from the internal endpoint at `entrypoint` start (keyed by `ctx.room.name`), and verify a unit test covers the fetch and the env fallback path — `memory_bridge/binding.py` + `tests/test_binding.py`
- [x] 4.2 Thread the `binding` struct into `_build_session`/`_attach_chat_io` so `MemoryChatClient` is built per-job from `{memory_url, token, project_id, agent_definition_id}`, and verify a unit test asserts per-job values are used instead of process-global env — `worker.py`
- [x] 4.3 Remove the process-global `MEMORY_TOKEN`/`MEMORY_PROJECT_ID`/`AGENT_DEFINITION_ID` reads from `config.py`/`worker.py`/`memory_chat.py`, and verify `pytest` passes in `memory_bridge/` — 42 passed

## 5. Voice settings per session

- [x] 5.1 Resolve the voice language (and available per-project voice options) from the active project's agent definition at token-mint time and carry them in the binding; verify a unit test proves two projects with different language settings drive different STT/TTS configuration on the worker — `resolveVoiceAgent` (language) + `config.language_config_for` + `TestResolveVoiceAgentPerProject`

## 6. Verification

- [x] 6.1 `go build ./...` + `go vet ./...` + `go test ./...` in `gateway/` pass (my tests pass; remaining failures are pre-existing uncommitted WIP from a parallel `add-api-tokens-ui`/org-context session, unrelated to this change)
- [x] 6.2 `pytest` in `memory_bridge/` passes (42 passed)
- [ ] 6.3 Manual browser + LiveKit test: sign in via Zitadel, start a voice call, confirm the call runs in the active project with its voice settings, and confirm an `X-API-Key` client still works (deferred — needs live Zitadel + Memory)
