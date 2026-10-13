## Why

The human-in-the-loop surfaces render the same two things — a pending question and a pending tool
approval — five different ways. Today `/settings/approvals` draws a question as bare buttons / a
checkbox group / a text input (`approvals.templ` `questionRow`), the chat transcript draws the same
question as a rich option-row card built in JavaScript (`chat-stream.js` `renderQuestion`), the
chat's no-JS dock draws a third variant (`chat_dock.templ`), the A2UI renderer a fourth, and the
public share page a fifth. They have already drifted: only the chat card shows option descriptions,
a selection indicator, an Esc hint, and the answered state. There is no single definition of what a
question or an approval card *is*.

Separately, an approval decision on `/settings/approvals` links back to the chat conversation that
produced it (`tool-approval-policy` "Audit record links to its session"), but a pending **question**
does not — the question API carries no conversation id at all, so the surface that actually needs an
answer gives the operator no way to jump to the conversation waiting on it.

## What Changes

- Expose `conversationId` on the agent-question API (`GET /api/projects/{projectId}/agent-questions`),
  resolved from the question's run by the same `agent_runs.session_id → chat_conversations.session_id`
  join the tool-approval list already uses. Absent for runs with no chat conversation.
- Render a **Session** link on each pending question on `/settings/approvals` pointing at
  `/chat?c=<conversationId>`, matching the link the approval rows already render; omit it when the
  run has no conversation. Both links carry stable `data-testid`s.
- Add one canonical pair of gateway components — `QuestionCard` and `ApprovalCard` — as the single
  source of that markup: the card shell, per-option rows with a selection indicator and optional
  description, the sanitized proposal slot, the footer, and the answered state.
- Render those components from `/settings/approvals` (interactive, POST-back) and from the chat's
  no-JS dock, replacing both bespoke implementations.
- Align the chat's client-side renderer (`chat-stream.js`) to the component's exact structure and
  classes, and guard the alignment with a contract test so the two cannot drift silently again.
- Fold the dock's `.dock-question` / `.dock-approval` CSS into the shared `memory-question` /
  `memory-approval` rules.

Not in scope (recorded as follow-ups, not silently fixed): the A2UI `question`/`approval` card
components (`agent-structured-ui`, in flight) and the public share page's approval card.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `tool-approval-policy`: a pending question on the human-input surface links to its originating
  conversation, like an approval audit row.
- `web-ui-components`: question and approval cards are single-sourced through the gateway component
  package, with the client-side chat renderer mirroring the same contract.

## Impact

- `apps/server/domain/agents/entity.go`, `dto.go`, `repository.go`: `ConversationID` on the question
  entity/DTO, resolved in `ListQuestionsByProject`.
- `apps/server/docs/swagger/`: regenerated artifacts for the changed DTO.
- `apps/web-ui/gateway/memory_runs.go`: `conversationId` on `AgentQuestionItem`.
- `apps/web-ui/gateway/components/question_card.templ`, `approval_card.templ` (new).
- `apps/web-ui/gateway/approvals.templ`, `chat_dock.templ`, `webui/css/app.css`.
- `apps/web-ui/gateway/webui/static/js/chat-stream.js` (+ contract test).
- `apps/web-ui/tests/e2e/scenarios/approvals-respond.spec.ts`: session-link assertion.
