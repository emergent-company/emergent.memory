## Purpose

Guarantees the signed macOS connector release artifact is built with modern concurrency checking and hardened-runtime protections, so the binary distributed to users is the one that was actually checked and hardened.

## ADDED Requirements

### Requirement: Release archive compiles in Swift 6 language mode

The Release build of the mac connector SHALL compile in Swift 6 language mode, matching the project's declared `SWIFT_VERSION`, so the shipped artifact is subject to strict concurrency checking. No build step SHALL downgrade the language mode for the release archive.

#### Scenario: Release archive uses Swift 6 mode

- **WHEN** the release archive is produced
- **THEN** the effective `SWIFT_VERSION` is `6.0` and no build script overrides it to a lower language mode

#### Scenario: Debug and release agree on language mode

- **WHEN** the Debug and Release configurations are compared
- **THEN** both compile in the same Swift language mode

### Requirement: Hardened runtime uses Enhanced Security protections

The signed app SHALL ship with hardened-runtime Enhanced Security enabled — the `com.apple.security.hardened-process` entitlement with enhanced-security version 2, hardened heap, dyld read-only, and platform restrictions — in addition to `ENABLE_HARDENED_RUNTIME`.

#### Scenario: Enhanced Security entitlements present on the signed app

- **WHEN** the signed app's entitlements are inspected
- **THEN** they enable the hardened-process family with enhanced-security version 2, hardened heap, dyld read-only, and platform restrictions

#### Scenario: Protected app still starts and supervises the engine

- **WHEN** the signed app with Enhanced Security entitlements launches with a configured project
- **THEN** it starts and supervises the embedded engine and helper as before

### Requirement: Stack variables are zero-initialized

The app build SHALL enable stack zero-initialization so uninitialized stack values cannot be used.

#### Scenario: Stack zero initialization enabled

- **WHEN** the app target's build settings are inspected
- **THEN** `CLANG_ENABLE_STACK_ZERO_INIT` is enabled

### Requirement: Pointer authentication and memory tagging are gated on dependency compatibility

Pointer authentication SHALL be enabled only after every linked binary — including Sparkle and the embedded engine and helper — is confirmed to ship an arm64e-compatible slice. Checked-pointer-arithmetic and hardware memory tagging SHALL remain disabled until the required hardware and dependency support are confirmed.

#### Scenario: Pointer authentication not enabled before the dependency check

- **WHEN** pointer authentication is enabled
- **THEN** Sparkle and the embedded binaries have first been confirmed to provide arm64e slices, or the setting remains off

#### Scenario: Deferred features stay off

- **WHEN** the build settings are inspected
- **THEN** checked-pointer-arithmetic and memory-tagging features are disabled
