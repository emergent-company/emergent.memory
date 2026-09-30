# Converge onto go-daisy-bundled htmx (self-hosted asset already on 4.0.0 final)

**Status:** partially done — self-hosted asset upgraded to 4.0.0 final; go-daisy convergence deferred (optional).
**Created:** 2026-09-09
**Source:** [2026-09-09-htmx-v4-migration](../sessions/2026-09-09-htmx-v4-migration.md)

## What

The self-hosted `gateway/webui/static/js/htmx.min.js` is now **4.0.0 final** (was `4.0.0-beta6`; upgraded in place, sha256 `e484d917…`). The v2-compat overlay in `ui.templ` (`implicitInheritance:true`, `noSwap:[204,304,"4xx","5xx"]`, `defaultTimeout:0`) was **retained** — the 4.0.0 defaults did not change, so the overlay still supplies the v2 semantics the app relies on. A regression guard (`TestHTMXAssetIsVersion400`) prevents silently re-vendoring a beta.

Remaining (optional, separate refactor):

1. Serve go-daisy's bundled `htmx.js` (4.0.0 final, via the vendored `staticfs`) from `ui.templ` instead of the self-hosted copy, dropping the duplicated asset.
2. Re-evaluate the compat overlay for removal only if/when the app migrates to v4-native defaults (error-swap + timeout behavior changes app-wide — see Notes).
3. Grep-verify no v2-camelcase htmx tokens or `evt.detail.elt`-style reads remain (including go-daisy components rendered into alfred pages).
4. Optionally re-bump the vendored go-daisy pin from `bbfb8a3` to current master if newer components are wanted.

## Why

The self-hosted asset was the 4.0.0-beta6 build that predated the final release; upgrading it in place picks up final 4.0.0 semantics (including `allowEmptySwapAfterOOB` replacing the removed `defaultSwapEmpty`) with minimal churn. The remaining drift — two htmx copies (self-hosted + go-daisy's bundled `htmx.js`) — is a convergence nicety, not a correctness fix.

## Depends on

- go-daisy htmx v4 migration — done, merged (go-daisy master `bbfb8a3`), vendored into alfred at `61eac68`.

## Notes

- Every alfred listener that referenced v2 behavior was already migrated (sidebar, settings, project_settings listener fixed in this session). The retained overlay (`implicitInheritance=true`/`noSwap 4xx,5xx`/`defaultTimeout=0`) gives v2 semantics; flipping to v4 defaults changes error-swap + timeout behavior app-wide — verify forms/toasts still behave before dropping it.
- alfred `vendor/` is gitignored; a go-daisy bump is a `go.mod`/`go.sum` change + local `go mod vendor`.
