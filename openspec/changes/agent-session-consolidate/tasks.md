## 1. Migration A — rename tables and columns

- [ ] 1.1 Add a Goose migration renaming `kb.acp_sessions` → `kb.sessions` and `kb.acp_run_events` → `kb.run_events`, incl. their indexes, with a working `down`; verify `task migrate:up` then `task migrate:down` round-trips cleanly on a dev DB
- [ ] 1.2 In the same migration rename the child columns `agent_runs.acp_session_id`, `chat_conversations.acp_session_id`, `agent_share_sessions.acp_session_id` → `session_id` and repoint the `session_todos.session_id` FK at `kb.sessions`; verify `\d kb.agent_runs` and `\d kb.session_todos` show `session_id` referencing `kb.sessions`
- [ ] 1.3 Add an integration test that applies the migration up and down against a real schema and asserts every former `kb.acp_sessions` reference resolves to `kb.sessions`; verify the test passes in `task test:integration`

## 2. Domain rename (Go types and fields)

- [ ] 2.1 Rename `ACPSession` → `Session`, `ACPRunEvent` → `RunEvent`, `ACPSessionID` → `SessionID` and update `bun` tags to `session_id` in `domain/agents/entity.go`; verify `go build ./...` and the agents unit suite pass
- [ ] 2.2 Rename `ExecuteRequest.ACPSessionID` → `SessionID` (thread) and `ExecuteRequest.SessionID` → `ConversationKey` (ADK key), updating all call sites in `executor.go`, `chat/handler.go`, `a2a_message.go`, `a2a_stream.go`, `share_service.go`; verify a unit test proves successive runs sharing a `ConversationKey` share ADK history while a thread `SessionID` never feeds it
- [ ] 2.3 Rename `acpSessionId` → `sessionId` JSON tags on the affected DTOs and update the DTO tests; verify the agents unit suite passes

## 3. Single SessionService create-or-get

- [ ] 3.1 Add `SessionService.Ensure(ctx, key)` returning the existing or newly created `Session` for a logical thread key, with unit tests covering create, reuse, and failure; verify the tests pass
- [ ] 3.2 Route the chat path (`EnsureConversationACPSession`, called from `chat/handler.go`) through `SessionService.Ensure` and delete the replaced helper; verify a unit test shows two turns on one conversation reuse a single session row
- [ ] 3.3 Route the A2A producers (`a2a_message.go`, `a2a_stream.go`) through `SessionService.Ensure`; verify a unit test shows a reused `contextId` resolves to the same session and a missing one is created lazily
- [ ] 3.4 Route the share producer (`share_service.go`) through `SessionService.Ensure`; verify a unit test shows one share session maps to one session row
- [ ] 3.5 Delete the unused `ListACPSessions`, `ArchiveACPSession`, `UnarchiveACPSession`, and `Session.IsArchived`; verify `go build ./...` and `task lint` pass with no references remaining

## 4. Wire compatibility (gateway and clients)

- [ ] 4.1 Update the gateway conversation/run DTOs and call sites (`extras.go`, `memory_share.go`, `chat_dock.go`, `ui.go`, `memory_conversations.go`, `session_dump.go`) to read `sessionId` and to emit both `sessionId` and the legacy `acpSessionId`; verify the gateway unit suite and `go build ./...` pass
- [ ] 4.2 Add gateway tests asserting dual-emit and that a legacy `acpSessionId` response is still parsed correctly; verify they pass under `go test ./...`
- [ ] 4.3 Update the CLI/SDK/iOS consumers of `acpSessionId` to read `sessionId`; verify each affected module builds and its unit tests pass

## 5. Status consolidation — drop session_status

- [ ] 5.1 Add a separate Goose migration dropping `agent_runs.session_status`, with a `down` that re-adds it with the `00026` default; verify `task migrate:up` then `task migrate:down` round-trips
- [ ] 5.2 Remove the `SessionStatus` type, `Repository.UpdateSessionStatus`, the six executor write sites, and the DTO `sessionStatus` field; verify `go build ./...`, the agents unit suite, and a repo-wide grep confirm no remaining reader
- [ ] 5.3 Regenerate swagger and verify the agent-run schema no longer contains `sessionStatus` and that `sessionId`/`acpSessionId` appear as expected

## 6. Verification and validation

- [ ] 6.1 Run the server suites (`task test`, `task test:integration`) and verify all pass
- [ ] 6.2 Run the A2A message/task e2e round-trip and verify `contextId` continuity and task-history reconstruction are unchanged
- [ ] 6.3 Run gateway `task lint` and `go test ./...`; manually smoke chat and the public share surface and verify a session-backed conversation renders and resumes
- [ ] 6.4 Run `openspec validate agent-session-consolidate --strict` and verify it passes
