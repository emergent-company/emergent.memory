## ADDED Requirements

### Requirement: Shared gateway markup lives in an importable component package

The gateway SHALL provide a `components` package at `apps/web-ui/gateway/components` (import path `github.com/emergent-company/emergent.memory/apps/web-ui/components`) as the single home for shared, generic view components. Templates in `package main` SHALL render these patterns by calling the package rather than by re-declaring the markup locally.

#### Scenario: Package builds and is importable

- **WHEN** `templ generate ./...` and `go build ./...` run from `apps/web-ui/gateway`
- **THEN** the `components` package compiles and any gateway template can reference its components

#### Scenario: Components stay domain-free

- **WHEN** the `components` package is inspected
- **THEN** it contains no import of a gateway domain type, no gateway route path literal, and no gateway configuration lookup; anything requiring a domain vocabulary stays in `package main` as a thin adapter that calls the component

#### Scenario: Component documentation states the contract

- **WHEN** the package documentation is read
- **THEN** it states that the package holds generic markup only and that domain decisions (status-to-intent mapping, route construction) belong to the caller

### Requirement: One-time secret reveal surfaces render through a single component

Every surface that reveals a one-time secret SHALL render through the shared secret-reveal component: the per-agent MCP key reveal, the MCP share reveal, and the API token reveal.

#### Scenario: Modal variant renders the reveal anatomy

- **WHEN** a reveal modal is rendered for an MCP key or an MCP share
- **THEN** it renders the key-round icon tile, the heading, the "shown only once" guidance, the secret value with a copy affordance, an optional endpoint with a copy affordance, the rotation warning, the optional snippet block, and a closing action

#### Scenario: Panel variant renders for inline reveals

- **WHEN** the API token reveal is rendered
- **THEN** it renders the same reveal anatomy as an inline panel inside its page

#### Scenario: Secret is revealed exactly once

- **WHEN** a secret is revealed after a mint or rotate
- **THEN** the secret value is shown in that reveal only, and the copy affordance targets that value

### Requirement: Key/value detail rows render through a single component

Key/value detail rows SHALL render through one shared meta-row component with a single label-style convention, replacing the four local implementations and the repeated inline rows.

#### Scenario: Row renders label and value

- **WHEN** a meta row is rendered with a label and a value
- **THEN** it emits one term/description pair using the single label-style convention shared by every meta row in the UI

#### Scenario: Empty value fallback

- **WHEN** a meta row is rendered with an empty value
- **THEN** it renders the shared empty-dash placeholder, unless the row opts out because it renders status copy instead of a value

#### Scenario: Mono opt-in

- **WHEN** a meta row is rendered with the mono option
- **THEN** the value renders in the monospace treatment

### Requirement: Table shells render through a single component

List and detail tables SHALL render inside one shared table-card shell, and tables inside that shell SHALL use the go-daisy table primitives rather than raw table markup.

#### Scenario: Shell renders around supplied content

- **WHEN** a page renders a table inside the shared shell
- **THEN** the shell supplies the bordered, overflow-hidden, shadowed card with a zero-padding body and the page supplies only the table markup

#### Scenario: No raw table shells remain

- **WHEN** the gateway templates are searched for a hand-written bordered zero-padding card wrapping a table
- **THEN** no occurrence remains outside the shared component

### Requirement: Dialog opening uses one client helper

Opening a native dialog SHALL go through a single client helper, and pages SHALL NOT declare per-dialog `showModal()` script blocks.

#### Scenario: Programmatic open

- **WHEN** a page needs to open a dialog from a script or inline handler
- **THEN** it calls the shared client helper with the dialog id

#### Scenario: Auto-open survives htmx swaps

- **WHEN** a dialog marked for auto-open is swapped into the DOM by an htmx response, or is present on initial load
- **THEN** the shared helper opens it without any page-local script

#### Scenario: Missing dialog is a no-op

- **WHEN** the shared helper is called with an id that is not present, or for an element that cannot be opened
- **THEN** it returns without throwing

### Requirement: Client configuration snippet blocks render through a single component

The MCP client snippet blocks SHALL render through one shared component with one class convention for the code block.

#### Scenario: Snippet renders with copy affordance

- **WHEN** a snippet block is rendered with a label and code
- **THEN** it renders the bordered block, the label, the code, and a copy control targeting that code

### Requirement: Settings toggle rows render through a single component

Settings rows that pair a toggle with a label and an explanatory tip SHALL render through one shared toggle-field component.

#### Scenario: Toggle renders name, state, and tip

- **WHEN** a toggle field is rendered
- **THEN** the checkbox carries the given form name and checked state, uses the toggle treatment, and renders the label with its info tip

### Requirement: Grouped select options render through a single renderer

Select controls that present options grouped under labels (provider/model groups) SHALL render their option groups through one shared renderer, while the grouping domain logic remains in the calling package.

#### Scenario: Groups and selection render

- **WHEN** grouped options are rendered with a currently selected value
- **THEN** each group renders as a labelled group containing its options in order, with the selected option marked

### Requirement: Sub-navigation rails render through shared components

Sub-navigation rails and their items SHALL render through a shared nav wrapper and item component that live in the component package, not inside a feature page file.

#### Scenario: Rail renders with accessible label

- **WHEN** a sub-navigation rail is rendered
- **THEN** it emits the rail container with the supplied accessible label and the shared item markup for each entry

#### Scenario: Active item is marked

- **WHEN** an item is rendered for the currently active destination
- **THEN** that item alone carries the active treatment

### Requirement: Status badges render through a single primitive

Status badges SHALL render through one shared status-badge primitive that takes an explicit badge intent and icon; domain status vocabularies SHALL map to intent in the calling package.

#### Scenario: Badge renders label, intent, and icon

- **WHEN** a status badge is rendered with a label, an intent, and an icon
- **THEN** it renders the shared badge primitive with that intent's treatment and that icon

#### Scenario: No raw-span status badges remain

- **WHEN** the gateway templates are searched for a hand-written status badge using a raw span with an intent class
- **THEN** no occurrence remains outside the shared primitive

### Requirement: Metadata chips render through a single primitive

Inline icon-plus-text metadata chips SHALL render through one shared chip primitive.

#### Scenario: Chip renders icon and text

- **WHEN** a metadata chip is rendered
- **THEN** it renders the icon and the text inside the shared inline chip wrapper

### Requirement: Standard panels render through a single primitive

The standard bordered panel with the shared body padding SHALL render through one shared panel primitive, with a class override available for intentional accents.

#### Scenario: Panel renders standard treatment

- **WHEN** a panel is rendered without overrides
- **THEN** it renders the standard bordered panel and body padding

#### Scenario: Accent override appends

- **WHEN** a panel is rendered with an extra class
- **THEN** the extra class is applied in addition to the standard treatment, not instead of it

### Requirement: List rows render through the shared list row

Rows that show a leading element, a title, and trailing metadata as a link SHALL render through the shared list-row component; hand-rolled equivalents SHALL be migrated to it.

#### Scenario: Row renders link, leading slot, title, and meta

- **WHEN** a list row is rendered
- **THEN** it emits the link with the supplied destination, the leading slot, the title, the metadata, and the trailing affordance

### Requirement: Extraction preserves rendered output

Extracting a pattern into the shared package SHALL NOT change rendered output, except for the three drifts explicitly normalized by the change (the meta-row label class, the snippet code-block class order, and the panel accent variant).

#### Scenario: Page markup is unchanged

- **WHEN** an affected page is rendered after extraction
- **THEN** its markup matches the pre-extraction markup, apart from the normalized drifts

#### Scenario: Existing render tests pass

- **WHEN** the gateway unit tests are run after extraction
- **THEN** all existing render tests pass, with any assertion that pinned a normalized drift updated in the same change

#### Scenario: Verification gate is clean

- **WHEN** `templ generate ./...`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, and `go test ./...` are run from `apps/web-ui/gateway`
- **THEN** all succeed

#### Scenario: No duplicated pattern remains

- **WHEN** the gateway templates are searched for the twelve extracted patterns
- **THEN** each pattern exists only in the shared package, and every former duplicate site calls the shared component
