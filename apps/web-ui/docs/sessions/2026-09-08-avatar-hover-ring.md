# 2026-09-08 — Account avatar hover ring

## Goal

Fix the topbar account-menu avatar trigger: hovering painted a rectangular "black box" (daisyUI `btn-ghost` background fill) behind the round avatar. Replace it with a subtle circular border ring around the avatar, with ~1px padding between the avatar's edge and the ring.

## Outcome

Done. Trigger hover now renders a low-contrast circular ring instead of the ghost-button box fill. Committed `57adf6b` and pushed to `master`.

## Decisions

- Route UI/interaction polish to @designer — styling + component feel is the designer lane; change stayed small but was purely visual.
- Drop `btn btn-ghost btn-sm rounded-btn px-1.5` entirely — the ghost hover was the box source; keeping `btn` while fighting its hover is more code than replacing it.
- Ring via Tailwind utilities on a `rounded-full p-px` wrapper — `p-px` gives the 1px breathing gap so the ring (box-shadow, drawn at the wrapper edge) lands just outside the circle; no custom CSS.
- Edit window confined to `gateway/account_menu.templ` lines 15-26 — a parallel session (account-identity-display) owned `gateway/webui/static/css/app.css` and other avatar-dropdown files; no overlap.
- Preserve trigger semantics (`tabindex=0`, `role=button`, `aria-label`, `data-testid`) and the dropdown structure below.

## Changes

- `gateway/account_menu.templ` — account-menu-trigger class string swapped from `btn btn-ghost btn-sm rounded-btn px-1.5` to `rounded-full p-px outline-none cursor-pointer transition hover:ring-1 hover:ring-base-content/25 focus-visible:ring-1 focus-visible:ring-base-content/45 active:ring-1 active:ring-base-content/30`. Ring: `base-content/25` hover (subtle, matches dropdown `border-base-content/10` token family), `/45` focus-visible (keyboard a11y), `/30` active. `outline-none` suppresses the default square browser outline that would clash with the circle. Avatar inner markup untouched.
- Regenerated `account_menu_templ.go` via `templ generate` (`*_templ.go` is gitignored / build-generated).

## Verification

- `PATH="/root/go/bin:$PATH" templ generate` — 24 updates, no errors.
- `PATH="/root/go/bin:$PATH" go build ./...` (from `gateway/`) — pass.
- `PATH="/root/go/bin:$PATH" go vet ./...` — pass.
- `PATH="/root/go/bin:$PATH" golangci-lint run ./...` — 0 issues.
- Commit `57adf6b` — only `gateway/account_menu.templ` staged (parallel session's `app.css` / `org_members_ui_templ.go` / spec WIP left untouched); pushed to `master`.

## Open questions / follow-ups

- Hover/focus ring not visually eyeballed in a browser (DevTools `localhost` resolves to the user's machine, not this server). Low risk (pure utility classes), but confirm the ring looks right live at next opportunity.
- No new task created for that check: the existing `verify-account-identity-browser` task already inspects the same topbar avatar trigger/dropdown region, so a separate task would duplicate it.
