## ADDED Requirements

### Requirement: Muted text and icon foreground comes from a scale

Muted text and icon emphasis in gateway-owned sources SHALL come from a single small scale of `--color-muted-*` tokens derived from `--color-base-content`, rather than arbitrary per-call-site opacities.

#### Scenario: Muted foreground uses scale steps

- **WHEN** the gateway's own templates, Go sources, JS, and `app.css` foreground rules are searched for muted content-opacity utilities
- **THEN** only the documented scale steps (`muted-subtle` 25%, `muted-faint` 40%, `muted` 50%, `muted-strong` 65%, `muted-bright` 80%) and the full `--color-base-content` token are used, and the previous spread of near-identical `text-base-content/N` opacities is gone

#### Scenario: The scale tracks the theme

- **WHEN** the brand theme's `--color-base-content` token changes
- **THEN** every muted text and icon follows it, because each `--color-muted-*` step is expressed as a mix of `--color-base-content` toward transparent

#### Scenario: go-daisy upstream muted classes are out of scope

- **WHEN** a muted class is emitted by a vendored go-daisy component (e.g. `ui.Eyebrow`, `nav.PageHeading`, `form.FormControl`)
- **THEN** it is allowed to keep its own `text-base-content/N` literal, tracked as go-daisy upstream work rather than migrated here

#### Scenario: Gateway mirrors of go-daisy runtime classes stay pinned

- **WHEN** a gateway-owned JS class toggle adds or removes the same class a vendored go-daisy component applies on its server render or runtime commit (e.g. the IconPicker label toggle in `webui/static/js/app.js`)
- **THEN** the gateway toggle pins the exact go-daisy class literal (`text-base-content/50`), not the muted-scale step (`text-muted`), so the toggle removes the class actually present rather than a scale token that was never applied
