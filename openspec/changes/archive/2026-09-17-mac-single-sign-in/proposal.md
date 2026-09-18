## Why

Every signed-out surface in the macOS connector app offers Production and
Development sign-in as peers: the menu-bar popover renders two stacked buttons
("Sign in to Prod", "Sign in to Dev"), the window-header control is a menu that
lists both environments, the Connection page leads with a `Prod | Dev` segmented
picker, and the Project & Account "Add account" menu lists both. Dev is an
internal deployment; presenting it beside Production makes the very first
sign-in look like a product decision, and a normal user can pick the wrong
environment by accident. The app should read as "sign in to Memory", with the
Development environment available only as a deliberate escape hatch.

## What Changes

- Collapse every signed-out surface to ONE prominent Production sign-in action:
  the window-header control, the menu-bar popover, the Connection page, and the
  Project & Account sign-in prompt.
- Remove the environment choice from all of those surfaces — no `Prod | Dev`
  picker, no Prod/Dev menu, no second "Sign in to Dev" button.
- Move Development sign-in into the About page as a non-prominent, secondary
  control with an explanatory caption; the About page is the ONLY surface that
  offers it.
- Collapse the Project & Account "Add account" menu (which listed Prod/Dev) to a
  single Production sign-in action.
- Encode the policy in one testable place — the set of environments offered per
  sign-in surface — so "Dev is About-only" is a unit-tested invariant rather
  than a convention repeated across views.
- Existing Dev accounts are unaffected: they still appear in the account
  switcher with their `Dev` badge and remain switchable.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities

- `mac-connector-app`: the signed-out surfaces (header, menu-bar popover,
  Connection page, Project & Account) each show a single prominent Production
  sign-in and no environment choice; Development sign-in lives only on About.
- `mac-connector-auth`: Production is the default sign-in target and the only
  one offered on primary surfaces; Development stays a first-class environment
  but is reachable only from the About page.

## Impact

- `MemoryConnector/Sources/Environment.swift` (or a new `SignInLocation.swift`):
  the per-surface environment policy + the primary environment.
- `MemoryConnector/Sources/AccountToolbarView.swift`: header control becomes a
  single Production sign-in button; the signed-in "Add account" submenu drops
  the environment choice.
- `MemoryConnector/Sources/MenuBarView.swift`: popover "signed out" state drops
  the Prod/Dev button pair for one prominent button.
- `MemoryConnector/Sources/ConnectionPage.swift`: removes the environment
  picker; one prominent sign-in button.
- `MemoryConnector/Sources/ProjectAccountPage.swift`: "Add account" menu → one
  Production action; the signed-out prompt and re-auth prompt stay single-action.
- `MemoryConnector/Sources/AboutPage.swift`: gains the non-prominent
  Development sign-in control.
- `MemoryConnector/Tests/`: new test asserting the per-surface environment
  policy (Dev only on About) and that Production is the primary environment.
- No changes to `AccountStore`, the OAuth flow, or the CLI: this is a UI
  surface + policy change only.
