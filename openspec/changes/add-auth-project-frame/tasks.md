## 1. Config & session foundation

- [x] 1.1 Add Zitadel env vars (`ZITADEL_ISSUER`, `ZITADEL_CLIENT_ID`, `ZITADEL_CLIENT_SECRET`, `ZITADEL_REDIRECT_URI`) and an `AUTH_MODE` toggle to `Config`/`LoadConfig`, and verify a unit test asserts the new fields parse with sensible defaults
- [x] 1.2 Implement the signed-cookie session (issue/verify/clear, payload `{access_token, refresh_token, active_project_id}`) and verify a unit test covers round-trip, tamper rejection, and expiry
- [x] 1.3 Implement the per-request `sessionContext` (token + projectID) and refactor `MemoryClient` to take credentials per call; verify `go test ./...` in `gateway/` passes after threading

## 2. OIDC sign-in flow

- [x] 2.1 Add `GET /auth/start` redirect to the Zitadel authorize endpoint and `GET /auth/callback` code exchange; verify a handler test with a fake token endpoint covers success and error paths (`GET /auth/login` renders the sign-in page whose action targets `/auth/start`)
- [x] 2.2 Add `POST /auth/logout` clearing the session cookie; verify a test asserts subsequent protected requests redirect to login
- [x] 2.3 Add the session-required UI middleware and the session-OR-`X-API-Key` API middleware; verify tests cover unauthenticated (redirect/401), session, and API-key paths

## 3. Project management

- [x] 3.1 Add Memory client methods `ListOrgs`, `ListProjects`, `CreateProject` and verify unit tests against the fake memory backend
- [x] 3.2 Add handlers/routes to list, switch (persist `active_project_id` in the cookie), and create (name + orgId) projects; verify handler tests cover empty list, switch, and create-with-org paths
- [x] 3.3 Thread the session's active project through project-scoped Memory calls, sending `X-Project-ID` (and `X-Org-ID` when known) headers on session-token requests; verify existing tests updated to pass a project context still pass

## 4. Web UI

- [x] 4.1 Add the login page (sign-in entry / redirect affordance) and verify a templ render test asserts it renders
- [x] 4.2 Add the project switcher + project-create form to the UI shell; verify render tests cover the switcher, empty state, and create form

## 5. Verification

- [x] 5.1 `go build ./...` + `go vet ./...` + `go test ./...` in `gateway/` pass (2 pre-existing failures `TestRenderRunPageChatBubbles`/`TestRenderRunPageEmptyTranscript` are unrelated and reproduce on HEAD before this change)
- [x] 5.2 `templ generate` produces no diff and `golangci-lint run ./...` passes for this change (note: `task lint` shells out to `lefthook`, which is not installed on this box — ran `golangci-lint` directly; the 3 findings are in pre-existing/parallel files, none in this change)
- [ ] 5.3 Manual browser test: sign in via Zitadel, switch project, create project, sign out — each step observable in the DevTools browser (blocked: requires real `ZITADEL_ISSUER`/client/redirect values from `emergent-infra` at deploy time)
