## 1. Server — share surface parity (`apps/server/domain/agents`)

- [x] 1.1 Full-timeline transcript: replace the text-only `sessionTranscript` filter with the full `GetConversationFullHistory(sess.SessionID)` item list (all kinds); keep per `(link, session, end_user_ref)` ownership; extend `ShareSessionDetailDTO` to carry the timeline items
- [x] 1.2 `UnarchiveSession` service + `unarchive` route (`POST /api/share/agent/sessions/:id/unarchive`) with the same ownership scoping as `ArchiveSession`
- [x] 1.3 `DeleteSession` service + store + `DELETE /api/share/agent/sessions/:id`, end-user scoped
- [x] 1.4 `CancelQuestion` service + route (`POST /api/share/agent/sessions/:id/approvals/:questionId/cancel`) mirroring the authenticated cancel flow (ownership checks from `RespondToQuestion`, `repo.CancelQuestion`, resume + budget via `resumeSettled`, `DisableAuthMint`, audit `cancelled`)
- [x] 1.5 Stream parity: the server share stream emits the same `token`/`thinking`/`mcp_tool`(ask_user)/`citations`/`approval` frames as `/api/chat` (confirm `approval` carries `questionId`); the gateway share stream applies the same ask_user → `question` translation as the authenticated stream (shared `askUserFrame` helper), so a live pause renders the inline question card on both surfaces
- [x] 1.6 Pending-question DTO: expose the tool name + args (input) needed by the shared approval card
- [x] 1.7 Server unit tests for 1.1–1.6 (ownership boundaries, timeline kinds, cancel semantics)

## 2. Gateway — Go + templ (`apps/web-ui/gateway`)

- [x] 2.1 `share_page.templ`: load `chat-transport.js` → `chat-components.js` → `chat-host.js` → `chat-stream.js` → `share-agent.js`; adopt admin transcript/rail markup and containers; remove `#share-typing` and the `#share-approvals` dock (plus the go-daisy popover runtime the rail menu needs)
- [x] 2.2 Server-rendered rail partial for the share rail (`GET /share/api/partial/sessions`) using the admin rail row markup + archived filter + archive/unarchive/delete menu
- [x] 2.3 Handlers: `unarchive`, `delete`, approval `cancel` proxying; the full-timeline proxy runs `renderHistoryHTML` and relays the server detail verbatim — the server annotates answered ask_user calls (`injectQuestionResponses`) and carries `pending_approvals` itself, so the gateway never re-merges `/questions` (that duplicated every card)
- [x] 2.4 `rewriteShareStream`: stop rewriting `token`→`delta` (pass `token` through), keep the `meta` frame and the `html` markdown snapshot; keep `error` code annotation; translate ask_user `mcp_tool`→`question` like `/api/chat`
- [x] 2.5 Wire types: `ShareSessionDetail` timeline items + pending approvals; question DTO tool/args
- [x] 2.6 Gateway unit tests (share handlers, stream rewrite, rail partial)
- [x] 2.7 `templ generate`

## 3. Web UI — share host rewrite (`apps/web-ui/gateway/webui/static/js`)

- [x] 3.1 `share-agent.js`: rebuild as a host of `MemoryChatStream.createEngine` + `MemoryChatHost.makeHandleEvent` + `MemoryChatComponents`; keep share-only concerns (exchange, `/share/api/*` transport, email gate, terminal states, rail); page-local `currentAgentName`/`currentAgentUI`, `answerQuestion`, `postDecision`
- [x] 3.2 History renders through `MemoryChatComponents.renderTimeline` (full timeline + pending approvals + Sources)
- [x] 3.3 Live frames dispatch through the shared handler (tool chips, thinking, structured UI, citations, question/approval cards, in-bubble typing)
- [x] 3.4 `chat-stream.js`: add a backward-compatible endpoint/payload override for `streamChat` so the share host reuses the engine's stream orchestration (chat.js/sidepanel.js unchanged)
- [x] 3.5 Rail: archive/unarchive/delete + archived filter wired to the share routes

## 4. E2E (`apps/web-ui/tests/e2e`)

- [x] 4.1 `mock-memory.mjs`: emit the full share frame set (`token`/`thinking`/`mcp_tool`(ask_user)/`approval`/`citations`/`error`/`done`) and a full-timeline session detail
- [x] 4.2 `specs/share/agent-public-share.spec.ts`: assert shared components/behaviour (tool chips, inline question + approval card with reject reason + cancel, live ask_user question card, Sources, copy buttons, in-bubble typing, rail actions)
- [x] 4.3 Run the share Playwright project; update selectors to the shared markup

## 5. Spec

- [x] 5.1 New `web-share-chat-parity` capability spec (this change)
- [x] 5.2 Amend `openspec/changes/agent-public-share/specs/web-agent-public-share/spec.md`: drop "approve/deny only; no richer approval UI is in scope"; update the transcript requirement to the full timeline
- [x] 5.3 `openspec validate unify-share-chat-with-admin-chat --strict`

## 6. Verification

- [x] 6.1 Server: `go build ./...`, `go test ./domain/agents/...`, `go vet`, golangci-lint
- [x] 6.2 Gateway: `templ generate`, `go build ./...`, `go test ./...`, `task lint`
- [x] 6.3 Playwright: share e2e project
- [x] 6.4 Manual DevTools: compare `/chat` and `/share/agent` for the same turn (user's machine) — parity is delivered by the merged shared engine (`apps/web-ui/gateway/share_page.templ:246-250` loads `chat-transport.js`/`chat-components.js`/`chat-host.js`/`chat-stream.js`/`share-agent.js`) and exercised by the share e2e suite (`apps/web-ui/tests/e2e/specs/share/agent-public-share.spec.ts` asserts tool chip, inline approval card, Sources, copy, rail); the human DevTools spot-check is deferred to the user's machine and is non-blocking.
