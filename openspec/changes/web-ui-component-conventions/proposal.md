## Why

The gateway grew a component library and a component package with no written rules for how components are
divided, composed, or named. A four-round audit and design review — recorded in full in
[PR #854](https://github.com/emergent-company/emergent.memory/pull/854) and its OpenSpec change
`web-ui-audit-remediation` — traced the recurring drift directly to that gap: five card constructors, four
form-field shapes, five spellings of the same tone axis, two sibling header components, and repeated
two-and-three-way duplication of the same surface.

The review process also showed the failure mode is structural, not incidental: new components and changes
were added without a rule set to check them against, and the same class of defect (a new requirement that
contradicts an existing one) was found twice by independent reviewers. Written conventions are the cheap
fix.

`apps/web-ui/gateway/AGENTS.md` currently carries **no** component guidance at all — it covers Go idioms,
generated files, and verify commands only. It is the file the repo already directs agents to read before
writing a gateway template, so it is the right home for the rules.

## What Changes

- **`apps/web-ui/gateway/AGENTS.md`** gains a component-conventions section: the layer table, the
  "does it earn a component" rule, the four variant axes, composition and attribute passthrough, htmx
  ownership, layout ownership, naming, and the test policy.
- **A new capability, `web-ui-component-conventions`**, states the same rules as testable requirements, so
  the enforceable subset is not just prose.

This change is **documentation and spec only**. It introduces no code and changes no rendered output.

### What this unit deliberately does not do

Three of the conventions are violated by the codebase today (`rounded-lg` literals, component-local padding
on daisyUI roots, and the tone spellings). Enforcement lands with the units that fix the code, so this unit
cannot leave a failing check behind:

- the theming unit (`web-ui-css`: radius, density, page background) makes the radius and density rules true;
- the consolidation unit (`web-ui-components` and the UX capabilities) makes the card, field, header, and
  tone rules true;
- the mechanised checks (no layout props, no second implementation of a surface, no class-only assertions)
  land with those units, not here.

## Capabilities

### New Capabilities

- `web-ui-component-conventions`: layering with one-way imports, the earning rule, the shared variant-axis
  vocabulary, composition via slots, attribute passthrough, who owns the htmx swap contract, layout
  ownership, naming, and what component tests may assert.

### Modified Capabilities

None. This unit only records conventions; the surface-level single-sourcing requirements live in
`web-ui-components` and are delivered by the follow-up units.

## Impact

- **Two files.** `apps/web-ui/gateway/AGENTS.md` and the capability spec. No Go, templ, or CSS change.
- **No behaviour change**, so no test or rendered-output impact. Verification is `openspec validate` plus a
  read-back that the AGENTS.md section is reachable from the repo root's "Before Writing Code" table.
- **Follow-ups** (separate changes, split from `web-ui-audit-remediation`): the theming/CSS unit, then the
  component-consolidation unit. PR #854 stays open as the audit record until this unit archives.
