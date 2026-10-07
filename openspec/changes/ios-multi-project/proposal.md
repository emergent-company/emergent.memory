## Why

Two related problems in the iOS voice client:

1. **QR onboarding is broken (401).** `POST /api/setup` is a public, session-less endpoint, but it reads the one-time setup token from a *project setting* via the memory API. Since #791 removed the gateway's process-global `MEMORY_TOKEN`, the session-less exchange sends an empty bearer and memory answers **401**. The endpoint has no credential path at all, so scanning a QR always fails. Unit tests miss this because they use a `fakeMemory` that does not enforce auth.

2. **iOS is single-project.** `MemoryConfig` models exactly one connection and QR onboarding blind-overwrites it. A device credential is inherently project-scoped (`CreateDeviceToken(projectID, ...)`), and the QR/response carry no project identity, so the app can neither label a connection nor switch between projects. The Mac connector already has multi-project select/connect; iOS has no equivalent.

## What Changes

- **Fix the setup exchange** by moving the one-time setup token → device-credential mapping out of memory and into a gateway-local, in-process store. Minting still runs on the session-authenticated devices page (so `CreateDeviceToken` still works); consuming becomes a no-backend lookup, which removes the 401.
- **Carry project identity end-to-end.** The QR payload and the `/api/setup` response gain `orgID`, `projectID`, and `projectName`, captured when the QR is minted under the devices page's project scope.
- **Add a multi-project store to iOS.** A new `MemoryProjectConnection` + `MemoryProjectStore` persists every onboarded project (its URL/token/info) plus the active project, and mirrors the active connection into the existing `alfred.*` config keys so all existing clients keep working unchanged.
- **Add a global project switcher** to the agents picker header (with an "Add project (QR)" action) and a Projects section in Settings (switch / add / remove).
- **Scope agent selection per project**, since each project has its own agent list.

## Capabilities

### New Capabilities

- `ios-multi-project`: the iOS client's multi-project model — project-scoped onboarding, a persisted project list with an active selection, a global project switcher, and per-project agent selection.

### Modified Capabilities

- `web-device-credential`: the setup exchange stores its one-time token in a gateway-local store (session-less consumption, fixing the 401), and the setup QR/response carry the project the onboarding is bound to.

## Impact

- `apps/web-ui/gateway/setup.go` — local setup-token store; mint/consume no longer use project settings; QR payload + setup response gain project identity.
- `apps/web-ui/gateway/main.go` — initialize the setup-token store.
- `apps/web-ui/gateway/setup_test.go`, `auth_test.go` — rework setup tests (seed the local store; assert a session-less exchange succeeds).
- `apps/ios/VoiceAgent/Memory/` — new `MemoryProjectConnection.swift`, `MemoryProjectStore.swift`, `MemoryProjectSwitcher.swift`; `MemoryConfig.swift` gains project keys; `MemoryQRScannerView.swift` upserts/switches on scan.
- `apps/ios/VoiceAgent/Agents/AgentStore.swift` — per-project selected agent.
- `apps/ios/VoiceAgent/App/AppShellView.swift`, `VoiceAgentApp.swift`, `Memory/MemorySettingsView.swift` — switcher, store wiring, Projects section.
- `apps/ios/VoiceAgent/Localizable.xcstrings`, `Helpers/Log.swift` — new strings and a `projects` logger.
- `apps/ios/VoiceAgentTests/` — new project-store/config tests.
