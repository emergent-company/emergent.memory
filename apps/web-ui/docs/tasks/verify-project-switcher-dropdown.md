# Verify project switcher dropdown layout

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-project-switcher-dropdown](../sessions/2026-09-08-project-switcher-dropdown.md)

## What

Manually (browser) or via e2e verify the project-switcher dropdown menu against
a **many-project** account, and confirm:

- The list renders as a **single vertical column** (no second column, no
  horizontal scroll) once the project count exceeds the `max-h-96` cap.
- The **New project / New org** footer stays **pinned to the bottom** while the
  list scrolls vertically behind it, with a solid background (scrolled projects
  don't show through).
- The dropdown **width fits the longest project name** (short names floor at
  `min-w-80` = 20rem), capped by `max-w-[calc(100vw-2rem)]` on narrow screens.
- Footer links hover **changes the label only** (no background highlight), the
  two links are **evenly split** with the vertical divider centered.
- Org header rows: label **left-aligned**, cogwheel **right-aligned**, same line.

## Why

The original bug (multi-column + horizontal scroll + footer at list end) only
manifests with many projects; the fix was validated against the user's account
but there is no scripted or reproducible check for the regressions.

## Depends on

none

## Notes

- The switcher lives in `gateway/auth_ui.templ` (`projectSwitcher`).
- daisyUI `.menu` is `flex-flow: column wrap`; the fix relies on `flex-nowrap` +
  `overflow-x-hidden` + `overflow-y-auto` and the `menu-title` opt-out on the
  footer row. Any e2e should assert the rendered classes or the visual layout.
