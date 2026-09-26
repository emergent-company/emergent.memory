## 1. Land the conventions

- [ ] 1.1 Add the component-conventions section to `apps/web-ui/gateway/AGENTS.md`: the layer table, the "does it earn a component" rule, the four variant axes (`Tone`/`Style`/`Size`/`State`), composition and attribute passthrough, htmx swap-contract ownership, layout ownership, naming, and the test policy.
- [ ] 1.2 Add the `web-ui-component-conventions` capability spec, so the enforceable subset is stated as testable requirements rather than prose.
- [ ] 1.3 Confirm the root `AGENTS.md` "Before Writing Code" table points at `apps/web-ui/gateway/AGENTS.md`, so the conventions are reachable from the entry point an agent reads first. Add the row if it is missing.
- [ ] 1.4 Ensure the doc and the spec state the same rules with no drift — a rule present in one and absent from the other is a defect in this change.

## 2. Verification

- [ ] 2.1 `openspec validate web-ui-component-conventions` passes.
- [ ] 2.2 Read-back check: every rule in the AGENTS.md section has a corresponding requirement in the capability spec, and vice versa.
- [ ] 2.3 Confirm this change touches no Go, templ, or CSS file (documentation and spec only), so there is no build or rendered-output impact to verify.

## 3. Deferred enforcement (not in this change)

Several conventions are violated by the codebase today, so their enforcement deliberately lands with the units that fix the code — this change must not leave a failing check behind.

- [ ] 3.1 Radius and density enforcement lands with the theming unit (`web-ui-css`: radius from theme variables, one central density source, single-source page background).
- [ ] 3.2 Card, form-field, header, and tone consolidation lands with the consolidation unit (`web-ui-components` plus the UX capabilities).
- [ ] 3.3 The mechanised checks — no layout props on shared components, no second implementation of an existing surface, no class-only assertions added — land with those units, not here.
