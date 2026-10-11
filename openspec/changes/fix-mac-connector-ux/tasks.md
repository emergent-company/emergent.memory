## 1. Footer — project-connection status only

- [x] 1.1 Added `ProjectConnectionSummary` (pure, SwiftUI-free): `(signedIn, hasConnectedProject)` → `.connected` (green, "1 project connected") or `.noProjects` (orange, "no projects connected"); a stale connected-project id while signed out reads as `.noProjects`.
- [x] 1.2 Rewrote `MainWindowView`'s footer to a single dot + label from the summary; removed the engine-status text, the bolt icon, and the right-aligned hub/instance detail.
- [x] 1.3 Kept engine/hub health reachable via the footer's `.help` tooltip; menu-bar icon untouched.
- [x] 1.4 `ProjectConnectionSummaryTests` covers signed-out-with-stale-id, signed-in+connected, signed-in+none, signed-out+none.

## 2. Accounts card — list stored accounts and re-authenticate

- [x] 2.1 `ProjectAccountPage` lists `AccountsCard` whenever stored accounts exist (even when the session is invalid), so a Dev account remains reachable; the generic `SignInCard` is shown only when there are no accounts; `ProjectCard`/`ProjectConnectionControl` stay signed-in-only.
- [x] 2.2 `AccountsCard.accountRow` shows a primary "Sign in again" (accessibility-labelled) alongside the destructive "Sign out" only when `needsReauthentication`.
- [x] 2.3 `ProjectAccountPage.reauthenticate(_:)` signs in to the account's OWN environment (`account.environment`) then reloads projects; explicit failures still surface an error.

## 3. Expected auth/session failures are never raw errors

- [x] 3.1 Extracted `AuthFailure.looksLikeAuthFailure(_:)` (not-signed-in / invalid_token / unauthorized / 401) and rewired `ProjectStore.errorLooksLikeAuthFailure` to it.
- [x] 3.2 `AccountStore.currentAccessToken(for:)` maps an auth/session failure to `OIDCError.notSignedIn` (keeping its side effects); other failures keep the `oidcError` mapping.
- [x] 3.3 `ProjectAccountPage.loadProjects()` suppresses `pageError` for auth/session failures (and when no longer effectively signed in); genuine errors still show.
- [x] 3.4 `AccountStore.signOut(accountID:)` clears `lastError` so a stale failed-sign-in message cannot linger.
- [x] 3.5 `AuthFailureTests` (Swift Testing) and `AccountStoreTests` cases (`testAccessTokenAuthFailureMapsToNotSignedIn`, `testSignOutClearsLastError`).

## 4. Sidebar — About pinned to the bottom

- [x] 4.1 `SidebarItem.group` is now optional; `.about` is `nil` (removed from the Settings cluster).
- [x] 4.2 `MainWindowView` pins About to the bottom via `.safeAreaInset(edge: .bottom)` as a one-row, selection-bound sidebar row, visually separated by a divider.

## 5. About page details

- [x] 5.1 Dropped the "Engine version" and "Server" rows from the Details card (Instance retained; app version/build stay in the header).
- [x] 5.2 Removed the unused `engineVersion` property; `mcpNodesURL` now prefers the active account environment's server URL.

## 6. Engine version matches the app release

- [x] 6.1 `MemoryConnector/Scripts/build-engine.sh` stamps the embedded engine with `-X main.version=$MARKETING_VERSION` (guarded when unset), so the bundled engine reports the same version as the app.

## 7. Housekeeping

- [x] 7.1 `.gitignore` ignores XcodeGen-generated `*.xcodeproj/`, `*.xcworkspace/`, `DerivedData/`.

## 8. Verification

- [ ] 8.1 Build + run the Mac connector unit-test suite on the Mac build machine (Swift 6). Blocked from this Linux orchestrator host (no Swift/Xcode); changes were reviewed statically.
- [ ] 8.2 Lint the touched Swift files on the Mac (`Scripts/lint-swift.sh`). Blocked: SwiftFormat/SwiftLint not installed on this host.
- [ ] 8.3 Manual smoke on a Mac: footer while signed out / connected, per-account "Sign in again" for a Dev account, About shows no engine/server rows, About pinned at the bottom.
