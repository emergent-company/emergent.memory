# Adopt go-daisy-bundled htmx 4.0.0 final (drop self-hosted beta6 + compat overlay)

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-htmx-v4-migration](../sessions/2026-09-09-htmx-v4-migration.md)

## What

Make the gateway load htmx **4.0.0 final** instead of its self-hosted copy, and drop the now-redundant compatibility wiring:

1. Remove the self-hosted `gateway/webui/static/js/htmx.min.js` (`4.0.0-beta6`) and serve go-daisy's bundled `htmx.js` (4.0.0 final, via the vendored `staticfs`) from `ui.templ`.
2. Re-evaluate the manual v2-compat config in the shell (`htmx.config.implicitInheritance`/`noSwap`/`defaultTimeout`): go-daisy and alfred are now fully v4-native (v4 event names, `{ctx}` detail, core `innerMorph`), so the config overlay may no longer be needed — keep only what still has a real reason.
3. Grep-verify no v2-camelcase htmx tokens or `evt.detail.elt`-style reads remain anywhere (including go-daisy components rendered into alfred pages).
4. While here: re-bump the vendored go-daisy pin from `bbfb8a3` to the current master (`25bd7f1`+, combobox labels/$data) if alfred wants the newer components.

## Why

alfred self-hosts htmx because go-daisy once bundled `alpha8` with v2-era component code. go-daisy now bundles 4.0.0 final (PR #6) and all component listeners are v4-native, so the override, the duplicated htmx asset, and the compat config are legacy. Converging removes drift between the two htmx copies and picks up final-event semantics.

## Depends on

- go-daisy htmx v4 migration — done, merged (go-daisy master `bbfb8a3`), vendored into alfred at `61eac68`.

## Notes

- Every alfred listener that referenced v2 behavior was already migrated (sidebar, settings, project_settings listener fixed in this session). The remaining risk is behavioral: `implicitInheritance=true`/`noSwap 4xx,5xx`/`defaultTimeout=0` currently give v2 semantics; flipping to v4 defaults changes error-swap + timeout behavior app-wide — verify forms/toasts still behave.
- alfred `vendor/` is gitignored; a bump is a `go.mod`/`go.sum` change + local `go mod vendor`.
