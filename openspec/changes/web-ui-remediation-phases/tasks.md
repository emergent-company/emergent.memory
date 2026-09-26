## 1. Settings information architecture (`settings-navigation`)

- [ ] 1.1 Settle the settings IA: extend the settings rail so it lists the sidebar Settings group's members, with the six project-setting sections (General, Assistant, Overrides, Providers, Voice, Devices) nested under a Project entry; adopt Approvals into the group and keep MCP sharing as a child view of MCP servers, so the rail and the sidebar group are one set, one order.
- [ ] 1.2 Render the rail on every Settings-group destination (project settings, API tokens, approvals, MCP servers, MCP sharing, blueprints, skills), and nowhere outside the group.
- [ ] 1.3 Unit test: for every settings route the rail entries equal the sidebar group's entries (same set, same order), and the six project-setting sections stay reachable; the active section and the current destination are both highlighted.
- [ ] 1.4 Preserve save/error behaviour on each sub-page; error feedback moves to the context of the failing control (page-level only when no control exists) without breaking the rail.
- [ ] 1.5 Verification: `templ generate ./...`, `go build ./...`, `go vet ./...`, `go test ./...`, `task lint` from `apps/web-ui/gateway`; `task e2e:test`; manual browser pass over every settings page.

## 2. Project settings feedback and confirmed removal (`project-settings-ui`, `inline-form-saving`)

- [ ] 2.1 Report a rejected project-info save in the context of the control that caused it (page-level only when the failure has no control to attach to), instead of only a generic page message.
- [ ] 2.2 Add an in-flight indicator and a failure surface to the silent `hx-swap="none"` settings autosave, additive to the `inline-form-saving` toast contract (never a replacement), and consistent with the `hx-swap="none"` mechanism the e2e coverage pins.
- [ ] 2.3 Add in-context failure feedback for form submissions rejected for a non-field reason (the form's own context, not only a page-level notice).
- [ ] 2.4 Route agent-override removal through the shared destructive confirmation before any request is issued.
- [ ] 2.5 Unit test: the in-flight and failed-state indicator markup; the contextual error markup; the "accompanying indicator does not replace the toast" contract for both success and failure; override removal requires confirmation.
- [ ] 2.6 Verification: `templ generate ./...`, `go build ./...`, `go vet ./...`, `go test ./...`, `task lint`, `task e2e:test`.

## 3. Blueprint pack-removal confirmation (`blueprint-gallery`)

- [ ] 3.1 Route pack removal through the shared destructive confirmation so no request is issued until the user confirms, and a dismissed dialog leaves the pack installed.
- [ ] 3.2 Unit test: activating remove shows the confirmation and removal only follows confirmation; dismissal sends no request.
- [ ] 3.3 Verification: `templ generate ./...`, `go build ./...`, `go test ./...`, `task lint`, `task e2e:test`.

## 4. Navigation cues and command-palette coverage (`web-ui-navigation`)

- [ ] 4.1 Standardise one page-header idiom per page class: list pages supply the list set of header parts, detail pages the detail set, and sibling pages in one section agree.
- [ ] 4.2 Build the kicker→sidebar-group mapping as a shared source of truth used by both the header and the test, so every kicker resolves to its sidebar group and no orphan vocabulary survives.
- [ ] 4.3 Derive `spotlightItems` from the sidebar groups so the command palette covers every destination and tracks nav changes without a separate edit.
- [ ] 4.4 Unit test: every page's kicker resolves to its sidebar group through the mapping; palette coverage equals nav coverage (the "palette lists every destination" scenario is e2e-only).
- [ ] 4.5 Verification: `templ generate ./...`, `go build ./...`, `go test ./...`, `task lint`, `task e2e:test`.

## 5. Accessibility floor (`web-ui-accessibility`)

- [ ] 5.1 Make `data-href` row targets keyboard-reachable (role/tabindex or a real link/button) without changing visuals; a focusable target per row, with the focus-visible treatment asserted at the source.
- [ ] 5.2 Emit a shared row-action style with a `@media (hover:none)` rule giving icon-only row actions ≥44×44 px on coarse-pointer devices, preserving desktop density.
- [ ] 5.3 Give icon-only controls an accessible name; associate a `<label for>` with the API-token name input.
- [ ] 5.4 Add per-field validation error presentation (the failing field is identified and programmatically associated with its error).
- [ ] 5.5 Add a non-colour-only status signal (text or shape) to every status/state indicator that currently signals by colour or animation alone.
- [ ] 5.6 Unit test: a focusable target per row, the coarse-pointer rule present in compiled CSS, icon-only controls named, and the status text/shape signals present.
- [ ] 5.7 Verification: `task e2e:test` green; browser accessibility audit over the changed pages reports no new violations.

## 6. Copy voice (`web-ui-copy`)

- [ ] 6.1 Normalise list-empty headings to "No `<thing>` yet" (exempting search/no-match and "not set"/"none configured" states), and update the capability specs that pin a non-canonical string in the same change.
- [ ] 6.2 Normalise load-failure headings to "Couldn't load `<thing>`", reconciling "unavailable" phrasings rendered as gateway headings.
- [ ] 6.3 Remove internal implementation detail from user copy and move over-long subtitles to body/help copy.
- [ ] 6.4 Enumerate the canonical-name map (e.g. "API tokens", "MCP sharing") and fix naming drift across sidebar, titles, and actions.
- [ ] 6.5 Unit test: the empty-state and error-heading vocabularies are restricted to the canonical forms via the enumerated canonical-name map.
- [ ] 6.6 Verification: `templ generate ./...`, `go build ./...`, `go test ./...`, `task lint`, `task e2e:test`.

## 7. Provenance and follow-up

- [ ] 7.1 After merge, file a follow-up issue for the still-pending `web-ui-components` component-consolidation requirements (copy-button single-sourcing, one toast dispatch, form-control routing, one card/field/header component, one shared tone type, and the remaining repeated primitives) so they are tracked rather than lost with #854.
