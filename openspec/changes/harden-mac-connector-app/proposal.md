## Why

The shipped mac connector app is built in Swift 5 language mode (`Scripts/build-dmg.sh:116` overrides `SWIFT_VERSION=5`), runs with an empty entitlements file (`MemoryConnector.entitlements` is `<dict/>`), and drives engine process I/O through `nonisolated(unsafe)` shared mutable state (`EngineManager.swift:105,110,54`). Users therefore run a binary with no hardened-runtime Enhanced Security protections and with strict-concurrency checking disabled precisely where it matters, while known concurrency hazards remain in engine logging and the engine config file is written by string concatenation that can be corrupted by ordinary values.

## What Changes

- Add hardened-runtime Enhanced Security entitlements (enhanced-security version 2, hardened heap, dyld read-only, platform restrictions) so the release artifact actually provisions the protections `ENABLE_HARDENED_RUNTIME` only nominally enables.
- Enable `ENABLE_ENHANCED_SECURITY` and `CLANG_ENABLE_STACK_ZERO_INIT`; enable pointer authentication only after Sparkle and the embedded binaries are confirmed to ship arm64e slices; keep memory-tagging and checked-pointer-arithmetic features deferred.
- Compile the Release archive in Swift 6 language mode instead of downgrading to Swift 5.
- Serialize engine and app log writes through one sink and remove the `nonisolated(unsafe)` shared mutable log state and background stderr buffer.
- Persist the engine configuration through safe encoding so a server URL, identifier, token, or path containing `:`, quotes, or a newline cannot corrupt the document or change another key's meaning.
- **Internal, behavior-preserving work tracked in `design.md`/`tasks.md` only (not new spec requirements):** migrate stores from `ObservableObject`/`@EnvironmentObject` to `@Observable`/`@Environment`, replace singleton reach-through with a composition root, factor oversized view sections into `View` structs, split the god stores, and migrate the test suite to Swift Testing.

**BREAKING**: none user-visible. All changes are behavior-preserving apart from the added integrity guarantees above.

## Capabilities

### New Capabilities

- `mac-connector-release-hardening`: guarantees of the signed release artifact — Swift 6 language mode, hardened-runtime Enhanced Security protections, and stack zero-initialization — plus the gating rule for pointer authentication and memory tagging.

### Modified Capabilities

- `mac-connector-app`: adds engine-configuration persistence integrity (values containing YAML-significant characters round-trip and cannot corrupt the document) and serialized, lossless engine/app log writing without unsynchronized shared mutable state.

## Impact

- **Build/signing**: `apps/connector.mac/MemoryConnector.entitlements`, `MemoryConnector.xcconfig`, `project.yml`, `Scripts/build-dmg.sh`.
- **App runtime**: `apps/connector.mac/MemoryConnector/Sources/EngineManager.swift`, `EngineConfigWriter.swift`, `ConnectorLog.swift`.
- **Tests**: new unit coverage for config encoding round-trips and log-sink serialization (TDD). The XCTest→Swift Testing migration is tracked as internal work.
- **No server, API, or CLI changes.**
