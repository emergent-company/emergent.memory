# web-ui-components Specification

## Purpose

Defines how the Memory gateway single-sources shared UI markup.

Two halves. First, the gateway's own generic component package
(`apps/web-ui/gateway/components`) carries the patterns go-daisy does not provide — one-time secret
reveals, key/value detail rows, table and panel shells, dialog auto-open, client snippets, settings
toggles, grouped select options, sub-nav rails, status badges, metadata chips, and list rows — with
domain vocabulary (status-to-intent maps, route paths, model grouping) staying in `package main`.

Second, where go-daisy already provides the component, the gateway adopts it rather than keeping a
local copy — dialogs, confirm dialogs, toasts, list sections, the command palette, detail-page
headings, sidebar nav rows, avatars, type accents, and bare card lists.

Both halves require that migrating a call site does not change rendered output, except for the
normalizations each change records explicitly.

## Requirements

### Requirement: Gateway dialogs render through the upstream go-daisy Dialog

The native `<dialog class="modal">` shell SHALL render through `ui.Dialog` (go-daisy `components/ui`), and the gateway SHALL NOT carry its own `modalShell`.

#### Scenario: Dialog shell is single-sourced

- **WHEN** the gateway templates are searched for a local `modalShell`
- **THEN** no local definition remains, and every former call site calls `ui.Dialog` with the same `ID`, `BoxID`, `BoxClass`, and `Attrs`

#### Scenario: Dialog output is unchanged

- **WHEN** a migrated dialog is rendered
- **THEN** it emits the same `<dialog class="modal" hx-boost="false">`, modal-box `id`/`class`, and `method="dialog"` backdrop form as before, with every id, `aria-*` attribute, and `data-dialog-open`/`data-dialog-close` wiring preserved

### Requirement: Destructive confirm dialogs render through ui.ConfirmDialog

The delete-confirmation modal (error icon circle, "Delete <noun>?" heading, description slot, action children) SHALL render through `ui.ConfirmDialog`, and the gateway SHALL NOT carry its own `confirmDeleteDialog`.

#### Scenario: Confirm dialog anatomy is single-sourced

- **WHEN** the gateway templates are searched for a local `confirmDeleteDialog`
- **THEN** no local definition remains, and each former call site calls `ui.ConfirmDialog` passing the same id, noun, and description component

#### Scenario: Confirm dialog markup is unchanged

- **WHEN** a migrated confirm dialog is rendered
- **THEN** it emits the same `bg-error/10 text-error grid size-10 shrink-0 place-items-center rounded-full` icon circle, the "Delete <noun>?" heading, the description, and the action children, with no class or structure change

### Requirement: Toasts render through ui.ToastQueueWithProps

The bottom-center toast queue SHALL render through `ui.ToastQueueWithProps` configured with `Position: ToastQueueBottomCenter`, `PauseOnHover: true`, and `Countdown: true`, and the gateway SHALL NOT carry its own `toastQueue`, `toastQueueState`, or `toastQueueInit`.

#### Scenario: Toast container id contract is preserved

- **WHEN** the app shell is rendered
- **THEN** the toast queue renders a `#toast-container` element whose Alpine `x-data` exposes `add`/`dismiss`/`pause`/`resume`, so `flashToast` and `app.js` `MemoryApp.toast` keep working unchanged

#### Scenario: Countdown bar CSS is single-sourced

- **WHEN** the gateway CSS is compiled
- **THEN** the `.toast-bar` rule and `@keyframes toast-shrink` originate from go-daisy's `components/css/custom.css` (already `@import`ed by the gateway build), and the gateway's own `webui/css/app.css` carries no duplicate

### Requirement: List sections render through ui.Section Rows

The titled card-list section SHALL render through `ui.Section` with `Rows: true` (the divide-y wrapper), instead of a hand-written `divide-y divide-base-200` div.

#### Scenario: Titled card list output is unchanged

- **WHEN** a titled `cardList` is rendered
- **THEN** it emits the same `<section>`/`<h2>`/card/`divide-y` structure, with the divide-y wrapper provided by `Section`'s `Rows` option

#### Scenario: Bare card list stays wrapper-free

- **WHEN** a title-empty `cardList` is rendered
- **THEN** it renders the bare `ui.CardRaw` without a `<section>` wrapper (ui.Section always emits `<section>`, which the bare variant never had)

### Requirement: Spotlight uses ui.CommandPaletteButton and FullScreenMobile

The topbar search button SHALL render through `ui.CommandPaletteButton`, and the spotlight palette SHALL render through `ui.CommandPalette` with `FullScreenMobile: true`, instead of a hand-rolled button and a hand-rolled mobile `<style>` block.

#### Scenario: Spotlight button output is unchanged

- **WHEN** the spotlight trigger is rendered
- **THEN** it emits the same visible "Search…" label, the same `⌘K` hint, and the same click/focus wiring targeting `#spotlight-toggle`/`#spotlight-input`

#### Scenario: Mobile CSS is generated from the palette ID

- **WHEN** the spotlight palette is rendered
- **THEN** the full-viewport mobile behaviour is emitted by `ui.CommandPalette`'s `FullScreenMobile` option, and the selectors target `#spotlight-toggle`, `#spotlight-input`, `#spotlight-results`, and `label[for=spotlight-toggle]` (derived from `ID: "spotlight"`)

### Requirement: Adoption preserves rendered output

Adopting a go-daisy component or a shared `components`-package component SHALL NOT change rendered output. The exact-render tests (`refactor_exact_test.go` and others) SHALL keep passing with the `want` strings unchanged, repointed at the library component; and every id, `data-testid`, `aria-*` attribute, `data-dialog-open`/`data-dialog-close` wiring, form `action`/`method`, and submit behaviour SHALL be preserved. Where a component's fixed shape does not fit a call site (bare checkbox toggles, a bordered settings grid that carries extra classes), the call site SHALL remain local rather than change its markup.

Two shell normalizations are intentional and documented, not regressions:

- Migrated dialog shells render through `modalShell`, which always emits `hx-boost="false"` on the `<dialog>` root — `modalShell`'s deliberate, pre-existing contract (locked by `TestRefactorOutputExact`). The affected dialogs' behaviour is unchanged: their inner forms are `method="dialog"` close forms or already carry `hx-boost="false"`.
- The two agent MCP endpoint lists render through `TableCard`'s standard card shell (`card bg-base-100 card-border overflow-hidden shadow-sm` + `card-body p-0`) instead of the hand-rolled `rounded-box border-base-200 border`; their `data-testid` and margin are preserved.

#### Scenario: Exact-render tests pass unchanged

- **WHEN** the gateway unit tests are run after adoption
- **THEN** the exact-render assertions for the migrated components pass with the same expected markup, updated only to call `ui.Dialog`/`ui.ConfirmDialog` instead of the deleted local components

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
- **THEN** all existing render tests pass without editing test assertions

#### Scenario: Verification gate is clean

- **WHEN** `templ generate ./...`, `go build ./...`, `go test ./...`, and `task lint` are run from `apps/web-ui/gateway`
- **THEN** all succeed

### Requirement: Detail-page headers render through nav.PageHeading

The detail-page header (breadcrumbs, optional kicker, h1, optional subtitle, actions) SHALL render through `nav.PageHeading`, and the gateway's `detailHeader` SHALL be a thin adapter delegating to it by default; a non-nil `TitleLeading` (inline icon tile before the h1) SHALL render locally via `detailHeaderWithTitleLeading`. The list-page `pageHeader` SHALL also render through `nav.PageHeading` as a thin adapter (Flat + Dashboard + no-top-margin variant), which reproduces it byte-for-byte.

#### Scenario: detailHeader delegates to PageHeading

- **WHEN** a detail header is rendered in its default, dashboard, bare, kicker, subtitle-full, or title-adornment variant
- **THEN** it emits the same breadcrumbs, title column, subtitle, and actions markup, with the header's children forwarded into `PageHeading`'s `Actions` slot

#### Scenario: TitleLeading variant renders inline before the h1

- **WHEN** a detail header uses the `TitleLeading` option (an agent icon tile before the title column)
- **THEN** the gateway renders the component inline immediately before the h1, on the same flex line and before any `TitleAdornment`, via a local `detailHeaderWithTitleLeading` render that mirrors `PageHeading`'s markup (because `PageHeading` has no pre-h1 slot), with `TitleLeading` ignored when `Bare`

#### Scenario: pageHeader delegates to PageHeading

- **WHEN** the list-page `pageHeader` is rendered
- **THEN** the gateway has no local `pageHeader` markup — `pageHeader` is a thin adapter over `nav.PageHeading` (Flat + Dashboard + no-top-margin variant) that emits the same single `<div class="mb-6 flex flex-wrap items-end justify-between gap-4">` wrapper, the kicker eyebrow and `lg:text-3xl` title, no breadcrumbs region, no `mt-2`, and the subtitle rendered full-width (`text-base-content/55 mt-1 text-sm`, no `max-w-2xl`)

### Requirement: Sidebar nav rows render through layout.SidebarNavItem

The sidebar nav link SHALL render through `layout.SidebarNavItem`, and the gateway SHALL NOT carry its own `sidebarNavRow` or `sidebarIndicatorID`.

#### Scenario: Nav row markup is single-sourced

- **WHEN** the gateway templates are searched for a local `sidebarNavRow` or `sidebarIndicatorID`
- **THEN** no local definition remains, and the app sidebar calls `layout.SidebarNavItem`

#### Scenario: Provider warning renders through the Warn slot

- **WHEN** the project has no LLM provider configured
- **THEN** the "Project" entry renders the app-specific warning indicator via `SidebarItem.Warn`, keeping the copy at the call site rather than baking it into the library

#### Scenario: htmx attributes are preserved

- **WHEN** a sidebar nav row is rendered
- **THEN** it carries the same `hx-target="#main-content"`, `hx-swap`, `hx-push-url`, and `hx-indicator` attributes as before

### Requirement: Account avatar sizes initials via AvatarFull TextClass

The account-menu avatar SHALL render through `ui.AvatarFull` with `TextClass` sizing the initials, instead of carrying the font-size class on an outer wrapper.

#### Scenario: Initials font-size is applied via TextClass

- **WHEN** the account avatar renders an initials fallback
- **THEN** the font-size class is applied to the initials text via `AvatarProps.TextClass`

#### Scenario: shrink-0 wrapper is preserved

- **WHEN** the account avatar is rendered
- **THEN** the outer `shrink-0` wrapper remains (AvatarFull has no shrink slot), so the avatar does not collapse in the flex menu rows

### Requirement: Type accents render through IconTile/Badge Color and Glyph

The schema-declared type icon tile and name chip SHALL render through `ui.IconTile` (`Color`, `Glyph`) and `ui.Badge` (`Color`, `Glyph`, `LabelClass`), while the app-specific icon catalog and name normalisation SHALL stay local.

#### Scenario: Color tint and glyph branch delegate to library props

- **WHEN** a type declares a color and/or a text/emoji glyph
- **THEN** the tile/chip renders the arbitrary CSS colour via the library `Color` prop and the glyph via the `Glyph` prop

#### Scenario: Icon catalog stays local

- **WHEN** a schema-declared icon name is resolved
- **THEN** the gateway's `supportedTypeIconClasses`/`typeIconClass`/`normalizeIconName` catalog remains local, because it exists for the Tailwind `@source` scan

### Requirement: Bare card lists render through Section.Wrapperless

The bare (title-empty) card list SHALL render through `ui.Section` with `Wrapperless: true`, instead of a hand-written `ui.CardRaw` branch.

#### Scenario: Wrapperless bare card output is unchanged

- **WHEN** a title-empty `cardList` is rendered
- **THEN** it emits the same bare card + `divide-y` rows without a `<section>` wrapper, via `Section`'s `Wrapperless` option

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

List and detail tables SHALL render inside one shared table-card shell, the caller SHALL supply the table markup, and the shell SHALL NOT impose a specific table primitive. Adopting the go-daisy table primitives is deferred: `table.TableWithProps` always emits its own `overflow-x-auto` wrapper, and every gateway raw table already sits inside one, so adopting the primitive would add a nesting level.

#### Scenario: Shell renders around supplied content

- **WHEN** a page renders a table inside the shared shell
- **THEN** the shell supplies the bordered, overflow-hidden, shadowed card with a zero-padding body and the page supplies only the table markup

#### Scenario: No page-defined table-card shells remain

- **WHEN** the gateway templates are searched for a hand-written bordered zero-padding card wrapping a table
- **THEN** no occurrence remains outside the shared component, and raw `<table>` markup inside the shared shell is acceptable until the go-daisy primitive adoption is unblocked

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

Extracting a pattern into the shared package SHALL NOT change rendered output, except for the intentional normalizations enumerated in the change's design (D2): the meta-row label class and row spacing, the snippet code-block class order, the panel accent variant, class-order-only differences that produce identical CSS, insignificant inter-element whitespace inside grouped selects, and the reveal panel's heading element.

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

### Requirement: Tool-group disclosures render through ui.Disclosure

The collapsible tool-picker group shell (capability groups and source/relay groups) SHALL render through `ui.Disclosure`, and the gateway SHALL NOT carry its own `agentToolDisclosure`.

#### Scenario: Capability group maps to Disclosure props

- **WHEN** a capability tool group is rendered
- **THEN** it calls `ui.Disclosure` with `GroupClass: "group/cap"`, `ItemsStart: true`, `Open` from the group's open state, `Attrs` carrying `data-testid="tool-group"` and `data-tool-group=<id>`, and `SummaryAttrs` carrying `data-testid="tool-group-header-<id>"`, with the capability leading block as the header and the capability controls as the trailing slot

#### Scenario: Source/relay group replaces the details and body bases

- **WHEN** a registry-server or relay-node tool group is rendered
- **THEN** it calls `ui.Disclosure` with `DetailsBase: "rounded-box border "+<BorderClass>` and `BodyBase: "flex flex-col border-t "+<BodyBorderClass>`, so the per-variant border/background tone is preserved and no unwanted `gap-2` or `p-3` is added to the body

#### Scenario: Tool-group markup is preserved

- **WHEN** a tool group is rendered
- **THEN** every `data-testid`, `data-tool-group`, form field `name`, `value`, and `checked` state asserted by `agent_ui_test.go` survives unchanged, and the chevron rotation (`group` vs `group/cap`) still works

### Requirement: Destructive-confirm icon circles render through a single component

The destructive-confirm icon circle — the error-tinted rounded square holding an icon — SHALL render through one shared `ConfirmIcon` component, so the delete / revoke / remove confirmation dialogs share a single definition.

#### Scenario: Default trash glyph

- **WHEN** `ConfirmIcon` is rendered with an empty icon
- **THEN** it renders the error-tinted rounded square holding the trash glyph

#### Scenario: Icon override

- **WHEN** `ConfirmIcon` is rendered with a caller-supplied icon
- **THEN** it renders that icon in place of the default trash glyph (for non-destructive error confirmations)

#### Scenario: No inline copies remain

- **WHEN** the gateway templates are searched for the `bg-error/10 text-error grid size-10` circle
- **THEN** no occurrence remains outside the shared component

### Requirement: Definition-list grids render through a single component

Definition-list grids of `MetaRow` / `MetaRowCustom` cells SHALL render through one shared `MetaGrid` component, with the row gap and any extra columns expressed as options rather than hand-rolled class strings.

#### Scenario: Grid renders the shared column/gap rhythm

- **WHEN** a `MetaGrid` is rendered
- **THEN** it emits the `<dl>` with the single grid layout, preserving the exact class order (`grid grid-cols-1 gap-x-6 {gap-y} sm:grid-cols-2 {cols}`)

#### Scenario: Extra columns and row gap are call-site options

- **WHEN** a page needs a different row gap or a wider column count
- **THEN** it passes those as options, and the rendered class string matches the pre-extraction grid exactly

### Requirement: Remaining duplicated markup is single-sourced

The last hand-rolled copies of already-shared patterns SHALL be replaced by calls to the shared component: the muted em-dash fallback (`EmptyDash`), the native dialog shell (`modalShell`), the bordered table card (`TableCard`), status badges (`StatusBadge`), and grouped select options (`SelectOptionGroups`).

#### Scenario: No remaining duplicate of the migrated patterns

- **WHEN** the gateway templates are searched for a local `emptyDash`, a hand-rolled confirm icon circle, a hand-rolled meta grid, a raw `<dialog class="modal">` shell, a hand-rolled bordered table shell, a raw revoked `@ui.Badge`, or a hand-rolled `<optgroup>` loop
- **THEN** each pattern exists only in its shared component, and the former duplicate sites call the shared component

### Requirement: Bare toggle inputs render through go-daisy form.ToggleInput

The six bare DaisyUI toggle checkboxes (agent tool-group and per-tool rows, MCP server and per-tool toggles, object boolean property, schedule enable) SHALL render through go-daisy `form.ToggleInput`, and the gateway SHALL NOT hand-roll these inputs.

#### Scenario: Each site maps to ToggleInput props

- **WHEN** one of the six bare toggle sites is rendered
- **THEN** it calls `form.ToggleInput` with the site's existing `Class` string passed through `Class`, its `Name`/`Value`/`Checked` fields set, and every `aria-label`, `data-*` hook, and `onchange` handler carried in `Attrs`, with the caller's surrounding markup (labels, forms, hidden inputs, wrapper divs) left untouched

#### Scenario: Canonical attribute order is the contract

- **WHEN** a bare toggle input is rendered
- **THEN** it emits the input in `ToggleInput`'s canonical order — `type`, `name`, `value` (when set), `class`, `checked` (when true), then `Attrs` alphabetically — and the resulting markup is semantically identical to the former hand-rolled input, differing only in attribute byte order and class placement

#### Scenario: Empty name on the MCP toggles is recorded

- **WHEN** the MCP server enable toggle or the MCP per-tool toggle is rendered
- **THEN** it emits `name=""` because `ToggleInput` always emits the `name` attribute, where the former hand-rolled inputs had none; the empty attribute carries no form value and is part of the component's locked contract
