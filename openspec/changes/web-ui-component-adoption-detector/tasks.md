## 1. Signature map and generator

- [x] 1.1 Add `cmd/componentgraph/signatures.go` with a hand-maintained `componentSignatures()` map naming each shared component and the wrapper class(es) it owns (`btn`, `badge`, `alert`, `card`/`card-border`, `toggle`, `modal`/`dialog` + the `dialog` tag); raw `table` is deliberately NOT a marker (slot-owned), and the daisyUI primitives (`select`/`input`/`label`/`nav`/`dl`/`dt`/`dd`/`optgroup`) are excluded; verify `go build ./...`
- [x] 1.2 Guard the map with `TestSignaturesInverted` — every marker maps to at least one owner and no marker is owned by conflicting components; verify `go test ./...`

## 2. Scanner

- [x] 2.1 Extend `cmd/componentgraph/main.go` with `scanAdoption` walking the gateway-root authored `.templ` files (never the generated `*_templ.go`), matching wrapper class tokens (`class="..."`, `class={...}` incl. multi-line, `templ.Attributes{"class": ...}`) and element tags, recording `AdoptionFinding{File, Line, Marker, Owners}` with the authored file and line; skip `devgallery*.templ`, `*_test.templ`, `fixtures_test.templ`; apply the hard exclusions (no `table` marker, no daisy primitives); verify with scanner unit tests
- [x] 2.2 Assert the scanner is deterministic and reproducible in `TestScanAdoption` / `TestAdoptionDeterministic`; verify `go test ./...`

## 3. Generated findings file

- [x] 3.1 Emit `devgallery_adoption_gen.go` (a `package main` slice of `AdoptionFinding`, deterministic order by file then line, do-not-edit header) from the same `cmd/componentgraph` binary via a new `-adoption-out` flag; verify `go run ./cmd/componentgraph` then `go build ./...`
- [x] 3.2 Assert determinism: running the generator twice on unchanged source produces byte-identical output; verify in `TestAdoptionDeterministic`

## 4. Runtime reader

- [x] 4.1 Add `devgallery_adoption.go` with `adoptionGaps` (aggregates the generated slice per view, applying `adoptionAllowlist` and the `<!-- adoption:ignore -->` escape hatch); verify `go build ./...`
- [x] 4.2 `adoptionAllowlist` is keyed by view basename → markers to ignore, plus the `<!-- adoption:ignore marker... -->` comment escape hatch; verify with a unit test that an allowlisted marker is suppressed and an unlisted one surfaces

## 5. Gallery section

- [x] 5.1 Add `adoptionGapSection` in `devgallery.templ` (unexported, so the committed component graph does not churn), shown above the catalog with per-view findings (`file:line → marker → owners`); verify `templ generate` then `go build ./...`
- [x] 5.2 Pass `adoptionGaps()` from `uiComponentGallery` into `ComponentGalleryPage`; verify via `httptest` that a finding-bearing tree renders the section and a clean tree renders the empty state

## 6. Tests

- [x] 6.1 Add the staleness test (mirroring `devgallery_test.go`'s graph check) that regenerates findings to a temp `-adoption-out` and fails when the committed file differs; it does not require `templ generate` and MUST NOT skip when `vendor/` is absent; verify it passes on a regenerated tree
- [x] 6.2 Add coverage in `devgallery_test.go` for the allowlist/ignore escape hatch, the aggregation output, and the flag-off 404; verify `go test ./...`

## 7. e2e

- [x] 7.1 Add a Playwright assertion that the flag-on gallery page renders the adoption-gaps section (non-empty) under the existing dev-gallery harness config; verify it passes

## 8. Verification

- [x] 8.1 Run `templ generate`, `go run ./cmd/componentgraph`, `go build ./...`, and `go test ./...` from `apps/web-ui/gateway`; all succeed
- [x] 8.2 Run web-ui lint (`task lint` from `apps/web-ui`); web-ui jobs clean (repo-wide lint additionally hits a pre-existing, unrelated `connector.linux` date-bomb test)
- [x] 8.3 Run `openspec validate web-ui-component-adoption-detector --strict`; clean
- [x] 8.4 Run the gallery e2e (`task e2e:gallery` from `apps/web-ui`); passes

## Out of scope

Refactoring any ad-hoc site to its shared component is **out of scope** for this change and tracked as a separate follow-up. This change detects and surfaces gaps only; it changes no rendered view markup. The scanner intentionally does not detect duplication expressed through a different component call (e.g. `@ui.Badge` instead of `StatusBadge`) or slot-owned raw tables — those are in the follow-up migration backlog.
