## 1. OIDC Revocation Support

- [x] 1.1 Add `RevocationEndpoint` field to `OIDCConfig` struct in `tools/cli/internal/auth/discovery.go`
- [x] 1.2 Create `Revoke(issuerURL, clientID, token, tokenTypeHint string) error` function in a new file `tools/cli/internal/auth/revoke.go` — discovers OIDC config, POSTs to revocation endpoint with `token`, `token_type_hint`, and `client_id` form params, uses 10-second HTTP timeout
- [x] 1.3 Write unit tests for `Revoke` in `tools/cli/internal/auth/revoke_test.go` — test successful revocation, missing revocation endpoint (skip gracefully), network error, timeout, non-2xx response

## 2. Enhanced Logout Command

- [x] 2.1 Add `--all` flag to `logoutCmd` in `tools/cli/internal/cmd/auth.go` (bound to a package-level `logoutAll` bool variable)
- [x] 2.2 Rewrite `runLogout` to: (a) load credentials, (b) attempt OIDC revocation of refresh token then access token if issuer URL is present (warn on failure), (c) delete credentials file, (d) if `--all`, load config, clear `api_key` and `project_token`, save config
- [x] 2.3 Update command `Long` description to document the `--all` flag behavior
- [x] 2.4 Print detailed output: what was revoked (or skipped), what files were cleaned, what config fields were cleared

## 3. Tests

- [x] 3.1 Update existing `TestLogout` in `tools/cli/internal/cmd/auth_test.go` to verify revocation is attempted (mock HTTP server for OIDC discovery + revocation endpoint)
- [x] 3.2 Add `TestLogoutAll` — verify that `--all` clears `api_key` and `project_token` from a temp config file while preserving `server_url` and other fields
- [x] 3.3 Add `TestLogoutNoIssuer` — verify that credentials without an issuer URL skip revocation gracefully
- [x] 3.4 Add `TestLogoutRevocationFails` — verify that revocation failure produces a warning but still deletes local credentials
