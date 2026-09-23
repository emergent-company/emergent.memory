## Why

PR #791 removed the shared `MEMORY_TOKEN` service account from the web-ui gateway — correct in direction, but it also broke the session-less device path: the iOS `VoiceAgent` voice client (`apps/ios/VoiceAgent/Net/GatewayHTTP.swift` sends `X-API-Key`) and the Mac wake-word console no longer authenticate, and device keys could never mint a Memory bearer (the gateway proxied an empty `Authorization: Bearer `). The fix must not re-introduce a shared ambient credential; it must give each device its own scoped, revocable credential. The full design (threat model, credential shape, scope ceiling, authority checks, revocation/audit, rejected alternatives, residual risks) is recorded in the design comment on issue #818, which is the design of record.

## What Changes

- **Credential reuses `core.api_tokens` with a reserved marker scope, maximally scoped.** A new marker scope `device:api` (alongside `mcp:agent-call` and `share:agent-chat`) is minted only by the internal `CreateDeviceToken(projectID, name, expiresAt)` path, with the hardcoded read-only scope set `device:api + agents:read + data:read` (strictly below `project_viewer`) and `user_id = NULL`. User-facing `Create`/`CreateAccountToken`/`UpdateScopes`/`UpdateAccountTokenScopes` reject the marker via `rejectReservedScopes`.
- **Validate-time exact-set ceiling + surface guard, fail-closed.** `validateAPIToken` rejects a `device:api` token whose scope set is not exactly the ceiling (defence against DB tampering); `RequireAuth` rejects a `device:api` token outside the device surface (modelled on `rejectShareTokenOutsideSurface`). Unknown/revoked/expired/store-down deny.
- **Best-effort `last_used_at` touch** for device-token use (the general token gap is tracked separately).
- **Gateway device branch.** `requireSessionOrKey` falls back to `requireDeviceCredential` when no session is present: it introspects the `emt_*` bearer for the `device:api` marker (via memory's `GET /api/auth/me`), enforces a gateway surface allowlist (`/api/token`, agent picker, chat/session relay, `/api/memories*`), proxies the credential verbatim, and derives project/org from the token — never from raw headers.
- **`/api/setup` returns the device credential.** The session-authenticated devices page mints the device credential server-side at QR time (storing it for a single claim and revoking any orphan on rotation); `POST /api/setup` exchanges the one-time token for the `emt_*` credential. The 64-hex `ios_device_keys` registry is retired (it already authenticated nothing since #791, which also closes the registry-race issue by retirement).
- **Devices UI** lists and revokes the project's device credentials via the shared project-token surface.

## Decisions

1. **Gateway recognition — marker introspection (recommended option chosen).** The gateway tells a device token from a user's programmatic `emt_*` token by introspecting the presented bearer for the reserved `device:api` marker (a lightweight `GET /api/auth/me` roundtrip). The simpler "treat any `emt_*` bearer as device-class" alternative was rejected because it would silently change gateway semantics for any future programmatic caller of the gateway. The marker keeps device and programmatic tokens distinct without a new token format.
2. **Read-only ceiling confirmed sufficient.** The ceiling `device:api + agents:read + data:read` covers the verified device surface — voice room JWT, agent picker, chat/session relay, read-memory browsing — with no known device write feature. No widening was made.

## Capabilities

### New Capabilities

- `web-device-credential`: the scoped, revocable per-device credential end to end — credential shape + internal mint, reserved-marker gating, validate-time exact-set ceiling + surface guard, gateway device branch with surface allowlist and token-derived context, per-device identity/expiry/revocation/last-used, and the `/api/setup` exchange retiring the registry.

### Modified Capabilities

None. Existing semantics — the reserved-scope mint pattern and the surface guard in `pkg/auth` (`rejectShareTokenOutsideSurface`), the scope-authority posture, and the project-token lifecycle — are referenced and reused, not restated.

## Impact

- `apps/server/domain/apitoken/`: `device:api` marker in `ValidApiTokenScopes`; `CreateDeviceToken` internal mint (hardcoded `device:api + agents:read + data:read`, `user_id = NULL`); `rejectReservedScopes` gains the `device:api` gate; `POST /api/projects/:projectId/device-tokens` handler/route.
- `apps/server/pkg/auth/`: `device:api` in the scope vocabulary; `deviceScopesMatchCeiling` + `deviceSurfaceAllowed` + `rejectDeviceTokenOutsideSurface`; ceiling check and best-effort `last_used_at` touch in `validateAPIToken`; surface guard in `RequireAuth`.
- `apps/web-ui/gateway/`: `requireSessionOrKey` device fallback (`requireDeviceCredential`), `deviceSurfacePath` allowlist, `attachDeviceContext`, `IntrospectDeviceToken`/`CreateDeviceToken` on the Memory client, `sessionContext.Device`, `voiceBindingFor` device path, and the `/api/setup` + devices-UI retirement of the registry.
- `apps/web-ui/docs/spec/` + `MEMORY_GUIDE.md`: the "removed, see #818" stubs replaced with the device-credential contract.
- No change to the server's route-auth posture: the server continues to expose only authenticated endpoints; the device credential is an authenticated, scope-bounded token.

## Back-compat

Existing 64-hex device keys authenticate nothing already (broken since #791), so there is no live migration — devices re-onboard via a fresh QR. The `ios_device_keys` registry setting becomes dead.
