## ADDED Requirements

### Requirement: Page client scripts load once from the shell

A page's client scripts that drive a component whose DOM is replaced by an
htmx-swapped fragment SHALL be loaded exactly once from the full page shell
(`ui.templ`), never from inside the swapped fragment. The chat client scripts
(`chat-components.js`, `chat-stream.js`, `chat-host.js`, `chat.js`) SHALL be
emitted in the shell's global script block in dependency order — `chat.js` after
`chat-host.js` (and its transitive dependencies), because `chat.js` reads
`MemoryChatComponents`, `MemoryChatStream`, and `MemoryChatHost` at module scope.
A fragment template (`chat.templ`, `runs.templ`) SHALL NOT re-emit those scripts:
htmx re-executing an in-fragment script during a swap races the swap, so the
script's init may observe the stale pre-swap DOM and leave the swapped-in element
uninitialized.

#### Scenario: Chat scripts are emitted once by the shell

- **WHEN** the gateway renders the full page shell
- **THEN** the shell emits `/assets/js/chat.js?v=<hash>` exactly once
- **AND** it is emitted after `chat-host.js`

#### Scenario: The chat fragment does not re-emit the scripts

- **WHEN** the gateway renders the `/chat` page fragment (`ChatPage`) or the run
  transcript fragment (`RunPage`)
- **THEN** neither fragment emits `src="/assets/js/chat.js"`

### Requirement: A swapped component re-initializes on htmx:after:swap

A component whose root element is replaced by an hx-boosted navigation (htmx
swaps `#main-content` in place and does not fire `DOMContentLoaded`) SHALL
re-initialize on the `htmx:after:swap` event. The re-init hook SHALL be
registered in the component's once-loaded client script (not in the swapped
fragment), and the component's `init()` SHALL be idempotent per root element via
a `data-ready` guard so a re-init never double-binds listeners.

#### Scenario: Boosted navigation re-initializes the swapped root

- **WHEN** the user navigates in place to `/chat` (e.g. clicks the rail's
  "New chat" link)
- **THEN** the swapped `#chat-root` is re-initialized: it carries `data-ready="1"`
- **AND** the composer submit handler is bound (no native `GET /chat?` full reload)

### Requirement: Chat composer and rail grip are live after in-place navigation

After any in-place navigation to `/chat`, the chat composer and the session-rail
resize grip SHALL be live. The composer MUST accept input and submit without the
browser falling back to a native full reload, and the session-rail resize grip
(`#chat-rail-resize`) MUST be present and bound.

#### Scenario: Boosted nav to chat keeps the composer and rail grip live

- **WHEN** the user reaches `/chat` via an hx-boosted navigation
- **THEN** the URL remains `/chat` (no `?` suffix, no native reload)
- **AND** `#chat-root` has `data-ready="1"` with the `#chat-rail-resize` grip present
- **AND** `#chat-input` is visible and usable
