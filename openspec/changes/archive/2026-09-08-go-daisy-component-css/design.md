## Context

The gateway currently serves two stylesheets: go-daisy's pre-compiled monolithic `app.css` (473KB, embedded via `staticfs`, referenced in `ui.templ` and `auth_ui.templ`) and its own Tailwind build (185KB). go-daisy is a separate Go module (`github.com/emergent-company/go-daisy`) whose source lives in its own repo. The gateway's Tailwind (`webui/css/app.css`) already compiles a dark-only brand theme with an unlayered `:root` override to suppress go-daisy's light `nord` default from leaking.

Tailwind v4 `@source` scans plain-text source for literal class names; daisyUI 5 supports per-module `include`/`exclude` inside `@plugin "daisyui"`. Both are already available — nothing new to install.

## Goals / Non-Goals

**Goals:**

- Ship component-scoped CSS from go-daisy; consumer compiles only what it uses.
- Eliminate the monolithic 473KB bundle and the fixed 275-icon safelist.
- Remove the theme-leakage workaround.
- Keep the change incremental and reversible (thin-wrapper before codegen).

**Non-Goals:**

- Rewriting go-daisy components to drop daisyUI classes (defeats the library's purpose).
- Building a full CSS-in-Go runtime/evaluator.
- Auto-detecting the component set from the Go import graph (deferred to a later phase, see Decisions).

## Decisions

**D1 — Consumer-side compilation via daisyUI `include` + `@source`, not a new CSS format.**
go-daisy stops shipping a pre-compiled `app.css`. The gateway declares the daisyUI component modules it uses via `@plugin "daisyui" { include: … }` and `@source`s go-daisy's component source so Tailwind generates used utilities/icons on demand.
*Alternatives considered:* per-component runtime CSS injection (runtime overhead, no build-time pruning); shipping a JS-style `@import` graph (no bundler in the Go toolchain). Rejected.

**D2 — Co-locate CSS per component group + a registry manifest in go-daisy.**
go-daisy keeps self-hosting (its `head/dependencies.templ` + `staticfs/static/css/app.css` stay for the library's own gallery/examples and other consumers), but its custom CSS is extracted out of `assets/app.css` into package-grouped source files under `components/`, and `assets/app.css` imports them. go-daisy publishes a `component package → { daisyUI modules, CSS files }` registry so consumers (and tooling) can resolve a component set.
*Alternative:* full per-component CSS files for all ~90 components. Rejected — disproportionate for a few KB of custom CSS; package grouping keeps it complete and low-risk.

**D3 — `go mod vendor` for a stable `@source` path.**
Tailwind can scan outside the project, but a Go module-cache path is non-reproducible. Vendoring go-daisy and `@source "./vendor/github.com/emergent-company/go-daisy"` makes the build deterministic.
*Alternative:* `@source` the module cache directly. Rejected — breaks across machines.

**D4 — Static, literal class strings become a go-daisy contract.**
Tailwind scans source as text; `fmt.Sprintf`-assembled class names are invisible. go-daisy components must emit complete literal class strings; anything dynamic goes through `@source inline("…")` safelists.
*Alternative:* ship a runtime CSS registry. Rejected — defeats build-time pruning.

**D5 — Thin-wrapper now, codegen later.**
Phase 2 ships the manual `include` list + `@source` + co-located CSS. Phase 3 (optional) adds a `go-daisy css` tool that walks the consumer's Go import graph to derive the component set automatically — Go imports are statically resolvable, which is this ecosystem's one advantage over JSX.
*Rationale:* the manual list is ~80% of the win with no new infra; codegen is a separable follow-up.

**D6 — Remove the unlayered `:root` theme override once the gateway stops loading go-daisy's CSS.**
The current workaround in `webui/css/app.css` exists only to beat go-daisy's light `nord` default when both stylesheets are loaded. Once the gateway serves only its own dark-only build, no competing theme exists, so the override can be removed.

## Risks / Trade-offs

- **[A component is rendered but its class isn't picked up by `@source`]** → loses styling. Mitigation: a usage audit + a full UI regression pass over every page; keep the go-daisy `<link>` behind a rollback until verified.
- **[daisyUI `exclude` list drops a needed module]** → component styles vanish. Mitigation: audit produced the used-module set; verify the unused sentinel is truly absent; regression pass.
- **[Vendoring adds repo weight]** → `vendor/` checked in. Mitigation: acceptable for determinism; revisit if it grows unwieldy.
- **[Icon/utility missed by scanner]** → blank icons. Mitigation: `@source inline("…")` safelist for any go-daisy icon used via non-literal data; the icon set must be audited.

## Migration Plan

1. **Phase 2a (go-daisy)** — extract custom CSS to co-located package files, publish the registry, remove the consumer icon safelist, keep the (slimmer) self-hosted `app.css`; commit + push go-daisy.
2. **Phase 2b (gateway)** — update `go.mod` to the new go-daisy, vendor it, rewrite `webui/css/app.css` with `@source` go-daisy + `@import` its CSS files + daisyUI `exclude`, remove the `<link>` and the theme workaround, regenerate `webui/static/css/app.css`. Full UI regression pass.
3. **Phase 3 (optional)** — codegen tool to derive the module set from the Go import graph.

Rollback: revert the gateway to the go-daisy monolithic `<link>`; go-daisy's self-hosted `app.css` remains shippable for other consumers.

## Open Questions

- Which daisyUI modules to `exclude` — resolved by the audit (task 1): gateway renders ~45 of ~70, so the unused sentinel set is the difference.
- Whether to check in `vendor/` or rely on a local `replace` during dev — resolved by D3 (vendor).
