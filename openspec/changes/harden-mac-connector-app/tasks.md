## 0. Baseline Swift 6 compile fix

- [x] 0.1 Fix the pre-existing Swift 6 actor-isolation error so the app compiles in Swift 6 language mode (`MCPServersStore.ownToolName` made `nonisolated`); verified a Debug build succeeds on the Mac build machine (remaining compiler warnings are pre-existing and non-blocking)

## 1. Release hardening

- [x] 1.1 Add hardened-runtime Enhanced Security entitlements (`com.apple.security.hardened-process`, enhanced-security version 2, hardened heap, dyld read-only, platform restrictions) to `MemoryConnector.entitlements`; file is valid and wired via `CODE_SIGN_ENTITLEMENTS`, and the release re-sign in `Scripts/build-dmg.sh` now passes `--entitlements` so they are not stripped. Caveat: ad-hoc/self-signed dev signing silently drops restricted hardened-process entitlements, so key presence in a signed artifact can only be confirmed on a Developer ID/notarized release build.
- [x] 1.2 Add `ENABLE_ENHANCED_SECURITY = YES` and `CLANG_ENABLE_STACK_ZERO_INIT` to the project/`xcconfig` settings; verified via `xcodebuild -showBuildSettings` (both resolve to YES).
- [ ] 1.3 Verify the Enhanced-Security build still launches and spawns the embedded engine and Reminders helper (signed launch smoke); capture evidence. Blocked in this environment: requires a Developer-ID/GUI launch session. A plain `open` smoke here launched the installed `~/Applications/Memory.app`, not this build, and restricted entitlements cannot be exercised under ad-hoc signing.
- [x] 1.4 Remove the `SWIFT_VERSION=5` override from the release archive step in `Scripts/build-dmg.sh`; the app compiles in Swift 6 language mode (verified via a full Debug build). Note: the full signed/notarized archive was not run here.
- [x] 1.5 Confirm Sparkle and the embedded binaries provide arm64e slices; Sparkle 2.x ships `x86_64 arm64` only (no arm64e) and the embedded engine/helper are arm64 — so `ENABLE_POINTER_AUTHENTICATION` is set to `NO` at the target and pointer authentication is deferred until a dependency ships arm64e.
- [x] 1.6 Re-sign hardening: the embedded-binary and app `codesign` calls now pass `--timestamp` (the manual re-sign replaces the export's timestamped signature; without it notarization can fail with "signature does not include a secure timestamp"), and the script verifies the hardened-process entitlements survived the re-sign and the signature is valid before notarizing.
- [ ] 1.7 Known scope limit: the Enhanced Security runtime entitlements are available on macOS 26+ (the app targets macOS 15.0). On older OSes the app still launches normally but the runtime protections are inert; the entitlement/build-setting claim is only realized on macOS 26+.

## 2. Engine configuration integrity (TDD)

- [x] 2.1 Added failing-first unit tests (`Tests/EngineConfigWriterEncodingTests.swift`): colon round-trip, newline does not corrupt other keys, legacy config loads, failed write leaves prior file intact
- [x] 2.2 Implemented structural scalar encoding/escaping in `EngineConfigWriter` (double-quote only when needed; reader unquotes only double-quoted scalars, legacy plain scalars unchanged); tests pass
- [x] 2.3 Added a full round-trip test over a representative config; all values match. Engine reader verified to use `gopkg.in/yaml.v3` `yaml.Unmarshal`, which accepts the emitted double-quoted scalars.

## 3. Log integrity (TDD)

- [x] 3.1 Added failing-first tests (`Tests/LogSinkTests.swift`): concurrent writers produce whole non-interleaved lines; stdout/stderr do not merge; lifecycle lines remain distinguishable
- [x] 3.2 Introduced a serialized `actor LogSink` owning the append-only handle and per-stream line framing; engine stdout/stderr and app lifecycle lines route through it; tests pass
- [x] 3.3 Removed `nonisolated(unsafe)` shared mutable log state (`EngineManager.logHandle`, `stderrBuffer`; `ConnectorLog.destinationURL`/`isSuppressed`); build is clean in Swift 6 mode

## 4. Internal modernization (behavior-preserving)

- [x] 4.1 Migrated leaf stores (`DashboardStore`, `IdentityStore`, `MCPServersStore`) to `@MainActor @Observable` (plain stored properties, no `@Published`/`ObservableObject`); store tests pass in the full green run
- [x] 4.2 Migrated `AccountStore`, `ProjectStore`, `EngineManager`, `StatusMonitor`, `ConnectorSettings`, `UpdaterModel`, `AppState` (+`AvatarStore`); removed `profilesRevision`/`configurationRevision` counters (observers now track the real property); tests pass
- [x] 4.3 `AppEnvironment` is the composition root (no longer `ObservableObject`; owns `avatarStore` and wires engine closures); `.environmentObject`→`.environment`, `@EnvironmentObject`→`@Environment`, `@StateObject`→`@State`/`@Bindable`, `@ObservedObject`→`let`; views no longer read `EngineManager.shared`/`StatusMonitor.shared`/`AvatarStore.shared`; build + tests green
- [x] 4.4 Factored oversized sections into separate `View` structs with narrow inputs: `DashboardPage` (project/stats/agents/tools/loading/error), `ProjectAccountPage` (accounts/sign-in/project/error), `AboutPage` (header/details/updates/info/development), `MCPServerDetailView` (header/content/footer). `MCPRegistryBrowseSheet` deliberately left (its sections read the `@StateObject` model + mutate parent `@State`; extraction needs threading the model and two bindings for no gain). Build + tests green.
- [ ] 4.5 Partially done: extracted the management-port concern out of `EngineManager` into a focused `ManagementPortProbe` (port, TCP probe, `shouldAbortStartForPortConflict`, injectable `portInUse`); the log concern was already extracted to `LogSink` in 3.x. NOT done (surfaced, not silently deferred): splitting `AccountStore`/`ProjectStore` — the cohesive candidates (token/session bridge, project selection) sit on top of the public test seams (`connectorCLI`, `configPath`, `cliConfigPath`, `repairConnectorSession`, `organizationNameLoader`) that 30KB+ test suites inject directly, so extracting them would break that seam and require large test rewrites. Needs an explicit decision.
- [ ] 4.6 Partially done: migrated the five pure suites (`EngineLifecyclePolicyTests`, `ScopeSwapGateTests`, `StatusSnapshotParserTests`, `StatusSnapshotJSONTests`, `RestartPolicyTests`) from XCTest to Swift Testing; all pass. NOT done (surfaced): making `StubURLProtocol` state per-suite — it is used only by the remaining XCTest network suites, which run serially (`parallelizable: false`), so it is not a parallel hazard yet; fully per-suite state belongs with the network-suite migration.

## 5. Verification

- [x] 5.1 Ran `Scripts/lint-swift.sh` with the CI tool versions (SwiftFormat 0.63.0, SwiftLint 0.65.1): 0 formatting issues, 0 lint violations. New violations from this change were fixed; the baseline was re-anchored to updated line numbers and reduced 32→23 entries (no net new debt).
- [x] 5.2 Built and ran the full unit-test suite on the Mac build machine in Swift 6 language mode — green after each phase (baseline, groups 1-3, group 4), no errors or failing tests.
- [ ] 5.3 Produce a release DMG and verify: signed entitlements include Enhanced Security, the app launches, the engine spawns, and no launch-time dynamic-library errors appear
- [ ] 5.4 Verify config/log behavior on a real run: an engine config with a `host:port` server URL loads correctly, and no log line is interleaved during engine churn
