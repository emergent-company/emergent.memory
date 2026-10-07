## 1. Gateway — setup exchange fix

- [x] 1.1 Add a `setupTokenStore` (mutex + map keyed by token; entry carries `token`, `orgID`, `projectID`, `projectName`, `deviceToken`, `deviceTokenID`, `createdAt`, `used`) and initialize it in `newServer`
- [x] 1.2 Rewrite `mintSetupToken` to reuse/store in the local store (no `GetProjectSetting`/`SetProjectSetting`), accept the request's project identity, keep the unclaimed-orphan revoke on rotation, and keep `CreateDeviceToken` minting
- [x] 1.3 Rewrite `consumeSetupToken` as a local single-use lookup that returns the device credential and the stored project identity; add a failure test proving no memory call is made
- [x] 1.4 Extend `setupQRPayload` to include `orgID`/`projectID`/`projectName`, resolved from the devices page request scope plus `GetCurrentProject`
- [x] 1.5 Extend `setupClient`'s 200 response with `orgID`/`projectID`/`projectName`
- [x] 1.6 Remove the now-unused `setupTokenCategory`/`setupTokenKey` constants and dead imports
- [x] 1.7 Rework `setup_test.go` + `auth_test.go`: seed via the local store, assert a session-less `POST /api/setup` returns 200 with the project identity, replace `TestSetupTokenSurvivesRestart` with a same-store single-use assertion
- [x] 1.8 Verify: `go build ./...`, `go test ./...`, `task lint` (gateway)

## 2. iOS — project model + store

- [x] 2.1 Add `MemoryProjectConnection` (Codable: `projectID`, `orgID`, `projectName`, `serverURL`, `tokenEndpoint`, `apiBaseURL`, `apiKey`, `ttsStrategy`, `addedAt`) with tolerant decoding
- [x] 2.2 Add `MemoryProjectStore`: persist `alfred.projects` + `alfred.activeProjectID`; `connections`, `activeConnection`, `activeProjectID`, `upsert`, `setActive`, `remove`; mirror the active connection into the `alfred.*` config keys on every mutation
- [x] 2.3 Add legacy migration: when `alfred.projects` is absent but a legacy `alfred.apiKey` is present, synthesize and activate one connection from the existing keys
- [x] 2.4 Extend `MemoryConfig` with `projectID`/`orgID`/`projectName` keys + values + save/reset
- [x] 2.5 Make `AgentStore` select per project (`alfred.agentName.<projectID>`), reset/reload on active-project change, fall back to the build default
- [x] 2.6 Verify: unit tests for store round-trip/upsert-by-project/setActive/remove/migration, config new keys, per-project agent key

## 3. iOS — onboarding + switcher UI

- [x] 3.1 Extend the QR payload model with `orgID`/`projectID`/`projectName`; on success `upsert` the connection, `setActive`, reload agents, post `.memoryConfigChanged`
- [x] 3.2 Add a global project switcher menu to the agents-picker header: list projects with the active checkmark, switch, and an "Add project" (QR) action
- [x] 3.3 Add a Projects section to Settings: list with active state, switch, add-via-QR, remove
- [x] 3.4 Add the new localization keys and a `Log.projects` category
- [x] 3.5 Verify: iOS build on `mcj-mini` (`apps/web-ui/tools/ios-build-mac.sh`) and SwiftFormat/SwiftLint clean

## 4. Integration + close-out

- [ ] 4.1 Manual end-to-end (requires a live gateway + real device; hand off to requester): scan the QR in project A, then project B; switch on device; confirm agents/objects/sessions follow the active project
- [x] 4.2 Copy review of the new user-facing strings (visual/interaction intent unchanged)
- [x] 4.3 Run `openspec validate ios-multi-project` and confirm `task openspec:archive-check` is clean
- [ ] 4.4 Archive the change post-merge (`openspec archive ios-multi-project --yes`)
