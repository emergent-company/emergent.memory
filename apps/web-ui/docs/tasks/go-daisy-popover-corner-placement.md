# go-daisy Popover: corner placement + synchronous positioning

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-org-projects-table](../sessions/2026-09-08-org-projects-table.md)

## What

Extend go-daisy's `Popover` component (`components/ui/popover.templ` + `go-daisy-popover.js`)
so it can open at a corner (e.g. bottom-left) and positions synchronously, then migrate the
org-landing row action menu back to it.

## Why

The org-landing row menu was hand-rolled on the native popover API because go-daisy's
`Popover` has two gaps:

1. Only single-axis placements (`top`/`bottom`/`left`/`right`), all centered — no
   bottom-left / start-aligned / end-aligned corners for right-edge action buttons.
2. Positions inside a `requestAnimationFrame` after `showPopover()`, so the menu paints at
   the default (centered/top-left) spot for a frame then jumps — visible to the user.

The hand-roll works but deviates from "every UI element is a go-daisy component"
(spec 04, D20). Fixing upstream restores that principle and gives every future table a
reusable corner-dropdown.

## Depends on

- none (upstream go-daisy change; see `godaisy-command-palette-keyboard` for the same
  fix-upstream-and-bump pattern).

## Notes

- go-daisy module pinned at `v0.9.1-0.20260907105153-04f43d9b664b` (see `gateway/go.mod`).
- Current hand-rolled implementation lives in `gateway/org_context.templ`
  (`[data-row-menu]` / `[data-row-menu-trigger]` / `[data-row-menu-content]` + the inline
  `<script>` under `window.__orgProjectsMenuInit`). It is the reference behavior to match.
- Corner math (bottom-left, flip-above, viewport clamp) is already written in that script —
  port it into `positionContent` with a `Placement`/alignment parameter.

## Progress (2026-09-09)

Upstream capability shipped: go-daisy `437a72d` adds `-start`/`-end` corner
placements on all four axes (`bottom-end` etc.) and positions **synchronously**
(no rAF flash); corner placements clamp instead of flip and render no arrow;
triggers track `aria-expanded`. Gateway pinned to
`v0.10.1-…437a72de6cd2`. No behavior change to existing pages (no gateway
consumer of go-daisy `Popover` yet).

**Remaining (browser-gated):** the org-landing row action menu migration to
`ui.Popover` shipped and merged (**PR #5**, `936fb98`): markup now uses
`ui.Popover(PopoverBottomEnd, Exclusive)` with `@ui.PopoverScript()` once;
`positionRowMenu`/aria script removed; transfer-dialog close uses
`goDaisy.popover.closeAll()`; e2e selectors moved to `data-gd-popover-*`.
CI (build/lint/test/vet/templ) green. **Outstanding:** visual pass + e2e run
in the browser (row menus open bottom-right of trigger, transfer flow,
delete-cancel keeps menu open), then flip done.
