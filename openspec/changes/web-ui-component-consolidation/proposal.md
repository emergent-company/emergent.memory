## Why

The gateway carries duplicate component-vs-component implementations and hand-rolled markup where a canonical
primitive already exists, so the same surface drifts independently across pages and token/scale changes fail to
propagate. The audit found six concrete gaps, all already recorded as pending migrations in the
`web-ui-canonical-primitives` change (section 6 of its `tasks.md`):

- **Panel/table card** — pages still open the raw `ui.CardRaw` shell instead of `components.PanelCard` /
  `components.TableCard`, so the card-body density and border tokens live in two places.
- **Form field** — `form.FormField` is still used after `form.FormControl` became the canonical wrapper; the
  canonical decision was deliberately deferred on #1747 and is resolved here.
- **Clipboard copy** — copy-to-clipboard markup is hand-rolled around a page-local `apiTokenCopyScript` /
  `shareManageScriptOnce` and `data-copy-target` attributes, where the canonical `form.ClipboardCopy` (go-daisy
  v0.15.1, `components/form`) already exists.
- **Note / empty-state copy** — muted note paragraphs are re-typed per page (`text-[11px]` dense-metadata tier
  included) where `ui.EmptyNote` and its size tiers exist.
- **Dialog heading** — dialog/modal headings are re-typed as `<h3 class="text-lg font-semibold">` where
  `ui.DialogTitle` exists.
- **Soft box** — the soft-box surface (`rounded-box border <border> bg-base-200/<fill>`) is duplicated as raw
  class strings where `ui.SoftBox`'s `Surface` axis exists.

Why now: go-daisy **v0.15.1** (bumped in this branch) ships `ui.DialogTitle`, `ui.EmptyNote` (with the `Tone`
axis that renames the muted-foreground utilities to `base-content/N`), the `ui.SoftBox` `Surface` axis, and the
`Attrs` slot on `ui.SoftBox` — the primitives whose absence previously blocked the go-daisy-dependent half of
this consolidation. The migrations are now unblocked.

Two more things make this CI-enforced rather than advisory. First, the ratchet that enforces these conventions
scans `.templ` sources only, so class lists assembled in **Go string literals** are invisible to it — the known
cases are the `DetailsBase:` soft-box strings in `agent.templ` and `mcp_servers.templ`. Second, the ratchet
budgets are maintained tightly, so an adoption must lower the budget in the same change and a new unbudgeted
finding fails the build. This change specifies both.

Third — the enforcement gap that makes this change real rather than cosmetic — **only** soft-box adoption moves
a ratchet budget today (`patternBudget()`'s `surface-opacity-drift` / `border-token-drift`, which flag only the
non-canonical `bg-base-200/<N≠40>` and `border-base-content/<N≠10>` drift, never the canonical surface itself).
The raw page `card-border` shells (direct `ui.CardRaw`), `form.FormField`, hand-rolled dialog `<h3>` titles, and
muted-note paragraphs are covered by **no** budget, so adopting them would not be CI-enforced at all. This change
adds marker/pattern coverage for each eliminated surface, seeded at the pre-adoption count and lowered as each
adoption lands.

## What Changes

- **Panel/table card adoption** — every page surface that opens the raw `ui.CardRaw` shell renders
  `components.PanelCard` / `components.TableCard` instead. The `ui.CardRaw` uses inside `components/` are the
  canonical shells themselves and are **not** migrated (the distinction is encoded in the spec).
- **Form-field adoption** — every remaining `form.FormField` call site renders through `form.FormControl`,
  resolving the deferred #1747 decision (FormControl canonical, FormField deprecated).
- **Clipboard-copy adoption** — clipboard-copy markup renders through `form.ClipboardCopy` (go-daisy v0.15.1),
  retiring the page-local copy script and `data-copy-target` wiring. The triplicated client-side copy JS in
  `gateway/webui/static/js/{chat-host,object-preview,chat-components}.js` is dispositioned explicitly (see tasks).
- **Note / empty-state adoption** — every note/empty-state renders through `ui.EmptyNote`, using its size tiers,
  including the `2xs` dense-metadata tier for the `text-[11px]` sites. The note surface is defined by an explicit
  predicate (muted-foreground token + size tier, with `dt`/`label`/`font-mono`/`tracking-*` exclusions) and the
  count is recorded at time of writing rather than asserted from a stale audit.
- **Dialog-heading adoption** — every dialog/modal heading renders through `ui.DialogTitle`.
- **`EmptyNote` name disambiguation** — the gateway already uses the bare identifier `EmptyNote` for its
  checkbox-picker content (struct field `EmptyNote` and helpers `pickerEmptyNote` / `delegateEmptyNote` /
  `skillEmptyNote` / `objectTypeEmptyNote` / `relationshipTypeEmptyNote` in `ui.templ`), which is a *different*
  concept from the go-daisy `ui.EmptyNote` note primitive this change adopts. Those local identifiers are renamed
  so a reader — and the component graph — cannot confuse picker content with the canonical note.
- **Soft-box adoption** — soft-box surfaces render through `ui.SoftBox`'s `Surface` axis, with a **named,
  justified exception list** for the soft boxes that cannot use the primitive (non-`div` elements, fills off the
  `Surface` axis, hover/conditional fills, and class lists assembled in Go string literals).
- **Ratchet-hole closure** — the scanner must cover class lists built in Go string literals too (or otherwise
  prove they cannot drift from the canonical surface), so the `DetailsBase:` soft-box strings cannot silently
  drift.
- **Ratchet semantics** — the budgets are maintained tightly: an adoption lowers the budget in the same change,
  and a new unbudgeted finding fails; the adoption is CI-enforced, not advisory.

The spec and its implementation ship in **one** PR; this directory is the spec half, reconciled here against the
shipped implementation.

## Capabilities

### Modified Capabilities

- `web-ui-component-conventions` — adds the six adoption requirements (panel/table card, form control,
  clipboard, empty note, dialog title, soft box), the ratchet-hole requirement, and the ratchet-semantics
  requirement; and extends the tracked ad-hoc-markup marker inventory so the detector and ratchet machine-check
  the new surfaces.

## Impact

- **Web UI gateway only** (`apps/web-ui/gateway`). No server, CLI, connector, or iOS change; no API, schema, or
  migration change.
- Adoptions are **output-neutral** — the rendered result is visually identical, with ids, `data-testid`, `aria-*`,
  `data-*` hooks, htmx wiring, and form `action`/`method` preserved — per the existing "Tracked ad-hoc markup gaps
  are closed" rule. Output-neutral is *not* byte-identical: the primitive composes the class attribute, so class
  order may differ and a token name may change to the primitive's canonical utility (e.g. `text-muted-faint` →
  `text-base-content/40`, colour-equivalent). The one exception is `form.FormField` → `form.FormControl`, which
  intentionally changes the wrapper markup and is recorded as a documented delta, not as output-neutral.
- The ratchet budget tables (`consistency_ratchet_budget_test.go`), the page-`ui.CardRaw` check
  (`card_raw_ratchet_test.go`), and the deprecated-primitive check (`canonical_ratchet_test.go`) are tightened in
  the same change as each adoption.
