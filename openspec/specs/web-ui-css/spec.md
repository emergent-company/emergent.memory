# web-ui-css Specification

## Purpose

Defines how the Memory gateway compiles and serves CSS so it ships only the go-daisy components, utilities, and icons it actually renders, without a monolithic pre-compiled bundle or theme leakage.

## Requirements

### Requirement: Styles for rendered components are complete

Every go-daisy component, utility class, and lucide icon the gateway renders SHALL have its styles present in the served CSS.

#### Scenario: Every page renders with full styling
- **WHEN** a user loads each page in the gateway (agents, objects, documents, schema, blueprints, skills, backups, sessions, usage, chat, settings, org/profile pages)
- **THEN** every rendered component, icon, and utility class is styled correctly, with no unstyled or partially-styled elements

#### Scenario: Icon used by a component is present
- **WHEN** a page renders a go-daisy component that uses a lucide icon
- **THEN** the icon's style rule is present in the compiled CSS and the icon renders

### Requirement: Unused component and icon styles are excluded

The compiled CSS SHALL NOT include styles for go-daisy components or icons the gateway does not use.

#### Scenario: Excluded component is absent from output
- **WHEN** the CSS build runs for the gateway's declared component set
- **THEN** the output does not contain style rules for components outside that set (e.g. daisyUI modules that are never rendered)

#### Scenario: Payload is smaller than the monolithic bundle
- **WHEN** the gateway's CSS is compiled with consumer-side compilation
- **THEN** the total compiled CSS is smaller than the previous go-daisy monolithic `app.css` plus the previous gateway `app.css`

### Requirement: No monolithic go-daisy bundle is served

The gateway SHALL NOT reference go-daisy's pre-compiled monolithic `app.css` in its HTML.

#### Scenario: Page HTML has no monolithic CSS link
- **WHEN** a user loads any gateway page
- **THEN** the document does not contain a `<link>` to go-daisy's `/static/css/app.css`

### Requirement: Brand theme is not overridden

The served CSS SHALL NOT leak a non-brand theme (such as go-daisy's light `nord` default) that changes the visual surface.

#### Scenario: Brand dark palette persists across navigation
- **WHEN** a user navigates across pages (including htmx partial swaps and full loads)
- **THEN** the computed theme tokens (e.g. `--color-base-100`) remain the brand dark values and no panel renders in a light theme

### Requirement: CSS build is deterministic

Given a fixed set of components, the CSS build SHALL produce a stable, reproducible output.

#### Scenario: Rebuild yields identical CSS
- **WHEN** the CSS build runs twice against the same declared component set and vendored go-daisy source
- **THEN** the two outputs are byte-identical

### Requirement: Chat rail badge labels clear WCAG AA in every row state

The label text of the chat session-rail status badge SHALL render at a contrast ratio of at least 4.5:1 against the badge's rendered background in every rail row state (base, hover, active) and for every bucket, without changing the badge geometry, the bucket tint and border alphas, or the token pair used by the badge's count pill.

#### Scenario: Failed label is legible in every row state
- **WHEN** a conversation has bucket `failed` and its rail row is in the base, hover, or active state
- **THEN** the label renders at ≥4.5:1 (measured 6.53 / 5.88 / 5.22), the accent still drives the tint and the border, and the count pill keeps its `--color-error` / `--color-error-content` pair

#### Scenario: Done label is legible in every row state
- **WHEN** a conversation has bucket `done` (or the empty fallback) and its rail row is in the base, hover, or active state
- **THEN** the label renders at ≥4.5:1 (measured 5.97 / 5.50 / 5.03)

#### Scenario: Already-passing buckets are unchanged
- **WHEN** a conversation has bucket `needs_input` or `running`
- **THEN** its label keeps its accent token and its measured ratio is unchanged (`needs_input` 7.07 / 6.30 / 5.56; `running` 6.02 / 5.37 / 4.75)

#### Scenario: The count pill is not part of this change
- **WHEN** the badge renders a pending count
- **THEN** `.memory-rail-count` keeps its per-bucket solid token pairs and geometry, and `background: currentColor` does not reappear

### Requirement: Dock pending-count caption clears WCAG AA

The dock's pending-count caption SHALL render at a contrast ratio of at least 4.5:1 against the dock background.

#### Scenario: Dock caption is legible
- **WHEN** the pending-work dock shows its "N pending" header
- **THEN** `.dock-count-label` renders at ≥4.5:1 (measured 6.79), while the `.dock-count` pill keeps `--color-warning` / `--color-warning-content` (9.91:1)

### Requirement: Badge stylesheet copies stay in sync

The rail-badge and dock-caption rules SHALL be identical in the source stylesheet and the runtime-injected stylesheet, since the injected copy is appended to `<head>` and wins at equal specificity.

#### Scenario: Both copies declare the same label colours
- **WHEN** `webui/css/app.css` and the injected `<style>` string in `webui/static/js/chat-components.js` are compared
- **THEN** the `failed` label mix, the `done` label alpha, and the dock caption alpha are the same in both
