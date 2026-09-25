## MODIFIED Requirements

### Requirement: Adoption preserves rendered output

Adopting a go-daisy component or a shared `components`-package component SHALL NOT change rendered output. The exact-render tests (`refactor_exact_test.go` and others) SHALL keep passing with the `want` strings unchanged, repointed at the library component; and every id, `data-testid`, `aria-*` attribute, `data-dialog-open`/`data-dialog-close` wiring, form `action`/`method`, and submit behaviour SHALL be preserved. Where a component's fixed shape does not fit a call site (bare checkbox toggles, a bordered settings grid that carries extra classes), the call site SHALL remain local rather than change its markup.

The normalizations below are intentional and documented, not regressions — each SHALL be enumerated here as it is introduced, and its golden updated in the same change:

- Migrated dialog shells render through `modalShell`, which always emits `hx-boost="false"` on the `<dialog>` root — `modalShell`'s deliberate, pre-existing contract (locked by `TestRefactorOutputExact`). The affected dialogs' behaviour is unchanged: their inner forms are `method="dialog"` close forms or already carry `hx-boost="false"`.
- The two agent MCP endpoint lists render through `TableCard`'s standard card shell (`card bg-base-100 card-border overflow-hidden shadow-sm` + `card-body p-0`) instead of the hand-rolled `rounded-box border-base-200 border`; their `data-testid` and margin are preserved.
- Button variant migration normalizes class **order** only (for example `btn btn-ghost btn-outline btn-sm` → `btn btn-ghost btn-sm btn-outline`); variant intent per action role is unchanged.
- The three unwrapped tables gain the shared shell wrapper, which adds an `overflow-x-auto` container; their table markup, headers, column alignment, and `data-testid`s are preserved. If the go-daisy table primitive is adopted instead, the inner table's own class changes and the `api_tokens_ui_test.go` `table table-sm` assertion is updated deliberately.
- Empty states adopted from hand-rolled blocks gain the shared component's anatomy (icon, title, description, action container); the sites whose shape genuinely does not match stay local, as listed in the empty-state requirement.
- Muted-text and icon-size normalization changes opacity/size utility values to the shared scale steps; the affected pinned class strings are updated with the scale.
- Card consolidation: the five card constructors (`ui.CardRaw`, `ui.Card`, `components.PanelCard`, `components.TableCard`, `ui.Section`) resolve to one card component plus a card list, with the chrome, padding, and shadow differences normalized to one treatment selected by a density variant — resolving the `shadow-sm` divergence between the table card and the panel card.
- Form-field consolidation: the four field shapes (`form.FormControl`, the page-local settings field, `components.ToggleField`, and the raw `<fieldset>`/`<legend>` controls) resolve to one field component with an optional tip, so label, hint, and error presentation are declared once.
- Page-header consolidation: the two header components (`pageHeader`, `detailHeader`) resolve to one component with optional kicker, breadcrumbs, and actions, and the margins currently baked into them move to the caller.
- Tone unification: the separate tone spellings (`Badge.Variant`, `Alert.Type`, `IconTile.Tone`, `Button.Variant`, `StatCard.IconColor`) resolve to one shared tone type, and the domain-to-tone mapping moves out of the shared layer.
- Layout-prop removal: `Margin`, `CardClass`, `BodyClass`, the generic class escape hatches, the stat-row margin parameter, and the card-list margin are replaced by density variants, with external spacing owned by callers.
- Theme-token migration: literal radius on controls and containers is replaced by the theme-driven radius utilities, app-owned CSS radius literals become `var(--radius-*)`, and component-local padding overrides on daisyUI roots are removed in favour of the central density block — so some class strings, and the rendered radius and padding, change.

#### Scenario: Exact-render tests pass unchanged

- **WHEN** the gateway unit tests are run after adoption
- **THEN** the exact-render assertions for the migrated components pass with the same expected markup, updated only to call the shared component instead of the deleted local one, and updated only where one of the enumerated normalizations above applies

#### Scenario: Identity, ARIA, and form wiring are unchanged

- **WHEN** an affected page is rendered after adoption
- **THEN** its ids, `data-testid`, `aria-*`, `data-dialog-open`/`data-dialog-close`, and form `action`/`method` match the pre-adoption markup

#### Scenario: Migrated dialog shells carry hx-boost="false"

- **WHEN** `deriveVersionDialog`, `deriveWarningDialog`, or `deriveBlueprintDialog` is rendered
- **THEN** each renders through `modalShell` and carries `hx-boost="false"` on its `<dialog>` root, with its `aria-labelledby`/`aria-describedby` preserved (the exact `modalShell` output is locked by `TestRefactorOutputExact`)

#### Scenario: Agent MCP endpoint lists render the standard card shell

- **WHEN** the agent MCP key list or session list is rendered
- **THEN** each renders through `TableCard`'s standard card shell (`card bg-base-100 card-border overflow-hidden shadow-sm`, `card-body p-0`) and keeps its `data-testid` (`agent-mcp-key-list` / `agent-mcp-session-list`) and margin

#### Scenario: Existing render tests pass

- **WHEN** the gateway unit tests are run after adoption
- **THEN** all existing render tests pass, with an assertion edited only where an enumerated normalization applies and the edit recorded in this change

#### Scenario: Verification gate is clean

- **WHEN** `templ generate ./...`, `go build ./...`, `go test ./...`, and `task lint` are run from `apps/web-ui/gateway`
- **THEN** all succeed

### Requirement: Destructive confirm dialogs render through ui.ConfirmDialog

The delete-confirmation modal (error icon circle, "Delete <noun>?" heading, description slot, action children) SHALL render through `ui.ConfirmDialog`, and the gateway SHALL NOT carry its own `confirmDeleteDialog`.

Every destructive action SHALL be confirmed through this component, and the gateway SHALL NOT gate a destructive action with a native `hx-confirm` attribute or a `window.confirm` call.

#### Scenario: Confirm dialog anatomy is single-sourced

- **WHEN** the gateway templates are searched for a local `confirmDeleteDialog`
- **THEN** no local definition remains, and each former call site calls `ui.ConfirmDialog` passing the same id, noun, and description component

#### Scenario: Confirm dialog markup is unchanged

- **WHEN** a migrated confirm dialog is rendered
- **THEN** it emits the same `bg-error/10 text-error grid size-10 shrink-0 place-items-center rounded-full` icon circle, the "Delete <noun>?" heading, the description, and the action children, with no class or structure change

#### Scenario: No native confirm remains

- **WHEN** the gateway templates are searched for `hx-confirm` or `window.confirm`
- **THEN** no occurrence gates a destructive action, and every former site renders a `ui.ConfirmDialog` instead

#### Scenario: Previously unprotected destructive actions are gated

- **WHEN** the blueprint unapply, project member remove, invite revoke, agent-override delete, device revoke, provider remove, and org tool delete actions are rendered
- **THEN** each is reachable only through a `ui.ConfirmDialog` confirmation, so no destructive request is issued from a single unconfirmed click

#### Scenario: Owner capabilities that act on plain activation are reconciled

- **WHEN** the blueprint-gallery pack removal or the project-settings agent-override removal is rendered
- **THEN** each routes through `ui.ConfirmDialog`, and the owning capability's scenario is updated in this change so the two contracts agree

### Requirement: Toasts render through ui.ToastQueueWithProps

The bottom-center toast queue SHALL render through `ui.ToastQueueWithProps` configured with `Position: ToastQueueBottomCenter`, `PauseOnHover: true`, and `Countdown: true`, and the gateway SHALL NOT carry its own `toastQueue`, `toastQueueState`, or `toastQueueInit`.

Dispatching a toast SHALL go through one client entry point (`MemoryApp.toast`), and no page SHALL declare its own toast function or otherwise write to the toast container directly. The server-rendered PRG flash path SHALL be preserved: because the app shell renders page content before it loads the client script bundle, the flash renderer SHALL NOT depend on the client entry point being present and SHALL keep its full-page-load fallback.

#### Scenario: Toast container id contract is preserved

- **WHEN** the app shell is rendered
- **THEN** the toast queue renders a `#toast-container` element whose Alpine `x-data` exposes `add`/`dismiss`/`pause`/`resume`, so the flash path and `app.js` `MemoryApp.toast` keep working unchanged

#### Scenario: Countdown bar CSS is single-sourced

- **WHEN** the gateway CSS is compiled
- **THEN** the `.toast-bar` rule and `@keyframes toast-shrink` originate from go-daisy's `components/css/custom.css` (already `@import`ed by the gateway build), and the gateway's own `webui/css/app.css` carries no duplicate

#### Scenario: One client dispatch entry point, PRG flash preserved

- **WHEN** the gateway templates and scripts are searched for a toast dispatch function
- **THEN** only one client-side dispatch implementation exists and the page-local copies are gone, while the server-rendered flash path remains and still surfaces its message on a full page load — not only after an htmx swap — via its queue-or-stash fallback, and any page that previously listened for the `memory-toast` event still receives it

### Requirement: Key/value detail rows render through a single component

Key/value detail rows SHALL render through one shared meta-row component with a single label-style convention, replacing the four local implementations and the repeated inline rows; raw `<dt>`/`<dd>` rows carrying the shared label treatment SHALL be migrated to it.

#### Scenario: Row renders label and value

- **WHEN** a meta row is rendered with a label and a value
- **THEN** it emits one term/description pair using the single label-style convention shared by every meta row in the UI

#### Scenario: Empty value fallback

- **WHEN** a meta row is rendered with an empty value
- **THEN** it renders the shared empty-dash placeholder, unless the row opts out because it renders status copy instead of a value

#### Scenario: Mono opt-in

- **WHEN** a meta row is rendered with the mono option
- **THEN** the value renders in the monospace treatment

#### Scenario: No raw term/description rows remain

- **WHEN** the gateway templates are searched for a hand-written `<dt>` carrying the shared label class
- **THEN** no occurrence remains outside the shared component

### Requirement: Table shells render through a single component

List and detail tables SHALL render inside one shared table-card shell, the caller SHALL supply the table markup, and the shell SHALL NOT impose a specific table primitive. The gateway SHALL present one table story: every table SHALL be wrapped by the shared shell (or rendered through the go-daisy table primitive, if this change adopts it), with no page declaring a raw `<table>` outside it.

This requirement rescinds the deferral recorded when the shared shell was introduced. That deferral kept raw tables acceptable because the go-daisy primitive always emits its own `overflow-x-auto` wrapper, adding a nesting level; the decision SHALL be re-examined in this change and its outcome recorded, not left implicit.

#### Scenario: Shell renders around supplied content

- **WHEN** a page renders a table inside the shared shell
- **THEN** the shell supplies the bordered, overflow-hidden, shadowed card with a zero-padding body and the page supplies only the table markup

#### Scenario: No page-defined table-card shells remain

- **WHEN** the gateway templates are searched for a hand-written bordered zero-padding card wrapping a table
- **THEN** no occurrence remains outside the shared component

#### Scenario: Every table sits in the shared shell

- **WHEN** the gateway templates are searched for a raw `<table>` element
- **THEN** each is wrapped by the shared shell, so the three genuinely unwrapped tables — the two preview tables in the migrations view and the blueprints table — no longer bypass it, while tables already inside `TableCard` (the API token list and the two agent MCP endpoint lists) keep their existing shell

### Requirement: Extraction preserves rendered output

Extracting a pattern into the shared package SHALL NOT change rendered output, except for the intentional normalizations enumerated in the change's design (D2): the meta-row label class and row spacing, the snippet code-block class order, the panel accent variant, class-order-only differences that produce identical CSS, insignificant inter-element whitespace inside grouped selects, and the reveal panel's heading element.

Assertions that pinned a normalized drift SHALL be brought into line in the same change. Where an assertion pinned only class names or class order — which the component conventions hold to be outside the component contract — it SHALL be narrowed or removed rather than re-pinned, so a cosmetic change does not fail a unit test.

#### Scenario: Page markup is unchanged

- **WHEN** an affected page is rendered after extraction
- **THEN** its markup matches the pre-extraction markup, apart from the normalized drifts

#### Scenario: Existing render tests pass

- **WHEN** the gateway unit tests are run after extraction
- **THEN** all existing render tests pass, with any assertion that pinned a normalized drift updated in the same change

#### Scenario: Class-only assertions are not re-pinned

- **WHEN** an existing assertion pinned only class composition that a normalization changes
- **THEN** the assertion is narrowed to the component's contract or removed, rather than updated to pin the new class string

#### Scenario: Verification gate is clean

- **WHEN** `templ generate ./...`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, and `go test ./...` are run from `apps/web-ui/gateway`
- **THEN** all succeed

#### Scenario: No duplicated pattern remains

- **WHEN** the gateway templates are searched for the twelve extracted patterns
- **THEN** each pattern exists only in the shared package, and every former duplicate site calls the shared component

### Requirement: Standard panels render through a single primitive

The standard bordered panel with the shared body padding SHALL render through the shared card component, which owns the standard chrome, body padding, and shadow; the panel is that component's default density. Intentional variation SHALL be expressed as a variant of that component rather than as a class override.

#### Scenario: Panel renders standard treatment

- **WHEN** a panel is rendered without variation
- **THEN** it renders the shared card component's standard chrome and body padding

#### Scenario: Accent override appends

- **WHEN** a caller needs different padding or an accent treatment
- **THEN** it selects a size or tone variant, and the caller's own class is appended to the variant's classes rather than replacing the standard treatment

### Requirement: Status badges render through a single primitive

Status badges SHALL render through one shared status-badge primitive that takes an explicit tone and icon; domain status vocabularies SHALL map to tone in the calling package.

#### Scenario: Badge renders label, intent, and icon

- **WHEN** a status badge is rendered with a label, a tone, and an icon
- **THEN** it renders the shared badge primitive with that tone's treatment and that icon

#### Scenario: No raw-span status badges remain

- **WHEN** the gateway templates are searched for a hand-written status badge using a raw span with an intent class
- **THEN** no occurrence remains outside the shared primitive

### Requirement: Detail-page headers render through nav.PageHeading

The gateway's page headers SHALL render through **one** shared header component built on `nav.PageHeading`, with an optional kicker, optional breadcrumbs, optional leading content, and an actions slot. The gateway SHALL NOT carry two sibling header adapters for the list and detail cases. Header margins SHALL be owned by the caller rather than baked into the component.

Which optional parts a given page supplies (the header idiom) is owned by the `web-ui-navigation` capability; this requirement owns the component.

#### Scenario: detailHeader delegates to PageHeading

- **WHEN** a detail header is rendered in its default, dashboard, bare, kicker, subtitle-full, or title-adornment variant
- **THEN** it renders through the shared header component with the breadcrumbs, title column, subtitle, and actions it supplied, with the header's children forwarded into the actions slot

#### Scenario: TitleLeading variant renders inline before the h1

- **WHEN** a header uses the optional leading content (an icon tile before the title column)
- **THEN** the gateway renders it immediately before the h1, on the same flex line and before any title adornment, by supplying the component's leading slot

#### Scenario: pageHeader delegates to PageHeading

- **WHEN** the list-page header is rendered
- **THEN** it renders through the same shared header component (flat, dashboard, no-top-margin), and the gateway carries no separate list-page adapter

#### Scenario: One component serves both page classes

- **WHEN** a list page and a detail page render their headers
- **THEN** both call the same component, differing only in which optional parts they supply, with the component's structure and kicker treatment identical

#### Scenario: Header spacing belongs to the caller

- **WHEN** a page renders its header
- **THEN** the space below the header is applied by the page, not baked into the component, so the previous per-class margins are the caller's responsibility

### Requirement: List sections render through ui.Section Rows

The titled card-list section SHALL render through the shared card-list component (see "Cards render through one component"), which supplies the titled-section structure and the row divider. While the library's `Rows` option exists it MAY provide the divide-y wrapper inside that component, but the gateway's contract is the shared component, not the library option.

#### Scenario: Titled card list output is unchanged

- **WHEN** a titled card list is rendered
- **THEN** it emits the same `<section>`/heading/card/`divide-y` structure it emitted before, supplied by the shared component

#### Scenario: Bare card list stays wrapper-free

- **WHEN** a title-empty card list is rendered
- **THEN** it renders the bare card without a `<section>` wrapper

### Requirement: Bare card lists render through Section.Wrapperless

The bare (title-empty) card list SHALL render through the shared card-list component with no section wrapper. While the library's `Wrapperless` option exists it MAY be used to provide that, but the gateway's contract is the shared component.

#### Scenario: Wrapperless bare card output is unchanged

- **WHEN** a title-empty card list is rendered
- **THEN** it emits the same bare card plus `divide-y` rows without a `<section>` wrapper

### Requirement: Definition-list grids render through a single component

Definition-list grids of `MetaRow` / `MetaRowCustom` cells SHALL render through one shared `MetaGrid` component, with the row gap and any extra columns expressed as options rather than hand-rolled class strings.

The contract is the grid's structure and its configured gap and columns. The exact rendered class **order** is not part of the contract, per the component test policy: this requirement SHALL NOT be read as pinning a byte-exact class string.

#### Scenario: Grid renders the shared column/gap rhythm

- **WHEN** a `MetaGrid` is rendered
- **THEN** it emits the single shared grid layout with the configured row gap and column count, rather than per-call-site grid markup

#### Scenario: Extra columns and row gap are call-site options

- **WHEN** a page needs a different row gap or a wider column count
- **THEN** it passes those as options rather than hand-rolling the grid's class string

## REMOVED Requirements

### Requirement: Destructive-confirm icon circles render through a single component

**Reason**: `ui.ConfirmDialog` already renders the error-tinted icon circle internally, byte-identical to the local component's output. Keeping a separate single-source requirement contradicts the "destructive confirm dialogs render through `ui.ConfirmDialog`" requirement, and left the icon circle declared single-sourced while five call sites still used the local component.

**Migration**: the five call sites (`mcp_shares.templ:159`, `agent_mcp_endpoint.templ:152,334`, `mcp_nodes.templ:150`, `project_settings.templ:1278`) move to `ui.ConfirmDialog`, which supplies the circle; `components/confirm.templ` and its `ConfirmIcon` are deleted.

## ADDED Requirements

### Requirement: Copy affordances render through a single component

Every copy-to-clipboard control SHALL render through one shared copy component backed by one client copy implementation, and the gateway SHALL NOT declare a second copy function or a page-local copy button. The snippet component and the one-time-secret-reveal component SHALL consume this shared copy component rather than embedding their own copy affordance.

#### Scenario: Copy control renders with a stable target

- **WHEN** a copy affordance is rendered for a value, a target element id, or a fetch-from-endpoint reveal
- **THEN** it renders the shared button with the variant and label supplied by the caller and a target that resolves to that value

#### Scenario: One copy implementation

- **WHEN** the gateway templates and scripts are searched for a clipboard write
- **THEN** only the shared implementation writes to the clipboard, and the former share-management copy function and its raw buttons are gone

#### Scenario: Reveal-fetch is a variant, not a fork

- **WHEN** a copy control must first retrieve the value from an endpoint before copying
- **THEN** it uses the shared component's reveal option rather than a separate script

#### Scenario: Snippet and reveal surfaces consume the shared component

- **WHEN** the client-configuration snippet or a one-time secret reveal renders its copy control
- **THEN** it renders through the shared copy component, so those surfaces are not a separate copy contract

### Requirement: Form controls render through the form primitives

Text, textarea, and select controls in the gateway SHALL render through the shared form primitives (`form.FormControl`, or the gateway's thin field adapter over it), and pages SHALL NOT hand-roll a labelled `input w-full`, `textarea`, or `select`. Bare toggles and date/chip inputs are out of scope here: toggles are covered by their own requirement and date/chip inputs keep their dedicated primitives.

#### Scenario: Settings controls are single-sourced

- **WHEN** a project-settings form control is rendered
- **THEN** it renders through the settings field adapter and the shared form primitive, preserving its id, `name`, help text, and htmx attributes

#### Scenario: No raw labelled controls remain

- **WHEN** the gateway templates are searched for a hand-written labelled text input, textarea, or select
- **THEN** no occurrence remains outside the form primitives among the scoped call sites

### Requirement: Stat grids render through a single component

Grids of metric cards SHALL render through one shared stat-grid component, and pages SHALL NOT hand-roll a bordered label/value metric cell.

#### Scenario: Stat grid renders the shared rhythm

- **WHEN** a stat grid is rendered with a set of metric cards
- **THEN** it emits the shared responsive column layout and the shared metric card treatment

#### Scenario: No hand-rolled metric cell remains

- **WHEN** the gateway templates are searched for a hand-written bordered label/value metric cell
- **THEN** no occurrence remains outside the shared component and the go-daisy stat components

### Requirement: Inline code renders through a single component

Inline monospace value pills SHALL render through one shared inline-code component, with a block variant where the value needs to wrap.

#### Scenario: Inline pill renders the shared treatment

- **WHEN** an inline code value is rendered
- **THEN** it emits the shared monospace pill treatment, selectable, and breaks long values without overflowing its container

### Requirement: Soft info panels render through a single component

Translucent bordered informational panels SHALL render through one shared component (or the existing go-daisy soft-panel component), and pages SHALL NOT hand-roll the translucent bordered box.

This is distinct from the standard bordered panel primitive: `PanelCard` is an opaque bordered panel that carries the shared body padding, whereas this requirement covers only the translucent decorative `bg-base-200/30|40` informational box. A panel that is opaque SHALL use the standard panel primitive, not this one, and the two requirements SHALL NOT drift apart.

#### Scenario: Info panel renders the shared treatment

- **WHEN** an informational panel is rendered
- **THEN** it emits the shared bordered, translucent, rounded panel with the shared padding

#### Scenario: No hand-rolled soft box remains

- **WHEN** the gateway templates are searched for a hand-written translucent bordered panel
- **THEN** no occurrence remains outside the shared component

### Requirement: Eyebrow labels render through ui.Eyebrow

Uppercase small-caps section labels SHALL render through `ui.Eyebrow`, and pages SHALL NOT hand-roll the uppercase eyebrow treatment. The `nav.PageHeading` kicker is exempt: its eyebrow is produced inside `PageHeading`, so `PageHeading` consumers SHALL NOT be changed or double-rendered while rendering their kicker through the library.

#### Scenario: Eyebrow renders the shared treatment

- **WHEN** a section eyebrow is rendered outside a page heading
- **THEN** it emits the shared eyebrow markup and no page declares its own uppercase label class combination

#### Scenario: The page-heading kicker is not double-owned

- **WHEN** a page renders its kicker through `nav.PageHeading`
- **THEN** the kicker's eyebrow is the one `PageHeading` emits, and the page does not additionally render `ui.Eyebrow` for it

### Requirement: Buttons render through ui.Button

Action buttons SHALL render through `ui.Button` with the library's variant, size, and shape props, so action ordering and variant treatment are consistent across pages.

#### Scenario: Action button maps to library props

- **WHEN** a page renders an action button
- **THEN** it calls `ui.Button` with an explicit variant, size, and (for icon-only actions) shape plus an accessible label, and no page declares a raw `btn btn-*` element

#### Scenario: Variant drift is normalized

- **WHEN** the migrated call sites are compared with their former classes
- **THEN** the previously inconsistent ghost/outline/square/size combinations resolve to one documented variant per action role, with class order as the only rendering change (enumerated in the adoption requirement)

### Requirement: Empty states render through ui.EmptyState

Genuine empty-state blocks SHALL render through `ui.EmptyState`, and pages SHALL NOT hand-roll an empty-state block. A site whose shape does not fit the component SHALL stay local rather than be forced into it: specifically, dropdown/select placeholders, single-line placeholder paragraphs used as inline hints, and any rail whose empty node carries stable ids that client script toggles are all out of scope, as is the `cardList` internal empty branch that is pinned byte-exactly by the exact-render tests.

#### Scenario: Empty state renders the shared anatomy

- **WHEN** a list or section with no content renders its empty state
- **THEN** it renders the shared empty-state component with the caller's title, description, and optional call to action

#### Scenario: Non-matching sites stay local

- **WHEN** a dropdown placeholder, an inline hint paragraph, a script-toggled rail empty node, or the `cardList` empty branch is rendered
- **THEN** it keeps its existing local markup, and no exclusion site is migrated for the sake of uniformity

### Requirement: Lists and tables follow one rendering rule

The gateway SHALL apply one rule for choosing a presentation: icon-plus-title-plus-metadata collections SHALL render as card or list rows, and genuinely tabular data SHALL render as a table in the shared shell.

This requirement decides which presentation applies; the table-shell requirement defines how a chosen table is wrapped. A collection this requirement sends to a table SHALL therefore sit in that shared shell, and the two requirements SHALL NOT drift apart.

#### Scenario: Row collection renders as rows

- **WHEN** a collection's primary content is an icon, a title, and trailing metadata
- **THEN** it renders through the shared list-row or card-list presentation rather than a table, and the reverse holds for tabular data

### Requirement: Cards render through one component

Card and panel markup SHALL render through one shared card component, with its internal density selected by
a size variant, and a card list for collected cards. The gateway SHALL NOT carry multiple card
constructors, and pages SHALL NOT hand-roll card chrome.

#### Scenario: One card treatment

- **WHEN** a card is rendered anywhere in the gateway
- **THEN** the same component supplies its chrome, and any variation is a size variant rather than a separate constructor

#### Scenario: No hand-rolled card chrome

- **WHEN** the gateway templates are searched for a hand-written card shell
- **THEN** no occurrence remains outside the shared component

#### Scenario: The card list composes cards

- **WHEN** a collection of cards is rendered
- **THEN** it uses the shared card list, which owns the between-card rhythm

### Requirement: Form fields render through one component

A labelled form control SHALL render through one shared field component providing label, hint, error, and
optional tip, with the control supplied as its content. Pages SHALL NOT hand-roll label, hint, or error
markup around a control.

#### Scenario: Label, hint, and error are declared once

- **WHEN** a form field is rendered
- **THEN** its label, hint, and error come from the shared component, and the caller supplies only the control

#### Scenario: No hand-rolled field chrome

- **WHEN** the gateway templates are searched for a hand-written fieldset or label-and-error wrapper around a control
- **THEN** no occurrence remains outside the shared component

### Requirement: Page headers render through one component

Page headers SHALL render through one shared header component that takes an optional kicker, optional
breadcrumbs, optional leading content, and an actions slot. The gateway SHALL NOT carry two sibling header
components differing only in whether the eyebrow is a kicker or a breadcrumb trail.

#### Scenario: One header component serves both page classes

- **WHEN** a list page and a detail page render their headers
- **THEN** both use the same component, differing only in which optional parts they supply

#### Scenario: The header does not own page spacing

- **WHEN** a page renders its header
- **THEN** the spacing below it is owned by the page, not baked into the component

### Requirement: Semantic tone is one shared type

Semantic colour SHALL be expressed through one shared tone type used by every **gateway-local** component that takes a tone, replacing the gateway's separate per-component spellings. The domain-to-tone mapping SHALL live outside the shared component layer.

The library's own per-component tone spellings are unified upstream (see the change's go-daisy dependency list); until they land, this requirement is scoped to components the gateway owns, and does not require changing library prop names.

#### Scenario: One tone type across components

- **WHEN** two different components express semantic colour
- **THEN** both accept the same shared tone type rather than each defining its own

#### Scenario: Domain mapping stays outside the shared layer

- **WHEN** a status must be mapped to a tone
- **THEN** the mapping is applied by the caller or an adapter, and the shared component receives only the tone
