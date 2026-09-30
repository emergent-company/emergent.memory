# UI / htmx / Alpine — patterns & unification

Authoritative reference for the web-ui gateway's rendering, interactivity, and
testing conventions. Complements `gateway/AGENTS.md` (Go/templ style + the
component-layer rules in `openspec/specs/web-ui-component-conventions/spec.md`).
Everything here is grounded in shipped code; paths are relative to
`apps/web-ui/gateway/` unless stated. Verify before asserting — line numbers
drift.

---

## 1. Page shell & composition

The full document is `appShell` (`ui.templ:27`). It renders the HTML shell
(dark theme, sidebar, topbar, scrollable main) rather than go-daisy's
`layout.PageFull` so the app can self-host htmx v4 with v2-compat config applied
immediately after load (`ui.templ:23-26`).

**Boosted `<main>`** (`ui.templ:120-128`):

```html
<main id="main-content" hx-boost="true" hx-target="#main-content"
      hx-swap="innerHTML" hx-history-elt="true">
```

`hx-history-elt="true"` is load-bearing: htmx v4 history restores select the
`[hx-history-elt]` out of the response and outerSync-swap it. Without it the
fallback targets `document.body`, which replaces the `<link>`/`<script>`s that
live in `<body>` (`ui.templ:140-183`) — see the full rationale at `ui.go:201-207`.

**Server-side page dispatch** is `Server.page` (`ui.go:208`): `render.IsPartial(r)`
→ `render.RenderPartial(w, r, partialWithTitle(title, content))` (`ui.go:210-217`),
else the full `appShell`. `partialWithTitle` (`ui.go:143`) prefixes partials with
a `<title>` so htmx keeps `document.title` in sync on boosted nav. `render.IsPartial`
checks the v4 `HX-Request-Type: partial` header and treats history-restore as
full (`render/render.go:40-49`). `pageTitle` (`ui.go:34`) joins segments with
" — " + brand; `pageTestID` (`ui.go:48`) derives the stable `page-<slug>`
`data-testid` from the first segment.

**Dominant per-page idioms:**

| Idiom | Where | Notes |
|---|---|---|
| `layout.Container(layout.ContainerLG, nil)` | `ui.templ:234`, `agent.templ:21` | go-daisy L0, `max-w-6xl` |
| `layout.Rail(nav, nil)` | `agent.templ:26` | sub-page rail (detail pages) |
| `pageHeader(kicker,title,subtitle)` | `ui.templ:601` | list-page header; plain Go func so children forward into `nav.PageHeading.Actions` |
| `detailHeader(crumbs,kicker,title,subtitle,opts…)` | `ui.templ:659` | detail-page header; `TitleLeading` routes through `detailHeaderWithTitleLeading` (`ui.templ:698`) |
| `pageError(heading, err)` | `ui.templ:208` | shared fetch-failure state (`ui.Alert`) |
| `ui.Section(title, SectionProps)` | `agent.templ:232` | titled panel + empty state |
| `components.PanelCard` | `components/panel.templ:13` | `ui.CardRaw` + `memory-density-comfortable` |
| `components.TableCard` | `components/table.templ:10` | `ui.CardRaw` + `memory-density-flush` (zero padding, table meets edges) |
| `components.MetaRow` / `MetaGrid` | `components/meta.templ:18,74` | definition-list cells; single label-style definition |
| `components.EmptyDash` | `components/meta.templ:55` | muted em-dash for absent values |
| `components.SubNav` / `SubNavItem` | `components/nav.templ:10,17` | sub-page nav rail |
| `components.ListRow` | `components/list.templ:10` | chat/session row |
| `components.StatusBadge` | `components/badge.templ:24` | L1 domain adapter |

`pageHeader`/`detailHeader` are **Go functions, not templ components**, because
their children must forward into `nav.PageHeading`'s `Actions` slot and templ
components clear children context before the body runs (`ui.templ:596-600`,
`ui.templ:651-658`).

**Known divergence — `objectDetailHeader`** (`objects.templ:829`) is the one
detail header not built on `detailHeader`. It hand-rolls breadcrumbs + title row
because it needs a type chip/badges *before* the `h1` and `nav.PageHeading`'s
`Leading` slot renders above the breadcrumbs, not inline. The **convention** is
`detailHeader` (or `pageHeader` for lists); `objectDetailHeader` is the
documented exception, not a second pattern. Prefer extending `detailHeader`
opts over forking another header.

---

## 2. PRG (post/redirect/get) flow contract

Canonical mutation shape: `form → POST → 303 → GET with a query flag →
flashToasts`. Flags are `?updated=1`, `?created=1`, `?deleted=1` (etc.); errors
ride `?err=<urlencoded>`.

**Helpers** (all in `ui.go` unless noted):

| Helper | Where | Does |
|---|---|---|
| `flashToast(kind, message)` | `ui.go:163` | inline script pushing into the Alpine `#toast-container` queue, or stashing `sessionStorage["memory-toast"]` for full loads |
| `flashToasts(flashMsg, flashErr)` | `ui.templ:1029` | renders error-then-success toast pair; both skipped when unset |
| `flashError(c)` | `ui.go:185` | decodes `?err=`; `"1"` is the legacy generic-failure sentinel |
| `redirectWithError(c, path, err)` | `ui.go:178` | 303 to `path?err=<urlencoded>` |
| `render.RedirectAfterMutation(w,r,path)` | go-daisy `render/render.go:129` | `HX-Redirect`+200 for HTMX requests, 303 otherwise |

The toast queue itself is `ui.ToastQueueWithProps` in the shell (`ui.templ:80`),
Alpine-owned (queue add, dismiss timer, countdown, hover-pause — `app.js:36-45`).

**Rule for redirects:**

- **`render.RedirectAfterMutation`** — use when the mutation can be reached by an
  htmx/boosted submit (single-row htmx submit, or any form inside the boosted
  `<main>`). It gives htmx an `HX-Redirect` and plain browsers a 303.
- **`redirectWithError`** — the error path: carry the real message so the target
  page surfaces it (no state change).
- **`c.Redirect(http.StatusSeeOther, …)`** — only for a guaranteed full-page
  (non-boosted) form post.

**Current inconsistency:** settings/shell-chrome flows use
`RedirectAfterMutation` — assistant (`settings_handlers.go:552`), providers
(`settings_providers.go:455`, with the shell-chrome rationale at
`settings_providers.go:418-422`), project delete/transfer/restore
(`org_context.go:516,580,611`). Several CRUD handlers instead emit raw 303s —
`schedules.go:149,164,173`, `org_context.go:368,434,450`. Raw 303 is correct for
non-boosted posts but would force a full navigation if the same form were ever
boosted. Unify on `RedirectAfterMutation` for any mutation a boosted submit can
reach; keep raw 303 only where the form is provably full-page.

Why shell chrome matters: a boosted 303 swaps only `#main-content`, leaving
elements outside it (topbar "Assistant" button, sidebar provider-warning badge)
stale until a manual refresh — hence `RedirectAfterMutation`'s `HX-Redirect`
full reload (`settings_handlers.go:534-541`, `settings_providers.go:418-422`).

---

## 3. htmx ownership in components

From `gateway/AGENTS.md:75-78` (verbatim policy):

> The page/integration site owns `hx-target`, `hx-swap`, `hx-trigger`,
> `hx-include` — pass them through `Attrs`. A component may own only
> self-contained client hints that are meaningless without it
> (`data-copy-target`, `data-dialog-autoopen`). A component whose whole contract
> is a swap region may ship a default but must expose an override.

Consequences:

- **Boosting is shell-wide** (`hx-boost` on `<main>`, `ui.templ:120-128`), not
  per-link. Opt a subtree out with `hx-boost="false"` where a full navigation is
  wanted (or the surface has third-party JS that can't survive a swap — see
  `render.ForceReload`, `render/render.go:156`).
- **Partial rendering** is decided by `render.IsPartial` + the `page()` dispatch
  (`ui.go:208-217`); pages never hand-branch on `HX-Request`.
- **Flash/title handling** lives in the page/dispatch layer, not components:
  title via `partialWithTitle` (`ui.go:143`), flash via `flashToasts` in the page
  body (`agent.templ:324`).
- **`hx-preserve`** (`render/render.go:149`) is available for elements that must
  survive any swap, but is discouraged in favor of rendering those surfaces
  outside `#main-content` (sidepanel, object preview — `ui.templ:132-138`).

---

## 4. Interactive-widget inventory

Legend: L0 = go-daisy primitive, L3 = page-local helper (see `gateway/AGENTS.md:46-52`).

| Widget | Implementation | Layer | Open/closed state rendering |
|---|---|---|---|
| Visibility listbox | `agent.templ:440-517` + `agent_visibility_dropdown.go:14` | L3 | Alpine `x-data`/`x-show`; closed state rendered **server-side** `style="display:none"` (`agent.templ:491`) |
| Icon picker | `ui.IconPicker` (go-daisy `icon-picker_templ.go:50` + runtime); used `ui.templ:484`, `agent.templ:403` | L0 | go-daisy runtime `commit()` + hidden input |
| Color picker | `ui.ColorPicker` (go-daisy `color-picker_templ.go:89` + runtime); used `ui.templ:494`, `agent.templ:416` | L0 | text input is the real form control; swatch synced |
| Checkbox pickers (skills/delegation) | `checkboxPicker` (`ui.templ:865`); used `ui.templ:508,525`, `agent.templ:591,629` | L3 | `ui.SoftBox` of checkbox rows; visible + hidden empty note |
| Model select | `modelSelect` (`ui.templ:976`) | L3 | native `<select>` with optgroups + synthetic "(current)" option |
| Native `<dialog>` | `ui.Dialog` (go-daisy `dialog_templ.go:42`, emits `<dialog>`); `agentFormDialog` (`ui.templ:458`) | L0 | `showModal()` via `MemoryApp.openDialog` |
| Confirm dialog | `ui.ConfirmDialog` (go-daisy `confirm-dialog_templ.go:33`); `deleteConfirmDialog` (`ui.templ:569`) | L0 | `MemoryApp.openDeleteConfirm` |
| Dialog auto-open | `components.DialogAutoOpen` (`components/dialog.templ:9`) → `data-dialog-autoopen` | L1 | `app.js` opens on load + after htmx swap (`app.js:671-678`) |
| Toasts | `ui.ToastQueueWithProps` (`ui.templ:80`) + `flashToast` (`ui.go:163`) | L0 | Alpine queue; add/dismiss/countdown |
| Command palette | `ui.CommandPalette` (`spotlight.templ:48`, go-daisy `command-palette_templ.go:49`) | L0 | `⌘K` modal; rows htmx-nav + uncheck `#spotlight-toggle` |
| Popover (row action menus) | `ui.Popover` (go-daisy `popover_templ.go:75`); `org_context.templ:80,445-457` | L0 | `window.goDaisy.popover.closeAll()` runtime |
| CSS-only dropdowns | `ui.Dropdown` (go-daisy `dropdown_templ.go:31`); project switcher `auth_ui.templ:180`, account menu `account_menu.templ:13`, search-mode split button `objects.templ:201` | L0 | focus/focus-within; no JS, no truthful `aria-expanded` |
| Search-mode split button | `objects.templ:182-230` + `objectsSearchModes` (`objects.go:448`) | L3 | `role="menuitemradio"` + `aria-checked`; submits `name="mode"` |
| autogrow + char-count | `app.js:488-567` (`data-autogrow`, `data-char-counter`) | vanilla `data-*` | delegated on `document`, re-run on `htmx:after:swap` |
| Schema property rows | `app.js:591-614` (`data-add-property` / `data-remove-property`) | vanilla `data-*` | clones a `<template>` row |
| Generic dialog open/close | `app.js:573-589` (`data-dialog-open` / `data-dialog-close`) | vanilla `data-*` | `showModal()` / `close()` |
| Blueprint edit-gate confirm | `app.js:619-640` (`data-requires-confirm`) | vanilla `data-*` | intercepts submit, re-submits on `close` confirm |
| Chat surface | `chat.js` (page client) | JS-heavy | SSE stream, bubbles, session rail |
| Assistant side panel | `sidepanel.js` (drawer outside `#main-content`, `sidepanel.templ:14`) | JS-heavy | `window.MemorySidepanel` API, localStorage transcript |
| Object preview drawer | `object-preview.js` (`object_preview.templ:17`) | JS-heavy | HTMX fetch of `/objects/:id/preview` |
| Public share page | `share-agent.js` | JS-heavy | self-contained, no shell/htmx |
| Voice call | `voice.js` | JS-heavy | LiveKit UMD client |
| Usage charts | `usage-charts.js` | JS-heavy | dependency-free SVG bars |

The visibility listbox's `style="display:none"` is **not** redundant with
`x-show`/`x-cloak` — see §6.

---

## 5. Alpine vs `data-*` vs go-daisy — policy

**Current reality.** Alpine is bootstrapped once in the app shell
(`@alpine.Tag()`, `ui.templ:181`) and then drives **two** custom widgets:

- The visibility listbox (`agent.templ:440-517`) — Alpine `x-data`/`x-show`/`x-cloak`.
- The org bulk-delete toolbar (`org_context.templ:289,310,330`) — Alpine `x-data`/`x-show`.

The toast queue (`ui.templ:80`) is Alpine-owned but comes from go-daisy
(`ui.ToastQueueWithProps`). Everything else is:

- **`data-*` delegation** in `webui/static/js/app.js` (one `document`-level
  listener per concern, survives htmx swaps), **plus**
- **go-daisy L0 runtime scripts** (icon picker, color picker, popover,
  command palette).

**Rule for the next widget:**

1. Prefer **go-daisy L0** if it exists (icon/color picker, dropdown, dialog,
   popover, palette, disclosure, toast queue). Do not reimplement.
2. For a genuinely custom control, prefer **`data-*` delegation in `app.js`**
   over Alpine — it is the established, single-file pattern, and its `data-*`
   attributes are what the unit contract tests assert on
   (`components/components_test.go`).
3. Reach for **Alpine only when the widget needs reactive local state
   keyboard/ARIA listbox semantics** (the visibility listbox case), and then
   follow §6's closed-state rule strictly.

---

## 6. htmx v4 + Alpine: the same-id attribute-restore trap

Tracked as **issue #1267** (open). Root cause, precisely:

htmx v4's `innerHTML` swap **copies then restores attributes for same-`id`,
same-tag elements** (the stable-id CSS-transition path). Across a swap, htmx
morphs the previous DOM element's attributes onto the incoming element, then
restores the incoming element's original attributes after settle. The incoming
server fragment has no inline `display`, so the restore **drops the inline
`display: none`** that Alpine's `x-show` had set on the previous DOM. Result: the
swapped-in element ends `display: block` while Alpine's data still says hidden.

Three facts that make this a trap:

- **`x-cloak` does NOT protect the post-swap window.** Alpine removes `x-cloak`
  via microtask; htmx's restore snapshot is pre-Alpine, so the restored
  attributes are captured before Alpine ever touched the fresh node.
- **Alpine's MutationObserver does auto-init swapped subtrees**, but it runs
  during htmx's settle window — after the attribute restore that stripped the
  inline `display`.
- The **`id` is what opts the element in**: only same-`id` elements enter the
  restore path.

**Rule for authors** — an Alpine-toggled element must satisfy one of:

- **(a)** have **no `id`**; or
- **(b)** render its closed state server-side — `style="display:none"` — so the
  restored attribute set already carries the hidden state (this is what the
  visibility listbox does, `agent.templ:475-491`, fixed in #1265); or
- **(c)** use `x-if`/`<template>` so nothing exists in the DOM when hidden
  (immune to attribute restore).

**Adopted fix: the `hx-alpine-compat` extension (#1275).** The official
extension is **now adopted** — vendored at
`webui/static/js/hx-alpine-compat.js` and loaded synchronously in `ui.templ:180`
after `htmx.min.js` (`ui.templ:147`) and before the deferred Alpine bootstrap
(`@alpine.Tag()`, `ui.templ:181`). It covers **all htmx swaps** by deferring
Alpine's mutations for the duration of the swap
(`window.Alpine.deferMutations()`) and flushing them once it settles
(`flushAndStopDeferringMutations()`), so Alpine always initialises the swapped
subtree against the settled (post-restore) DOM. **This is what fixes #1267** —
upgrading htmx from `4.0.0-beta6` to `4.0.0` final did **not**: the upstream
`__startCSSTransitions` / same-id restore path is byte-identical in both builds.

**Other platform-level options** (not adopted): `htmx.config.morphIgnore =
["style"]`; or `hx-swap="innerHTML settle:0"` to skip the settle-window restore.
The extension is preferred because it fixes the whole class rather than one
attribute. `ui.templ:155-159` still sets only `implicitInheritance`, `noSwap`,
and `defaultTimeout`.

---

## 7. JS widget architecture (known debt)

Static scripts load in `ui.templ:180-189`:

| File | Responsibility |
|---|---|
| `hx-alpine-compat.js` | htmx `alpine-compat` extension (#1275): defers Alpine mutations across every htmx swap so Alpine initialises against the settled DOM — see §6 |
| `app.js` | app-level client: `data-*` delegation, toasts, agent form/delete dialogs, autogrow/char-count, schema-editor dialogs, `MemoryApp` facade (`app.js:681-688`) |
| `chat-transport.js` | shared no-DOM chat helpers: `parseSSE`/`streamSSE` wire transport + `agentIconifyClass` normalization (loaded on the shell **and** the public share page) |
| `chat-components.js` | shared renderers: tool-call badges, thinking blocks, A2UI surface cards — "exactly one implementation to keep in sync" |
| `chat-stream.js` | shared streaming engine (`createEngine`); pure helpers + stateful engine taking a host `ctx` |
| `chat-host.js` | shared host glue: composer helpers, drag-resize width, timeline sort, SSE dispatcher, gateway POST |
| `chat.js` | `/chat` page client (SSE, persistence, session rail) |
| `sidepanel.js` | global assistant drawer (outside `#main-content`; localStorage restore) |
| `object-preview.js` | inline object-reference preview drawer |
| `share-agent.js` | public share page — deliberately self-contained (no shell/htmx/engine); loads only `chat-transport.js` |
| `usage-charts.js` | dependency-free SVG bar charts |
| `voice.js` | LiveKit voice-call client (wires elements rendered elsewhere) |

`chat-components.js`, `chat-stream.js`, and `chat-host.js` were extracted from
the `chat.js`/`sidepanel.js` pair precisely to kill duplication — see the
"used to duplicate" note in `chat-host.js:4-7` and the "pixel-identical … one
implementation" headers in `chat-components.js:3-4` / `chat-stream.js:4-5`.

**Known debt:** card rendering (thinking/approval/question cards, session-rail
rows) and the composer bar are still partially duplicated between `chat.js` and
`sidepanel.js` — `chat-host.js` absorbed the composer helpers and drag-resize,
but the two surfaces each keep their own element/state accessors and
page-local callbacks (see `chat.js:92-102` vs `sidepanel.js:71-81`). Treat any
new chat-surface work as an opportunity to move shared markup/behaviour into
`chat-host.js` rather than adding a third copy.

---

## 8. Testing

- **Render/unit tests assert the contract — not class strings.** ids,
  `data-testid`, `aria-*`, `hx-*`, `data-copy-target`, `data-dialog-autoopen` —
  and omission where it matters (`gateway/AGENTS.md:84-89`). Class order is not
  the contract; a theme tweak must not fail a unit test. The few exceptions
  (tool-group disclosure bases, confirm-dialog icon circle) are documented in
  the component-conventions spec.
- **Hermetic js-dom gate** — `apps/web-ui/tests/e2e/js-dom.config.ts` targets
  only `specs/js/`, starts no server, and loads the shipped
  `chat-components.js` verbatim into a bare page via `page.addScriptTag`
  (`specs/js/a2ui-wiring.spec.ts`). It guards the #1216-class defect (a runtime
  `ReferenceError` a syntax check can't see). Run
  `npx playwright test --config=js-dom.config.ts` (or `task e2e:js`); the same CI
  job runs `node --check` over every static script first
  (`tests/e2e/README.md:332-379`).
- **Playwright e2e** — `apps/web-ui/tests/e2e/playwright.config.ts` (session mode,
  real gateway + Zitadel + memory API) owns visual/interaction coverage: navigation,
  mutations, live-LLM scenarios. Not CI-runnable; `tests/e2e/README.md` documents
  the coverage, the `data-testid` convention (`page-<slug>` anchor), and the
  gaps tracked in `openspec/changes/web-ui-e2e-coverage/`.
