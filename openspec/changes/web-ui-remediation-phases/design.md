## Context

Four of the seven re-homed deltas involve real design decisions, not just mechanical extraction. These are
recorded here so the choice, its alternatives, and its rationale survive review; the rest of the change
(the copy voice, the accessibility floor) is a direct requirement-to-task mapping with no open design axis.

## D1 — The settings rail becomes the navigator for the whole Settings area

**Decision.** The existing settings rail (today: the six project-setting sections) is extended to list the
members of the sidebar's Settings group — Project (with General/Assistant/Overrides/Providers/Voice/Devices
nested under it), API tokens, Approvals, MCP servers, MCP sharing, Blueprints, and Skills. The rail is
rendered on **every** Settings-group destination, and its entries are byte-identical to the sidebar group.

**Why.** The audit found two contradictory facts that cannot both hold today: the rail lists seven
destinations while the sidebar Settings group lists a different five, and four settings pages render no rail
at all. Approvals is a top-level ungrouped nav item; MCP sharing is a nested page. Leaving the two lists
divergent forces every future settings page to guess which navigator is authoritative.

**Alternatives rejected.** (a) Keep the rail scoped to project settings and add a second rail for the rest —
reintroduces the two-navigator split the audit set out to remove. (b) Drop the rail and rely on the sidebar —
loses in-area context on a long settings page. (c) Leave Approvals ungrouped — leaves a Settings-group member
outside both navigators.

**Settled placements.** Approvals becomes a member of the Settings group; MCP sharing stays a child view of
MCP servers (both appear in the rail accordingly) so the rail and the sidebar group are one set, one order.

## D2 — One destructive-confirmation convention, applied everywhere

**Decision.** Every destructive POST — delete, revoke, remove, rotate, unapply, reduce access — goes through
the shared `ui.ConfirmDialog` modal. The change first inventories every destructive route, then migrates each
site that does not already confirm, and adds a guard test that every destructive route is reachable only
through `ui.ConfirmDialog`.

**Why.** The audit found three conventions (native `hx-confirm`, `window.confirm`, bespoke `ui.Dialog`) plus
seven sites with no confirmation at all — the worst being Blueprints "Remove", which silently destroys a
schema install. Confirmation is a safety property; a single convention is the only way a guard test can keep
the inventory honest as routes are added.

**Included here.** The two flows whose existing specs currently act on plain activation — `blueprint-gallery`
"Remove a pack" and `project-settings-ui` agent-override removal — are re-specified to confirm first, and the
standalone `ConfirmIcon` is retired in favour of `ui.ConfirmDialog`'s own icon circle (a `web-ui-components`
concern, noted for the consolidation workstream, not re-specified here).

## D3 — The page kicker is a deterministic mapping, not free-form text

**Decision.** The kicker↔sidebar-group relationship is expressed as a shared source of truth — a mapping used
by both the header renderer and the test — rather than as a prose convention.

**Why.** Kickers are currently free-form strings ("Memory" on a page in the "Agents" group). A rule that says
"the kicker should name the group" is untestable without a machine-readable mapping; with one, every kicker
must resolve through it, and no page can invent a category the navigation does not show.

## D4 — Autosave feedback is additive, never a replacement

**Decision.** The in-flight and failure surfaces added for settings autosave accompany the toast contract
already owned by `inline-form-saving`; they do not replace it. A swapped in-field "saved" marker is
reconciled with the `hx-swap="none"` mechanism the e2e coverage pins, not asserted over it.

**Why.** Two capabilities (`inline-form-saving` and the e2e coverage change) already pin the toast and the
`hx-swap="none"` autosave contract. Introducing a field-level indicator that *replaced* the toast would
create three conflicting owners of one behaviour. Making it additive keeps every existing contract intact
while closing the "failed save is indistinguishable from an untouched field" gap.
