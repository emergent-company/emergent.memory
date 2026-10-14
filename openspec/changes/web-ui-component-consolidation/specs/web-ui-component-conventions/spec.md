## MODIFIED Requirements

### Requirement: Tracked ad-hoc markup gaps are closed

The gateway's authored `.templ` sources SHALL render through the owning shared component for every
detector-tracked wrapper marker, so that `adoptionGaps()` is empty apart from sites explicitly recorded in
`adoptionAllowlist` with a documented reason. Sites SHALL be migrated without changing rendered output: ids,
`data-testid`, `aria-*`, `data-*` hooks, htmx wiring, and form `action`/`method` SHALL be preserved.

"Output-neutral" has a precise meaning and SHALL be read this way: the rendered result is **visually identical**.
It does **not** promise a byte-identical `class` attribute. The owning primitive composes the attribute, so
(a) class *order* may differ from the hand-rolled list, and (b) a class *token name* may differ where the
primitive owns the canonical utility. The canonical example is the muted-foreground tone: `ui.EmptyNote` emits
`text-base-content/40` where the page previously wrote `text-muted-faint`. Those are **colour-equivalent**, not a
regression — both resolve to `color-mix(in oklab, var(--color-base-content) 40%, transparent)` — because
go-daisy's `Tone` axis renamed the utility, not the colour. A reviewer SHALL treat a token rename to the
primitive's canonical utility as output-neutral, and only a change to a *rendered value* (an id, a testid, a hook,
a colour step, a layout box) as a real output change.

The one adoption that is **not** output-neutral is the form wrapper: `form.FormField` → `form.FormControl`
changes the wrapper markup (`form-control mb-3` + `label` → `fieldset` + `fieldset-legend`), recorded in "Form
fields render through FormControl" as a documented delta, not as output-neutral.

- `btn`: `<button>` sites render through `ui.Button`; `<a>` sites through `ui.Button` with a link type; join-group `<label>` sites through `ui.Button` with a label type. Variants map to the variant axis, app-specific classes to the extra-class slot, and htmx/`data-*`/`aria-*` to `Attrs`.
- `card` / `card-border`: shells render through `ui.CardRaw` / `components.PanelCard` / `components.TableCard`, passing the existing card and body classes so the wrapper markup is unchanged.
- `badge`: raw spans render through `ui.Badge` / `components.StatusBadge`.
- `toggle`: bare toggle inputs render through go-daisy `form.ToggleInput`.
- `form-field`: `form.FormField` call sites render through `form.FormControl` (the canonical form wrapper); `form.FormField` is deprecated. See "Form fields render through FormControl".
- `clipboard`: raw copy-to-clipboard markup renders through `form.ClipboardCopy`, retiring the page-local copy script and `data-copy-target` wiring. See "Clipboard-copy markup renders through ClipboardCopy".
- `empty-note`: raw muted note/empty-state paragraphs render through `ui.EmptyNote` at the matching size tier. See "Notes and empty-state copy render through EmptyNote".
- `dialog-title`: raw dialog/modal headings render through `ui.DialogTitle`. See "Dialog and modal headings render through DialogTitle".
- `soft-box`: raw soft-box surfaces render through `ui.SoftBox`'s `Surface` axis, except the recorded residual. See "Soft-box surfaces render through SoftBox's Surface axis".

A site that cannot adopt without changing rendered output SHALL be recorded in `adoptionAllowlist` keyed by view basename and marker, with a documented reason, and — where a shared-component limitation blocks it — filed upstream before the gap is considered closed.

#### Scenario: Tracked button sites render through the shared button

- **WHEN** the gateway templates are searched for a raw `btn` class outside `components/`
- **THEN** no finding remains, because `<button>`, `<a>`, and join-group `<label>` sites render through `ui.Button`

#### Scenario: Tracked card sites render through the shared card

- **WHEN** the gateway templates are searched for a raw `card` / `card-border` shell outside `components/`
- **THEN** no unallowlisted finding remains, because the shells render through the shared card components

#### Scenario: Tracked badge and toggle sites render through the shared primitive

- **WHEN** the gateway templates are searched for a raw `badge` span or a bare `toggle` input
- **THEN** no unallowlisted finding remains, because the sites render through the shared badge primitive or `form.ToggleInput`

#### Scenario: Tracked consolidation sites render through the owning component

- **WHEN** the gateway templates are searched for a raw `form-field`, `clipboard`, `empty-note`, `dialog-title`, or `soft-box` marker outside `components/`
- **THEN** no unallowlisted finding remains, because each site renders through its owning component named in the marker inventory

#### Scenario: Accepted local markup is deliberate and documented

- **WHEN** a legitimate raw site cannot adopt without changing rendered output
- **THEN** it appears in `adoptionAllowlist` with a documented reason, and the gallery suppresses it

#### Scenario: The gallery reports no unaccepted gaps

- **WHEN** the findings are regenerated on the adopted tree
- **THEN** every remaining finding is an allowlisted site, and the gallery adoption-gaps section renders its empty state

## ADDED Requirements

### Requirement: Adoption depends on the pinned go-daisy version

The notes adoption SHALL depend on go-daisy ≥ v0.15.0 for the `Tone` axis (which renames the muted-foreground
utilities to their `base-content/N` equivalents, e.g. `text-muted-faint` → `text-base-content/40`) and ≥ v0.15.1
for the `Attrs` slot on `ui.SoftBox`; the gateway pins **v0.15.1**. This is an explicit dependency, not incidental:
the primitive owns the class list, so the canonical utility names (and the `Attrs` pass-through that lets a soft
box carry caller attributes) only exist at those versions. A bump that removes either axis SHALL fail the adoptions
that rely on it.

#### Scenario: The pinned version provides the adopted axes

- **WHEN** the gateway adopts `ui.EmptyNote` (needing the `Tone` axis) and `ui.SoftBox` with caller attributes (needing the `Attrs` slot)
- **THEN** `go.mod` pins go-daisy v0.15.1, which ships both, and no adoption writes the canonical utility by hand

### Requirement: Page card surfaces render through the shared card shell

A page surface that opens the raw `ui.CardRaw` shell SHALL render through `components.PanelCard` or
`components.TableCard` instead of invoking `ui.CardRaw` directly. The `ui.CardRaw` uses **inside `components/`**
are the canonical card shells themselves (`components.PanelCard`, `components.TableCard`, and the
`components.SecretRevealPanel` / `components.ApprovalCard` / `components.QuestionCard` variants that layer a
domain wrapper over the shell) and SHALL NOT be migrated; they are the owning implementations, not adopters.

The migration is enabled by **optional** parameters added to the two shells, whose defaults reproduce the previous
page literals exactly so no existing caller changed: `components.PanelCard(extraClass, attrs, bodyClass ...string)`
(default body density `memory-density-comfortable`) and `components.TableCard(attrs, opts ...TableCardOpt)` where
`TableCardOpt{CardClass, BodyClass}` default to `overflow-hidden shadow-sm` / `memory-density-flush`. A page whose
panel needs a different density or whose flush list uses a shadow-less border passes its exact classes through
those options rather than opening the raw shell.

#### Scenario: A page shell resolves to the shared card

- **WHEN** a page renders a card-like shell for a panel or a list table
- **THEN** it renders `components.PanelCard` (panel) or `components.TableCard` (bordered table wrapper) rather than a direct `ui.CardRaw` call

#### Scenario: The canonical card shells keep their own primitive

- **WHEN** the detector scans `components/` for `ui.CardRaw`
- **THEN** those uses are not flagged, because they are the owning card shells, not page-level adopters

#### Scenario: Card adoption preserves the rendered wrapper

- **WHEN** a page card shell is migrated
- **THEN** the rendered wrapper classes (card, card-body density, border) and any `data-testid` / htmx wiring are preserved — the optional parameters default to the prior literals — matching the "Tracked ad-hoc markup gaps are closed" output rule

### Requirement: Form fields render through FormControl

`form.FormControl` SHALL be the canonical form wrapper; every remaining `form.FormField` call site SHALL
render through `form.FormControl`, and `form.FormField` SHALL be treated as a deprecated primitive. This
resolves the #1747 decision that was deferred in the canonical-L0 registry.

The migration reduces the `form.FormField` reference count from **7 to 1**: the 6 `share_manage.templ` call
sites migrate to `form.FormControl`, and the single remaining reference is the exempt gallery fixture
(`devgallery_daisy_fixtures.go`), which demonstrates the deprecated primitive and is not a page rendering it.
This migration **intentionally changes wrapper markup** — `form.FormField` emits `form-control mb-3` + a `label`,
while `form.FormControl` emits a `fieldset` + `fieldset-legend` — and is therefore a documented behaviour-visible
delta, not an output-neutral swap.

#### Scenario: A form field resolves to FormControl

- **WHEN** an authored source calls `form.FormField`
- **THEN** it is migrated to `form.FormControl` (label position, hint, and error message mapped to its parameters)

#### Scenario: FormField is banned by the ratchet

- **WHEN** the ratchet scans the authored sources for `form.FormField`
- **THEN** it fails if a direct call site is introduced, mirroring the existing deprecated-primitive check (`layout.Page`, `ui.PaginationCircle`, `form.FormToggle`)

#### Scenario: The gallery fixture is not a page adopter

- **WHEN** the gallery's fixture registry exercises a deprecated primitive to demonstrate it
- **THEN** it is exempt from the adoption rule, because it is documentation of the component, not a page rendering it

### Requirement: Clipboard-copy markup renders through ClipboardCopy

Clipboard-copy markup SHALL render through the canonical `form.ClipboardCopy` primitive (go-daisy v0.15.1,
`components/form`), retiring the page-local copy script (`apiTokenCopyScript` / `shareManageScriptOnce`) and the
`data-copy-target` wiring it depends on. A copy affordance SHALL be expressed as a `ClipboardCopyItem` (id,
label, value, mono) rather than as a hand-rolled input plus button plus script.

The ratchet floor for the hand-rolled clipboard surface is **11**, held as two recorded residuals with their
reasons:

- `object_preview.templ` — 1: an icon-only copy with its own `data-testid`/sr-only target wired by
  `object-preview.js`, not a `form.ClipboardCopy` field.
- `share_manage.templ` — 10: the reveal-once flow inlines the secret via `Mono`, which `form.ClipboardCopy`
  documents as unsuitable for on-demand secrets, so it stays a bespoke `data-reveal-url` fetch.

`apiTokenCopyScript` is deleted; `SnippetCard` is now a 2-argument mono badge (`SnippetCard(label, value)`), and
the chroma JSON syntax highlighting it previously rendered is removed with it (the caller owns highlighting when it
wants it).

#### Scenario: A copy affordance renders through ClipboardCopy

- **WHEN** a page renders a click-to-copy field or a copy button for an already-known value
- **THEN** it renders `form.ClipboardCopy` with the value's `ClipboardCopyItem`, and no page-local copy script remains

#### Scenario: Copy feedback is the component's concern

- **WHEN** a copy button is clicked
- **THEN** the "Copied" feedback comes from `form.ClipboardCopy`'s own client wiring, not from a page-authored `navigator.clipboard` handler

#### Scenario: An unsuitable copy affordance is a recorded residual

- **WHEN** a copy affordance cannot adopt `form.ClipboardCopy` (an icon-only copy, or a reveal-once secret the component documents as unsuitable)
- **THEN** it is recorded in the clipboard budget with its reason, rather than re-typed or silently left to drift

#### Scenario: Client-side copy JS is not duplicated into the server templ path

- **WHEN** a page adopts `form.ClipboardCopy`
- **THEN** the server-templ copy affordance no longer depends on the client-rendered copy JS in `gateway/webui/static/js/{chat-host,object-preview,chat-components}.js`, and the disposition of that JS (retained for client-rendered panels, or retired) is recorded rather than left to drift

### Requirement: Notes and empty-state copy render through EmptyNote

Muted note and empty-state copy SHALL render through `ui.EmptyNote`, selecting the size tier that matches the
site: `sm` (`text-sm`, the default), `xs` (`text-xs`), or `2xs` (`text-[11px]`, the dense-metadata tier for
timestamps, keyboard hints, and identifiers). A raw muted paragraph that is note copy — not an `Eyebrow` label,
not a `font-mono` code fragment, not a field `dt` label — SHALL adopt the primitive.

A **note site** is defined by an explicit predicate, so a ratchet can hold the count rather than a prose
approximation. The predicate is: an authored `.templ` `<p>` element (outside `components/`, excluding generated
`*_templ.go`) whose `class` attribute carries, as whole tokens, at least one muted-foreground token — `text-muted`,
`text-muted-faint`, `text-muted-strong`, `text-base-content/40`, `text-base-content/50`, or `text-base-content/60`
(`text-muted-bright` is deliberately absent: it is the brighter step, not the muted note tier) — and at least one
size token — `text-sm`, `text-xs`, or `text-[11px]` — in any order. Non-note elements are excluded: anything carrying
`font-mono`, `uppercase`, `tracking-*`, or `hidden` (code fragments, eyebrow/label copy, and the hidden duplicate
note); `<dt>` field labels are outside the `<p>`-only scope. The ratchet implements this token-exact,
order-independent predicate in `scanPatternFile` / `isNoteSite` (`consistency_ratchet_test.go`).

The adoption lands **~224** `ui.EmptyNote` call sites across the authored templates, and the ratchet holds two
recorded residuals with their reasons:

- `object_preview.templ` — 1: an icon-led note with `gap-1` + `size-3`, where `EmptyNote`'s icon gap is fixed at
  `1.5`.
- `schema.templ` — 1: a bordered centred empty-properties box (`rounded-box border-dashed text-center`), which is a
  box, not a note.

`devgallery*` files are outside the scan by design (the gallery's own fixtures), matching the pre-existing
`scanAdoption` skip rule.

The gateway also reused the bare identifier `EmptyNote` for its checkbox-picker content — the struct field
`EmptyNote` (`ui.templ`) and the helpers `pickerEmptyNote` / `delegateEmptyNote` / `skillEmptyNote` /
`objectTypeEmptyNote` / `relationshipTypeEmptyNote` — a *different* concept from the go-daisy `ui.EmptyNote`
note primitive. Those local identifiers are renamed to `EmptyContent` / `pickerEmptyContent` /
`delegateEmptyContent` / `skillEmptyContent` / `objectTypeEmptyContent` / `relationshipTypeEmptyContent` (the
`pickerHiddenNote` helper keeps its name) so a reader and the component graph cannot mistake picker content for the
canonical note.

#### Scenario: A note adopts the matching tier

- **WHEN** a page renders a muted note or empty-state line
- **THEN** it renders `ui.EmptyNote` at the tier whose type scale matches the prior markup (`sm`, `xs`, or `2xs` for `text-[11px]`)

#### Scenario: The dense-metadata tier is used, not re-typed

- **WHEN** a site renders `text-[11px]` muted copy that is note text
- **THEN** it renders `ui.EmptyNote` with `Size: EmptyNoteSize2XS` instead of a literal `text-[11px]` class

#### Scenario: Labels and code are not mistaken for notes

- **WHEN** a `text-[11px]` element is an `Eyebrow` kicker, a `font-mono` code span, or a field `dt` label
- **THEN** it is not adopted to `EmptyNote`, because those are not note copy

### Requirement: Dialog and modal headings render through DialogTitle

A dialog, modal, panel, or card heading SHALL render through `ui.DialogTitle`, using its `Level` (`h3` default,
`h2`), `Size` (`lg` default, `md`), and `Tracking` degrees of freedom instead of a hand-typed
`text-lg`/`text-base font-semibold` class list.

The gate keeps **two separate patterns** for the heading shape: the `<h3>` dialog-heading shape (`dialog-title`)
and the `<h2>` section-heading shape (`dialog-title-section`). The `<h3>` shape is **fully adopted — 0 residual**
(the only non-default sizing is a single `DialogTitleSizeMD` site in `mcp_registry.templ`). The `<h2>` shape is a
**recorded residual of 7** (agent.templ ×1, blueprints.templ ×1, schema.templ ×2, share_manage.templ ×2,
share_page.templ ×1): those are section/card headings, not dialog titles, and no canonical primitive exists for a
bare section heading, so they are a deliberate residual rather than an adoptable surface.

#### Scenario: A dialog heading adopts the primitive

- **WHEN** a dialog or modal renders a heading
- **THEN** it renders `ui.DialogTitle` (children carry the title text), preserving the semantic heading level and any `id` the dialog's `aria-labelledby` points at

#### Scenario: A section heading is a recorded residual, not a dialog title

- **WHEN** an `<h2>` carries the `text-lg font-semibold` shape as a section/card heading rather than a dialog title
- **THEN** it is budgeted under `dialog-title-section` with its reason, because no bare-section-heading primitive exists

#### Scenario: The heading shape is not re-typed per page

- **WHEN** a heading's level, scale, or tracking needs to change
- **THEN** the change is expressed through `ui.DialogTitle`'s props, not by editing a per-page class list

### Requirement: Soft-box surfaces render through SoftBox's Surface axis

A soft-box surface — `rounded-box` plus a hairline border plus a `bg-base-200` fill — SHALL render through
`ui.SoftBox`, selecting the fill with the `Surface` axis (`SoftBoxDefault` = `bg-base-200/40`,
`SoftBoxMuted` = `bg-base-200/30`, `SoftBoxPlain` = unfilled). The border token `border-line-translucent`
(which `ui.SoftBox` emits) resolves to the same value as the literal `border-base-content/10` previously
hand-rolled, so adoption changes no colour. The `Attrs` slot (go-daisy ≥ v0.15.1) carries caller attributes onto
the primitive's `<div>`.

After adoption the ratchet floor is **8**, held as five recorded residuals (each is a deliberate exception, not
silent drift):

- `agent.templ` — 1: `border-dashed` (the agent tool-group empty state); `ui.SoftBox` is a solid hairline only.
- `backups.templ` — 2: non-`div` `<label>` backup-include rows; `ui.SoftBox` renders a `<div>`.
- `mcp_nodes.templ` — 1: a selected/unselected conditional fill (`templ.KV bg-primary` vs `bg-base-200/40`); there
  is no data-driven surface.
- `mcp_servers.templ` — 3: a non-`div` `<details>` row, a `DetailsBase:` Go-string literal, and a `border-dashed`
  group.
- `ui.templ` — 1: a non-`div` `<pre>` code block; `ui.SoftBox` renders a `<div>`.

The residual reasons are therefore exactly four: dashed borders, non-`div` hosts (`<pre>`/`<label>`/`<details>`
contexts), off-axis/dynamic fills, and `DetailsBase:` Go-string literals. Every remaining raw soft-box surface is
one of these recorded residuals, so none drift unrecorded. A shared-component limitation that blocks an otherwise
adoptable soft box SHALL be filed upstream, mirroring the existing "Tracked ad-hoc markup gaps are closed" rule.

#### Scenario: A soft box adopts the Surface axis

- **WHEN** a page renders a `rounded-box` + border + `bg-base-200/40` or `bg-base-200/30` surface on a `div`
- **THEN** it renders `ui.SoftBox` with the matching `Surface` (`SoftBoxDefault` or `SoftBoxMuted`)

#### Scenario: A non-div or off-axis soft box is a recorded exception

- **WHEN** a soft-box surface is a non-`div` element, uses a fill outside `/40` and `/30`, is hover/conditional, uses a dashed border, or is a Go-string literal
- **THEN** it appears in the recorded residual list with its reason, rather than adopting `ui.SoftBox` or being left as silent drift

#### Scenario: The residual is deliberate and reviewable

- **WHEN** the tree is scanned for raw soft-box surfaces
- **THEN** every remaining site is one of the recorded residuals, so no soft-box surface drifts unrecorded

### Requirement: Class lists assembled in Go string literals are recorded residuals

Class lists assembled in Go string literals (rather than authored directly in `.templ` markup) SHALL be governed
by the same canonical-surface check. The `.templ` line-scanner (`scanPatternFile`) already reads every line of an
authored `.templ` file, so a literal `DetailsBase:` string that spells out the canonical soft-box tokens (e.g.
`"rounded-box border border-base-content/10 bg-base-200/40"` in `mcp_servers.templ`) is matched and recorded as a
soft-box residual. A class list that is split across string concatenation or built from a prop whose fill is not in
the literal (e.g. `"rounded-box border " + p.BorderClass` in `agent.templ`) cannot drift from the canonical surface
in the literal and is likewise recorded as a residual rather than parsed. Where a Go-string surface cannot be
resolved to the canonical check, it SHALL be a recorded residual, never silent drift.

#### Scenario: A literal Go-string class list cannot drift silently

- **WHEN** a soft-box or token class list is spelled out in a Go string literal in an authored `.templ` file
- **THEN** the `.templ` line-scanner matches it to the canonical surface (or records it as a residual), so it cannot drift undetected

#### Scenario: A concatenated or prop-fed class list is a recorded residual

- **WHEN** a class list is built by string concatenation or receives its fill from a prop
- **THEN** the site is recorded as a residual with its reason, rather than being parsed or left unrecorded

### Requirement: Adoption is CI-enforced by a tight ratchet

Adoption of a canonical primitive SHALL be CI-enforced, not advisory: the ratchet budgets
(`adoptionBudget()` and `patternBudget()` in `consistency_ratchet_budget_test.go`, the page-`ui.CardRaw` check in
`card_raw_ratchet_test.go`, and the deprecated-primitive check in `canonical_ratchet_test.go`) are maintained at
exact equality against the current tree.

The ratchet semantics SHALL be read precisely:

- **Exact equality in both directions.** A budget entry that no longer matches the tree (actual 0) fails the same
  as a new unbudgeted finding.
- **A new unbudgeted finding fails the build.** Authored markup that introduces a raw marker or drift token with no
  budget entry fails until it is adopted or given a budget entry with a justification note.
- **A fix must lower the budget in the same commit.** An adoption that removes raw markup SHALL lower (or remove)
  the matching budget entry in the same change; the ratchet fails if the tree and budget disagree in either
  direction.
- **Test-only.** The ratchet lives in `_test.go` files (`consistency_ratchet_budget_test.go`,
  `consistency_ratchet_test.go`, `card_raw_ratchet_test.go`, `canonical_ratchet_test.go`); it is a compile-time
  `go test` gate and never a runtime check.
- **Scan scope.** The pattern scanner covers authored top-level gateway `*.templ`, excluding `devgallery*`,
  `*_test.templ`, and `fixtures_test.templ` — mirroring the pre-existing `scanAdoption` skip rule (the
  `components/` package is outside the top-level scan by construction).

#### Scenario: An adoption lowers the budget in the same change

- **WHEN** a site adopts a shared component and removes raw markup
- **THEN** the matching budget entry is lowered (or removed) in the same commit, and the ratchet fails if the tree and budget disagree in either direction

#### Scenario: A new unbudgeted finding fails the build

- **WHEN** authored markup introduces a raw marker or drift token with no budget entry
- **THEN** the ratchet fails, so the drift cannot land unreviewed

#### Scenario: A residual is a decision, not a defect

- **WHEN** a raw site legitimately cannot adopt
- **THEN** it is admitted only through a budget entry (or `adoptionAllowlist` entry) carrying a justification note — to add one, the author appends an entry naming the pattern, the file, the exact count, and the reason, in the same commit as the tree change

#### Scenario: A justified exception is budgeted, not ignored

- **WHEN** a raw site legitimately cannot adopt
- **THEN** it appears as a budget entry with its justification, mirroring the gallery's allowlist reasoning

#### Scenario: A surface gains coverage before it is adopted

- **WHEN** a surface currently unobservable to the detectors (panel/table card, form field, clipboard, empty note, dialog title, or the soft-box surface itself) is adopted
- **THEN** marker/pattern coverage for that surface is added in the same change, seeded at the pre-adoption count, so the adoption is ratchet-enforced rather than silent

### Requirement: Accepted behaviour-visible deltas are documented consequences

Adoption SHALL record its behaviour-visible consequences as deltas, not as defects or as output-neutral claims. The
deltas accepted by this change are:

- Copy values render as a `<code>` badge (the `Mono` variant) or a read-only `<input>` (the default variant), not
  as a highlighted block.
- The copy button is an outline rather than a ghost button.
- `break-all` / `min-w-0` wrapping classes are dropped where the primitive owns the line-breaking.
- `SnippetCard` loses its chroma JSON syntax highlighting (the value is now a plain mono string; the caller owns
  highlighting when it wants it).
- `form.FormField` → `form.FormControl` changes the wrapper markup (`form-control mb-3` + `label` → `fieldset` +
  `fieldset-legend`).

Each SHALL be stated in the change where the adoption that introduced it lives, so a reviewer reads it as accepted,
not as a regression.

#### Scenario: A behaviour change is declared, not hidden

- **WHEN** an adoption changes a rendered value or structure that is not output-neutral
- **THEN** the change documents the delta as an accepted consequence, naming the before and after

### Requirement: The Playwright suite is realigned to the adopted DOM

The Playwright e2e suite SHALL be realigned to the DOM the adopted primitives produce, rather than left asserting
the retired markup. Specifically:

- Secrets (`#api-token-secret`, `#mcp-share-key`, the agent MCP key secret) are read via `inputValue()` /
  `toHaveValue`, because `form.ClipboardCopy`'s input variant stores the plaintext in a read-only `<input value=…>`,
  not `textContent`.
- The endpoint URL's anchor is restored via a wrapper (`#agent-mcp-endpoint-url`) so the spec's selector survives
  the `Mono` variant (which renders a `<code>` badge with no id).
- The retired `data-copy-target` wiring assertion is replaced by the stronger clipboard-payload poll
  (`navigator.clipboard.readText`).

The **session-mode** suite requires a live gateway in session mode (`AUTH_MODE=session` + Zitadel) plus a Zitadel
test user, and SHALL be recorded as **not run in this environment** rather than implied green — only the realigned
specs are committed; the live suite is run where a session-mode gateway and test user are available.

#### Scenario: Secrets are read from the input value

- **WHEN** a spec reads a copied secret
- **THEN** it uses `inputValue()` (or `toHaveValue`) on the `form.ClipboardCopy` input, not `textContent`

#### Scenario: A removed hook is not asserted

- **WHEN** an adoption retires a DOM hook (e.g. `data-copy-target`)
- **THEN** the spec drops that assertion in favour of the surviving observable (the clipboard payload), not a stale selector

#### Scenario: Session-mode coverage is not claimed green

- **WHEN** the session-mode suite cannot run (no live gateway / Zitadel test user)
- **THEN** the change records that it was not run, rather than implying a passing run

### Requirement: Shared-component limitations are fixed upstream, not worked around

Limitations discovered in the shared go-daisy components during adoption SHALL be fixed upstream rather than worked
around in the gateway. The out-of-scope fixes that landed upstream as part of this work are:

- `form.ClipboardCopy` rendered `type=""` (defaulting to a submit button) with no failure path — fixed upstream.
- `ui.SoftBox` had no `Attrs` slot, so caller attributes could not pass through — added upstream (≥ v0.15.1).
- `templ.RenderAttributes` silently drops non-string attribute values — documented in go-daisy.

#### Scenario: A shared-component gap is filed, not papered over

- **WHEN** an adoption is blocked by a shared-component limitation (a missing slot, a wrong default, a silent drop)
- **THEN** the limitation is fixed or documented upstream, and the gateway adopts the fixed primitive rather than a local copy
