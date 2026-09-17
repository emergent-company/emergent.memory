## 1. Gateway: run state derivation

- [x] 1.1 Extend the conversation state computation in `apps/web-ui/gateway/conversation_events.go` to return, alongside the existing `runEndCount`/`pendingApprovals`/`pendingQuestions`, the active run id, the active run status, and the derived run bucket (`needs_input` > `failed` > `running` > `done`) using the newest `run_start`/`run_end` item; verify unit tests cover each bucket, the pending-approval priority over `failed`, an `input-required` run, and a conversation with no run items
- [x] 1.2 Include `bucket`, `runId`, `runStatus`, `pendingApprovals`, and `pendingQuestions` in the published refresh payload while keeping `{"type":"refresh"}` consumers working; verify a unit test asserts the payload shape and that an unknown extra field is ignored by the existing fingerprint logic
- [x] 1.3 Confirm the derivation adds no upstream request: verify a test asserts history is fetched once per poll cycle regardless of conversation count

## 2. Gateway: run cancel

- [x] 2.1 Add a `CancelAgentRun` client in `apps/web-ui/gateway/memory_runs.go` targeting the existing upstream cancel endpoint, supporting both the project/agent-scoped `POST .../agents/:id/runs/:runId/cancel` form and the ACP `DELETE /agent-chat/v1/agents/:name/runs/:runId` form; verify unit tests assert the resolved path and that a non-2xx response becomes a typed error
- [x] 2.2 **Spike:** determine which upstream cancel variant the gateway can address with identifiers it already holds for an agent-backed chat conversation (agent name from the ACP session, or agent definition id from the conversation), and record the chosen variant plus the resolution chain in the change's `design.md`; the project/agent-scoped variant was chosen (idempotent; the ACP variant needs a slug and returns 409 for terminal runs) and the chain is recorded under Risks in `design.md`. Exercising the route against the dev server for a real in-flight run remains part of task 9.5
- [x] 2.3 Add the `POST /api/chat/runs/:runId/cancel` route in `apps/web-ui/gateway/handlers.go` and register it in `main.go`; verify unit tests cover success, already-terminal (idempotent, non-error), and unresolvable-identifier (`ok:false` with a reason) responses
- [x] 2.4 Verify the unresolvable-identifier path returns `{"ok":false,"reason":…}` and never reports success when no upstream cancel was issued

## 3. Gateway: session todos

- [x] 3.1 Add a `ListSessionTodos` client in `apps/web-ui/gateway/memory_runs.go` for `GET /api/v1/agent/sessions/:sessionId/todos`; verify unit tests decode the documented `SessionTodo` fields (`id`, `content`, `status`, `order`) and tolerate an empty list
- [x] 3.2 Resolve the session id from the conversation's `acpSessionId` and return an empty result (not an error) when the conversation has no ACP session; verify unit tests cover both the present and absent cases
- [x] 3.3 Add `GET /partial/chat-todos?c=<conversationId>` returning the `TodoCard` fragment or an empty fragment; verify handler tests assert both responses

## 4. Gateway: dock and rail data

- [x] 4.1 Add `apps/web-ui/gateway/chat_dock.go` with the `ApprovalCard`, `QuestionCard`, and `TodoItem` types as frozen in `design.md`; verify a compile-time unit test constructs each type from the upstream payload shapes. Note: `QuestionCard.Options` was corrected from `[]string` to `[]AgentQuestionOption` so the dock submits an option's value rather than its label; `design.md` records the corrected shape
- [x] 4.2 Add `apps/web-ui/gateway/chat_dock.templ` with `DockPanel`, `TodoCard`, and `RailStatusBadge` using the frozen signatures; verify `templ generate` produces matching Go and unit tests render each component for the populated and empty cases
- [x] 4.3 Add `GET /partial/chat-dock?c=<conversationId>` returning `DockPanel` with the conversation's pending approvals and pending questions, and an empty fragment when there are none; verify handler tests assert both responses and that the count reflects only pending items
- [x] 4.4 Carry `bucket`, `pendingApprovals`, and `pendingQuestions` on rail rows in `apps/web-ui/gateway/ui.go` and render them via `RailStatusBadge` in the rail partial; verify unit tests assert the row attributes for each bucket and a non-zero pending count

## 5. UI: queue lane

- [x] 5.1 Add the queue lane to the composer in `apps/web-ui/gateway/chat.templ` with queued rows exposing edit and "send now" actions; verify queued rows render and an empty queue renders nothing
- [x] 5.2 Implement queue state, edit, send-now, and ordered auto-release on turn end in `chat.js`; verify enqueue, edit, send-now promotion, ordered release, and clearing after release
- [x] 5.3 Scope the queue by conversation id so a rail switch never delivers a parked message into another conversation, and reopen restores it; verify switch-away, switch-back, and no cross-conversation delivery
- [x] 5.4 Wire `Enter` to follow turn state and `Cmd/Ctrl+Enter` to always queue; verify both key paths in both turn states
- [x] 5.5 Ensure a mid-turn abort does not auto-release queued messages; verify the queue is retained after abort

## 6. UI: dock, todos, and rail badges

- [x] 6.1 Render the dock above the composer by loading `/partial/chat-dock` and refreshing it on the refresh event; verify the dock appears with pending items, disappears when empty, and does not disturb composer position. `DockPanel` also pre-renders inside `<noscript>` so the surface exists without JS, with no duplicate cards (the JS path replaces `innerHTML`; the dock's fragment root no longer carries the mount id)
- [x] 6.2 Render the todo card in the transcript from `/partial/chat-todos`, collapsed by default, refreshed on the refresh event, and absent when there are no todos; verify populated, empty, and refresh cases. The user's expanded state is preserved across refreshes
- [x] 6.3 Wire rail rows to `data-bucket` and pending counts and confirm badge updates without a full page reload; verify the rail re-renders on a refresh event carrying a changed bucket, with the server badge updated in place rather than duplicated
- [x] 6.4 Show the live turn status in the chat header, distinguishing `working` from `input-required` with a waiting-on-you indication; verify both states and the cleared state after the run ends

## 7. UI: turn footer, copy, typed markers

- [x] 7.1 Add a turn footer rendering the run's model and its duration from `completed_at − created_at` (falling back to item `duration_ms`), with the end timestamp revealed on hover; verify a completed run, a run with `duration_ms` only, and a running turn showing no duration
- [x] 7.2 Add a copy-turn action in the footer and copy actions on assistant messages; verify copied text equals the turn's assistant text
- [x] 7.3 Add per-code-block copy actions that copy only the block's source without surrounding markup or highlight spans; verify the copied text for a highlighted block and for a block containing HTML-like content
- [x] 7.4 Render `run_start`/`run_end` as turn boundaries with status-distinct `failed` and `input-required` states, surfacing `error_message` for failed runs and the model for `run_start`; verify failed, input-required, and completed boundaries
- [x] 7.5 Confirm copy affordances leave message layout and reading flow unchanged; achieved by CSS rather than markup (controls are non-interactive until hover/focus and hidden on touch), so no automated markup comparison was needed

## 8. Styling and responsiveness

- [ ] 8.1 Style the dock, queue rows, todo card, rail badges, and turn footer to match existing chat surfaces, and confirm the transcript keeps its scroll anchoring against the pinned composer while the dock is present. Styles and scroll handling are implemented; the manual pass at desktop and below-`md` widths still needs a running gateway
- [x] 8.2 Verify keyboard access for the dock controls, queue rows, copy actions, and todo card toggle, with visible focus states

## 9. Verification

- [x] 9.1 Run `templ generate` and confirm no diff beyond the intended generated files
- [x] 9.2 Run `go build ./...` and `go test ./...` from `apps/web-ui/gateway`; all pass
- [x] 9.3 Run `task lint` in `apps/web-ui`; all pass
- [ ] 9.4 Extend the Playwright chat coverage to cover the dock appearing with a pending approval and its decision controls, a queued message auto-releasing in order, and a rail badge reflecting a bucket change without a reload. Scenarios are written in `apps/web-ui/tests/e2e/scenarios/chat-run-control.spec.ts` (live-LLM, env-gated on `E2E_SCENARIO_LLM_API_KEY`, following the existing `agent-proposal-card` / `mcp-servers-tool-call` pattern) and the file is `tsc`-clean, but the suite has NOT been executed
- [ ] 9.5 Manual pass against a live agent run: confirm stop cancels the server-side run, the dock shows and clears pending decisions, the todo card tracks a plan, the footer reports duration, and the queue releases in order
- [ ] 9.6 Confirm no regression in the sidepanel chat host, which shares the streaming engine and markdown renderer. The shared renderer was left intact by design (the dock, queue, and footer surfaces are all no-ops when their mount points are absent), but this has not been exercised in a browser
- [x] 9.7 Run `openspec validate add-agentic-chat-control --strict`
