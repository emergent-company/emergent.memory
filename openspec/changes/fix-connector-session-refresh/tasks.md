## 1. `auth import` preserves a stored refresh token (TDD)

- [ ] 1.1 In `apps/connector.linux/cmd/memory-connector/auth_import.go`, load the existing session for `--server` before saving and carry over its `RefreshToken` when the payload's is empty; a non-empty payload refresh token still wins. Verify: `cd apps/connector.linux && go build ./...`.
- [ ] 1.2 Add `auth import` unit tests: payload without `refresh_token` over an existing refreshable session keeps the stored refresh token; payload with `refresh_token` replaces it; payload without a stored session stores no refresh token. Verify: `go test ./cmd/memory-connector/...` passes.

## 2. Classify refresh failures in the SDK (TDD)

- [ ] 2.1 In `apps/server/pkg/sdk/auth/oauth.go`, add an exported `RefreshError{StatusCode int, Code string, Err error}` (with `Error()`/`Unwrap()`) and return it from `OAuthProvider.Refresh` for non-200 responses (populating `Code` from the OAuth error body when present). Verify: `cd apps/server/pkg/sdk && go build ./...` and `go test ./...` pass.
- [ ] 2.2 Add SDK unit tests asserting a non-200 refresh yields a `*RefreshError` with the status/code, and that transport failures are not `*RefreshError`. Verify: `go test ./auth/...` passes.

## 3. Only clear the session on an authentication rejection (TDD)

- [ ] 3.1 In `apps/connector.linux/internal/account/account.go`, change `Manager.Refresh` to call `Logout` only when the error is a `*auth.RefreshError` with status 400/401 or code `invalid_grant`/`invalid_token`; otherwise keep the session and return the error. Verify: `cd apps/connector.linux && go build ./...`.
- [ ] 3.2 Add `account` unit tests: a transient error (plain error / 5xx) retains the stored session and refresh token; a 400 `invalid_grant` clears it. Encode the refresh dependency to return each case. Verify: `go test ./internal/account/...` passes.

## 4. Stop the app overwriting the session it just created (TDD)

- [ ] 4.1 In `apps/connector.mac/MemoryConnector/Sources/AccountStore.swift`, remove the unconditional `importSessionToConnectorCLI` call that runs immediately after a successful `auth complete`; leave the `ensureConnectorSession` self-heal path intact. Verify: xcodebuild builds the app (see 5.1).
- [ ] 4.2 Update `apps/connector.mac/MemoryConnector/Tests/AccountStoreTests.swift` so sign-in asserts `auth import` is **not** invoked, while the self-heal test still asserts it is invoked when the CLI reports signed out. Verify: the hosted unit tests pass (see 5.1).

## 5. Verification

- [ ] 5.1 On the Mac build machine: `cd ~/code/memory-macfix && xcodebuild -project apps/connector.mac/MemoryConnector/MemoryConnector.xcodeproj -scheme MemoryConnector -destination 'platform=macOS' -derivedDataPath build/DerivedData CODE_SIGNING_ALLOWED=YES CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY=- ENABLE_HARDENED_RUNTIME=NO test`.
- [ ] 5.2 Go checks: `cd apps/connector.linux && gofmt -l . && go build ./... && go test ./... && go vet ./...`; `cd apps/server/pkg/sdk && go build ./... && go test ./...`.
- [ ] 5.3 `openspec validate fix-connector-session-refresh --strict`.
