## Why

On the web UI chat page, an hx-boosted in-page navigation (clicking the sidebar
"Chat" link or the rail's "New chat" link) leaves the composer dead: typing +
Send does nothing and the browser falls back to a native `GET /chat?` full
reload. The session-rail resize grip is also unbound ("cannot resize sessions
panel"). A fresh full page load of `/chat` works.

The chat page's client scripts were emitted INSIDE the swapped fragment
(`#chat-root`), so htmx re-executed them in-fragment, racing the swap. `chat.js`
boots only on `DOMContentLoaded` (or immediately when the document is already
loaded); when htmx re-executed it during the swap, its `init()` read the STALE
pre-swap `#chat-root` (already `data-ready="1"`) and returned early — then htmx
replaced it with a fresh, uninitialized `#chat-root`. The intended design (stated
in `chat.js`'s comments: "Registered once here (not in init())", "shell
re-renders #chat-root on HTMX sidebar swaps, so init() is re-runnable") was never
wired: the scripts were globally loaded in `ui.templ` but also duplicated in the
fragments, and the re-init hook on swap was missing.

## What Changes

- The chat client scripts (`chat-components.js`, `chat-stream.js`, `chat-host.js`,
  `chat.js`) load ONCE from the shell (`ui.templ`), never inside an
  htmx-swapped fragment. `chat.js` is added to the shell's global script block
  immediately after `chat-host.js` (order matters — `chat.js` reads
  `MemoryChatComponents`, `MemoryChatStream`, `MemoryChatHost` at module scope).
- `chat.templ` and `runs.templ` no longer emit those four script tags inside
  `#chat-root`. The voice scripts (only when `voiceEnabled`) and the inline
  todo-toggle bridge remain.
- `chat.js` re-inits on `htmx:after:swap` (init is idempotent per `#chat-root`
  via the `data-ready` guard), so any in-place navigation to `/chat` re-binds the
  composer and session-rail grip.

## Capabilities

### New Capabilities

- `web-ui-client-bootstrap`: page client scripts load once from the shell; a
  component whose DOM is replaced by an hx-boosted swap re-initializes on
  `htmx:after:swap`; the chat composer and session-rail grip are live after any
  in-place navigation to `/chat`.

### Modified Capabilities
<!-- none -->

## Impact

- `apps/web-ui/gateway/ui.templ` — add `chat.js` to the global script block.
- `apps/web-ui/gateway/chat.templ` — drop the four in-fragment script tags.
- `apps/web-ui/gateway/runs.templ` — drop the same four in-fragment script tags.
- `apps/web-ui/gateway/webui/static/js/chat.js` — add the `htmx:after:swap`
  re-init listener.
- `apps/web-ui/gateway/runs_ui_test.go` — stop asserting the run page emits the
  script tags; assert it does not.
- `apps/web-ui/gateway/ui_test.go` — shell owns `chat.js` exactly once; the
  `ChatPage` fragment does not.
- `apps/web-ui/tests/e2e/specs/sessions/chat-bootstrap-ui.spec.ts` — new
  regression spec (mutations project) proving a boosted nav to `/chat`
  re-initializes the composer and rail grip without starting an LLM run.

No behaviour change for the voice scripts (same in-fragment race, tracked as a
separate follow-up).
