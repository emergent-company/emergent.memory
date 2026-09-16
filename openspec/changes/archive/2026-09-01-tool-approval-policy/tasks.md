## 1. Memory backend — policy model (external repo: `emergent.memory`)

- [x] 1.1 Widen `ToolPolicy` to a `Mode` enum (`allow`/`deny`/`ask`) or keep booleans, and add `AgentDefinition.DefaultPolicy` so absent entries no longer silently mean allow. Verify: memory-side unit tests cover default-allow vs default-deny for unlisted tools.
- [x] 1.2 Map existing semantics (`Disabled`→deny, `Confirm`→ask, absent→allow) without breaking stored JSONB. Verify: migration/read-back unit test over an existing `ToolPolicies` blob.

## 2. Memory backend — structured rejection + in-stream approval event

- [x] 2.1 Change `injectToolResponse` reject branch to inject a structured result `{policy_decision:"rejected", reason:<message>}` instead of an error FunctionResponse. Verify: unit test asserts a rejected call yields a result (not error) with the message.
- [x] 2.2 Add an in-stream approval event (`StreamEventApprovalRequest` or a `kind` discriminator on the question event) emitted by the confirm gate, and map it in the chat `streamCallback`. Verify: unit test asserts the event reaches the chat stream alongside the existing out-of-band emit.
- [x] 2.3 Add a system-prompt instruction that a `rejected` result means "stop this direction, do not retry". Verify: unit test on prompt assembly includes the instruction.

## 3. Memory backend — audit + revocation

- [x] 3.1 Add `approvals` and `approval_decisions` tables recording `(run_id, project_id, agent, tool, args_summary, decision, message, user, timestamp)` with argument redaction. Verify: unit test writes a decision and reads it back redacted.
- [x] 3.2 Add pending-approval revocation (`cancelled` state) that resolves the pause with the call treated as not taken. Verify: unit test revokes a pending approval and asserts the run resumes without executing the tool.

## 4. Gateway — approval event passthrough + respond route

- [x] 4.1 Add an approval-event branch to `rewriteChatStream` (`gateway/sse_markdown.go`) that passes the in-stream approval event through to the client. Verify: unit test feeds a stream containing an approval event and asserts it surfaces in the rewritten output.
- [x] 4.2 Extend the question-respond route (`gateway/handlers.go`) to carry `{approve, message}` and map it to memory's respond call. Verify: handler unit test asserts approve/reject/message round-trip to the memory client.

## 5. Gateway — approval card UI

- [x] 5.1 Render an approval card (distinct from question cards) in `sidepanel.js` and `chat.js`: tool name + argument summary + Approve / Reject with optional message. Verify: `node --check` passes and templ UI test asserts the card markup renders.
- [x] 5.2 Wire Approve/Reject to the respond route; show the resolved state (approved vs rejected+message) in the conversation stream. Verify: browser smoke test (DevTools) exercises approve and reject-with-message.

## 6. Gateway — audit view + authz

- [x] 6.1 Add an audit view (templ + handler) rendering the approval history from memory's audit API. Verify: templ UI test renders a decision row; handler test passes through the audit data.
- [x] 6.2 Enforce project-scope + role on the approve and audit endpoints. Verify: authz unit test rejects a cross-project / unauthorized request.

## 7. Integration verification

- [x] 7.1 `templ generate` + `go build ./...` + `go test ./...` + `task lint` all pass. Verify: clean output.
- [ ] 7.2 End-to-end: configure an assistant agent with a tool under `ask` policy, trigger "write with assistant", confirm the approval card appears in the side panel, approve and reject-with-message, and confirm an audit record. Verify: observable behavior matches the spec scenarios.

## 8. Phase 2 (deferred — requires parallel tool dispatch in memory)

- [x] 8.1 Confirm memory supports parallel tool dispatch in a single assistant message before starting. Verify: a spike run emits multiple tool calls in one turn.
- [x] 8.2 Replace the single-slot `SuspendContext`/`ToolConfirmPauseState` with a per-slot approvals table and all-or-nothing batch resume. Verify: unit test resumes a batch only after all decisions resolve, injecting rejects in slot order.
