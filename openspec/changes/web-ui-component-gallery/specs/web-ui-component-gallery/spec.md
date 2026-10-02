## Purpose

A dev-gated component gallery for the gateway: it catalogs the reusable templ components the app is built from, reports what each one depends on, and renders each one in isolation with sample data so its visual and interactive behaviour can be reviewed and fixed.

## ADDED Requirements

### Requirement: Gallery catalogs reusable templ components

The gallery SHALL list the gateway's reusable templ components: those in the shared `components` package, reusable composites written in `package main`, and the go-daisy components those entries render. A `package main` composite SHALL be in scope only when it is reused across two or more page features or encapsulates an invariant, and the in-scope set SHALL be an explicit registry seed rather than inferred from a naming or visibility convention. "Reused across two or more page features" SHALL mean the generated used-by edges reach two or more distinct page components; page-local (L3) helpers SHALL be excluded by layer regardless of call count. Page-level templates and page-private helpers SHALL be excluded. Entries SHALL be grouped by component layer and category.

#### Scenario: Shared components are listed

- **WHEN** the gallery page is opened
- **THEN** every reusable component in the `components` package and every in-scope `package main` composite appears in the listing

#### Scenario: Single-use exported helper is excluded

- **WHEN** an exported `package main` templ func is used by only one page feature
- **THEN** it is treated as page-private and does not appear as a catalog entry

#### Scenario: Used go-daisy components are listed

- **WHEN** a go-daisy component is referenced by a listed component but is not itself cataloged
- **THEN** it appears in the listing as a go-daisy (L0) entry

#### Scenario: Pages and private helpers are excluded

- **WHEN** the listing is generated
- **THEN** page templates and page-private helpers do not appear as catalog entries

### Requirement: Each component reports its metadata

For every catalog entry the gallery SHALL show its name, layer, source file, props type, a short description, and its own components-versus-primitives classification.

#### Scenario: Metadata is shown

- **WHEN** a component is selected in the gallery
- **THEN** its layer, source file, props type, and description are displayed

### Requirement: Each component reports its dependencies

For every catalog entry the gallery SHALL show the components it uses, the call sites that render it, and its external dependencies. A `uses` edge SHALL point at a catalog entry, including go-daisy components, which the gallery adds as L0 entries on demand. Used-by SHALL include call sites in files that are not catalog entries — in particular pages — because a shared component's only callers are pages. External dependencies SHALL be reported at package granularity and SHALL list the go-daisy packages rendered plus any client wiring the component emits (Alpine, Stimulus, htmx, or `data-*` markers).

#### Scenario: Uses and used-by are shown

- **WHEN** a component is selected
- **THEN** the gallery lists the components it renders and the call sites that render it, including pages that are not catalog entries

#### Scenario: External dependencies are shown

- **WHEN** a component renders a go-daisy package or emits client wiring such as `x-data`, `data-controller`, or `hx-*`
- **THEN** those external dependencies are listed for that component

#### Scenario: Dependency targets resolve within the catalog

- **WHEN** the catalog reports that a gateway component uses another component
- **THEN** the target is itself a catalog entry — a gateway entry, or a go-daisy L0 entry added on demand

### Requirement: Components render in isolated previews with sample data

The gallery SHALL render each catalog entry against sample data in an isolated preview that does not share the page's DOM. Interactive components SHALL be previewable without their overlays escaping the preview, and component ids SHALL NOT collide across previews.

#### Scenario: Preview renders with fixture data

- **WHEN** a component with a curated fixture is previewed
- **THEN** it renders its representative states with sample data rather than empty placeholders

#### Scenario: Missing fixture falls back and is flagged

- **WHEN** a component has no curated fixture
- **THEN** it is previewed through an authored default render closure passing zero values, and is marked as needing a fixture when that render is empty (no non-whitespace text and no element) or panics on a nil slot; a panic SHALL NOT crash the gallery or the preview

#### Scenario: Interactive previews are isolated

- **WHEN** a component that opens a dialog, dropdown, or overlay is previewed
- **THEN** the overlay is confined to the preview area and does not cover the gallery page

#### Scenario: Preview cannot mutate the backend

- **WHEN** a previewed component contains a form or an htmx post control
- **THEN** the preview's sandbox prevents the session cookie from authorising a mutation and any submission fails harmlessly

### Requirement: Gallery is dev-gated

The gallery SHALL only be served when it is explicitly enabled by configuration. When it is disabled, its routes SHALL respond as not found after authentication (an unauthenticated request is redirected to sign-in first, as for any protected route) and the gallery SHALL NOT be linked from the main navigation.

#### Scenario: Disabled gallery is not served

- **WHEN** the gallery is disabled and an authenticated request is made to a gallery route
- **THEN** the response is 404 and no gallery content is rendered

#### Scenario: Enabled gallery is served

- **WHEN** the gallery is enabled and a request is made to the gallery page
- **THEN** the catalog is rendered inside the application shell

#### Scenario: No navigation link when disabled

- **WHEN** the gallery is disabled
- **THEN** no gallery entry appears in the sidebar navigation

#### Scenario: Dev routes are not project-scoped

- **WHEN** the gallery is enabled and the session has no active project
- **THEN** the gallery is served without redirecting to project selection

### Requirement: Dependency graph is generated and consistent

The dependency graph SHALL be produced by a generator invoked through `go:generate`, and the generator SHALL analyze component render call sites rather than a hand-maintained list. The generated graph SHALL be deterministic and SHALL compile as part of the gateway.

#### Scenario: Generator produces the graph

- **WHEN** the generator runs against the gateway source
- **THEN** it writes a dependency graph that builds and whose edges match the component render call sites

#### Scenario: Generation is deterministic

- **WHEN** the generator runs twice on unchanged source
- **THEN** it produces identical output

#### Scenario: Known edge is captured

- **WHEN** a shared composite renders a known go-daisy primitive
- **THEN** the generated graph records that edge
