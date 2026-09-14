# hx-boost: audit JS-handled form submits under #main-content

**Status:** done
**Created:** 2026-09-10
**Source:** [2026-09-10-scenario-chat-journey](../sessions/2026-09-10-scenario-chat-journey.md)

## What

Audit every `<form>` rendered inside the hx-boosted `<main id="main-content">`
(`gateway/ui.templ`) whose submit is handled in JS, and add `hx-boost="false"`
where the boost swap would intercept or double-handle the submit.

## Why

The chat composer (`#chat-form`) was silently intercepted by htmx boost:
submitting triggered a `#main-content` swap with a fresh GET of the page, which
reloaded mid-turn and wiped the streaming assistant bubble — a fully successful
agent chat turn never rendered. Fixed by `hx-boost="false"` on `#chat-form`
(gateway PR #23). Other JS-submitted forms may share the same class of bug.

## Depends on

- Gateway PR #23 (`fix/chat-form-opt-out-hx-boost`) merging.
- Related but distinct: [boost-context-stale-shell](boost-context-stale-shell.md)
  (stale shell chrome, not intercepted submits).

## Notes

- Server-side PRG forms (provider config, blueprint install, object create) are
  *meant* to be boosted — only forms whose submit is handled in JS need the opt-out.
- Grep `gateway/*.templ` for `<form>` elements referenced from
  `addEventListener("submit", …)` (e.g. `chat.js`, `app.js`) or otherwise submitted
  from JS.

## Resolution

Audited 2026-09-10 — **no further instances**. JS-handled submits found:

- `#chat-form` (`chat.js`) — inside the boosted `#main-content`; **was** intercepted
  and is fixed (`hx-boost="false"`, gateway PR #23).
- Side-panel composer (`sidepanel.js`) — rendered outside `<main>` (after `</main>`
  in `ui.templ`), so not in the boosted subtree.
- `#agent-form` (`app.js`, delegated `document` submit) — rendered inside the
  `hx-boost="false"` modal `dialog` (`gateway/ui.templ`), so opted out.
- Org-delete form (`org_context.templ`) — already `hx-boost="false"`.

No inline `hx-on` submit handlers exist. Remaining forms are htmx-driven
(`hx-post`/`hx-confirm`) or native PRG forms, which are *meant* to be boosted.
