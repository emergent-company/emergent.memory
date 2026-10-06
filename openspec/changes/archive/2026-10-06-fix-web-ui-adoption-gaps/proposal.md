## Why

The component gallery's adoption-gap detector (`web-ui-component-adoption-detector`, merged
2026-10-05) surfaces authored `.templ` markup that raw-redeclares a shared component's wrapper class.
That change was detection-only by design: "Migrating any ad-hoc site to its shared component is
explicitly out of scope ... tracked as a separate follow-up." This is that follow-up.

On `main` the detector reports **62 findings across 18 view files**: 27 `btn`, 17 `card`/`card-border`,
9 `badge`, 4 `toggle`, 3 `modal-action`, 2 `alert`. Until they are closed the section is noise — a
reviewer cannot tell a genuine regression from the known backlog, and the "no raw-span status badges"
/ "no inline copies remain" promises in `web-ui-components` stay unverified.

Three of the 62 are **false positives**: the `modal-action` findings are children of an already-adopted
`@ui.Dialog(...)`. `ui.Dialog` owns its shell (`modal`/`modal-box`/`modal-backdrop`/`dialog`) but never
the caller's action row, yet `signatures.go` still lists `modal-action` for `Dialog`. The signature map
must match the component's real contract.

The remainder are genuine. Most map cleanly onto components that already exist and support the needed
shape (`ui.Button` renders `<a>`/`<label>`/`<button>`; `ui.CardRaw` takes the card and body classes;
`form.ToggleInput` is a bare toggle). A small residue cannot adopt without changing rendered output —
link-styled badges (`ui.Badge` has no `href`), rich-child alert banners (`ui.Alert` has no children
slot), a `<form class="card ...">`, and a border-only row. Those are accepted through the detector's
existing allowlist, each with a documented reason, and filed upstream for a future go-daisy extension.

## What Changes

- **Detector signature fix.** Drop `modal-action` from the `Dialog` signature (the caller owns the action
  row); keep it for `SecretRevealModal`, which renders its own. Add `ToggleInput` (go-daisy
  `form.ToggleInput`) as an owner of the `toggle` marker. Regenerate the findings file.
- **Button adoption (27).** Migrate every `btn` site to `ui.Button`: `<button>` sites via `Variant`/`Size`/
  `Shape`/`ExtraClass`/`Attrs`, `<a>` sites via `Type: ButtonTypeLink`, the join-group `<label>` via
  `Type: ButtonTypeLabel`. htmx/`data-*`/`aria-*` wiring stays in `Attrs`; app classes in `ExtraClass`.
- **Card adoption (14).** Migrate `card card-border` shells to `ui.CardRaw` (which already emits
  `card bg-base-100` + `card-body`), passing the existing card/body classes so output is unchanged.
- **Badge adoption (7).** Migrate raw `<span class="badge ...">` to `ui.Badge` / `components.StatusBadge`.
- **Toggle adoption (4).** Migrate bare `<input class="toggle ...">` to go-daisy `form.ToggleInput`.
- **Allowlist + upstream (7).** Record the 2 link-badges, 2 rich-child alerts, the form-card, and the
  border-only row in `adoptionAllowlist` with reasons; file upstream go-daisy issues for a `Badge` href
  and an `Alert` children slot.
- **Regenerate + verify.** Findings regenerate to zero visible gaps (allowlisted sites suppressed);
  gallery renders the empty state; unit + e2e + lint green.

No component behaviour, API, schema, or route changes. Every adoption preserves rendered output, ids,
`data-testid`, `aria-*`, and htmx wiring.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-ui-component-conventions`: the findings requirements are tightened — the signature map must reflect
  each component's real owned wrapper (a dialog does not own its caller-supplied action row), the bare
  toggle primitive is tracked, and the tracked ad-hoc markup backlog must be closed: every remaining
  finding is either adopted or a documented allowlist entry.

## Impact

- `apps/web-ui/gateway/cmd/componentgraph/signatures.go` — signature correction (`modal-action` removed
  from `Dialog`; `ToggleInput` added).
- `apps/web-ui/gateway/cmd/componentgraph/adoption_test.go` — signature/expectation updates.
- `apps/web-ui/gateway/devgallery_adoption_gen.go` — regenerated.
- `apps/web-ui/gateway/devgallery_adoption.go` — `adoptionAllowlist` entries with reasons.
- 18 authored `.templ` view files under `apps/web-ui/gateway/` — the adoptions.
- No server, CLI, connector, iOS, database, or public-API change.
