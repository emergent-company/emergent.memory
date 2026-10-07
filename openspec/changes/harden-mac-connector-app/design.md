## Context

See `proposal.md` — Why. The mac connector is a Swift 6.0 / macOS 15 SwiftUI + AppKit menu-bar app in `apps/connector.mac/MemoryConnector`, built with XcodeGen (`project.yml`) and released as a signed DMG (`Scripts/build-dmg.sh`). It embeds a Go engine binary and a `swiftc`-compiled Reminders helper via pre-build scripts, and links Sparkle 2.x via SPM. Constraints that shape this change:

- The Release artifact must keep working with the embedded binaries and Sparkle; security hardening that breaks dynamic library loading is not acceptable.
- The engine config file is consumed by the Go engine, so any format change must remain compatible with how the engine reads it.
- The app is distributed to end users via Sparkle, so a bad release cannot be recalled cheaply — hardening must land behind build + launch verification.

## Goals / Non-Goals

**Goals:**

- The release artifact is compiled and checked in the same Swift 6 language mode as Debug.
- The signed artifact carries hardened-runtime Enhanced Security protections.
- Engine config writes and log writes are corruption- and interleaving-free without unsynchronized shared mutable state.

**Non-Goals:**

- Full App Sandbox adoption (the app needs TCC Automation, Keychain, and `~/.config` writes; evaluated separately).
- Enabling pointer authentication / memory tagging before dependency and hardware support are confirmed.
- Any change to connector CLI, engine behavior, or the server API.
- The internal `@Observable`/DI/view-factoring/test modernization is planned here but carries no new spec requirement (no observable behavior change).

## Decisions

### D1 — Remove the Release Swift 5 downgrade instead of freezing language mode

`Scripts/build-dmg.sh:116` sets `SWIFT_VERSION=5` in the archive step. Drop the override so the release uses the project's `SWIFT_VERSION: "6.0"`. Alternative considered: keep Swift 5 mode for releases and only check concurrency in Debug — rejected, because the shipped binary is exactly where race bugs matter, and the downgrade hides the debt the app already carries.

### D2 — Enhanced Security entitlements now, sandbox later

Add the `com.apple.security.hardened-process` family to `MemoryConnector.entitlements` and `ENABLE_ENHANCED_SECURITY = YES`. This is low-risk and reversible. Full App Sandbox is explicitly deferred: the app writes `~/.config/memory-connector.yml`, drives AppleScript/TCC Automation, and manages Keychain-backed sessions, so sandboxing is a materially larger and riskier change. Alternative considered: sandbox now — rejected on risk.

### D3 — A single serialized log sink

Introduce one `LogSink` actor that owns the log file/terminal handle and writes whole lines. Engine stdout, engine stderr, and app lifecycle lines are routed through it, replacing `nonisolated(unsafe) var logHandle`, the `nonisolated(unsafe) var stderrBuffer`, and the concurrent `FileHandle` writes in `EngineManager.swift`/`ConnectorLog.swift`. Alternative considered: a `DispatchQueue`-backed writer — rejected in favor of an actor for idiomatic Swift 6 isolation and testability.

### D4 — Structural encoding for the engine config

`EngineConfigWriter` currently builds YAML by string concatenation with no escaping. Move to a structural emit that quotes/escapes scalar values (or a small safer representation the engine already tolerates), plus a round-trip parse. The reader must stay backward-compatible with existing files. Alternative considered: swap to JSON — viable only if the engine already accepts it; verify before choosing, otherwise keep the format and fix the encoder.

### D5 — Defer pointer authentication / memory tagging

Enable `ENABLE_POINTER_AUTHENTICATION` only after confirming Sparkle and the embedded engine/helper ship arm64e slices. `CLANG_ENABLE_STACK_ZERO_INIT` is zero-risk and enabled now. Checked-pointer-arithmetic and MTE stay off. This is captured as spec requirements with a gating scenario.

### D6 — Internal modernization sequencing (not spec-bearing)

Order to avoid a half-migrated graph: (1) log sink, (2) `@Observable` leaf stores, (3) remaining stores, (4) composition root replacing singleton reach-through, (5) view-section extraction, (6) god-store splits, (7) Swift Testing migration. Each step compiles and keeps tests green; no step leaves `.environmentObject` and `.environment` mixed across the same graph.

## Risks / Trade-offs

- [Enhanced Security breaks embedded binary or Sparkle loading] → Verify a signed launch spawns the engine and helper before merging; keep the entitlement change isolated to project config so it is a one-line revert.
- [Swift 6 mode surfaces many concurrency errors] → Land the `LogSink` actor and remove `nonisolated(unsafe)` state first, then flip release mode; fix remaining warnings incrementally.
- [Config format change breaks the engine's read] → Keep the on-disk format and fix encoding only; add round-trip unit tests, including legacy-file load.
- [Half-migrated `@Observable`/DI graph] → Do the modernization as one coordinated sequence (D6); never mix observation APIs for the same graph.
- [Menu-bar/entitlement behavior cannot be verified from a cloud agent] → Verification tasks call the Mac build machine (`tools/mac-build.sh` / `task mac:...`) and inspect signed entitlements; a signed launch smoke is required before release.

## Migration Plan

1. Entitlements + Enhanced Security build settings (revertible in isolation).
2. Remove Release `SWIFT_VERSION=5`; fix surfaced concurrency findings.
3. `LogSink` actor + remove unsafe shared log state.
4. Engine config structural encoding (backward-compatible read).
5. Internal modernization sequence (D6).
6. Release: build DMG, verify signed entitlements, launch smoke, engine spawn.

Rollback: each step is an independent commit; the security settings and config encoder are the only two with runtime blast radius and both revert cleanly.

## Open Questions

- Does the pinned Sparkle 2.x release ship arm64e slices, and do the embedded engine/helper? This blocks only D5 (pointer authentication) and does not change the specs, approach, or task breakdown.
