## 1. Server: conversation id on agent questions

- [x] 1.1 Add `ConversationID *string` to the `AgentQuestion` entity (`bun:"-" json:"conversationId,omitempty"`), mirroring `AgentToolApproval`; verify a unit test asserts `ToDTO()` carries it through and omits it when nil
- [x] 1.2 Resolve it in `ListQuestionsByProject` via a LEFT JOIN `kb.agent_runs` → `kb.chat_conversations` on `session_id` (same shape as `ListToolApprovals`); verify a repository test asserts a question whose run has a conversation gets the id and one whose run has none gets nil
- [x] 1.3 Add `ConversationID *string` to `AgentQuestionDTO` and populate it in `ToDTO()`; verify the handler test asserts the JSON field name is `conversationId`
- [x] 1.4 Regenerate the committed swagger/docs artifacts for the changed DTO and check the diff is only the intended field

## 2. Gateway: surface the link

- [x] 2.1 Add `ConversationID string \`json:"conversationId,omitempty"\`` to `AgentQuestionItem` (`memory_runs.go`); verify a decode test asserts the field round-trips from the API JSON
- [x] 2.2 Render a Session link (`lucide--messages-square`, `/chat?c=<url-escaped id>`) on each pending question, omitted when the id is empty; add `data-testid="question-session-link"` and give the existing approval link `data-testid="approval-session-link"`; verify a page-render test asserts the link, its href, and its absence when the id is empty

## 3. Canonical cards

- [x] 3.1 Add `components/question_card.templ` with `QuestionCardProps` + `QuestionCard` (card shell, per-option rows with indicator and optional description, proposal slot, footer/answered state, `Mode` enum, `Attrs` spread last); verify a render test asserts the option rows, the indicator roles (`radio`/`checkbox`), and the answered state disables controls
- [x] 3.2 Add `components/approval_card.templ` with `ApprovalCardProps` + `ApprovalCard` (tool, argument summary, decision badge, reject reason field, actions); verify a render test asserts the tool name and the action testids
- [x] 3.3 Render both from `/settings/approvals`, wrapping the component's controls in the existing respond/cancel forms; verify the existing approvals page tests still pass and the `pending-question` / `pending-question-proposal` testids are preserved
- [x] 3.4 Render both from the chat no-JS dock (`chat_dock.templ`), retiring the bespoke `.dock-card dock-question` markup; verify a dock render test asserts the same component testids
- [x] 3.5 Fold the `.dock-question` / `.dock-approval` CSS into the shared `memory-question` / `memory-approval` rules and delete the dead selectors; verify no orphaned `.dock-*` rule remains for the replaced markup

## 4. Chat renderer alignment

- [x] 4.1 Align `chat-stream.js` `renderQuestion` / `renderApproval` to the component's structure and classes (option rows, indicator, description, answered state); verify `node --check` and the existing chat-stream unit specs pass
- [x] 4.2 Add a contract test comparing the component's rendered class skeleton + `data-testid` set against the JS renderer's, failing on divergence; verify the test fails when either side is perturbed

## 5. Verify

- [x] 5.1 `go build ./...` and `go test ./...` in `apps/server`
- [x] 5.2 `templ generate` (no unexpected diff), `go build ./...`, `go test ./...`, `task lint` in `apps/web-ui/gateway`
- [x] 5.3 `task lint` in `apps/web-ui`
- [x] 5.4 Extend `tests/e2e/scenarios/approvals-respond.spec.ts` to assert the pending row's session link and href; confirm `scenarios/chat/chat-question-resume.spec.ts` still passes
- [x] 5.5 `openspec validate --changes` clean
