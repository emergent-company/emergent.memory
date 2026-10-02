## Purpose

A dev-gated component gallery for the gateway: it catalogs the reusable templ components the app is built from, reports what each one depends on, and renders each one in isolation with sample data so its visual and interactive behaviour can be reviewed and fixed.

## ADDED Requirements

### Requirement: Gallery catalogs reusable templ components

The gallery SHALL list the gateway's reusable templ components: those in the shared `components` package, shared composites written in `package main`, and the go-daisy components referenced by them. It SHALL exclude page-level templates and page-private helpers, and it SHALL group entries by component layer and category.

#### Scenario: Shared components are listed

- **WHEN** the gallery page is opened
- **THEN** every reusable component in the `components` package and every shared `package main` composite appears in the listing

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

For every catalog entry the gallery SHALL show the components it uses, the components that use it, and its external dependencies. External dependencies SHALL include the go-daisy packages it renders and any client wiring it emits (Alpine, Stimulus, htmx, or `data-*` markers).

#### Scenario: Uses and used-by are shown

- **WHEN** a component is selected
- **THEN** the gallery lists the components it renders and the call sites that render it

#### Scenario: External dependencies are shown

- **WHEN** a component renders a go-daisy primitive or emits client wiring such as `x-data`, `data-controller`, or `hx-*`
- **THEN** those external dependencies are listed for that component

#### Scenario: Dependency targets resolve within the catalog

- **WHEN** the catalog reports that component A uses component B
- **THEN** B is itself a catalog entry listed by the gallery

### Requirement: Components render in isolated previews with sample data

The gallery SHALL render each catalog entry against sample data in an isolated preview that does not share the page's DOM. Interactive components SHALL be previewable without their overlays escaping the preview, and component ids SHALL NOT collide across previews.

#### Scenario: Preview renders with fixture data

- **WHEN** a component with a curated fixture is previewed
- **THEN** it renders its representative states with sample data rather than empty placeholders

#### Scenario: Missing fixture falls back and is flagged

- **WHEN** a component has no curated fixture
- **THEN** it is previewed with zero-value props and is marked as needing a fixture when it renders empty

#### Scenario: Interactive previews are isolated

- **WHEN** a component that opens a dialog, dropdown, or overlay is previewed
- **THEN** the overlay is confined to the preview area and does not cover the gallery page

### Requirement: Gallery is dev-gated

The gallery SHALL only be served when it is explicitly enabled by configuration. When it is disabled, its routes SHALL respond as not found and the gallery SHALL NOT be linked from the main navigation.

#### Scenario: Disabled gallery is not served

- **WHEN** the gallery is disabled and a request is made to a gallery route
- **THEN** the response is 404 and no gallery content is rendered

#### Scenario: Enabled gallery is served

- **WHEN** the gallery is enabled and a request is made to the gallery page
- **THEN** the catalog is rendered inside the application shell

#### Scenario: No navigation link when disabled

- **WHEN** the gallery is disabled
- **THEN** no gallery entry appears in the sidebar navigation

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
