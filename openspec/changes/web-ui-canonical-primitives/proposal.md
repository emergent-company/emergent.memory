## Why

Issue #1809 ("component-vs-component duplication; no canonical L0 named per surface") describes a gap the
adoption scanner cannot see. The `web-ui-component-conventions` spec already governs *inline-markup-vs-component*
— when an authored `.templ` file raw-redeclares a shared component's wrapper class instead of rendering it —
and the adoption ratchet (`TestAdoptionConsistencyRatchet`) machine-checks that axis. But nothing names
**which L0 primitive is canonical per surface** for the *component-vs-component* axis: when two components both
render the same surface (a page shell, a toggle, a list row, a status badge, a card), which one an author must
reach for. The consequences are concrete:

- **Duplicate live implementations.** The same surface has two first-class implementations that each accumulate
  call sites (e.g. `form.FormControl` vs `form.FormField`; `components.PanelCard`/`TableCard` vs direct
  `ui.CardRaw` in pages).
- **A committed component graph that silently merges same-named components.** The graph
  (`devgallery_graph_gen.go`) keys by bare identifier, so a local component and a go-daisy primitive sharing a
  name collapse into one node and the committed graph is wrong.

The machine-checkable half of #1809 already landed in PR #1816 (merge `8b770bbb`): the three ratchet gates,
including `canonical_ratchet_test.go` (deprecated-primitive check + local/go-daisy name-collision check). This
change delivers the rest of #1809 as one spec-plus-implementation unit: the **canonical-L0 registry in the
spec**, the **ListRow name-collision resolution**, and the **recorded dispositions** for the remaining surfaces.

## What Changes

**Spec — a canonical-L0 registry requirement.** Add a requirement to `web-ui-component-conventions` that each
UI surface has exactly one canonical primitive, named in a registry table in the spec; that deprecated or
non-canonical alternatives MUST NOT be used directly; and that a local component name MUST NOT collide (by bare
identifier) with a go-daisy primitive name referenced from authored sources. The registry table records, per
surface, the canonical primitive and its non-canonical alternatives, with the pending migration notes.

**Implementation — resolve the one active collision.** Rename the local `components.ListRow` (a link-style
list-page row) to `components.LinkRow`, so it no longer merges with go-daisy `ui.ListRow` (a card-style session
rail row) in the bare-identifier graph. The rename is output-neutral: no markup, class, or attribute change.

**Implementation — tighten the ratchet.** Empty `declaredCollisions()` (zero collisions now occur) and extend
`deprecatedPrimitives()` with the third banned primitive, `form.FormToggle` → canonical `form.ToggleInput`.

**Implementation — a watched latent hazard.** Record `StatusBadge` as a latent collision: the local
`components.StatusBadge` (37 direct `components.StatusBadge(` call sites, intent+icon wrapper over `ui.Badge`)
shares a bare identifier with go-daisy `ui.StatusBadge`, which exists in the pinned version but is never
referenced. It is **not** renamed (37 sites, no active hazard); it is watched so the hazard cannot silently
become active.

**Dispositions (no code change).** #1809 item 6's "migrate deprecated primitives" is **VOID** — the only two
deprecated primitives already tracked by the ratchet (`layout.Page`, `ui.PaginationCircle`) have zero call
sites. The stragglers (`SecretRevealPanel`, `AlertModal`, `SubNav`, `SnippetCard`) are recorded as **retained**,
each encapsulating an invariant and below the Rule-of-Three bar to consolidate.

## Capabilities

### Modified Capabilities

- `web-ui-component-conventions`: adds the canonical-L0-per-surface requirement (registry table, MUST-NOT-use
  deprecated alternatives, MUST-NOT-collide name rule) and its scenarios.

## Scope Boundary

- **Web UI gateway only.** No server, CLI, connector, or iOS change; no API, schema, or migration change.
- **This change resolves the *name collision* and pins the *registry*.** It does not perform the follow-up
  migrations the registry records as pending — those are deferred (see `tasks.md` archive-hold):
  - migrate the 8 `ui.CardRaw` page sites (across 6 files) onto `components.PanelCard`/`components.TableCard`
    (the other 5 `ui.CardRaw` sites are the canonical card shells themselves in `components/` and must not
    migrate);
  - resolve `form.FormField` vs `form.FormControl` (decision tracked in #1747);
  - adopt the go-daisy-dependent compositions (soft box → `ui.SoftBox`, dialog title, empty note, clipboard,
    tabs, dropdown) pending the upstream tag + gateway bump.
- **No rename of `StatusBadge`.** The 37-site local wrapper stays; the bare-name hazard is watched, not resolved.

## Impact

- **Gateway files:** `apps/web-ui/gateway/components/list.templ` (rename), `objects.templ`, `agent.templ`,
  `sessions.templ` (3 call-site files, 5 sites total), `inbox.templ` (comment only), the ratchet
  `*_test.go` files, and the regenerated component graph (`devgallery_graph_gen.go`).
- **No rendered-output change.** The rename is identifier-only; the graph regeneration reflects it.
- **Verification:** `templ generate`, `go build ./...`, `go test ./...`, `go test -run Ratchet -v ./...`,
  `golangci-lint run ./...` from `apps/web-ui/gateway`.
