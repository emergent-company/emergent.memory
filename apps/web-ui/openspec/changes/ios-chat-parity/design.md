## Context

iOS text chat runs over LiveKit: the client sends typed text via `session.send(text:)` (LiveKit `lk.chat`), the bridge worker (`alfred_bridge/worker.py`) turns that into `sess.generate_reply(input_modality="text")`, and the reply streams back as plain `.agentTranscript`. The web chat runs over HTTP `/api/chat` → SSE, which is why it can render tool chips, thinking blocks, and approval/question cards.

The bridge's `MemoryChatClient.stream()` already decodes the full memory SSE event set (`meta`, `token`, `mcp_tool`, `thinking`, `approval`, `error`, `done`), but `MemoryLLMStream._run()` forwards only `token` and drops `mcp_tool`, `thinking`, and `approval`. The memory backend also emits `ask_user` as an `mcp_tool` event; the gateway synthesizes a `question` event from it. So the rich data already exists on the wire — the worker discards it and iOS has no channel to receive it.

Memory's decision endpoints are `POST /api/projects/{projectID}/agent-questions/{questionId}/respond` (approve/reject or free-text answer + optional message) and `.../cancel` (`gateway/memory.go` `RespondQuestion`/`CancelQuestion`).

## Goals / Non-Goals

**Goals:**
- Parity of rendering: full markdown (code + highlight, tables, images, task lists, links), tool chips, thinking blocks, approval/question cards, typing indicator, stop, suggested prompts.
- Keep a single iOS transport (LiveKit) — no HTTP chat client, no new auth surface.
- Reuse the existing recorded-session tool-call components for live chips.

**Non-Goals:**
- Attachments / image / file upload (web also lacks it).
- Session rename / delete / search / pin (web also lacks it).
- Per-message timestamps, presence, per-message copy/edit/regenerate (web also lacks these).
- Voice-mode parity beyond text turns (voice already matches).

## Decisions

### 1. Markdown rendering: hand-rolled SwiftUI renderer over `swift-markdown`

Parse agent markdown with Apple's `swift-markdown` (AST, no UI, multiplatform) and render with a small SwiftUI view tree (headings, paragraphs, emphasis, inline + fenced code, tables, blockquotes, task lists, images via `AsyncImage`, links via `Link`). Use `Splash` (John Sundell) for fenced-code syntax highlighting.

- *Alternative A — `MarkdownUI` (gonzalezreal):* richer out of the box but its visionOS story is uncertain and it fights custom styling; the app targets iOS + macOS + visionOS.
- *Alternative B — keep `AttributedString(markdown:)`:* cannot do tables, task lists, or inline images, and has no syntax highlight — insufficient for parity.

The renderer is a pure function of markdown text → view, so it is unit-testable against fixture markdown (deterministic, per repo convention).

### 2. Transport: two new LiveKit text-stream topics

- `lk.chat.events` (worker → client): JSON events `tool_call`, `tool_result`, `thinking`, `approval`, `question`. Client renders known events, ignores unknown ones.
- `lk.chat.decision` (client → worker): JSON `{type: "approval"|"question", questionId, action|answer, message}`. Worker subscribes and forwards to memory.

- *Alternative A — switch iOS text chat to HTTP `/api/chat` (mirror web):* gives everything for free but requires the iOS app to talk to the gateway, reintroduce HTTP auth/SSE handling, and sever the chat from the LiveKit voice session. Rejected: it fragments the session lifecycle.
- *Alternative B — piggyback on `lk.transcription`:* conflates activity events with speech transcripts and breaks the user/agent identity logic. Rejected.

### 3. Worker forwards the events it already receives

Extend `MemoryLLMStream._run()` to forward `mcp_tool`, `thinking`, and `approval` events (currently dropped) onto `lk.chat.events`, and synthesize a `question` event from `ask_user` `mcp_tool` calls, mirroring `gateway/sse_markdown.go` `rewriteChatStream`. Forward raw `mcp_tool` events (tool, status, result) so iOS renders the same expandable chips the web shows; iOS maps `ask_user` to a question card (or the worker pre-synthesizes `question` — resolved in tasks, both keep the spec).

### 4. Decision response: worker proxies to memory, then resumes

When the client sends a decision, the worker calls memory's `agent-questions/{questionId}/respond` (or `cancel`) through `MemoryChatClient`, then resumes streaming by issuing a fresh `generate_reply` (a "continue" turn) so the paused run's output streams back to iOS. This mirrors the gateway's `answerQuestion`/`postDecision` + `waitForResume` flow.

### 5. Reuse the timeline tool components

Factor the `ToolsCard`/`ToolCallRow` detail views in `SessionDetailView.swift` so the live chat and the recorded timeline share one tool-detail rendering path (one source of truth for name/args/result/error display).

## Risks / Trade-offs

- [Approval pause breaks the livekit `generate_reply` lifecycle] → mirror the gateway resume flow; if memory's chat stream ends at the pause, resume via a fresh `generate_reply`; confirm exact stream behavior during implementation.
- [Worker needs `projectID` for the respond endpoint but only has `agentDefinitionId`] → obtain it from config or the `meta` event; confirm the source before wiring the respond call.
- [Event ordering vs token stream] → forward events on the same ordered stream; keep a tool chip "running" until its `tool_result` arrives.
- [Syntax highlight cost on large blocks] → highlight lazily on expand, or cap highlighted length; plain monospaced fallback.
- [visionOS rendering] → hand-rolled renderer avoids a third-party UI library's visionOS gaps; verify on iOS/macOS first, visionOS follows.

## Migration Plan

- Worker change is additive: new topics and forwarded events are ignored by older clients; no protocol break. The existing `lk.transcription` path is untouched.
- iOS change is additive: new message/event rendering and `lk.chat.decision`; older workers simply never emit `lk.chat.events`, so the chat degrades to today's transcript-only behavior.
- No data migration; recorded session history is unaffected.

## Open Questions

- Does memory's `/api/chat/stream` hold the connection open across an approval pause, or end the stream and resume only via the respond endpoint + a new turn? (Determines the exact resume strategy; both paths are covered above.)
- How does the worker obtain the `projectID` required by `agent-questions/{id}/respond` — from config, from the `meta` event, or from a non-project-scoped memory endpoint?
