## 1. Account identity + session plumbing

- [x] 1.1 Extract the Zitadel `sub` claim in `decodeIDTokenClaims` (oidc.go) and return it; verify a unit test asserts `sub` is decoded alongside name/email/picture and empty when absent
- [x] 1.2 Thread `Sub` through `sessionClaims`, `sessionContext`, and `currentUser`; verify a unit test asserts `sub` survives `issueSession`→`verifySession` and `attachSession` round-trips

## 2. Install-id cookie + in-memory account registry

- [x] 2.1 Add signed install-id cookie helpers (`issueInstallID`, `readInstallID`, `clearInstallID`) in auth.go; verify a unit test asserts issue→read round-trip and tamper rejection
- [x] 2.2 Add `accountSession` type and a mutex-guarded in-memory `accountRegistry` (`get`/`put`/`remove`/`list` keyed by install id) in a new account.go; verify unit tests cover add/list/remove and concurrent access

## 3. Multi-account handlers + routes

- [x] 3.1 Add a `/auth/add` variant of `authStart` that appends `prompt=select_account` to the Zitadel authorization URL; verify a unit test asserts the prompt param is present on the add path and absent on the normal path
- [x] 3.2 Add `POST /auth/switch` (body `sub`): rotate active claims into the registry, pop the target, lazily refresh its token, re-issue the active cookie restoring its remembered project/org; verify a handler test asserts the active identity and project switch
- [x] 3.3 Extend `authLogout` to sign out only the current account (clear active cookie, drop active from consideration) leaving other accounts in the registry; verify a handler test asserts other accounts remain listed after logout
- [x] 3.4 Dedup on callback: a new `sub` that already matches an existing account (active or registry) becomes a switch, not a duplicate; verify a unit test asserts no duplicate entry and the active account flips
- [x] 3.5 Register `/auth/add` and `/auth/switch` in main.go (and mark them public in `publicAuthPath` where appropriate); verify `go build ./...` passes

## 4. Account menu UI

- [x] 4.1 Add a `userAccountMenu(user, accounts)` templ component (avatar dropdown: active + other accounts, "My Profile", "Add another account", "Log out"), hidden when `user == nil`; verify a render test asserts the menu renders with active + other accounts and is absent for nil user
- [x] 4.2 Wire `appShell`/`page()` to compose the accounts list (active from cookie + inactive from registry) and render the menu in the topbar; verify a render test asserts the topbar shows the avatar for a signed-in user
- [x] 4.3 Remove the sidebar "Account" group (ui.go) and `userProfileFooter` (sidebar_user.templ); verify `go test ./...` after updating the affected tests

## 5. Verification

- [x] 5.1 Update `TestSidebarGroupsWorkspaceAccount` (no Account/Profile group) and `sidebar_user_test.go` (footer removed); verify `go test ./...` passes in `gateway/`
- [x] 5.2 `templ generate` produces no diff; `go build ./...` and `go vet ./...` pass; `golangci-lint run` reports no new issues
- [ ] 5.3 Manual browser test: sign in, "Add another account" shows the Zitadel account picker, both accounts appear in the menu, switching flips identity and project context, "Log out" ends only the current account — each step observable in the DevTools browser
