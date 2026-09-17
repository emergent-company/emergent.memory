## ADDED Requirements

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

Adopting a go-daisy component SHALL NOT change rendered output; the exact-render tests (`refactor_exact_test.go` and others) SHALL keep passing with the `want` strings unchanged, repointed at the library component.

#### Scenario: Exact-render tests pass unchanged

- **WHEN** the gateway unit tests are run after adoption
- **THEN** the exact-render assertions for the migrated components pass with the same expected markup, updated only to call `ui.Dialog`/`ui.ConfirmDialog` instead of the deleted local components

#### Scenario: Verification gate is clean

- **WHEN** `templ generate ./...`, `go build ./...`, `go test ./...`, and `task lint` are run from `apps/web-ui/gateway`
- **THEN** all succeed
