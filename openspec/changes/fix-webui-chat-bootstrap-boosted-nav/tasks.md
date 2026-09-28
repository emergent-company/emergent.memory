## 1. Load the chat scripts once from the shell

- [x] 1.1 Add `chat.js` to the global script block in `ui.templ`, immediately after `chat-host.js` (order matters: `chat.js` reads `MemoryChatComponents`, `MemoryChatStream`, `MemoryChatHost` at module scope).
- [x] 1.2 Delete the four in-fragment script tags (`chat-components.js`, `chat-stream.js`, `chat-host.js`, `chat.js`) at the end of `chatWorkspace` in `chat.templ`; keep the voice scripts (`voice.js` + livekit vendor, only when `voiceEnabled`) and the inline todo-toggle script, and leave a short comment explaining the chat scripts load once from the shell.
- [x] 1.3 Delete the same four in-fragment script tags inside `#chat-root` in `runs.templ`, dropping the now-unused `webui` import.

## 2. Re-init chat.js on hx-boosted swaps

- [x] 2.1 In `chat.js`, add `document.addEventListener("htmx:after:swap", init);` in the boot section next to the existing `window.addEventListener("resize", railGrip.onWindowResize)` / boot block, keeping the existing `DOMContentLoaded` / immediate boot. `init()` is already idempotent per `#chat-root` via the `data-ready` guard.

## 3. Go render-contract tests

- [x] 3.1 `runs_ui_test.go`: remove the four `src="/assets/js/…"` expectations from `TestRenderRunPage`; assert the run page does NOT contain `src="/assets/js/chat.js`.
- [x] 3.2 `ui_test.go`: add `TestChatScriptLoadedOnceFromShell` — the full shell (via `renderPageShell`) includes `/assets/js/chat.js?v=` exactly once, and `ChatPage(...)` output does NOT include `src="/assets/js/chat.js`.
- [x] 3.3 Grep the gateway tests for any other assertions pinning these script tags or exact chat-page HTML and update them (none found beyond `runs_ui_test.go`).

## 4. e2e regression (acceptance test)

- [x] 4.1 Add `apps/web-ui/tests/e2e/specs/sessions/chat-bootstrap-ui.spec.ts` (`-ui.spec.ts` → serial `mutations` project): create an agent via `createAgentViaModal`, `goto('/chat')` and assert `#chat-input` visible, click `[data-testid="new-chat"]`, assert the swapped `#chat-root` has `data-ready="1"` and `#chat-rail-resize` is present, assert the URL is still exactly `/chat` (no native `?` reload), and clean up the created agent.

## 5. Out of scope (reported, not fixed)

- [ ] 5.1 `voice.js` (+ livekit vendor) still load in-fragment in `chat.templ` (same in-fragment re-execution race). Separate follow-up.
