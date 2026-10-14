## ADDED Requirements

### Requirement: Each UI surface has exactly one canonical primitive

Each UI surface in the gateway SHALL have exactly one canonical primitive, named in a registry table in this
spec. Authors SHALL render the canonical primitive for a surface; a deprecated or non-canonical alternative
named in the "Not canonical" column SHALL NOT be used directly. A local component name SHALL NOT collide (by
bare identifier) with a go-daisy primitive name referenced from authored sources, because the committed
component graph keys components by bare identifier and would merge the two into one node.

The registry SHALL be maintained in this same change as the surface set evolves: adding a surface, promoting a
legacy alternative to canonical, or banning a primitive SHALL update the table, the ratchet declaration, and the
adoption signatures together.

#### Scenario: An author resolves to the canonical primitive

- **WHEN** an author needs a primitive for a surface listed in the registry
- **THEN** they render the canonical primitive named for that surface rather than a non-canonical alternative

#### Scenario: A non-canonical primitive is not used directly

- **WHEN** the gateway templates are searched for a primitive named in the "Not canonical" column
- **THEN** no authored call site remains (outside a documented migration task), and the ratchet's
  deprecated-primitive check fails if one is introduced

#### Scenario: A local name cannot collide with a referenced go-daisy name

- **WHEN** a local component and a go-daisy primitive referenced from authored sources share a bare identifier
- **THEN** the ratchet's name-collision check fails, forcing either a rename of the local component or a fold
  onto the go-daisy primitive

#### Scenario: The registry, ratchet, and signatures stay in lockstep

- **WHEN** a surface's canonical primitive changes
- **THEN** the registry table, the ratchet declaration, and the adoption signatures are updated in the same
  change, so no one source of truth drifts from the others

### Requirement: The canonical-L0 registry records the surface canon

The registry SHALL record, for each surface, the canonical primitive, its non-canonical (deprecated or legacy)
alternatives, and any pending migration note. The current registry is:

| Surface | Canonical | Not canonical (deprecated/legacy) | Notes |
|---|---|---|---|
| Page shell | `layout.PageFull` | `layout.Page` | `layout.Page` is banned; 0 call sites. |
| Toggle field | `form.ToggleInput` (bare) + `components.ToggleField` (row) | `form.FormToggle` | `form.FormToggle` is banned; 0 call sites. |
| Pagination | `ui.PaginationWithProps{Style: ui.PaginationStyleCircle}` | `ui.PaginationCircle` | `ui.PaginationCircle` is banned; 0 call sites. |
| Link/list row | local `components.LinkRow` | go-daisy `ui.ListRow` (reserved for chat's card-style session-rail rows) | Resolved by renaming the former `components.ListRow`. |
| Status badge | local `components.StatusBadge` (intent + icon) | go-daisy `ui.StatusBadge` (not used — watched hazard) | `ui.StatusBadge` exists in the pinned go-daisy but is never referenced. |
| Panel / table card | `components.PanelCard` / `components.TableCard` | direct `ui.CardRaw` in pages | Non-canonical; migration task pending (8 page sites across 6 files; the other 5 `ui.CardRaw` sites are the canonical card shells themselves and are not migrated). |
| Form field | `form.FormControl` (dominant) | `form.FormField` | Decision pending #1747 — do not decide here. |

#### Scenario: The registry names one canonical per surface

- **WHEN** the registry table is read
- **THEN** every surface has exactly one canonical entry, and any non-canonical alternative appears only in the
  "Not canonical" column with a note

#### Scenario: A banned primitive has zero call sites

- **WHEN** the ratchet scans the authored sources for a banned primitive
- **THEN** each banned primitive (`layout.Page`, `ui.PaginationCircle`, `form.FormToggle`) has zero direct call
  sites, and the ratchet fails if one is introduced

#### Scenario: The status-badge hazard stays latent

- **WHEN** the local `components.StatusBadge` is compared with go-daisy `ui.StatusBadge`
- **THEN** the local component is defined and the go-daisy twin remains unreferenced, so the bare-name hazard
  is watched rather than silently becoming an active collision

#### Scenario: Card pages resolve to the shared card shell

- **WHEN** a page renders a card-like shell
- **THEN** it renders `components.PanelCard` or `components.TableCard`, and direct `ui.CardRaw` use in pages is
  recorded as a pending migration task rather than accepted as a second canonical
