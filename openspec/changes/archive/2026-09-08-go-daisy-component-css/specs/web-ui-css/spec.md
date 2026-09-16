## Purpose

Defines how the Memory gateway compiles and serves CSS so it ships only the go-daisy components, utilities, and icons it actually renders, without a monolithic pre-compiled bundle or theme leakage.

## ADDED Requirements

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
