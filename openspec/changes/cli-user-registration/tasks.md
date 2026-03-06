## 1. CLI: Register Command

- [x] 1.1 Add `registerCmd` cobra command definition in `tools/emergent-cli/internal/cmd/auth.go` with `Use: "register"`, short description "Create a new Emergent account", and long description explaining the device-flow process
- [x] 1.2 Implement `runRegister()` function: load config, validate server URL is set, initiate OIDC discovery (`auth.DiscoverOIDC`), request device code (`auth.RequestDeviceCode`), print verification URL + user code, attempt browser open, poll for token (`auth.PollForToken`)
- [x] 1.3 In `runRegister()`, after token acquisition, call `GET /api/auth/me` with the access token and parse the `TokenInfoResponse` (user_id, email, type)
- [x] 1.4 In `runRegister()`, save credentials to `~/.emergent/credentials.json` via `auth.Save` (same as login), then print success message including confirmed email and user_id
- [x] 1.5 Add standalone-mode detection in `runRegister()`: call `GET /health`, check if response indicates standalone mode, print appropriate error and return early if so
- [x] 1.6 Wire `registerCmd` into the root command in `init()` alongside `loginCmd` (line ~987 of auth.go)

## 2. CLI: Status UX Update

- [x] 2.1 Update the unauthenticated branch of `runStatus()` in `tools/emergent-cli/internal/cmd/auth.go` to suggest both `emergent login` (returning users) and `emergent register` (new users) as next steps

## 3. E2E Test: Auth Me Endpoint

- [x] 3.1 Create `apps/server-go/tests/e2e/authinfo_test.go` with a test suite `TestAuthInfoSuite` using `testutil.BaseSuite`
- [x] 3.2 Implement `TestAuthMeReturnsUserInfo`: call `GET /api/auth/me` with `Authorization: Bearer e2e-test-user`, assert HTTP 200, assert response `user_id` is non-empty, assert `type` field is present
- [x] 3.3 Implement `TestAuthMeReturns401WithoutToken`: call `GET /api/auth/me` without auth header, assert HTTP 401

## 4. Verification

- [x] 4.1 Build the CLI (`task cli:install`) and run `emergent register --help` to verify the command is wired and help text is correct
- [x] 4.2 Run the E2E tests: `POSTGRES_PORT=5436 POSTGRES_PASSWORD=local-test-password go test ./tests/e2e/ -run TestAuthInfoSuite` from `apps/server-go` and verify all pass
- [x] 4.3 Run `emergent status` with no credentials and verify both `login` and `register` appear in the output hints
