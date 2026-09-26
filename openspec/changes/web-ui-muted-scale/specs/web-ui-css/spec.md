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
