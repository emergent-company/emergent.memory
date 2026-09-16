## 1. Markdown rendering (iOS)

- [ ] 1.1 Add `swift-markdown` (Apple) + `Splash` (John Sundell) as SPM package dependencies in `VoiceAgent.xcodeproj`; verify `xcodebuild` resolves packages and the app target builds
- [ ] 1.2 Implement a `MarkdownRenderer` view tree (headings, paragraphs, emphasis, inline code, links) driven by the `swift-markdown` AST; verify unit tests render fixture markdown to the expected blocks
- [ ] 1.3 Add fenced code blocks with `Splash` syntax highlighting and a monospaced fallback when highlighting fails; verify a unit test asserts a code block renders and a highlight-failure falls back without crashing
- [ ] 1.4 Add tables, blockquotes, task lists, and inline images (`AsyncImage` from remote URLs); verify unit tests cover each construct
- [ ] 1.5 Replace `agentText(_:)` in `ChatView.swift` with `MarkdownRenderer`; verify a render test shows rich markdown and a malformed-markdown test degrades to plain text without crashing
- [ ] 1.6 Factor the `ToolsCard`/`ToolCallRow` detail views out of `SessionDetailView.swift` into a shared component; verify the existing session-timeline tests still pass and new component tests cover name/args/result/error rendering

## 2. Rich-event transport (worker)

- [x] 2.1 Publish a `lk.chat.events` text stream from the worker and forward `mcp_tool`, `thinking`, and `approval` events that `MemoryLLMStream` currently drops; verify a worker unit test feeds a fake memory stream and asserts the forwarded payloads
- [x] 2.2 Synthesize a `question` event from `ask_user` `mcp_tool` calls, mirroring `gateway/sse_markdown.go` `rewriteChatStream`; verify a unit test maps an `ask_user` started+running pair into one `question` event
- [x] 2.3 Register a `lk.chat.decision` text-stream handler and decode the client's decision JSON; verify a unit test decodes approve/reject and free-text answer payloads and ignores malformed ones
- [x] 2.4 Add `respond`/`cancel` methods to `MemoryChatClient` hitting `agent-questions/{questionId}/respond` and `.../cancel`; verify a unit test (mocked httpx) asserts the path and body
- [x] 2.5 Wire a decision into `respond`/`cancel`, then resume the paused turn via a fresh `generate_reply` "continue"; verify an integration test with a mocked memory client asserts respond-then-resume ordering

## 3. iOS: consume rich events + render activity

- [ ] 3.1 Add `Decodable` chat-event models (tool_call, tool_result, thinking, approval, question) and a decoder that ignores unknown events; verify unit tests decode each type and drop unknown types safely
- [ ] 3.2 Register a `lk.chat.events` handler in `AlfredSessionController` and route decoded events into the chat's message/activity store; verify a unit test asserts events update the store
- [ ] 3.3 Render live tool-call chips (expandable, name/args/result/error) using the shared components from task 1.6; verify a unit/render test shows a chip and its expanded details
- [ ] 3.4 Render thinking blocks (collapsible, live-delta accumulation) above the reply; verify a unit test accumulates thinking deltas into one block

## 4. iOS: interactive cards + decision

- [ ] 4.1 Render an approval card (tool name + argument summary + approve/reject with optional reason); verify a render test shows the card and its controls
- [ ] 4.2 Render a question card (buttons / multi-select / free-text, answered state once submitted); verify a render test covers the card controls and the answered state
- [ ] 4.3 Send the decision back over `lk.chat.decision` (approve/reject/answer + message); verify a unit test encodes the decision payload correctly

## 5. Composer affordances

- [ ] 5.1 Show a typing indicator in the agent position while awaiting the first reply token and dismiss it on the first token; verify a render test covers show/dismiss
- [ ] 5.2 Add a stop/interrupt affordance that calls `session.interrupt()` while a reply is generating, returning the composer to ready; verify a test asserts the stop action interrupts without ending the session
- [ ] 5.3 Add suggested-prompt chips to the empty chat state that fill the composer without auto-sending; verify a render test asserts a tap populates the composer

## 6. Verification

- [x] 6.1 Worker: run `pytest` in `alfred_bridge/` and the linter; all pass
- [ ] 6.2 iOS: build via `tools/ios-build-mac.sh` and run `VoiceAgentTests`; all pass
- [ ] 6.3 Manual: build the iOS app on the Mac, connect to an agent, send text, and confirm rich markdown, tool chips, and thinking blocks render; trigger an approval and a question and confirm each renders and resumes after a decision
