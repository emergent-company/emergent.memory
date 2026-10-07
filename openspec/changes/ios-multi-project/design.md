## Context

- The web gateway (`apps/web-ui/gateway`) is the single origin the iOS app talks to. It mints a project-scoped `emt_*` device credential on the session-authenticated devices page and hands it to the app through a one-time setup token embedded in a QR.
- Device credentials are project-bound by construction (`MemoryClient.CreateDeviceToken` → `POST /api/projects/{projectId}/device-tokens`; scope ceiling `device:api`, `agents:read`, `data:read`). There is no user-level token on the device, and the device surface derives project/org from token introspection, never from headers.
- The gateway holds no process-global memory credential after #791: every memory call borrows the *request's* session token (`tokenFor`). A session-less request therefore sends an empty bearer.

## Goals / Non-Goals

### Goals

- `POST /api/setup` succeeds from a device that has no session.
- Every onboarding records which project it connects, and the device can hold several projects and switch between them.
- All existing iOS clients keep reading the one active connection with no interface change.

### Non-Goals

- A user-level (multi-project) token on the device, or server-side project listing for a device credential. Onboarding stays QR-per-project.
- Persisting pending setup tokens across gateway restarts (see Decision 3).
- Any change to the device-credential scope ceiling or surface guard.

## Decisions

### Decision 1 — Store the one-time setup token in a gateway-local in-process store

The setup token, its target project identity, and the pre-minted device credential live in a `setupTokenStore` on `*Server` (mutex + map keyed by token), not in a memory project setting. Minting runs on the session-authenticated devices page and still calls `CreateDeviceToken` (memory, session-authenticated). Consuming is a pure local lookup.

- **Why:** the only reason the exchange needs memory is to read the stored credential, and it has no credential with which to do so. Removing the backend read removes the 401 and the circular dependency.
- **Consequence:** the store records `orgID`/`projectID`/`projectName`, which is exactly what the response must return for multi-project.

### Decision 2 — Mint-time rotation keeps revoking unclaimed orphans

On rotation (expired or already-used token, or a new project), the store revokes the prior **unclaimed** device credential through the session-authenticated `RevokeAPIToken` call, and a claimed credential is never revoked. This preserves the existing "an abandoned QR never leaks a live credential" invariant.

### Decision 3 — No restart persistence (accepted tradeoff)

The store is in-process. A gateway restart drops pending (unclaimed) setup tokens; the QR is re-minted on the next devices-page load, and the 10-minute TTL bounds the window. We deliberately do not introduce a plaintext-at-rest file or a new server endpoint for this; if restart survival becomes required, the fix is a server-owned setup endpoint, tracked as follow-up.

### Decision 4 — QR payload and response carry project identity

`setupQRPayload` resolves the devices page's project scope and emits `{"setupURL","token","orgID","projectID","projectName"}`; `setupClient` echoes the same identity alongside the per-device config. The iOS app stores it directly rather than trying to resolve a project from a token that lacks `projects:read`.

### Decision 5 — iOS active connection mirrors into existing `alfred.*` keys

`MemoryProjectStore` persists the full list under `alfred.projects` and the active id under `alfred.activeProjectID`, and on every switch/onboard writes the active connection's `serverURL`/`tokenEndpoint`/`apiBaseURL`/`apiKey`/`ttsStrategy`/`project*` into the existing `MemoryConfig` keys. `MemoryConfig()` therefore keeps returning the active connection and no client or controller changes its read path. Switching posts the existing `.memoryConfigChanged` notification, which already rebuilds the session controller.

### Decision 6 — Per-project agent selection

`AgentStore` keys the selected agent on the active project (`alfred.agentName.<projectID>`), so switching projects selects that project's agent (or its default). A missing key falls back to the build default.

### Decision 7 — Migration of the legacy single connection

On first launch after upgrade, if `alfred.projects` is absent but a legacy `alfred.apiKey` is set, synthesize one connection from the existing keys with empty project identity and mark it active. This avoids dropping an already-onboarded server; the next QR scan fills in real project identity.

## Risks

- **In-process loss on restart** — mitigated by TTL + re-mint; documented above.
- **Device credential plaintext in UserDefaults** — status quo (already stored there); no change in trust level.
- **`/api/setup` remains public** — unchanged; the one-time token is still the only authority and is now single-use in the local store.
