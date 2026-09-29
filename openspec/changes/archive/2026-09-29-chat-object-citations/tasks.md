<!-- openspec:archive-ready -->

# Tasks

Worktree: `/root/emergent.memory-wt/chat-object-citations` (branch `feat/chat-object-citations`).
Contract is frozen in `design.md`; both lanes code against it.

## 1. Backend — citations derivation (Lane A)

- [ ] 1.1 New package `apps/server/domain/chat/citations`: `Citation` struct (`kind,id,type,label,url`); `Candidates(toolCalls []*agents.AgentRunToolCall) map[string]Reference` walking outputs for objects (`id` UUID + `type`) and relationships (`src_id` + `dst_id`)
- [ ] 1.2 `Derive(answerText string, surfaces []a2ui.Message, candidates map[string]Reference) []Citation` — parse `/objects/<uuid>` links (markdown + bare) and `#relationship-<uuid>` fragments, intersect with candidates, dedupe, order by first reference
- [ ] 1.3 `NeutralizeLinks(answerText string, citations []Citation) string` — demote `/objects/<id>` links with unvalidated ids to plain label text; drop unvalidated `#relationship-<rel>` fragments
- [ ] 1.4 Unit tests per tool shape; retrieved+referenced kept; hallucinated dropped; unreferenced retrieved dropped; relationship case; neutralization

## 2. Backend — transport (Lane A)

- [ ] 2.1 `apps/server/pkg/sse/events.go`: `citations` event type + constructor (payload `{type,citations}`)
- [ ] 2.2 `streamAgentChat` + `QueryStream`: after the run, load tool calls via `FindToolCallsByRunID(result.RunID)`, derive citations from the final answer + A2UI surfaces, emit the `citations` event before `done`
- [ ] 2.3 `ConversationHistoryItem` gains `Citations []citations.Citation` (json `citations,omitempty`); `GetConversationFullHistory` populates it on `assistant_message` items by deriving from that run's tool calls + message text
- [ ] 2.4 (if cheap) attach citations to run-full messages in `GetProjectRunFull`; otherwise leave for follow-up and note it
- [ ] 2.5 Unit tests: SSE emits citations before done; history item carries citations; no-citation turn unchanged

## 3. Backend — prompt + catalog (Lane A)

- [ ] 3.1 Shared citation instruction constant; included in `resolveInstruction` (`executor.go`) for KB-backed agents and/or in `graphQueryAgentSystemPrompt` (`repository.go`) + `personalKBAgentSystemPrompt` (`personal_kb_agent.go`). Must state the reference format and "use exact ids from tool results; never invent ids"
- [ ] 3.2 `apps/server/pkg/a2ui/a2ui.go`: add `sources` component (`items`) to `BasicCatalog`, additive
- [ ] 3.3 Unit test: `sources` validates; existing catalog unchanged

## 4. Web — Sources block + link rule (Lane B)

- [ ] 4.1 `sse_markdown.go`: capture the turn's citations from the `citations` event; forward the event; render the snapshot with `NeutralizeLinks`
- [ ] 4.2 `markdown.go` + `renderHistoryHTML`: apply the citation link rule to history renders using each item's citations
- [ ] 4.3 Sources block under an assistant answer with citations (live turn + history), entries link to `url`; no block when empty; render via `textContent`/sanitized markdown only, never `innerHTML` of agent text
- [ ] 4.4 Pass `citations` through `run_history.go` / timeline parsing to the client
- [ ] 4.5 `chat-components.js`: `a2uiEntity` links its `id` to `/objects/<id>`, including relationship `target`/`id`; new `a2uiSources` renderer for the `sources` component (items → links); keep fallback for unknown components
- [ ] 4.6 Wire the `citations` SSE case in `chat-host.js` and render in `chat.js` + `sidepanel.js`
- [ ] 4.7 `node --check` on every touched JS file

## 5. Verify (both lanes + orchestrator)

- [ ] 5.1 `go build ./...` && `go test ./...` in `apps/server`
- [ ] 5.2 `templ generate` then `go build ./...` && `go test ./...` in `apps/web-ui/gateway`
- [ ] 5.3 gateway `task lint`
- [ ] 5.4 `openspec validate --changes` clean
- [ ] 5.5 Manual: one KB-backed chat turn shows inline links + Sources; a bogus id link renders as plain text

## 6. Ship

- [ ] 6.1 Commit on `feat/chat-object-citations`, push, `gh pr create --base main`
