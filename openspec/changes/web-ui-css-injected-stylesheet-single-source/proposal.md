## Why

Issue #1026 tracks two CSS requirements that were **deferred** during the #890 theme-hygiene work and
exist only as unchecked task lines (§5.1/§5.2) inside that merged PR. This change resolves §5.1:
**"Runtime-injected styles are not a second stylesheet."**

`webui/static/js/chat-components.js` injected a runtime `<style>` (`ensureChatControlStyle`) that was a
byte-for-byte mirror of the chat run-control surface already defined in `webui/css/app.css` — live run
status, typed run markers, turn footers, copy affordances, the composer queue, the pending-work dock, the
session todo card, and the session-rail badge. The two copies were kept identical **by convention**, with
comments on both sides saying "keep this rule identical to the other copy". That is exactly the duplicate
stylesheet the requirement forbids: any change to one side silently desyncs the other, and the injected
literal `border-radius` values actually won the cascade (appended after `app.css`, unlayered) and so
overrode the theme tokens.

Measurement (§5.1) found 47 of the injected sheet's 84 selectors duplicated in `app.css`. All of them belong
to the chat-control sheet. The badge/thinking shell (`ensureBadgeStyle`) is a genuinely runtime-only subset
that `app.css` does not define, plus two documented `.memory-tool-chip[data-status]` border overrides, and
is not duplicated — so it stays.

## What Changes

- Delete `ensureChatControlStyle()` from `chat-components.js` and its only call site (`chat.js` boot), so
  `app.css` is the single source of truth for the chat run-control surface and the session-rail badge.
- Keep `ensureBadgeStyle()` — the only remaining injected sheet — which owns just the expandable-badge shell
  and shimmer that `app.css` deliberately does not define, plus the documented tool-chip status override.
- Update the `app.css` comments that described the injected copy (radius exception list, run-control header,
  the rail-badge `.hidden` note).
- Add a guard test that fails if an `app.css`-owned selector ever reappears as an injected rule.
- Replace the `web-ui-css` "Badge stylesheet copies stay in sync" requirement (which mandated the two-copies
  convention) with "Runtime-injected styles are not a second stylesheet".

Rendered-output change: the chat-control controls whose injected radii were literal (`.memory-copy-btn`,
`.memory-run-marker-model`, `.memory-queue-send/remove` at `.375rem`, `.memory-run-marker-failure` at
`.6rem`) now follow the theme tokens (`var(--radius-field)` = 0.25rem, `var(--radius-box)` = 0.5rem). That
is the intended outcome of #890's radius migration and is not visible to a user on the default theme, but
does make those controls follow a theme radius change.

## Capabilities

### Modified Capabilities

- `web-ui-css` — removes the "Badge stylesheet copies stay in sync" requirement and adds "Runtime-injected
  styles are not a second stylesheet".

## Scope

- **Gateway web UI only.** `webui/static/js/chat-components.js`, `webui/static/js/chat.js`,
  `webui/css/app.css`, one new guard test, and the OpenSpec delta.
- **Out of scope:** the muted-text/icon scale (issue #1026's second requirement, §5.2). It is a ~500-site
  migration whose rule admits no opacity outside the scale, so a partial migration would leave it
  unsatisfied. It stays a separate follow-up unit — see the deferred note below.

## Impact

- **Gateway files:** `chat-components.js` (delete `ensureChatControlStyle`), `chat.js` (drop one call),
  `app.css` (comment updates), `injected_stylesheet_test.go` (new guard).
- **No Go logic, API, schema, or migration change.**
- **Verification:** `templ generate`, `go build ./...`, `go test ./...`, `task css`, `task lint`, and
  `openspec validate --all --strict` from `apps/web-ui/gateway`.

## Deferred (out of this change)

- **Muted text/icon scale (§5.2)** — the second requirement in #1026. Defining a scale that 12+ ad-hoc
  opacities and a drifting icon-size spread must migrate to, under a "no opacity outside the scale" rule,
  is a ~500-site sweep. It belongs in its own unit; this change does not half-implement it.
