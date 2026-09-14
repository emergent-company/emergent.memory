# go-daisy codegen for the CSS module set

**Status:** done
**Created:** 2026-09-08
**Source:** [2026-09-08-webui-perf-css](sessions/2026-09-08-webui-perf-css.md)

## What

Build the optional Phase-3 tool for go-daisy: a `go-daisy css` generator that walks a
consumer's Go import graph of go-daisy component packages and emits the daisyUI
`include`/`exclude` list (+ co-located CSS files) for their Tailwind build.

## Why

Today the gateway's daisyUI `exclude` list is hand-maintained from a static audit of
class tokens; it drifts as components change. Go imports are statically resolvable, so the
"used components" question is answerable at build time — the natural extension of the
`components/css/registry.go` added in `868b5d4`.

## Depends on

- go-daisy `868b5d4` (`components/css/registry.go`, `custom.css`).
- The archived OpenSpec change `go-daisy-component-css` (design.md Phase 3).
- Nothing in alfred.

**Resolved:** 2026-09-08 — lean version shipped as `cmd/godaisy-css` + `css.ModulesFor`/`CSSFilesFor` (go-daisy `eff4bb8`): prints the daisyUI-module union + co-located CSS for a package set, registry-backed, so the list is derived instead of hand-maintained. The fuller import-graph walker was deemed unnecessary; the consumer passes its go-daisy package set explicitly.

## Notes

- Lives in the go-daisy repo, not alfred.
- Registry entries are static-analysis-derived today; a codegen step could also refresh them.
