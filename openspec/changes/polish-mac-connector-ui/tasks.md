## 1. Accessibility

- [x] 1.1 Added `.accessibilityLabel` to empty-title toggles and icon-only controls (`MCPServersPage`, `ToolsPage`, `ProjectConnectionControl`, `AccountToolbarView`) via an `AccessibleNames` helper; controls with visible titles left unchanged. Manual VoiceOver check pending (not verifiable from a cloud agent).
- [x] 1.2 Added `AccessibleNamesTests` asserting each empty-title toggle exposes a non-empty accessible name; passes in the full unit-test run.

## 2. Stable row identity (TDD)

- [x] 2.1 Added failing-first `MCPServerFormArgsTests` (add preserves values; remove-middle keeps identity).
- [x] 2.2 Modelled MCP arg rows with a `UUID` id and bind `ForEach($args)` in `MCPFormEditors.swift`; tests pass.
- [x] 2.3 Switched read-only lists to element identity (`DashboardPage` enabled tools/agents, `AgentDetailView` names); no index-based identity remains in a mutating list.
- [x] 2.4 Persisted payload shape (`HostedMCPServerConfig.args: [String]?`) unchanged; round-trip decode test passes.

## 3. Deprecated API migration

- [x] 3.1 Replaced `.menuStyle(.borderlessButton)` with `.menuStyle(.button)` + `.buttonStyle(.borderless)` in `AccountToolbarView` and `ProjectSwitcherView` (indicator/padding retained). Visual parity is a manual-verify item.

## 4. Menu-bar and application menu

- [x] 4.1 Both primary (popover) and secondary (`NSMenu`) click paths expose the same action set (Open, Check for Updates, Settings, Sign out, Quit). Manual both-path smoke pending.
- [x] 4.2 Menu-bar sign-out now routes through a confirmation (`SignOutConfirmation` copy, shared by both paths); cancel behaviour is a manual-verify item.
- [x] 4.3 Added `.commands` replacing `.appSettings` (Settings ⌘,) and `.appInfo` (About), routing through `WindowRouter`. ⌘, behaviour is a manual-verify item.

## 5. Localization foundation

- [x] 5.1 String Catalog `Resources/Localizable.xcstrings` is wired into `project.yml` and now populated with the app's user-facing keys via Xcode localization extraction (`-exportLocalizations`/`-importLocalizations`); the app builds green with it. Note: because the catalog is source-language-only (every value equals its key) Xcode emits no separate `.lproj/Localizable.strings` — literals resolve through the catalog with unchanged English rendering.
- [x] 5.2 Retyped user-facing computed strings to `LocalizedStringResource` (`AppStatus`, `MainWindowView`, `ProjectSwitcherView`, `MCPServersPage`, `MCPServerDetailView`, `ToolsPage`, `ProjectConnectionControl`, `MCPFormEditors`); app builds and English renders identically.
- [x] 5.3 Removed runtime case transform (`DashboardPage` `.uppercased()`) and fragment join (`AddMCPServerSheet` `joined(separator:)` → `Text(values.formatted())`).
- [x] 5.4 Replaced fixed point sizes with semantic text styles; `minimumScaleFactor` retained (no `ViewThatFits`). Pseudo-long-translation check is a manual-verify item.
- [x] 5.5 Audited directional layout — all `.leading`/`.trailing`, no hard-coded left/right; RTL mirroring is a manual-verify item.

## 6. Content de-duplication

- [x] 6.1 `ProjectAccountPage` signed-out now renders a single sign-in prompt and a single sign-out (per-account row); duplicate prompt and session card removed.
- [x] 6.2 About shows version/build once; OIDC/PKCE/callback internals moved behind an info tooltip on both `AboutPage` and `ConnectionPage`.

## 7. Verification

- [x] 7.1 Ran `Scripts/lint-swift.sh` with the CI tool versions (SwiftFormat 0.63.0, SwiftLint 0.65.1) — 0 formatting issues, 0 lint violations. (The binaries are not installed in the container by default; obtained the CI release binaries to run it.)
- [x] 7.2 Built and ran the full unit-test suite on the Mac build machine (Swift 6 language mode) — green, no errors or failing tests.
- [ ] 7.3 Manual smoke on a Mac (menu-bar both paths, confirmed sign-out, ⌘, Settings, About). Blocked: requires a GUI session; not verifiable from a cloud agent.
- [x] 7.4 De-duplicated page arrangements reported for design review (see checkpoint).
- [x] 7.5 Takeover-review follow-up: shared card/info/loading components (`ConnectorTheme`) and the remaining stat/status literals now take `LocalizedStringResource`; AppKit `NSMenu`/`NSAlert` titles wrapped in `String(localized:)`; repeated per-account sign-out buttons carry an account-context accessibility label; environment/agent badges darkened in light mode to meet WCAG AA for small text; `ConnectionPage` sign-out now confirms via the shared `SignOutConfirmation`. Verified: Swift 6 build succeeds with no new warnings and the full unit suite is green (419 tests) on Xcode 27. Residual: the new keys still need a String Catalog re-extraction on the Mac (`xcodebuild -exportLocalizations`) so translators see them — rendering already falls back to English; `EnvironmentBadge.label` and `MCPServerToolsSection.toggleHelp` remain `String`.
