## Why

Shared component library adoption is low. A broad audit of the gateway's authored `.templ` sources finds ~180 ad-hoc markup sites across 34 of 45 view files — the bulk raw `@ui.Badge` renders, hand-rolled `card card-border` shells, and unwrapped `<table>` blocks that duplicate components already in the `components` package. The dev component gallery (`web-ui-component-gallery`) can only see authored component definitions and their generated call graph — it cannot see the inline markup that *should* have been a shared component, so these gaps are invisible to the person reviewing the gallery. `web-ui-component-conventions` already promises "no raw-span status badges remain" and "no inline copies remain", but there is no tooling that surfaces where those promises are broken.

The detector in this change targets a **conservative, high-confidence subset** of that audit: raw markup that literally re-declares a shared component's wrapper class (`badge`, `card`/`card-border`, `btn`, `toggle`, `modal`/`dialog`) in authored `.templ` source. Duplication expressed through a *different* component call (e.g. `@ui.Badge` where a `StatusBadge` is intended) is already visible in the gallery's generated dependency graph and is left to the follow-up migration; the detector does not attempt it. This change adds detection and surfacing only: it does not migrate any call site.

## What Changes

- Extend the existing component-graph generator (`cmd/componentgraph`, run via `//go:generate`) with an **adoption scan** over the authored `.templ` sources (not the generated `*_templ.go`, which loses the source line mapping), driven by a hand-maintained signature map of the wrapper classes each shared component owns (`btn`, `badge`, `card`/`card-border`, `toggle`, `modal`/`dialog`).
- Emit a **generated, compiler-checked findings file** (`devgallery_adoption_gen.go`) listing one `AdoptionFinding{File, Line, Marker, Owners}` per matched raw-marker line, deterministically.
- Add a **runtime reader** (`devgallery_adoption.go`) that surfaces the findings through an allowlist (`adoptionAllowlist`, keyed by view basename and marker) of accepted raw sites and a `<!-- adoption:ignore -->` escape hatch for legitimate carve-outs.
- Add an **"adoption gaps" section** to the component gallery (`adoptionGapSection` in `devgallery.templ`) that lists each view's findings, showing the marker, line, and owning component(s) for every finding.
- Add a **staleness test** (mirroring `devgallery_test.go`) that regenerates the findings and fails when the committed file differs, so the detector cannot silently drift.

Scope is **detection and surfacing only**. Migrating any ad-hoc site to its shared component is explicitly out of scope for this change and is tracked as a separate follow-up.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-ui-component-conventions`: the convention rules gain a verification surface — the gallery SHALL report per-view ad-hoc markup that raw-redeclares a shared component's wrapper class, generated deterministically from authored sources, excluding slot-owned markup (tables) with an allowlist/ignore escape hatch, guarded by a staleness test.

## Impact

- `apps/web-ui/gateway/cmd/componentgraph/` — the generator is extended (not replaced): `signatures.go` (new), `main.go` (`scanAdoption`/`renderAdoptionSource`/`-adoption-out`), `adoption_test.go` (new).
- `apps/web-ui/gateway/devgallery_adoption_gen.go` (new, generated) — `AdoptionFinding` records.
- `apps/web-ui/gateway/devgallery_adoption.go` (new) — `adoptionGaps`, `adoptionAllowlist`, the `<!-- adoption:ignore -->` escape hatch, and the gap-aggregation runtime.
- `apps/web-ui/gateway/devgallery.go` (modified) — handler passes `adoptionGaps()` into the page.
- `apps/web-ui/gateway/devgallery.templ` (modified) — `adoptionGapSection`.
- `apps/web-ui/gateway/devgallery_test.go` (modified) — staleness + rendering tests.
- Tests: `cmd/componentgraph/adoption_test.go`, gateway `devgallery_test.go`, and a Playwright e2e assertion that the gallery renders findings.
- No changes to the server, CLI, iOS app, database, or any public API.
