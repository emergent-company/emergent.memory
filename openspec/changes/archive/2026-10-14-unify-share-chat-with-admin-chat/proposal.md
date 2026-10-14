## Why

The public agent share page (`/share/agent`) and the authenticated chat workspace (`/chat`) render chat with two different implementations. `/chat` and the global side panel share one engine (`chat-components.js`, `chat-host.js`, `chat-stream.js`); the share page ships a bespoke renderer (`share-agent.js`) that produces look-alike bubbles but omits tool chips, thinking blocks, A2UI cards, citations/Sources, markdown copy buttons, inline question/approval cards, and in-bubble typing. It sends tool calls, thinking, structured UI and citations nowhere, and its approvals use a separate "dock" card with approve/deny only.

The result is a visibly different product on the share surface and two renderers to keep in sync. This change makes the share page a third host of the shared chat engine — exactly like the side panel — so both surfaces render identical components and behaviour.

## What Changes

- **Share page loads the shared engine.** `share_page.templ` loads `chat-transport.js` → `chat-components.js` → `chat-host.js` → `chat-stream.js` → `share-agent.js` (CSP already permits self-hosted scripts) and adopts the admin transcript/rail markup; the bespoke `#share-typing` dock is removed (typing renders inside the assistant bubble).
- **`share-agent.js` becomes an engine host.** It keeps the share-only concerns (fragment key exchange, sealed cookie, `/share/api/*` transport, email gate, terminal states, session rail) and supplies page-local `currentAgentName`/`currentAgentUI`, `answerQuestion`, `postDecision`, and history rendering through `MemoryChatComponents.renderTimeline`, driving streams with `MemoryChatStream.createEngine` + `MemoryChatHost.makeHandleEvent`.
- **Live-frame parity.** The share SSE stream carries the same frame types as `/api/chat` (`token`, `thinking`, `mcp_tool`, `question`, `approval`, `citations`, `ui`, `live_replay`, `error`, `done`) and the share host dispatches them through the shared handler, so tool chips, thinking, structured cards and Sources render identically. The server share stream gains the `question` (ask_user) and `citations` frames it currently omits; the gateway stops rewriting `token`→`delta` so the wire matches `/api/chat` (the markdown `html` snapshot is retained).
- **Full-timeline history.** The share session transcript returns the full run timeline (all item kinds — messages, tool calls, run markers, reasoning, citations) instead of the plain user/assistant text filter, and the gateway renders it through the same `renderHistoryHTML`/`injectQuestionAnswers`/pending-approval path as `/api/chat`. Raw tool `input`/`output` are included (deliberate full parity on this surface).
- **Inline question and approval cards.** Pending approvals and `ask_user` questions render as the same inline cards the admin chat uses (approve / reject-with-reason / cancel), posting to new share decision routes.
- **Rail parity.** The share rail reuses the admin rail component/actions: server-rendered fragment, archived filter, archive/unarchive/delete.
- **Server additions:** full-timeline transcript, `unarchive` + `delete` session routes, an approval `cancel` route (mirrors the authenticated `HandleCancelQuestion` flow), `question`/`citations` SSE frames, and tool name/args on the pending-question DTO.

## Scope guards

- The public surface keeps every existing guard: sealed-cookie exchange, signed end-user ref, edge rate limiting, per-link budget, `show_session_list`, `allow_end_user_approvals`, share CSP/security headers, and the reserved `share:agent-chat` scope bound to `/api/share/agent`.
- Raw tool input/output on the shared transcript is intentional (full parity); it is called out in the capability spec.
