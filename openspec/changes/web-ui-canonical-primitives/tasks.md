<!-- openspec:archive-hold: go-daisy upstream bump + follow-up migrations pending (CardRaw→PanelCard/TableCard, FormField vs FormControl per #1747, go-daisy-dependent compositions) -->

## 1. Canonical-L0 registry in the spec

- [x] 1.1 Add the "each UI surface has exactly one canonical primitive" requirement to `web-ui-component-conventions`, including the MUST-NOT-use-deprecated-alternatives and MUST-NOT-collide-by-bare-identifier rules, with scenarios.
- [x] 1.2 Add the "canonical-L0 registry records the surface canon" requirement with the registry table (page shell, toggle field, pagination, link/list row, status badge, panel/table card, form field) and scenarios.
- [x] 1.3 Validate with `openspec validate web-ui-canonical-primitives`.

## 2. Resolve the ListRow name collision (output-neutral rename)

- [x] 2.1 Rename local `components.ListRow` → `components.LinkRow` in `components/list.templ` (definition + doc comment), with no markup/class/attribute change.
- [x] 2.2 Update the 5 call sites: `objects.templ` (3), `agent.templ` (1), `sessions.templ` (1).
- [x] 2.3 Update the `inbox.templ` doc comment (line ~158) that references `components.ListRow`.
- [x] 2.4 Regenerate the component graph so `devgallery_graph_gen.go` shows `LinkRow` (and no longer merges the link row with `ui.ListRow`).

## 3. Tighten the ratchet

- [x] 3.1 Empty `declaredCollisions()` (zero active collisions) and update the file-header rationale to state there are now zero collisions, noting `StatusBadge` as a watched latent hazard.
- [x] 3.2 Extend `deprecatedPrimitives()` with `form.FormToggle` → canonical `form.ToggleInput` (keeping `layout.Page` and `ui.PaginationCircle`). Keep `deprecatedBudget()` empty/seed-consistent.
- [x] 3.3 Add a minimal latent-collision watch: each declared latent pair (currently `StatusBadge`) must still have its local component defined and its go-daisy twin unreferenced, so the hazard cannot silently become active.

## 4. Recorded dispositions (no code change)

- [x] 4.1 Record #1809 item 6 "migrate deprecated primitives" as **VOID**: the two already-banned primitives (`layout.Page`, `ui.PaginationCircle`) have zero call sites; nothing to migrate.
- [x] 4.2 Record the stragglers as **retained**, one line each (below the Rule-of-Three bar; each encapsulates an invariant):
  - `SecretRevealPanel` (1 site) — encapsulates the one-time-secret reveal contract (self-destructing client wiring + copy target).
  - `AlertModal` (1 site) — encapsulates the destructive-confirmation modal idiom.
  - `SubNav` (0 direct call sites — reached only through `SettingsRail`, 6 sites) — encapsulates the sub-navigation rail's active-state/route invariant.
  - `SnippetCard` (2 sites) — encapsulates a card body variant not yet expressible as a `PanelCard` prop.

## 5. Verification

- [x] 5.1 `templ generate` (from `apps/web-ui/gateway`) succeeds after the `.templ` changes.
- [x] 5.2 `go build ./...` passes.
- [x] 5.3 `go test ./...` passes.
- [x] 5.4 `go test -run 'Ratchet' -v ./...` passes.
- [x] 5.5 `golangci-lint run ./...` passes.
- [x] 5.6 Confirm the rename is output-neutral: the diff shows only identifier changes (no markup/class/attribute change).

## 6. Deferred (follow-up migrations — recorded, not implemented)

- [ ] 6.1 Migrate direct `ui.CardRaw` in pages (8 sites across 6 files) onto `components.PanelCard`/`components.TableCard` (the other 5 `ui.CardRaw` sites are the canonical card shells in `components/` and are not migrated).
- [ ] 6.2 Resolve `form.FormField` vs `form.FormControl` (Refs #1747).
- [ ] 6.3 Adopt the go-daisy-dependent compositions (soft box → `ui.SoftBox`, dialog title, empty note, clipboard, tabs, dropdown) pending the upstream tag + gateway bump.
