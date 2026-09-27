## Context

See `proposal.md` — Why. The runtime model already has the right two-level shape: `kb.acp_sessions` is a thread that groups `kb.agent_runs` turns. The problems are accumulated naming and a misplaced status field, not the shape.

Verified constraints that shape the approach:

- The thread table is referenced by four child columns: `agent_runs.acp_session_id`, `chat_conversations.acp_session_id`, `agent_share_sessions.acp_session_id`, and `session_todos.session_id` (`migrations/00082`, `00115`, `00160`, `00101`/`00102`).
- `kb.acp_run_events` references `agent_runs`, not the thread — it is renamed for vocabulary consistency only.
- Session creation is inline in four places: `agents/repository.go` (`EnsureConversationACPSession`, called from `chat/handler.go`), `agents/a2a_message.go`, `agents/a2a_stream.go`, and `agents/share_service.go`.
- Two fields named "session" collide inside one struct: `ExecuteRequest.ACPSessionID` (thread, `executor.go:263`) and `ExecuteRequest.SessionID` (ADK conversation-history key, `executor.go:278`).
- `agent_runs.session_status` (`SessionStatus`: provisioning/active/completed/error) is written by the executor at six sites and read by no caller; A2A maps `run.Status`, and the gateway derives its bucket from run status.
- `Repository.ListACPSessions` / `ArchiveACPSession` / `UnarchiveACPSession` (`repository.go:2946-2983`) and `ACPSession.IsArchived` have no callers.
- `retire-acp-for-a2a` promised this rename as "a separate naming-cleanup change"; `openspec/specs/a2a-acp-migration`, `a2a-message-flow`, and `backup-export-coverage` name the old table.

## Goals / Non-Goals

**Goals**

- One thread identity (`Session`) and one turn identity (`Run`) that every interface maps onto.
- One create-or-get path for sessions, replacing four inline producers.
- Exactly one status per run; no parallel "session" status vocabulary.
- A reversible rename; the wire key is renamed hard, with every in-repo consumer updated in the same change (no compatibility window).

**Non-Goals** (design-level boundaries, beyond `proposal.md`)

- No new aggregate or persistence layer; `Session` and `Run` stay as the existing tables, renamed.
- No wire-visible change to A2A `contextId` semantics or values.
- Not merging the public-share end-user identity or the MCP transport session into `Session`; they stay adapter-local by design.
- Not moving the gateway's derived run bucket server-side, and not shipping the archive feature — both are later changes.
- Not renaming `pkg/acpslug` (unrelated to sessions).

## Decisions

### D1: Keep two entities, rename rather than re-model

`Session` (thread) and `Run` (turn) remain the model; only names change. A run that is part of a thread has a required session link; ad-hoc/scheduled runs have none.

*Alternatives considered:* (a) collapse the thread into `chat_conversations` — rejected, because A2A tasks and share sessions need a thread with no conversation, and `session_todos` already hangs off the thread. (b) Introduce a new `sessions` aggregate alongside the old table — rejected, it duplicates state and forces a backfill for no benefit.

### D2: Resolve the `SessionID` collision by renaming the ADK key

`ExecuteRequest.ACPSessionID` becomes `ExecuteRequest.SessionID` (the thread). The existing `ExecuteRequest.SessionID` (ADK conversation-history key) becomes `ExecuteRequest.ConversationKey`. `AgentRun.ACPSessionID` becomes `AgentRun.SessionID`.

*Why:* naming the thread `SessionID` while a second `SessionID` means "ADK session key" would preserve the exact ambiguity this change removes. The ADK key is conversation-history scoping, not session identity.

*Alternative:* keep the ADK field as `SessionID` and name the thread `ThreadID` — rejected, it contradicts the chosen `Session` vocabulary and the `sessionId` wire key.

### D3: Delete `session_status`; provisioning is a phase of `working`

Drop `agent_runs.session_status`, the `SessionStatus` type, `Repository.UpdateSessionStatus`, and the six executor write sites. A run setting up its workspace reports `working`; a provisioning failure reports `failed` with `error_message`.

*Why:* it is write-only today, it is per-run despite its name, and it is the primary source of the "what status can a session have" confusion. Removing it leaves exactly one run status model.

*Alternative:* keep it as a sub-state surfaced separately — rejected; if a distinct "setting up sandbox" display is ever needed, a nullable `workspace_ready_at` timestamp is additive and does not resurrect a competing enum.

### D4: One repository resolution path for sessions

A thread's Session is resolved or created only through the Repository's session methods — `EnsureConversationSession` (chat), `EnsureSessionForContext` (A2A get-or-create), and `CreateSession` (the per-share-session thread). Interfaces never insert a Session row themselves.

*Why:* interfaces should adapt to one core; the inline creates drift (different `agent_name`/title handling, different failure behaviour). `EnsureConversationSession` is the reference create-or-get behaviour.

*Alternative:* a standalone `SessionService` type — rejected: the agents package already exposes an ADK `session.Service` (fx `provideSessionService`), so a second "session service" type invites confusion for no functional gain, and the Repository is already the single DB boundary every producer holds.

### D5: Hard rename of the wire key (no dual-emit)

`acpSessionId` is renamed to `sessionId` outright across the server DTOs and the in-repo consumers (gateway, CLI, SDK, iOS). No legacy alias is emitted.

*Why:* the only known consumers are in-repo and are updated in the same change; A2A's client-facing identifier is `contextId`, which is unchanged. A dual-emit window would add a second field to several DTOs and a retirement deadline for no external client we can point at.

*Alternative:* dual-emit `sessionId` + `acpSessionId` for one release — deferred; can be added if an out-of-repo consumer surfaces.

### D6: Two migrations, rename first, status drop second

Migration A renames tables/columns/indexes and updates FKs. Migration B drops `agent_runs.session_status`. Kept separate so the mechanical rename can land and be verified independently of the status change.

## Risks / Trade-offs

- **A missed FK/child column leaves the rename half-applied** → the migration renames all four child columns and the `session_todos` FK target; a migration up/down test plus a schema assertion covers it.
- **Renaming the wire JSON key breaks external clients** → the only known consumers are in-repo and are renamed in the same change; A2A's client-facing `contextId` is unchanged. If an out-of-repo consumer surfaces, add a dual-emit window then.
- **The `SessionID`/`ConversationKey` swap silently changes cross-run history sharing** → rename is mechanical; a unit test asserts successive trigger calls with the same `ConversationKey` share the ADK session and that a thread `SessionID` does not leak into it.
- **Deleting `session_status` removes visibility a client actually reads** → grep confirms no server/gateway consumer; verify SDK/iOS before dropping, and keep the drop in its own migration so it can be reverted independently.
- **Rename touches backup export** → `backup-export-coverage` updated; exporter table-name references are updated in the same change.
- **Larger diff than a feature change** → phased task list; each phase is individually revertable.

## Migration Plan

1. Land migration A (rename) + Go type/field renames + repository ensure methods. The wire key renames to `sessionId` across server and in-repo consumers.
2. Land migration B (drop `session_status`) + remove `SessionStatus` type and executor writes + DTO/swagger update.
3. Verify: `openspec validate`; server `task test` + `task test:integration`; migration up/down; gateway `go test ./...` + `task lint`; A2A message/task e2e round-trip; manual chat + share smoke.

**Rollback:** migrations A and B each have a down step (rename back; re-add the column with the `00026` default). The Go rename reverts with the branch. No client-visible compatibility shim exists to unwind.

## Open Questions

- Whether to add a dual-emit `sessionId` + `acpSessionId` window if an out-of-repo consumer of the old key surfaces — deferred; no known consumer today, and no legacy key is emitted.
- Whether to rename `pkg/acpslug` → `pkg/slug` — deferred non-goal; unrelated to session identity.
- Whether a future `workspace_ready_at` timestamp is needed for provisioning display — only if a UI requirement appears; additive, no impact on this change.
