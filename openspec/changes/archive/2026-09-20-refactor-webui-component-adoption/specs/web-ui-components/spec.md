## MODIFIED Requirements

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

## ADDED Requirements

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
