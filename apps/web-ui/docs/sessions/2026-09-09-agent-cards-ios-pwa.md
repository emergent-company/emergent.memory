# 2026-09-09 — Agent cards + iOS PWA polish

## Goal
Redesign the Agents list from a data table to a mobile-friendly card grid (name + icon, chat-first), and add iOS "Add to Home Screen" PWA polish (splash screens, maskable icon, safe-area handling, standalone detection).

## Outcome
Done. Merged to `master` via PR #15 (`8da619a`).

- **Agents page** — table → responsive card grid (1/2/3 cols). Each card = bot icon + truncated name + right-aligned chat icon. Whole card opens `/agents/<id>`; the chat icon is the sole exception and deep-links to `/chat?agent=<id>`. Removed the model/tools/flow/visibility columns and the "With tools" stat; edit/delete affordances removed from cards (settings reachable from the dashboard).
- **iOS PWA** — 40 `apple-touch-startup-image` splash images (iPhone 8→16/17 + iPad, portrait + landscape), maskable 512 icon, manifest `short_name` + `purpose: maskable`, `env(safe-area-inset-*)` padding on chrome + chat composer, `data-standalone` detection (JS + `@media (display-mode)` CSS hook).
- **Reconcile** — swept concurrent lanes' pending work (connector MCP app OpenSpec + Go, docs sessions/tasks/specs, e2e env), resolved three doc merge conflicts, merged `origin/master`.

## Decisions
- **Cards over table for the agents list** — user wants a short, chat-first list showing only name + icon. → D31
- **Whole card → details; chat icon → chat** — user reversed the initial chat-first design after feedback (round 3 of the card work).
- **Bot icon tile, not initials avatar** — the initials avatar read as "strange"; reverted to the old `lucide--bot` `IconTile`.
- **Drop model/tools/flow/visibility from cards** — user explicitly wanted only name + icon.
- **iOS PWA via meta tags + `apple-touch-startup-image`, not the manifest** — iOS never reads manifest splash/display/orientation (research lane). → D32
- **No manifest `orientation`** — iOS ignores it, and locking portrait would hurt iPad/landscape.
- **Splash generator = throwaway Go script (stdlib bilinear)** — no ImageMagick/PIL on the host; stdlib `image/png` + a hand-rolled bilinear scaler.

## Changes
- `gateway/ui.templ` — `agentsTable`/`agentRow` → `agentCardGrid`/`agentCard`; dropped `agentsStats`; card carries `data-href="/agents/<id>"` + a real chat `<a>`.
- `gateway/ui.go` — removed dead `agentsWithTools`; briefly added then removed `agentAvatarTone` (initials avatar was reverted).
- `gateway/agent_ui_test.go` — reworked `TestAgentModelDisplay` case 5 + agents-route tests for the card grid (details `data-href` + chat `href`, no edit/delete).
- `gateway/webui/static/js/app.js` — widened the `[data-href]` click delegate (row → any element); added `data-standalone` PWA detection.
- `gateway/pwa.templ` — new `appleSplashLinks()` component (40 `<link rel="apple-touch-startup-image">`).
- `gateway/ui.templ` / `gateway/auth_ui.templ` — head: `@appleSplashLinks()` + manifest / apple-touch-icon links.
- `gateway/webui/css/app.css` — safe-area padding (topbar wrapper, sidebar, sidepanel, chat composer/rail, run transcript) + `@media (display-mode: standalone)` hook.
- `gateway/webui/static/manifest.webmanifest` — `short_name` + maskable icon entry.
- `gateway/webui/static/splash/*` (40 PNG) + `gateway/webui/static/icon-maskable-512.png` — generated assets.

## Verification
- `templ generate` — ✓ (complete)
- `go build ./...` (gateway) — ✓
- `go test ./...` (gateway) — ✓
- `golangci-lint run ./...` — ✓ 0 issues
- `go build ./...` + `go test ./...` (connector) — ✓ (swept lane's work)
- CI on PR #15 — build / lint / templ / test / vet all pass.

## Open questions / follow-ups
- The splash/maskable generator is a throwaway Go script in `/tmp` — not in the repo; needs a home under `tools/` so assets can be regenerated when the brand icon changes (see task).
- M4-era iPad splash sizes (2420×1668, 2752×2064) were skipped.
- `data-standalone` / `display-mode` hooks are unused infrastructure — no PWA-only styling consumes them yet.
- Splash shows only on cold launch (warm relaunch = iOS screenshot); the user must re-add to Home Screen to pick up new launch images.

## Tasks
- [pwa-splash-regenerator](../tasks/pwa-splash-regenerator.md) — commit the splash/maskable generator + add missing M4 iPad sizes
- [pwa-install-hint](../tasks/pwa-install-hint.md) — optional "Add to Home Screen" hint for iOS
