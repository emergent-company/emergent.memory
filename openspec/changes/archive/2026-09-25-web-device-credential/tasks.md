## 1. Server — vocabulary + mint

- [x] 1.1 Add `device:api` to `ValidApiTokenScopes` and `pkg/auth` scope vocabulary (reserved marker, absent from user-facing oneof tags)
- [x] 1.2 Add `CreateDeviceToken(projectID, name, expiresAt)` internal mint with the hardcoded scope set `device:api + agents:read + data:read`, `user_id = NULL`
- [x] 1.3 Gate `device:api` behind `rejectReservedScopes` (new `allowDeviceAPI` flag) so no user-facing mint/update path can attach it
- [x] 1.4 Add `POST /api/projects/:projectId/device-tokens` handler + route
- [x] 1.5 Unit tests: exact-ceiling set, reserved-scope rejection, vocabulary

## 2. Server — validate-time ceiling + surface guard

- [x] 2.1 `deviceScopesMatchCeiling` rejects a `device:api` token whose set is not exactly the ceiling (DB-tamper defence)
- [x] 2.2 `rejectDeviceTokenOutsideSurface(method, path, scopes)` + `deviceSurfaceAllowed` in `RequireAuth`
- [x] 2.3 Best-effort, non-blocking `last_used_at` touch for device-token use in `validateAPIToken`
- [x] 2.4 Unit tests: ceiling match, surface allowlist, fail-closed paths

## 3. Gateway — device branch + surface allowlist

- [x] 3.1 `sessionContext.Device` + `attachDeviceContext` (proxy credential verbatim, token-derived project/org)
- [x] 3.2 `requireDeviceCredential` in `requireSessionOrKey`: marker introspection, surface allowlist, fail-closed 401/403
- [x] 3.3 `deviceSurfacePath` gateway allowlist; `IntrospectDeviceToken`/`CreateDeviceToken` on the Memory client
- [x] 3.4 `voiceBindingFor` uses the device credential verbatim as the worker token
- [x] 3.5 Unit tests: surface allowlist + fail-closed recognition (missing/revoked/expired/store-down/off-surface)

## 4. Gateway — setup swap + retire registry

- [x] 4.1 `mintSetupToken` mints the device credential server-side at QR time; revokes orphan on rotation
- [x] 4.2 `consumeSetupToken` returns the device credential; `POST /api/setup` returns it as `apiKey`
- [x] 4.3 Retire the 64-hex registry; repoint devices UI at project API tokens (list/revoke by id)
- [x] 4.4 Unit tests: device-credential lifecycle, exchange returns `emt_*`, registry key rejected

## 5. Docs + OpenSpec

- [x] 5.1 `09-security.md`, `10-api-contracts.md`, `04-go-application.md`, `07-clients.md`, `MEMORY_GUIDE.md` — remove "removed, see #818" stubs, document the device credential
- [x] 5.2 OpenSpec change `web-device-credential` (proposal + delta specs in SHALL form)

## 6. Verification

- [x] 6.1 `go build ./...` (server + gateway) clean
- [x] 6.2 Server `go test` for `pkg/auth` + `domain/apitoken`; gateway `go test ./...` green
- [x] 6.3 `apps/server/scripts/lint-ratchet.sh` all "ratchet ok"
- [x] 6.4 Gateway `task lint` 0 issues
- [x] 6.5 `openspec validate --all --strict` green
