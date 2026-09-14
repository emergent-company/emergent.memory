# go-daisy component polish (Rail width, RelativeTime fallback, avatar parity)

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-godaisy-component-backport](../sessions/2026-09-10-godaisy-component-backport.md)

## What
Small cleanups surfaced while shipping go-daisy v0.11.0/v0.12.0:

- `layout.Rail` hardcodes the `11rem` nav column width — expose it as a size/param if a variant appears.
- `shared.RelativeTime` returns `"—"` on empty/parse error; the gateway keeps a thin local `relTime` wrapper to preserve its raw-`iso` fallback. Make the fallback configurable upstream, then drop the wrapper.
- `ui.AvatarFull` was adopted with two accepted visual deltas: the icon fallback uses proportional `size-[55%]` (the app used fixed `size-4`/`size-6`), and the `img` branch gains `object-cover w-full h-full`/`overflow-hidden` with a name-only `alt`. Revisit if pixel parity is wanted.

## Why
Tidies the shared component API and closes the small behavior deltas recorded as "accepted" during adoption.

## Depends on
- none

## Notes
- Upstream files: `components/layout/rail.templ`, `shared/reltime.go`, `components/ui/avatar.templ`; app wrapper: `gateway/ui.go` (`relTime`).
- No functional urgency — the deltas are visual-neutral in practice (square avatars, parse errors rare).
