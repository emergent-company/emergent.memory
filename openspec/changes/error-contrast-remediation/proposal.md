## Why

The chat session rail draws a status badge per conversation: a small (10px, weight 600) uppercase label in the bucket's accent colour, sitting on a translucent tint of that same accent. The rail row underneath is not a fixed colour — it is `--color-base-100` at rest, gains a 4% white veil on hover, and gains a 13% `--color-primary` tint when the row is the active conversation. The label's effective background therefore depends on row state, and measured with OKLCH→sRGB→WCAG it fails the WCAG 2.1 AA 4.5:1 minimum for small text in two of the four buckets:

| bucket | base row | hover row | active row |
|---|---|---|---|
| `needs_input` | 7.07 | 6.30 | 5.56 |
| **`failed`** | **3.96** | **3.57** | **3.17** |
| `running` | 6.02 | 5.37 | 4.75 |
| **`done`** | **3.65** | **3.46** | **3.25** |

The dock's pending-count caption fails the same way: `.dock-count-label` inherits its colour from `.dock-head` (`--color-base-content` at 45%), which measures 3.89:1 on the dock background. (The dock's `.dock-count` pill itself is fine — `--color-warning` / `--color-warning-content` = 9.91:1.)

The root cause is structural, not a stray value: on this dark theme a translucent accent tint lightens the chip, so an accent label must be *lighter* than the raw accent token to clear AA. `--color-error` (oklch 0.63) and the 45%-alpha grey are both below that bar.

A separate, wider finding is recorded as Scope B below but deliberately **not** implemented here: `--color-error` / `--color-error-content` is 3.52:1, which caps every solid error surface in the app.

## What Changes

- Lighten the `failed` rail-badge label only: `--color-error` → `color-mix(in oklab, var(--color-error) 60%, var(--color-base-content))`. The accent still drives the tint and the border; the count pill is untouched.
- Raise the `done` rail-badge label from 45% to 65% `--color-base-content`.
- Raise the dock pending caption (`.dock-head`, which colours `.dock-count-label`) from 45% to 65% `--color-base-content`.
- Apply each change to **both** copies of the stylesheet — the source `webui/css/app.css` and the runtime-injected `<style>` string in `webui/static/js/chat-components.js` — which must stay in sync.

Explicitly **not** changed: badge geometry (padding, radius, `min-width`/`height`, font-size), the bucket tint and border alphas, the label hue of the two already-passing buckets (`needs_input`, `running`), and the `.memory-rail-count` pill token pairs settled in PR #561.

## Capabilities

### New Capabilities

<!-- None: this extends the existing web-ui-css capability. -->

### Modified Capabilities

- `web-ui-css`: adds a contrast requirement for the small accent-on-tint labels and captions the gateway renders inside the chat rail and dock.

## Scope B — theme `--color-error` pair (recommendation only, not implemented)

`--color-error: oklch(0.63 0.2 26)` with `--color-error-content: oklch(0.97 0.01 26)` measures **3.52:1**, below AA for small text. Blast radius, by surface class:

- **Solid error surfaces** (error background + error-content foreground): ~18 `ui.ButtonError` destructive buttons (`btn-error`) across the gateway, `#voice-call-btn.voice-calling`, `alert alert-error` (`chat.templ`). All 3.52:1.
- **Tint chips with error text** (`bg-error/10 text-error`, `border-error/*`): icon tiles in `ui.templ`, `schedules`, `skills`, `org_context`, `project_settings`, `mcp_shares`, `mcp_nodes`, `agent_mcp_endpoint`, plus `blueprints` diff badges, `chat-components.js` error blocks, `.memory-run-status[data-state="failed"]`, `.memory-rail-badge[data-bucket="failed"]`. The text itself uses `--color-error` (4.55:1 on `base-100`), so these pass; the *tint* is decorative.
- **Error text on dark surfaces** (`text-error`): ~25 call sites. `--color-error` on `--color-base-100` = **4.55:1** — passes, and must not regress.

Recommended fix: change **only the foreground**, `--color-error-content: oklch(0.2 0.04 26)` — the same dark-ink value the theme already uses for `--color-warning-content` and `--color-primary-content`. That is a hue/chroma-preserving change (the error accent itself is untouched), lifts every solid error surface from 3.52 → **4.75:1**, leaves error-as-text at 4.55:1, and brings `--color-error-content` in line with the other `-content` tokens.

Rejected alternative: deepening `--color-error` (e.g. `oklch(0.55 0.2 26)` gives 4.91:1 on solid surfaces) drops error-as-text on `--color-base-100` from 4.55 → **3.26:1**, regressing ~25 passing surfaces. Any deepening that keeps error-as-text readable would need a chroma reduction — a real palette shift.

Both options repaint every destructive button in the app, so this is a brand-owner decision and is flagged, not applied. Because the recommendation keeps `--color-error` unchanged, the Scope A label mixes (which derive from `--color-error`) stay valid.

## Impact

- **CSS only** — no Go, templ markup, or JS behaviour change. Affected files: `apps/web-ui/gateway/webui/css/app.css`, `apps/web-ui/gateway/webui/static/js/chat-components.js`.
- The served stylesheet is regenerated by `task css`; `webui/static/css/app.css` is a gitignored build artifact.
- No currently-passing surface regresses: the two changed labels only gain contrast, and the dock pill and count pill are untouched.
