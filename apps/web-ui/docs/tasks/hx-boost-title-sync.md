# Fix stale document.title on hx-boost partial swaps

**Status:** done
**Created:** 2026-09-08
**Source:** [2026-09-08-webui-perf-css](../sessions/2026-09-08-webui-perf-css.md)

## What

Boosted (in-page) navigation left `document.title` stale because partial
responses have no `<head>`. htmx extracts the first `<title>` from a swapped
fragment and applies it before the innerHTML swap.

## Resolution

`gateway/ui.go` `page()` now renders every partial (hx-boost + history restore)
through `partialWithTitle`, prefixing an HTML-escaped `<title>`; htmx applies it
to `document.title` and removes it before the swap. Verified by
`TestUIObjectPartialIncludesTitle` and the `object-create-ui` e2e (asserts the
dynamic key in the title after a boosted navigation). See
[2026-09-08-prg-toast-titles](../sessions/2026-09-08-prg-toast-titles.md).
