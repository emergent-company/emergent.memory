## 1. Migration A — rename tables and columns

- [x] 1.1 Add a Goose migration renaming `kb.acp_sessions` → `kb.sessions` and `kb.acp_run_events` → `kb.run_events`, incl. their indexes, with a working `down`; verify `task migrate:up` then `task migrate:down` round-trips cleanly on a dev DB
- [x] 1.2 In the same migration rename the child columns `agent_runs.acp_session_id`, `chat_conversations.acp_session_id`, `agent_share_sessions.acp_session_id` → `session_id` and repoint the `session_todos.session_id` FK at `kb.sessions`; verify `\d kb.agent_runs` and `\d kb.session_todos` show `session_id` referencing `kb.sessions`
- [ ] 1.3 Add an integration test that applies the migration up and down against a real schema and asserts every former `kb.acp_sessions` reference resolves to `kb.sessions`; verify the test passes in `task test:integration`

## 2. Domain rename (Go types and fields)

- [x] 2.1 Rename `ACPSession` → `Session`, `ACPRunEvent` → `RunEvent`, `ACPSessionID` → `SessionID` and update `bun` tags to `session_id` in `domain/agents/entity.go`; verify `go build ./...` and the agents unit suite pass
- [x] 2.2 Rename `ExecuteRequest.ACPSessionID` → `SessionID` (thread) and `ExecuteRequest.SessionID` → `ConversationKey` (ADK key), updating all call sites in `executor.go`, `chat/handler.go`, `a2a_message.go`, `a2a_stream.go`, `share_service.go`; verify a unit test proves successive runs sharing a `ConversationKey` share ADK history while a thread `SessionID` never feeds it
- [x] 2.3 Rename `acpSessionId` → `sessionId` JSON tags on the affected DTOs and update the DTO tests; verify the agents unit suite passes

## 3. Centralized session resolution

- [x] 3.1 Add `Repository.EnsureSessionForContext(ctx, projectID, contextID, agentName)` returning the existing session for a supplied `contextId` and creating one when absent, with unit tests covering reuse, lazy creation, and unknown-id; verify the tests pass
- [x] 3.2 Confirm the chat path resolves its thread through `Repository.EnsureConversationSession` and never constructs a Session row inline; verify a unit test shows two turns on one conversation reuse a single session row
- [x] 3.3 Route the A2A producers (`a2a_message.go`, `a2a_stream.go`) through `EnsureSessionForContext`; verify a unit test shows a reused `contextId` resolves to the same session and a missing one is created lazily
- [x] 3.4 Confirm the share producer obtains its per-share-session thread through `Repository.CreateSession` and never inserts a Session row itself; verify the share unit tests pass
- [x] 3.5 Delete the unused `ListSessions`, `ArchiveSession`, `UnarchiveSession`, and `Session.IsArchived`; verify `go build ./...` and `task lint` pass with no references remaining

## 4. Wire rename (session identity)

- [x] 4.1 Update the gateway conversation/run DTOs and call sites (`extras.go`, `memory_share.go`, `chat_dock.go`, `ui.go`, `memory_conversations.go`, `session_dump.go`) to read and emit `sessionId` (`session_id` for the history key); verify the gateway unit suite and `go build ./...` pass
- [x] 4.2 Update the CLI/SDK/iOS consumers of `acpSessionId` to `sessionId` (none remain outside the gateway per repo-wide grep); verify each affected module builds and its unit tests pass

## 5. Status consolidation — drop session_status

- [x] 5.1 Add a separate Goose migration dropping `agent_runs.session_status`, with a `down` that re-adds it with the `00026` default; verify `task migrate:up` then `task migrate:down` round-trips
- [x] 5.2 Remove the `SessionStatus` type, `Repository.UpdateSessionStatus`, the six executor write sites, and the DTO `sessionStatus` field; verify `go build ./...`, the agents unit suite, and a repo-wide grep confirm no remaining reader
- [x] 5.3 Regenerate swagger and verify the agent-run schema no longer contains `sessionStatus` and that `sessionId` appears as expected

## 6. Verification and validation

- [x] 6.1 Run the server suites (`task test`, `task test:integration`) and verify all pass
- [ ] 6.2 Run the A2A message/task e2e round-trip and verify `contextId` continuity and task-history reconstruction are unchanged
- [ ] 6.3 Run gateway `task lint` and `go test ./...`; manually smoke chat and the public share surface and verify a session-backed conversation renders and resumes
- [x] 6.4 Run `openspec validate agent-session-consolidate --strict` and verify it passes
