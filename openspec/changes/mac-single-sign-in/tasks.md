## 1. Sign-in surface policy (unit-tested)

- [x] 1.1 Add a `SignInLocation` value (one case per surface that can offer a sign-in action: window header, menu-bar popover, Connection page, Project & Account, About) plus `Environment.primary` and `Environment.signInEnvironments(for:)`, where Production is offered everywhere and Development only for `.about`
- [x] 1.2 Add `Tests/SignInLocationTests.swift` asserting: `Environment.primary == .prod`; every `SignInLocation` offers `.primary`; Development is offered for `.about` and for no other location; and `Environment.all` still contains both built-ins (the policy restricts surfaces, it does not delete the environment)
- [x] 1.3 Verify all locations are enumerated (a `CaseIterable` sweep over every case, not a hand-written list) so a newly added surface cannot silently inherit Development

## 2. Remove the Production/Development choice from signed-out surfaces

- [x] 2.1 `ConnectionPage`: remove the `Environment` segmented picker and the selected-environment server-URL caption; render ONE prominent `borderedProminent` "Sign in with Memory" button driven by `Environment.signInEnvironments(for: .connectionPage)`; keep the `isSigningIn` progress state, the `lastError` inline message, and the caption about the Zitadel PKCE client
- [x] 2.2 `AccountToolbarView`: replace the signed-out sign-in `Menu` (which listed Prod/Dev) with a single padded, right-aligned sign-in button that starts sign-in for the location's environment; keep the padding/stroke styling, `isSigningIn` spinner, and `.help(...)`; the signed-in account switcher keeps its accounts list but its "Add account" submenu drops the environment choice
- [x] 2.3 `MenuBarView`: replace the "Sign in to Prod" / "Sign in to Dev" button pair in the signed-out state with ONE prominent `borderedProminent` button; keep the "Not signed in" copy, the `isSigningIn` row, and the disabled-while-signing-in behaviour
- [x] 2.4 `ProjectAccountPage`: collapse the "Add account" `Menu` (which listed Prod/Dev) to a single Production sign-in action; leave the signed-out prompt and the expired-session re-auth prompt as single-action controls
- [x] 2.5 Verify by inspection that no view enumerates `Environment.all` for *new* sign-in selection any more — every sign-in affordance resolves its environment through the location policy. (Exception: the expired-session re-auth paths in `ProjectAccountPage` and `ProjectSwitcherView` still resolve the *active account's* own environment, so a Dev account re-auths against Dev — an intentional exception, not an environment choice.)

## 3. Development sign-in on About only

- [x] 3.1 `AboutPage`: add a NON-prominent Development sign-in control (secondary/link styling, never `borderedProminent`) with a short caption naming the environment it targets (`Memory Dev`) and its server URL, offering the action only from `Environment.signInEnvironments(for: .about)`
- [x] 3.2 Wire the About control to `AccountStore.signIn(environment:)` + the same project reload the other surfaces perform, and surface `isSigningIn` and `lastError` so a cancelled or failed Dev sign-in reports in place and is non-fatal
- [x] 3.3 Verify no other surface (window header, menu-bar popover, Connection page, Project & Account, Dashboard, MCP Tools/Servers, Permissions) exposes a Development sign-in action

## 4. Verification

- [x] 4.1 `xcodegen generate` + `xcodebuild -scheme MemoryConnector -destination 'platform=macOS' build` succeeds on the Mac build machine
- [x] 4.2 `xcodebuild … test` runs the unit-test bundle green, including the new `SignInLocationTests` — 343 tests, 0 failures
- [x] 4.3 Spec stays in sync: the `mac-connector-app` and `mac-connector-auth` delta specs in this change match the shipped UI
- [ ] 4.4 Manual check on the built app: signed-out menu-bar popover and Connection page each show exactly ONE prominent sign-in action with no Prod/Dev choice; the About page shows the non-prominent Development sign-in; a pre-existing Dev account still lists with its `Dev` badge and switches normally — the built app launches and stays running on the Mac build machine, but the visual pass could not be completed from this host (no GUI/screenshot access over ssh). Needs a human eyeball before/at merge.
