## Context

`web-ui-component-gallery` gives the gateway a dev-gated catalog of reusable templ components with a static dependency graph and isolated previews, but it can only see components that exist — it cannot see the ad-hoc markup that *should* be a shared component. `web-ui-components` and `web-ui-component-conventions` encode a large set of "single-sourced, no raw copy remains" rules (status badges, chips, panels, list rows, table shells, nav rails, toggles, grouped selects, snippet blocks), yet adoption is uneven. This change adds a detector that scans authored `.templ` sources for markup that duplicates a shared component and surfaces the gaps in the gallery.

The detector reads **authored `.templ` sources**, never the generated `*_templ.go`. The generated code is a build product that is regenerated on every `templ generate`; more importantly, its line numbers do not map back to the lines a developer edits in `.templ`, so a finding pointed at `agent_templ.go:1234` would be useless for the person fixing it. Findings must carry `.templ` file/line so they are directly actionable. The scan is therefore a source-text scan over the gateway's `.templ` files, keyed off a hand-maintained map of the wrapper classes each shared component owns.

## Goals / Non-Goals

**Goals**
- Deterministically scan authored `.templ` sources for ad-hoc markup duplicating a shared component, producing compiler-checked `AdoptionFinding{File, Line, Marker, Owners}` records.
- Surface those findings in a gallery "adoption gaps" section, grouped per view.
- Exclude slot-owned markup and any legitimate raw carve-out, via hard exclusions plus an allowlist/ignore escape hatch.
- Guard regeneration with a staleness test so the committed findings cannot drift.

**Non-Goals**
- Migrating any call site to a shared component (separate follow-up change).
- Detecting every possible duplication — the signature map is explicit and hand-maintained; detection is limited to the wrapper classes it owns.
- Replacing the convention spec's own exact-render/absence tests; the detector complements them by surfacing where they are currently unsatisfied.
- Gating production behaviour — the gallery is already dev-gated; the detector rides that same gate.

## Audit report

The inventory below is a broad as-scanned audit of the gateway's authored `.templ` view files. It is **wider than what the detector emits**: it counts all duplication, including duplication expressed through a *different* component call (e.g. `@ui.Badge` where a `StatusBadge` is intended) and unwrapped `<table>` shells. The detector deliberately targets the conservative subset that a source-text scan can prove — raw authored markup that literally carries a shared component's wrapper class — and emits **62 findings across 18 files**. The counts below are the migration backlog the follow-up change works from, not the detector's output. Per file it records the number of shared-component render sites, the number of ad-hoc sites, and the dominant duplication category.

| File | Shared components | Ad-hoc sites | Top duplication |
|---|---|---|---|
| agent.templ | 13 | 6 | badge |
| backups.templ | 3 | 5 | badge |
| blueprints.templ | 4 | 14 | badge + table |
| board.templ | 5 | 6 | badge |
| documents.templ | 4 | 2 | list-row |
| embeddings.templ | 0 | 4 | badge |
| inbox.templ | 0 | 5 | badge |
| mcp_nodes.templ | 3 | 3 | badge / list-row |
| mcp_servers.templ | 4 | 5 | badge |
| migrations.templ | 6 | 10 | badge + table |
| object_preview.templ | 1 | 2 | badge |
| objects.templ | 5 | 13 | badge / card / select |
| org_context.templ | 5 | 3 | badge |
| org_members_ui.templ | 11 | 1 | badge |
| project_settings.templ | 11 | 2 | nav-rail |
| proposal.templ | 0 | 14 | badge + card / snippet |
| runs.templ | 0 | 5 | badge |
| schedules.templ | 5 | 6 | badge |
| schema.templ | 5 | 8 | badge |
| schema_packs.templ | 5 | 4 | badge |
| sessions.templ | 4 | 4 | badge |
| share_manage.templ | 4 | 13 | badge + card + toggle |
| share_page.templ | 0 | 1 | badge |
| skills.templ | 3 | 7 | badge |
| ui.templ | 5 | 7 | snippet / chip / badge |
| agent_ui.templ | 0 | 1 | chip |
| type_ui.templ | 0 | 1 | chip |
| notifications_bell.templ | 0 | 1 | badge |
| approvals.templ | 0 | 5 | card |
| chat.templ | 0 | 1 | badge |
| mcp_shares.templ | 10 | 1 | badge |
| agent_mcp_endpoint.templ | 8 | 0 | — |
| api_tokens.templ | 6 | 0 | — |
| account_menu.templ | 0 | 0 | — |
| auth_ui.templ | 0 | 0 | — |
| pwa.templ | 0 | 0 | — |
| schema_nav.templ | 1 | 0 | — |
| share_owner.templ | 0 | 0 | — |
| share_session_transcript.templ | 0 | 0 | — |
| sidebar_user.templ | 0 | 0 | — |
| sidepanel.templ | 0 | 0 | — |
| snippet_ui.templ | 0 | 0 | — |
| spotlight.templ | 0 | 0 | — |
| usage.templ | 0 | 0 | — |

### Dominant duplication (migration backlog)

- ~90 raw `@ui.Badge` renders where a `StatusBadge`/intent-mapped badge (or a `countBadge`) should be used. **Not detected by the scanner** — `@ui.Badge` is a component call, not authored raw markup; the gallery graph already shows these uses, and migration is the follow-up change.
- ~8 raw `card card-border` shells instead of `PanelCard`/`TableCard`.
- 3 unwrapped `<table class="table">` blocks that are not inside the `TableCard` shell: `blueprints.templ:917`, `migrations.templ:344`, `migrations.templ:441`. **Not detected by the scanner** — `table` is a slot-owned marker and is hard-excluded (see Decisions); these are listed as follow-up migration targets only.
- A long tail of chip / list-row / nav-rail / toggle / select / snippet duplicates, most of it outside the scanner's marker set.

### Local helper duplication (helper file:line → duplicate of)

Local, page-private helpers that re-implement a shared component. `(adopted)` marks helpers whose site already calls the shared primitive — these are carried for completeness and must not be flagged as gaps.

- `agentToolCountBadge` agent.templ:886 → StatusBadge / count-badge
- `agentNameChip` agent_ui.templ:37 → chip
- `diffChangeChip` blueprints.templ:495 → chip
- `requiredBadge` blueprints.templ:554 → badge
- `propertyTypeBadge` blueprints.templ:885 → badge
- `chipDiffRow` blueprints.templ:693 → list-row
- `documentStatusBadge` documents.templ:142 → StatusBadge (adopted)
- `workerStateBadge` embeddings.templ:235 → badge
- `mcpNodeStateBadge` mcp_nodes.templ:127 → StatusBadge (adopted)
- `mcpShareStatusBadge` mcp_shares.templ:147 → StatusBadge (adopted)
- `objectScoreBadge` objects.templ:612 → badge
- `objectBadges` objects.templ:643 → badge
- `roleBadge` org_members_ui.templ:119 → badge (adopted)
- `providerRateBadge` project_settings.templ:1455 → badge
- `scheduleLastRunBadges` schedules.templ:175 → badge
- `provenanceBadge` schema.templ:129 → badge (adopted)
- `ownerBadge` schema_packs.templ:164 → badge (adopted)
- `runStatusBadge` sessions.templ:391 → badge (adopted)
- `toolStatusBadge` sessions.templ:404 → badge (adopted)
- `shareLinkStatusBadge` share_manage.templ:339 → badge
- `shareBehaviourChip` share_manage.templ:736 → chip
- `typeNameChip` type_ui.templ:56 → chip
- `chipRow` ui.templ:781 → chip / list-row
- `countBadge` ui.templ:805 / ui.templ:810 → badge
- `promptBlock` ui.templ:799 → snippet

### Legitimate raw carve-outs (must not be flagged)

- Raw `<table>` inside the `TableCard` shell — the shell owns the card, the caller supplies the table (per `web-ui-components` "Table shells render through a single component"): `agent_mcp_endpoint.templ:237`, `agent_mcp_endpoint.templ:478`, `api_tokens.templ:158`.
- daisyUI primitives rendered directly (L0 usage is not a gap).
- Static/custom shells with no shared equivalent: `auth_ui.templ` `.login-card`, `proposal.templ` `.proposal-card`, `chat_dock.templ` `.dock-card` / `.todo-card`, `share_page.templ` `.share-terminal-card`.
- Bare toggles that do not fit the `ToggleField` contract (per `web-ui-components` §6.1 / the bare-toggle carve-out).
- `snippet_ui.templ` rollback surfaces.

## Decisions

**Scan authored `.templ` text, keyed off a hand-maintained signature map.** The detector walks each authored `.templ` file's source at the gateway root, matching the marker classes in a hand-maintained `componentSignatures()` map that names, for each shared component, the wrapper class(es) it owns: `btn`, `badge`, `alert`, `card`/`card-border`, `toggle`, and `modal`/`dialog` (plus the `dialog` tag). A finding is recorded when one of those markers appears as a raw class/tag in authored markup rather than through the component. The map is hand-maintained because "which class a shared component owns" is a design decision, not something a source scan can infer; it is guarded by an invariant test (`TestSignaturesInverted`) that every marker maps to at least one owner and no marker is owned by conflicting components. A marker renders one finding per matched line; an element carrying both `card` and `card-border` therefore yields two rows.

**Explicit exclusions, not heuristics.** Some authored markup legitimately carries these classes. The detector hard-excludes slot-owned markup (`table` is not a signature marker at all — the `TableCard` shell owns the card and the caller supplies the table, per the `web-ui-components` table-shell carve-out) and the daisyUI primitives (`select`/`input`/`label`/`nav`/`dl`/`dt`/`dd`/`optgroup`) that the gateway adopts rather than re-implements. Static/custom shells and bare toggles are handled by the runtime allowlist: the scanner records them, and the runtime `adoptionAllowlist` (a hand-maintained map of view basename → markers to ignore for that file) plus the `<!-- adoption:ignore marker... -->` comment escape hatch (which suppresses the named marker on the next element) keep the gallery honest about *why* a raw site is accepted. This is deliberate: a site excluded by name silently rots when the component moves, whereas an allowlist entry is re-reviewed when the file changes.

**Generated findings file + thin runtime reader.** The existing `cmd/componentgraph` generator is extended (not duplicated): a second pass plus an `-adoption-out` flag emits `devgallery_adoption_gen.go` — a `package main` slice of `AdoptionFinding{File string; Line int; Marker string; Owners []string}` — with a do-not-edit header, alongside the main graph the same binary already writes. A thin runtime file `devgallery_adoption.go` loads that slice, applies the allowlist/ignore filter, and aggregates gaps per file for the gallery. Splitting generated data from runtime logic keeps the generated file a dumb, deterministic list (diffable in CI) while the allowlist lives in hand-edited code.

**Findings surface in the gallery as an "adoption gaps" section.** `adoptionGapSection` (unexported, like `galleryLayerSection`, so the generator does not add a graph edge and the committed graph does not churn) renders in `devgallery.templ` above the catalog, listing per-view gaps, each entry showing the marker, the line, and the owning component(s) it should render. The section is only rendered with content when findings exist (a clean tree renders an empty state), and it rides the existing `MEMORY_COMPONENT_GALLERY` gate — no new flag.

**Staleness test mirrors the gallery's own.** A test runs the generator to a temp `-adoption-out` and fails when the committed `devgallery_adoption_gen.go` differs, exactly as `devgallery_test.go` does for the graph. It does not need `templ generate` and MUST NOT skip when `vendor/` is absent, because the detector reads only in-repo authored `.templ` sources and never go-daisy.

## Risks / Trade-offs

- **Signature map drifts as components change** → the map is hand-maintained and guarded by an invariant test (`TestSignaturesInverted`: every marker has an owner, no conflicting ownership); widening or narrowing it is a reviewed edit.
- **Line numbers shift under refactor** → findings are keyed by file+line and the allowlist by basename+marker, so a moved site is re-surfaced and re-reviewed (intended, not a bug).
- **The scanner under- or over-reports** → the map is explicit and conservative; `table` and daisy primitives are hard-excluded, the escape hatch is the safety valve for legitimate raw carve-outs.
- **Findings are stale in CI** → the staleness test regenerates and fails on drift.

## Migration Plan

Additive; no data or API migration. The detector rides the existing `MEMORY_COMPONENT_GALLERY` gate and only changes what the gallery renders. `go:generate` runs in the dev loop (`task dev`/air) after `templ generate`, and the extended `cmd/componentgraph` is covered by the gateway `./...` build/vet/lint. Rollback is unsetting the flag.

## Open Questions

None.
