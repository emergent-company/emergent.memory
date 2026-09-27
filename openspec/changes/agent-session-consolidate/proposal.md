## Why

The runtime session model accumulated one thread identity per interface plus two impostor fields also named "session". A chat conversation, an A2A `contextId`, and a public-share session each create their own `kb.acp_sessions` row with slightly different semantics, while `agent_runs.session_status` stores *workspace provisioning* per run and `ExecuteRequest.SessionID` is an ADK conversation-history key — both named as if they were the thread. The ACP protocol was retired in `retire-acp-for-a2a`, which explicitly deferred the `acp_*` rename to "a separate naming-cleanup change"; this is that change. Doing it now removes the ambiguity behind "what status can a session have", lets one archive concept span chat/share/A2A, and makes A2A, REST chat, share, and CLI thin adapters over one core instead of four independent writers.

## What Changes

- **Rename the thread identity, dropping the retired "ACP" name.** Tables `kb.acp_sessions` → `kb.sessions`, `kb.acp_run_events` → `kb.run_events`; columns `agent_runs.acp_session_id`, `chat_conversations.acp_session_id`, `agent_share_sessions.acp_session_id` → `session_id`; Go types `ACPSession` → `Session`, `ACPRunEvent` → `RunEvent`, `ACPSessionID` → `SessionID`, `ExecuteRequest.ACPSessionID` → `ExecuteRequest.SessionID`. **BREAKING**: the wire JSON key `acpSessionId` → `sessionId`; every in-repo consumer (gateway, CLI, SDK, iOS) is updated in the same change.
- **Disambiguate the ADK session key.** Rename `ExecuteRequest.SessionID` (the caller-supplied ADK conversation-history key) → `ConversationKey`, so `SessionID` unambiguously means the thread. No behaviour change to cross-run history.
- **Fold workspace provisioning into run execution status.** Delete `agent_runs.session_status`, the `SessionStatus` type, `Repository.UpdateSessionStatus`, and the executor write sites. Provisioning is a phase of the run's `working` status; a provisioning failure is a `failed` run with `error_message`.
- **Centralize session creation in one repository path.** Route the producers through the Repository's session methods — `EnsureConversationSession` (chat), `EnsureSessionForContext` (A2A get-or-create), `CreateSession` (per-share-session thread) — so no interface inserts a Session row itself.
- **Delete dead archive code.** `Repository.ListACPSessions`, `ArchiveACPSession`, `UnarchiveACPSession`, and the unused `ACPSession.IsArchived` field — no callers today. The `sessions.is_archived` column itself is intentionally retained (it is not dropped by this change); removing it is a separate migration. A thread-level archive is a deliberate later change, not this one.
- **Keep genuinely-distinct identities adapter-local and explicitly out of the core.** The public-share end-user session (`kb.agent_share_sessions`, its own archive and cap), the MCP transport session, and the chat conversation's soft-delete remain interface concerns — documented as such, not merged.
- **Non-goals:** moving the gateway's derived run bucket server-side; adding the user-facing archive feature; renaming `pkg/acpslug`; any wire-visible change to A2A `contextId` semantics.

## Capabilities

### New Capabilities
- `agent-session`: the canonical runtime model — a `Session` thread grouping one or more `Run` turns, the single Repository session-resolution path every interface maps through, the status model (run execution status only), and the adapter boundary that keeps share/MCP/chat-specific identity out of the core.

### Modified Capabilities
- `a2a-acp-migration`: the deferred naming cleanup has landed — persistence tables are renamed while A2A `contextId` continuity and task-history reconstruction are preserved.
- `a2a-message-flow`: `Task.contextId` maps to the renamed session record (`kb.sessions`) rather than `kb.acp_sessions`; the wire `contextId` contract is unchanged.
- `backup-export-coverage`: the exporter's excluded-table list names the renamed session table.

## Impact

- **Migrations**: one Goose migration renaming 2 tables (+ indexes) and 3 FK columns, updating the `session_todos` FK target; a second migration dropping `agent_runs.session_status`. Both reversible.
- **Server**: `domain/agents` (`entity.go`, `repository.go`, `executor.go`, `dto.go`, `a2a_message.go`, `a2a_stream.go`, `a2a_mapping.go`, `share_service.go`, `share_entity.go`, `share_store.go`), `domain/chat` (`handler.go`, `entity.go`), `domain/sessiontodos`; new `SessionService`; delete dead archive methods; regenerate swagger.
- **Gateway**: conversation/run JSON now carries `sessionId` (`extras.go`, `memory_share.go`, `chat_dock.go`, `ui.go`, `memory_conversations.go`, `session_dump.go` and their tests).
- **Clients**: CLI, iOS, and SDK consumers read `sessionId`; A2A clients are unaffected (`contextId` unchanged).
- **Tests**: agents + chat unit tests updated for the rename and status fold; migration up/down test; A2A message/task e2e round-trip to prove no interop regression.
