## Context

The web chat is a Go templ + HTMX gateway that proxies a memory service. Recon established the following facts, which drive every decision here:

| Need | Existing source | Server change required? |
|---|---|---|
| Run identity for the active/in-flight run | `ConversationHistoryItem.RunID` on every item (`apps/server/domain/agents/repository.go:3141`) | No |
| Run status | `run_status` on `run_start`/`run_end` items (`repository.go:3227`, `:3290`), from `AgentRunStatus` | No |
| Pending approvals | `ListToolApprovals` → `GET /api/projects/:projectId/agent-approvals` (`memory_runs.go:238-256`) | No |
| Pending questions | already fetched for the reminder/approval flows | No |
| Cancel a run | upstream `POST /api/projects/:projectId/agents/:id/runs/:runId/cancel` (`apps/server/domain/agents/routes.go:36`), plus ACP `DELETE /agent-chat/v1/agents/:name/runs/:runId` | No (proxy only) |
| Session todos | `GET /api/v1/agent/sessions/:sessionId/todos` (`apps/server/domain/sessiontodos/routes.go:10`) | No (proxy only) |
| Conversation → session | `ConversationDetail.ACPSessionID` (`extras.go:25`), `ConversationHistory.acp_session_id` (`extras.go:33`) | No |
| Turn duration | `run_end.completed_at − run_start.created_at`, or item `duration_ms` | No |
| Steering a running turn | nothing — no endpoint injects a message into a non-paused run | **Yes — deferred** |
| `notification` / `compaction` items | no server-side emission | **Yes — deferred** |
| Per-run tokens/cost | OTEL spans only, via `GET .../agent-runs/:runId`; usage API is project + time window | **Yes — deferred** |

Because every in-scope requirement is satisfiable from data the gateway already fetches or already knows how to proxy, this change is gateway + web assets only. That is the central design constraint.

## Decision 1: No server change

Two tempting shortcuts were rejected:

- **Emitting `runId` on the chat SSE `meta` event.** `NewMetaEventWithRun` exists (`apps/server/pkg/sse/events.go:51`) but is dead code; the chat path emits `NewMetaEvent(conv.ID.String())` (`apps/server/domain/chat/handler.go:799`). Wiring it would be a small server change, but it is unnecessary: the events poller already fetches conversation history every 1.5s and the newest `run_start`/`run_end` item carries both `run_id` and `run_status`. Deriving from that keeps the whole change in one module and reuses an existing fetch instead of adding a live status channel.
- **Proxying an existing "run status" endpoint.** The gateway's `GetAgentRun` (`memory_runs.go:96`) returns only `TraceID` and spans — no status. Extending it would mean touching the server's run DTO. The history item already carries status.

Consequence: the active run's status is at most ~1.5s stale. That is acceptable for a badge and far cheaper than a new push channel; the existing refresh event already drives re-render.

## Decision 2: Buckets reference `acp-run-lifecycle`, they do not redefine it

`AgentRunStatus` is `submitted | working | completed | skipped | failed | input-required | cancelled | cancelling` (`apps/server/domain/agents/entity.go:57-68`). `acp-run-lifecycle` already specifies status mapping and cancel semantics, and `agent-run-preview` already specifies in-progress run display. This change therefore adds only the *aggregation rule* (`needs_input > failed > running > done`) and its presentation. No requirement restates the enum, the cancel transition, or pause semantics.

Priority order mirrors the reference implementation researched for this change: a pending human decision must outrank a failure, because the failure is already resolved by the time the user acts, whereas a pending decision blocks the run.

## Decision 3: Approval semantics untouched

`tool-approval-policy` already specifies that approvals reach the conversation, that multiple approvals can be concurrent and decided independently, that an approval waits indefinitely, and that decisions are audited. The dock is a placement change: same items, same response routes, same audit. The only new obligation is that the dock shows a count and refreshes on the existing refresh event.

## Decision 4: The queue is client-side and per-conversation

The queue never touches the server until a message is released, so it cannot affect run semantics and needs no new endpoint. It is scoped by conversation id so a rail switch cannot deliver a parked message into the wrong conversation. `Cmd/Ctrl+Enter` always queues, giving an unambiguous way to park a thought mid-stream without changing the meaning of `Enter`.

This deliberately omits **steer** (injection into a live turn). Steering needs a server-side mid-turn injection endpoint that does not exist, and the gateway cannot fake it: `/api/chat/stream` always starts a new run (`apps/server/domain/chat/handler.go:1215-1235`). Claiming to steer while actually starting a second run would corrupt the transcript. Queue gives the user a real, honest capability now; steer becomes its own change once the agent runtime exposes injection.

## Decision 5: Typed markers limited to what exists

The history item kinds are `user_message`, `assistant_message`, `tool_call`, `tool_result`, `run_start`, `run_end` (`repository.go:3139-3143`). There is no `notification` or `compaction` kind, so no requirement is written for them. Requirements are limited to rendering `run_start`/`run_end` as turn boundaries with status-distinct `failed` and `input-required` states plus the run's `error_message` — all of which have a data source today.

Similarly, per-turn token and cost display is out of scope: usage is reported per project and time window (`usage-dashboard`), and per-run tokens exist only as OTEL spans on the run trace endpoint, which `/runs/:runId` already consumes. The footer shows model and duration, which are free.

## Frozen interface

Both implementation lanes code against this contract. Signatures are fixed so the lanes can proceed in parallel.

New templ components in `apps/web-ui/gateway/chat_dock.templ`:

```
templ DockPanel(convID string, approvals []ApprovalCard, questions []QuestionCard)
templ TodoCard(todos []TodoItem)
templ RailStatusBadge(bucket string, pendingApprovals, pendingQuestions int)
```

Types (`apps/web-ui/gateway/chat_dock.go`):

```
type ApprovalCard struct { QuestionID, ToolName, ArgsSummary, ConversationID string }
type QuestionCard struct { QuestionID, Prompt, Kind string; Options []AgentQuestionOption }
type TodoItem     struct { ID, Content, Status string; Order int }
```

Routes:

| Route | Returns |
|---|---|
| `POST /api/chat/runs/:runId/cancel` | `{"ok":true,"runId":"…"}` or `{"ok":false,"reason":"…"}` |
| `GET /partial/chat-dock?c=<conversationId>` | HTML fragment — `DockPanel` or empty |
| `GET /partial/chat-todos?c=<conversationId>` | HTML fragment — `TodoCard` or empty |

Extended refresh payload (existing `{"type":"refresh"}` gains fields; consumers that ignore unknown fields keep working):

```json
{"type":"refresh","bucket":"needs_input","runId":"…","runStatus":"input-required",
 "pendingApprovals":2,"pendingQuestions":0}
```

Rail row attributes: `data-bucket`, `data-pending-approvals`, `data-pending-questions`.

Bucket derivation lives in `conversation_events.go` and is passed through the existing `conversationState` computation so no additional upstream fetch is introduced.

## Lane split

| Lane | Owner | Scope | Files |
|---|---|---|---|
| A | `@fixer` | Gateway plumbing: state computation, cancel proxy, todos proxy, rail row data, new routes, new `chat_dock.go` types and `chat_dock.templ` components | `conversation_events.go`, `memory_runs.go`, `ui.go`, `handlers.go`, `main.go`, new `chat_dock.go`, new `chat_dock.templ` |
| B | `@designer` | User-visible surfaces: composer queue, turn footer, copy affordances, typed run markers, dock/todo/rail-badge wiring, CSS | `chat.templ`, `chat.js`, `chat-stream.js`, `chat-components.js`, `chat-host.js`, gateway CSS |

`chat.templ` has a single owner (lane B); lane A confines itself to the new `chat_dock.templ` file. Lane B consumes lane A's component signatures as frozen above.

## Risks

- **Cancel identifier resolution — RESOLVED (spike).** Chosen variant: the project/agent-scoped `POST /api/projects/:projectId/agents/:id/runs/:runId/cancel`. Resolution chain: `POST /api/chat/runs/:runId/cancel` → `GetAgentRun(runId)` (which returns the runtime agent UUID) → `CancelAgentRun(agentID, runID)`. It was preferred over the ACP `DELETE /agent-chat/v1/agents/:name/runs/:runId` variant because it is idempotent (the upstream repository sets `cancelled` unconditionally and returns success even for terminal runs), whereas the ACP variant returns 409 for terminal states, and because resolving an ACP slug additionally requires the agent definition name while the ACP project id is empty on the gateway's OAuth-session path. When the run id cannot be resolved to an agent, the route returns `{"ok":false,"reason":…}` and the UI aborts locally while telling the user the run could not be cancelled — it never claims success.
- **Dock position vs. transcript scroll.** The dock changes the transcript's available height. The transcript is already scroll-filling against a pinned composer; the dock must not break scroll anchoring while a turn streams.
- **Queue and abort interaction.** After a mid-turn abort, queued messages must not auto-release into an aborted turn.
- **Polling staleness.** Badges lag up to ~1.5s. Acceptable; documented rather than worked around.
- **Optimistic queue semantics.** "Send now" on a queued row while a turn is running promotes the message to the head of the queue and releases it the instant the turn ends, rather than starting a second concurrent run. A second `/api/chat` turn mid-stream would clobber the live bubble; genuine mid-turn injection is the deferred steer capability.
