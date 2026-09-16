## ADDED Requirements

### Requirement: Locator policy prioritizes semantic anchors
New specs SHALL prefer semantic locators and SHALL add `data-testid` only where no stable semantic anchor exists.

#### Scenario: Semantic locators preferred
- **WHEN** an element has a role, accessible name, form `name=`, or stable `#id`
- **THEN** the spec locates it via `getByRole`, `input[name=]`, or the `#id` rather than a test id

#### Scenario: Test ids reserved for unstable targets
- **WHEN** an element is a dynamically rendered list row, a row menu, a destructive confirm trigger, a status badge, a chart container, or a one-time secret reveal panel
- **THEN** a `data-testid` attribute is added to the template and used by the spec

#### Scenario: Test id naming is stable and scoped
- **WHEN** a test id is added
- **THEN** it is kebab-case and prefixed by its feature area (for example `agent-row-menu`, `token-secret-panel`)

### Requirement: Every rendered page exposes a stable page anchor
The gateway SHALL continue to render a `data-testid="page-<slug>"` anchor on every full page, and specs SHALL be able to assert it to distinguish a loaded page from a redirect to login.

#### Scenario: Page anchor present
- **WHEN** any full page is rendered via the page helper in `gateway/ui.go`
- **THEN** the root `<main>` carries `data-testid="page-<title-slug>"`

#### Scenario: Redirect detection
- **WHEN** a session-mode request is unauthenticated
- **THEN** the request is redirected to `/auth/login` and no page anchor is rendered

### Requirement: Shared helpers cover the recurring interaction primitives
The suite SHALL provide a shared helper for any interaction primitive that more than one spec needs, so that phases do not re-implement it per spec. A primitive with a single consumer MAY instead be handled by a spec-local helper.

#### Scenario: Toast assertion helper
- **WHEN** a spec expects flash feedback after a mutation
- **THEN** it uses a shared toast helper that settles the flash render rather than asserting on a transient class

#### Scenario: Dialog and row-menu helpers
- **WHEN** a spec triggers a confirm dialog or opens a row menu that more than one spec needs
- **THEN** it uses a shared dialog/row-menu helper
- **AND** a primitive with a single consumer may define its helper locally in the spec instead (the shipped project-restore spec defines `openRowMenu`/`deleteDialog` locally)

#### Scenario: Credential helpers
- **WHEN** a spec creates or reads a token or share secret
- **THEN** it uses a shared helper that handles the one-time reveal panel

#### Scenario: Scratch tenant helper
- **WHEN** a destructive spec needs an isolated project
- **THEN** it creates and deletes a scratch project through a shared bootstrap helper

### Requirement: The project execution graph is declared and documented consistently
The Playwright configuration SHALL declare the project dependency graph, and the suite documentation SHALL describe that graph accurately — including the deliberate choice that the mutation project depends on `setup` only.

#### Scenario: Dependency graph declared
- **WHEN** the suite is executed
- **THEN** `setup` runs first, and `chromium`, `mutations` and `scenarios` each declare `setup` as their dependency

#### Scenario: A single mutation spec stays cheap to run
- **WHEN** one mutation spec is run on its own
- **THEN** only `setup` and that spec execute, because the mutation project does not depend on the read surface

#### Scenario: Documentation matches configuration
- **WHEN** `tests/e2e/README.md` describes the project order
- **THEN** it matches the declared dependencies in `playwright.config.ts`, including that the mutation project does not wait for the read surface

### Requirement: Mutation specs isolate and clean up their state
Specs that create or mutate state SHALL confine themselves to entities they create, and SHALL remove them on completion. Resources the product deliberately retains as an audit trail are exempt from removal; for those, the requirement is that no LIVE instance remains.

#### Scenario: Writers confined to the mutation project
- **WHEN** a spec performs a create, update, or delete
- **THEN** it is named `*-ui.spec.ts` and runs in the `mutations` project with `workers: 1`

#### Scenario: Scratch entity cleanup
- **WHEN** a spec creates a disposable entity
- **THEN** it deletes that entity in cleanup regardless of pass or fail

#### Scenario: Audit-retained resources
- **WHEN** a spec creates a credential the product retains as an audit trail on revoke (a revoked API token stays as a row with a `Revoked` badge)
- **THEN** cleanup revokes any remaining live instance and the guard asserts no live token remains, rather than requiring the audit row to be deleted

#### Scenario: Self-cleanup guard
- **WHEN** a spec creates an enumerable resource under the bootstrap tenant
- **THEN** a trailing guard test asserts that no leftover `E2E`-prefixed resource remains

#### Scenario: Shared bootstrap state is not depended on
- **WHEN** a spec requires seeded schemas, blueprints, or providers to survive
- **THEN** it uses a scratch project instead of mutating the shared bootstrap project's configuration

### Requirement: Tests with live external dependencies skip rather than fail
A spec whose execution requires a third-party or network dependency SHALL gate itself at runtime and skip with a stated reason.

#### Scenario: Missing credentials produce a skip
- **WHEN** the required provider key, MCP endpoint, or credential is absent
- **THEN** the spec skips with an explanatory message and the suite remains green

#### Scenario: Gated specs are not silently disabled
- **WHEN** a spec is gated
- **THEN** the gate is a runtime condition rather than a static `only`, `fixme`, or unconditional `skip`

### Requirement: Templ changes are verified through the standard pipeline
Any change to a `.templ` file made to support a spec SHALL be verified through the gateway's required commands.

#### Scenario: Regeneration and compile
- **WHEN** a `.templ` file is modified
- **THEN** `templ generate` is run and the gateway builds with `go build ./...`

#### Scenario: Lint and test
- **WHEN** a phase that touched templates or specs is completed
- **THEN** the gateway lint task and the affected Playwright projects run green before the phase is committed
