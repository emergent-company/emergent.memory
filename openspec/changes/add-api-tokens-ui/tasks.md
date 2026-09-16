## 1. MemoryClient project-scoped API-token methods (TDD)

- [ ] 1.1 Add typed structs (`APIToken`, `APITokenList`, `CreateAPITokenRequest`, `CreateAPITokenResponse`) modeling the full server DTO (incl. `lastUsedAt`, `isRevoked`, `expiresAt`) and a `ListAPITokens` method wrapping `GET /api/projects/{projectId}/tokens`. Verify: `go build ./...` compiles.
- [ ] 1.2 Add `CreateAPIToken` (POST), `GetAPIToken` (GET /:tokenId), `UpdateAPITokenScopes` (PATCH), `RevokeAPIToken` (DELETE), and `RegenerateAPIToken` (POST /:tokenId/regenerate). Verify: `go build ./...` compiles.
- [ ] 1.3 Thread the session's active project + bearer token through the six calls via `projectIDFor`/`sessionHeaders` (send `X-Project-ID`). Verify: `go build ./...` compiles after the per-request credential refactor.
- [ ] 1.4 Write `httptest` unit tests for list/create/get/update/revoke/regenerate covering success, empty list, missing name/scopes (400), duplicate name (409), forbidden (403), and unreachable. Verify: `go test ./...` in `gateway/` passes.

## 2. MemoryClient account-scoped API-token methods (TDD)

- [ ] 2.1 Add `ListAccountAPITokens` (GET /api/tokens), `CreateAccountAPIToken` (POST), `GetAccountAPIToken` (GET /:tokenId), `UpdateAccountAPITokenScopes` (PATCH), `RevokeAccountAPIToken` (DELETE), `RegenerateAccountAPIToken` (POST /:tokenId/regenerate), scoped to the signed-in user (no project header). Verify: `go build ./...` compiles.
- [ ] 2.2 Write `httptest` unit tests for the six account methods covering the same success/error paths as §1.4. Verify: `go test ./...` passes.

## 3. Project API tokens page handler and routes

- [ ] 3.1 Add `uiAPITokens` handler and `GET /settings/tokens` route, plus an API Tokens sidebar item in `sidebarGroups()`; render the token list with scopes and revocation state and an error state when Memory is unreachable. Verify: `go build ./...` compiles and `/settings/tokens` renders.
- [ ] 3.2 Add `POST /settings/tokens` (create, renders plaintext once), `POST /settings/tokens/:tokenId/revoke` and `POST /settings/tokens/:tokenId/scopes` (PRG, flash toast), and `POST /settings/tokens/:tokenId/regenerate` (render once, no redirect). Verify: `go build ./...` compiles.
- [ ] 3.3 Write handler unit tests for happy paths (create shows the token once, revoke marks revoked, regenerate returns new token, scope edit persists) and error paths (empty name, no scopes, unsupported scope, duplicate name 409, permission 403, revoke/regenerate rejected, unauthenticated redirect). Verify: `go test ./...` passes.

## 4. Account API tokens section (profile) handler and routes

- [ ] 4.1 Extend `uiProfile` to render an account API Tokens section listing the signed-in user's account tokens. Verify: `go build ./...` compiles and `/profile` renders the section.
- [ ] 4.2 Add `POST /profile/tokens` (create), `POST /profile/tokens/:tokenId/revoke`, `POST /profile/tokens/:tokenId/scopes`, and `POST /profile/tokens/:tokenId/regenerate`, mirroring the project handlers but user-scoped. Verify: `go build ./...` compiles.
- [ ] 4.3 Write handler unit tests for the account flows (mirroring §3.3). Verify: `go test ./...` passes.

## 5. Templates

- [ ] 5.1 Create `api_tokens.templ` with the token list (name, prefix, scopes, last-used, revoked badge), a create form (name + grouped scope checkboxes), edit-scope controls, one-shot plaintext panels for create/regenerate with copy, and empty/error states. Omit `admin:all` for callers who cannot hold it. Verify: `templ generate` succeeds and the page renders.
- [ ] 5.2 Add the account tokens section to the profile template (reusing the same list/form/plaintext-panel patterns). Verify: `templ generate` succeeds and `/profile` renders.
- [ ] 5.3 Write `.templ` render tests (mirroring `project_settings_ui_test.go`) asserting the project list, empty state, create form, plaintext panel, and the profile account-tokens section. Verify: `go test ./...` passes.

## 6. Build, lint, and manual verification

- [ ] 6.1 Run `templ generate`, `go build ./...`, and `task lint`; fix any issues until clean. Verify: all three commands succeed.
- [ ] 6.2 Manual browser test via DevTools: navigate to `/settings/tokens` and the `/profile` tokens section; create (name + scopes) and confirm the plaintext is shown once with copy, list with scopes, edit scopes, regenerate, revoke, and confirm the unreachable-Memory, unauthenticated (redirect), duplicate-name, and permission-denied states. Verify: each flow behaves per spec. (Headless smoke test: `/settings/tokens` returns HTTP 200 and renders; interactive DevTools pass deferred to the user's local browser.)
