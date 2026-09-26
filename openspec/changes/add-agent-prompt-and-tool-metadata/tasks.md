## 1. Server: record instruction + tool-call id

- [x] 1.1 Persist the composed system instruction as the run's first `system` run-message in `executor.runPipeline` (after workspace augmentation, before the user message)
- [x] 1.2 Add the tool-call id to `agents.ConversationHistoryItem` and populate it (with `DurationMs`) for tool-call records in `GetConversationFullHistory`
- [x] 1.3 Unit tests: run messages include a `system` row with the composed instruction; history tool-call record carries id + duration

## 2. Gateway: plumb fields, skip system markdown

- [x] 2.1 Add `ID` and `DurationMs` to `TimelineItem`; add `DurationMs` to the gateway `AgentRunToolCall` DTO
- [x] 2.2 Carry `id` + `duration_ms` on `runToolItem` in `runTimelineItems`
- [x] 2.3 `renderHistoryHTML` leaves `system` records unrendered (no markdown html) so the client owns the prompt card
- [x] 2.4 Extract the first `system` instruction from the timeline for both surfaces

## 3. Session trace viewer (templ)

- [x] 3.1 Add an agent-prompt card (collapsible, tool-call-card styling) rendered at the top of the timeline
- [x] 3.2 Show tool-call id + execution duration on `sessionToolItem`
- [x] 3.3 `templ generate`

## 4. Live chat (JS)

- [x] 4.1 Render the agent-prompt card once at the start of `renderTimelineItems`; skip `system` messages in the loop
- [x] 4.2 Show id + duration in the tool detail (`renderToolDetails`) and pass them through the history chip payload
- [x] 4.3 Add a shared `agentPromptCard` builder in `chat-components.js`

## 5. Verify

- [x] 5.1 `go build ./...` + `go test ./...` in `apps/server`
- [x] 5.2 `go build ./...` + `go test ./...` in `apps/web-ui/gateway`
- [x] 5.3 `task lint` (gateway) and server lint
- [ ] 5.4 Browser check of a session and a live chat with a tool call (not run: branch not deployed; markup covered by render tests)
