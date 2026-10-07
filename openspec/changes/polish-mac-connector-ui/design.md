## Context

See `proposal.md` — Why. The mac connector app (`apps/connector.mac/MemoryConnector`) is a SwiftUI menu-bar + window app. The relevant current state from the UI review:

- Accessible names are missing on `Toggle("", isOn:).labelsHidden()` sites and the icon-only account `Menu`; `IdentifierReveal` is the only control using `.accessibilityLabel`.
- Editable MCP argument rows are keyed by index (`MCPFormEditors.swift:61` `ForEach(args.indices, id: \.self)`), while the sibling key/value editor already uses a `UUID`-keyed model.
- `MenuStyle.borderlessButton` is soft-deprecated (used in `AccountToolbarView.swift`, `ProjectSwitcherView.swift`).
- `StatusItemController` shows a popover on primary click and an `NSMenu` on secondary click; Quit/Settings live only in the menu; menu-bar sign-out is unconfirmed.
- There is no `Settings` scene or `.commands`, so ⌘, does nothing while the app is frontmost.
- Sign-in prompts and version strings are repeated across pages; About exposes Zitadel/PKCE/callback-scheme copy.
- No String Catalog exists; several display strings are built with runtime case transforms or `joined(separator:)`, and the dashboard uses fixed point sizes.

Constraint: the primary multi-step layout decision here is design-taste-sensitive; existing deliberate design (the `ConnectorCard` system, fixed stat-tile heights, email-first account rows) must be preserved unless a skill mandate forbids it.

## Goals / Non-Goals

**Goals:**

- Remove a11y, identity, deprecated-API, menu-bar, app-menu, and duplication defects without redesigning the visual language.
- Add a localization foundation without regressing current English rendering.

**Non-Goals:**

- Redesigning the app's visual system or page layout.
- Adopting SwiftUI 27 APIs that require raising the macOS deployment target above 15 (see D6).
- Changing connector behavior, sign-in policy, or the engine.

## Decisions

### D1 — Accessible names added alongside tooltips, not instead of them

Add `.accessibilityLabel` to every empty-title toggle and the icon-only account `Menu`, keeping `.help(...)` as supplementary. Toggles that already carry a visible title keep it — do not add a duplicative label. Mirror the `IdentifierReveal` pattern. Rationale: on macOS a tooltip is not a reliable VoiceOver name. Alternative: expose the title visually — rejected, changes the deliberate compact row design.

### D2 — Introduce a `UUID`-identified model for editable argument rows

Model MCP argument rows like the existing `MCPServerForm.Entry` (`let id = UUID()`), bind with `ForEach($args)`. Read-only lists switch to element identity (`ForEach(items)` where `Identifiable`, or `id: \.self` for genuinely value-unique strings). Rationale: per `swiftui-specialist/references/foreach.md`, index identity resets state/focus on mutation and is actively wrong for editable rows. Verify any persisted form state still decodes.

### D3 — Deprecated menu style migrated with visual parity check

Replace `.menuStyle(.borderlessButton)` with `.menuStyle(.button)` + `.buttonStyle(.borderless)` at all four sites, then verify the toolbar dropdowns render as before (indicator, padding, plain appearance). Rationale: mandated by `soft-deprecated-apis.md`; the risk is visual, so parity is an explicit acceptance step.

### D4 — One action list for both menu-bar click paths, confirmation reused

Render the same action list (open, check for updates, Settings, sign out, Quit) in both the AppKit `NSMenu` (secondary click) and the popover footer (primary click), and route sign-out through the same confirmation used in the window. Rationale: users expect the same affordances on click; destructive consistency matters more than the current left/right split.

### D5 — Standard application menu via `.commands`

Add `.commands { CommandGroup(replacing: .appSettings) { … } }` (and About) routing to `WindowRouter`/sidebar selection. Note ⌘, currently only works on the status menu; this makes it work while frontmost. Alternative: a `Settings` scene — workable, but the app's settings live inside the main window, so routing to the existing window/page is simpler and avoids a second window type.

### D6 — Localization foundation, SwiftUI-27 item bindings deferred

Add a String Catalog and wire it into `project.yml`; retype user-facing computed strings to `LocalizedStringResource`; replace runtime `.uppercased()` with literal-cased strings, `joined(separator:)` with `Text(values.formatted())`, and fixed `.system(size:)` with semantic text styles. The SwiftUI-27 `alert(_:item:)`/`confirmationDialog(_:item:)` overloads would simplify the closure bindings but require macOS 27; keep a `Bool` + payload or `presenting:` decomposition on the macOS 15 target and revisit when the deployment target rises. Alternative: adopt item bindings behind `#available` — rejected as unnecessary churn now.

### D7 — De-duplication preserves cards, removes repetition only

Collapse to one sign-in prompt and one sign-out per page, show the version once on About, and move protocol internals behind a disclosure/tooltip. Keep the `ConnectorCard` structure intact. This is a design-taste change; @designer owns the final arrangement.

## Risks / Trade-offs

- [Menu-bar behavior is hard to verify remotely] → Explicit manual smoke task on a Mac; menu-bar item is not reachable from a cloud agent.
- [`.menuStyle` migration changes appearance] → Parity check is an acceptance criterion, not an afterthought.
- [Row-identity model change breaks decoding of persisted form state] → Round-trip/decoding unit tests; migrate or default unknown ids.
- [String Catalog wiring via XcodeGen misses the catalog] → Asset-like source entry in `project.yml`; verify the catalog compiles into the bundle.
- [Removing runtime `.uppercased()` changes visible casing] → Curate literal-cased strings so English rendering is unchanged.
- [Fixed-height stat tiles clip longer translations] → Use text styles + `ViewThatFits`/`minimumScaleFactor`; verify with a pseudo-long translation.

## Migration Plan

1. Accessibility labels (isolated, low risk).
2. Row identity (model change + tests).
3. Deprecated menu style + parity check.
4. Menu-bar unification + confirmation + `.commands`.
5. Localization foundation (catalog + string retyping + text styles).
6. De-duplication of sign-in/sign-out/version/protocol copy.
7. Verify: lint/format, unit tests on the Mac build machine, manual menu-bar smoke.

Rollback: each item is an independent commit; the localization catalog is the only broad-blast-radius change and lands last among the code changes so it can be reverted alone.

## Open Questions

- None. The item-binding API question is resolved by deferral (D6); everything else is a normal implementation choice.
