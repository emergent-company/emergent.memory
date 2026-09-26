## Why

PR #854 ("web-ui audit remediation — component single-sourcing, CSS/vendor hygiene, UX consistency")
proposed a large spec-driven remediation of the gateway web UI (`apps/web-ui/gateway`, Go templ + HTMX +
daisyUI 5). That PR was **closed as superseded** without archiving its delta specs, because two of its
workstreams were split out and merged separately:

- **#874** ("gateway UI component conventions", unit 1) took the *component-conventions / test-policy* part
  and created the `web-ui-component-conventions` capability (archived by #897).
- **#890** ("theme-token hygiene — radius, density, page background", unit 2) took the *CSS / vendor
  hygiene / theming* part and created the active `web-ui-css-theme-hygiene` change. It also **reversed** one
  of the original `web-ui-css` claims (the `/static/*` mount is intentional and stays).

But **seven delta specs from #854 were never carried into a live change and now have no home**:

| Capability | Kind | What it still requires |
|---|---|---|
| `settings-navigation` | MODIFIED + ADDED | the settings rail becomes the navigator for the whole Settings area and agrees with the sidebar group |
| `project-settings-ui` | MODIFIED + ADDED | in-context save failure, autosave in-flight/failure feedback, confirmed override removal |
| `inline-form-saving` | MODIFIED | field-level indicators are additive to (not a replacement for) the toast contract |
| `blueprint-gallery` | MODIFIED | pack removal routed through the shared destructive confirmation |
| `web-ui-navigation` | NEW | one page-header idiom per page class, kicker↔sidebar-group mapping, command-palette coverage |
| `web-ui-accessibility` | NEW | keyboard-reachable rows, 44 px touch targets, per-field validation errors, non-colour-only status |
| `web-ui-copy` | NEW | canonical empty-state and error headings, no internal detail in copy, one name per concept |

None of these is a new feature; all of them are accumulated cross-page drift in the existing gateway UI.
Each gets more expensive the longer it sits — the destructive-action gaps are the sharpest end (a pack
"Remove" and an override "Remove" currently act with no confirmation).

## What Changes

**Phase 1 — Settings information architecture** (`settings-navigation`). Make the settings sub-navigation
rail the navigator for the whole Settings area: every Settings-group destination renders the rail, the six
project-setting sections stay reachable (nested under Project), and the rail entries become byte-identical
to the sidebar Settings group (settling the placement of Approvals and MCP sharing).

**Phase 2 — Project settings feedback and confirmed destructive actions** (`project-settings-ui`,
`inline-form-saving`). Add in-context failure reporting for project-info saves, an in-flight + failure
surface for the silent `hx-swap="none"` autosave (additive to the existing toast contract), and route agent
override removal through the shared confirmation.

**Phase 3 — Blueprint pack-removal confirmation** (`blueprint-gallery`). Route pack removal through the
shared destructive confirmation, closing the gap where it currently removes on plain activation.

**Phase 4 — Navigation cues and command-palette coverage** (`web-ui-navigation`). One header idiom per page
class, a kicker→sidebar-group mapping that makes the kicker vocabulary deterministic, and a command palette
derived from the sidebar groups so it covers every destination.

**Phase 5 — Accessibility floor** (`web-ui-accessibility`). Keyboard-reachable row targets, 44 px
touch-target minimums for icon-only row actions, per-field validation errors, and non-colour-only status.

**Phase 6 — Copy voice** (`web-ui-copy`). Canonical "No `<thing>` yet" empty-state and "Couldn't load
`<thing>`" error headings, no internal implementation detail in user copy, and one name per concept.

## Capabilities

### New Capabilities

- `web-ui-navigation` — page-level navigation cues: header idiom per page class, the kicker vocabulary, and
  command-palette coverage. (The settings rail stays with `settings-navigation`.)
- `web-ui-accessibility` — keyboard reachability, touch-target size, per-field validation feedback, and the
  non-colour-only status rule.
- `web-ui-copy` — voice rules for user-facing strings.

### Modified Capabilities

- `settings-navigation` — rail becomes the whole-Settings-area navigator and agrees with the sidebar group.
- `project-settings-ui` — in-context failure, autosave in-flight/failure feedback, confirmed override removal.
- `inline-form-saving` — clarifies that accompanying field indicators are additive to the toast contract.
- `blueprint-gallery` — pack removal confirmed through the shared destructive dialog.

### Dropped from this change (superseded — not silently omitted)

- **`web-ui-css`** — superseded by **#890** (`web-ui-css-theme-hygiene`): vendor pin + `vendor_consistency_test.go`,
  theme-driven radius, centralised density, page background, and colour tokens. #890 also reversed the
  original "SHALL NOT serve the bundle over HTTP" claim (the `/static/*` mount is intentional and stays).
- **`web-ui-component-conventions`** (the conventions / test-policy part of the original
  `web-ui-components` delta) — superseded by **#874** and archived by **#897**; the capability now lives at
  `openspec/specs/web-ui-component-conventions/spec.md`.

## Scope Boundary

- **Web UI gateway only.** No server, CLI, connector, or iOS change; no API, schema, or migration change.
- **Component single-sourcing (consolidation) is out of scope.** The `web-ui-components` ADDED requirements
  from #854 (copy-button single-sourcing, one toast dispatch, form-control routing, one card/field/header
  component, one shared tone type, and the remaining repeated primitives) are a separate workstream. Some of
  #854's `web-ui-components` MODIFIED requirements already landed on `main` via the go-daisy adoption track;
  the residual consolidation requirements are **not** carried here and remain a candidate follow-up change.
- **CSS/theming is out of scope.** Already owned by #890 (`web-ui-css-theme-hygiene`).
- **go-daisy upstream work is out of scope.** #854's cross-repo dependency list (secret-reveal back-port,
  meta rows, `ToggleInput`/`Select` extensions, `CodeBlock` copy, radius literals) belongs in
  `emergent-company/go-daisy`, which has no OpenSpec root. This change adds no new go-daisy dependencies.

## Impact

- **Web UI gateway only.** Primary files: the settings templates (`project_settings.templ`, the settings rail
  and sidebar definition), `blueprints.templ`, `ui.go` (sidebar groups), `spotlight.templ`, `app.css`, and
  the ~30 page templates referenced by the phases.
- **Assertions are the contract, not the class list.** Phase work adds contract assertions (ids,
  `data-testid`, `aria-*`, `hx-*`) and narrows or removes class-only assertions rather than re-pinning them.
- **Verification gate per phase:** `templ generate ./...`, `go build ./...`, `go vet ./...`, `go test ./...`,
  and `task lint` from `apps/web-ui/gateway`, plus `task e2e:test` and a manual browser pass for UI-visible
  phases.

## Provenance

This is a **fresh change**, not a resurrection of #854. #854 was closed as superseded; its two merged
workstreams are #874 (unit 1) and #890 (unit 2). This change is **unit 3** in that sequence, re-homing the
seven still-pending deltas onto current `main`. The per-delta verdict (still-pending vs superseded) is
tabulated in the PR body and in the archive note.
