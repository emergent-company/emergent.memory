## Context

Two chat renderers exist. The authenticated `/chat` workspace (`chat.js`) and the global side panel (`sidepanel.js`) are two hosts of one shared engine:

- `chat-components.js` — `MemoryChatComponents`: bubbles, `enhanceMessage` (copy message/code), `expandableBadge`/`renderToolDetails` tool chips, `createThinkingBlock`, A2UI cards, citations/Sources footer, `renderTimeline`.
- `chat-host.js` — `MemoryChatHost`: composer helpers, `makeHandleEvent` (SSE dispatcher), `createResizeGrip`, gateway POST helper.
- `chat-stream.js` — `MemoryChatStream.createEngine(ctx)`: `addUserMessage`, `addAssistantMessage`, `openAssistantBubble`, `appendToken`, `updateBubbleText`, `handleToolEvent`, `renderQuestion`, `renderApproval`, `streamChat`, `finishStream`, `failStream`, `noteUISurface`, `autoGrow`.

The share page (`share_page.templ` + `share-agent.js`) is a third, bespoke renderer. This change makes it a third host of the shared engine, mirroring `sidepanel.js`.

## Decisions

### D1 — Share is a host, not a fork
`share-agent.js` instantiates the same engine (`MemoryChatStream.createEngine`), the same dispatcher (`MemoryChatHost.makeHandleEvent`), and the same component builders. Only genuinely page-local pieces stay in the host file: fragment key exchange, sealed-cookie transport against `/share/api/*`, the email gate, terminal states (invalid/revoked/expired/rate-limited/budget-exceeded), the session rail, `answerQuestion`, `postDecision`, and `currentAgentName`/`currentAgentUI`.

### D2 — Wire parity: drop the `token`→`delta` rewrite
`rewriteShareStream` currently rewrites `token` frames to `{type:"token", delta}`. The shared engine reads `token`. We stop rewriting so the share wire matches `/api/chat` byte-for-byte for frame `type`/field names, keeping the leading `meta` frame (session id) and the terminal `html` markdown snapshot. The share host consumes `meta` itself; every other frame goes through the shared dispatcher.

### D3 — Server emits the frames parity needs
The server share stream currently emits `token`/`thinking`/`mcp_tool`/`error`/`approval`/`done`. To render inline question cards and Sources live it must also emit `question` (ask_user, reusing the `/api/chat` emit path) and `citations`. `approval` already carries `questionId`.

### D4 — Full-timeline transcript, raw tool I/O
`ShareService.sessionTranscript` filters to `kind=="message"` + role user/assistant + text. We replace it with the full `GetConversationFullHistory(sess.SessionID)` timeline (all kinds). The gateway runs that items array through the same `renderHistoryHTML` + `injectQuestionAnswers` + pending-approval annotation used by `/api/chat/:id/history`. Raw tool `input`/`output` are exposed — full parity, deliberate (see capability spec security note). The run's composed system instruction (owner system prompt + skills appendix, persisted as a `role:"system"` message the authenticated chat renders as a prompt card) is **stripped** before the transcript leaves the server: it is not on the deliberate list and must never reach an anonymous end user.

### D5 — Inline question/approval cards + a cancel route
Pending approvals and `ask_user` questions render as the shared inline cards. The existing share approve route already accepts `message` (reject-with-reason works). Cancel is a distinct server flow (`HandleCancelQuestion` → `repo.CancelQuestion`, resuming the run with the call not taken), so a new share route `POST /api/share/agent/sessions/:id/approvals/:questionId/cancel` calls a new `ShareService.CancelQuestion` mirroring the authenticated ownership checks + share resume/budget (`DisableAuthMint`, `resumeSettled`).

The pending-question DTO must expose the tool name + args so the shared approval card renders the same body as admin (`{questionId, tool, input}`).

Cancel is a decision that resumes the run, so it is gated by `allow_end_user_approvals` exactly like approve/reject.

### D6 — Rail parity: server-rendered fragment + archive/unarchive/delete
The share rail mirrors the admin rail (server-rendered partial, archived filter, `ui.ListRow` markup, per-row menu). New server routes: `POST /sessions/:id/unarchive`, `DELETE /sessions/:id`. Delete is end-user-scoped (own sessions only).

### D7 — Spec placement
`web-agent-public-share` lives only in the open `agent-public-share` change (no main spec), so this work adds a new `web-share-chat-parity` capability (ADDED) and, in the same PR, drops the now-contradictory "approve/deny only; no richer approval UI is in scope" clause from `agent-public-share`'s spec.

## Interfaces

### Share API (gateway `/share/api/*`, cookie-gated)
| Method | Path | Notes |
|---|---|---|
| GET | `/share/api/sessions?filter=active\|all\|archived` | unchanged (JSON) |
| POST | `/share/api/sessions` | unchanged |
| GET | `/share/api/sessions/:id` | now returns full-timeline `items` + `pending_approvals` (admin shape) |
| POST | `/share/api/sessions/:id/archive` | unchanged |
| POST | `/share/api/sessions/:id/unarchive` | new |
| DELETE | `/share/api/sessions/:id` | new |
| GET | `/share/api/partial/sessions?filter=&includeArchived=` | server-rendered rail fragment (admin `ui.ListRow` markup; existing endpoint upgraded) |
| GET | `/share/api/questions` | unchanged shape (pending approvals) |
| POST | `/share/api/sessions/:id/approvals/:questionId` | `{decision, message}` (unchanged; reject reason works) |
| POST | `/share/api/sessions/:id/approvals/:questionId/cancel` | new |
| POST | `/share/api/chat` | SSE; frames now match `/api/chat` (`token` not `delta`); `meta` kept |

### Server (`apps/server/domain/agents`)
| Route | Handler |
|---|---|
| `POST /api/share/agent/sessions/:id/unarchive` | `UnarchiveSession` |
| `DELETE /api/share/agent/sessions/:id` | `DeleteSession` |
| `POST /api/share/agent/sessions/:id/approvals/:questionId/cancel` | `CancelQuestion` |
| `GET /api/share/agent/sessions/:id` | full-timeline transcript |
| `POST /api/share/agent/stream` | adds `question` + `citations` frames |

### DOM contract (share host-local)
Keep the existing share ids: transcript `#share-messages`; rail list `#share-session-list` (partial URL `/share/api/partial/sessions`); archived filter `#share-archived-filter`; composer `#share-input` / `#share-send` / `#share-stop`; chat log `#share-chat-log`; header `#share-agent-name` / `#share-agent-description`; email gate + terminal + `#share-first-load` unchanged. Removed: `#share-typing` (typing renders in the assistant bubble) and `#share-approvals` (approvals render inline in the transcript).

## Risks / Trade-offs

- **Raw agent tool I/O on an anonymous surface** — deliberate, documented. Existing guardrails (rate limit, budget, CSP, scoped session visibility) are unchanged.
- **Two changes touch the share capability spec** — coordinated in one PR; the `agent-public-share` edit only removes the contradicting clause.
- **Run markers / reasoning exposed** — accepted as full parity.
