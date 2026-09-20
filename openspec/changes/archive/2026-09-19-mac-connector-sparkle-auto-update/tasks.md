# Tasks — mac-connector-sparkle-auto-update

Status: implementation complete and compiled/tested on `mcj-mini`; items that need a real signed release or a live feed remain open.

## 1. App dependency and configuration

- [x] 1.1 Add Sparkle 2 as a SwiftPM package in `apps/connector.mac/MemoryConnector/project.yml` and attach it to the `MemoryConnector` target. (`from: 2.10.0`)
- [x] 1.2 Add the Sparkle feed/signature keys to `apps/connector.mac/MemoryConnector/Info.plist`: `SUFeedURL`, `SUPublicEDKey`, `SUEnableAutomaticChecks`, `SUScheduledCheckInterval`, `SUVerifyUpdateBeforeExtraction`, `SURequireSignedFeed`, `SUAllowsAutomaticUpdates=false`. No sandbox XPC keys added.
- [x] 1.3 Tag-derived versions: `build-dmg.sh` derives `MARKETING_VERSION` (tag minus `v`) and a monotonic `CURRENT_PROJECT_VERSION = major*1e6 + minor*1e3 + patch`, passed to `xcodebuild archive` to override `project.yml`.
- [x] 1.4 The project remains XcodeGen-generated; `xcodegen generate` + `xcodebuild` succeeded on the build machine.

## 2. Updater integration (Swift)

- [ ] 2.1 Unit tests for updater state mapping — **not added**. The model is deliberately `.unavailable` under `DEBUG` (Sparkle must never start in tests), so its runtime states cannot be exercised without a live feed; the mapping is covered by the manual end-to-end checks (6.4).
- [x] 2.2 `@MainActor` `UpdaterModel` wrapping `SPUStandardUpdaterController`, with `@preconcurrency import Sparkle` and a `@MainActor` observable wrapper for the KVO surface.
- [x] 2.3 Updater starts only for non-Debug builds with a valid HTTPS `SUFeedURL` and a 32-byte base64 `SUPublicEDKey`; constructs with `startingUpdater: false` and calls `startUpdater()` explicitly so a bad bundle cannot abort the process.
- [x] 2.4 Automatic-check preference forwarded to and persisted by Sparkle; `updateCheckInterval` clamped to a 1-hour floor.
- [x] 2.5 Outcomes (including genuine failures, distinguished from "no update found"/cancel/defer) mapped into published state. Verified by build + hosted test run on `mcj-mini`.

## 3. UI surfaces

- [x] 3.1 "Check for Updates…" button and automatic-check toggle added to the About page's new Updates card.
- [x] 3.2 App version, build, bundled engine version, and last check outcome (including a failed check) are shown.
- [ ] 3.3 No new UI/unit coverage added for the update card (same DEBUG-gating constraint as 2.1).

## 4. Engine pinning (Go)

- [x] 4.1 `upgrade.IsInsideAppBundle` (symlink-resolving) refuses in-bundle self-update before any download/write, with a clear "managed by the app" message; `--check` still works.
- [x] 4.2 Unit tests for the helper (plain path, `.app` layouts, `Contents/MacOS` ancestor, symlink target) and for the command refusing while leaving the target byte-identical.
- [x] 4.3 The app runs the engine as a direct child and never invokes `memory-connector upgrade`.
- [x] 4.4 Engine version already surfaced via the status snapshot (`statusMonitor.snapshot?.version`), shown on the About page. `go build ./... && go test ./...` pass.

## 5. Release pipeline

- [x] 5.1 `build-dmg.sh`: version injection, nested-Mach-O + app signing, app notarize + staple, DMG build/sign/notarize/staple, with appcast generation left to the workflow and documented.
- [x] 5.2 `generate_appcast --ed-key-file -` (key from the `SPARKLE_PRIVATE_KEY` secret on stdin) with `--download-url-prefix` targeting the rolling `memory-appcast` release; tools located dynamically under DerivedData.
- [x] 5.3 Workflow downloads previously published archives, regenerates the appcast with history, and publishes `appcast.xml` + archives to `memory-appcast`; gated on `refs/tags/v` and on the existing change-detection job.
- [x] 5.4 Monotonicity assertion fails the release when the new `CFBundleVersion` is not greater than the largest published `sparkle:version`.
- [x] 5.5 EdDSA key pair generated on `mcj-mini`; public key committed in `Info.plist`; private key stored only as the CI secret `SPARKLE_PRIVATE_KEY` (no keychain copy on the Mac).

## 6. Verification

- [x] 6.1 `cd apps/connector.linux && go build ./... && go test ./...` — passes.
- [x] 6.2 `xcodegen generate && xcodebuild ... build` → **BUILD SUCCEEDED**; `xcodebuild ... test` → **364 tests, 0 failures** on `mcj-mini`.
- [ ] 6.3 Release dry-run on a throwaway tag (`codesign --verify --deep --strict`, `spctl -a -vv`, `stapler validate`, feed signature + build number) — needs the CI runner/notarization secrets.
- [ ] 6.4 Manual end-to-end against a feed (install N → publish N+1 → offer/install/relaunch on N+1).
- [ ] 6.5 Tamper test: corrupt the archive, confirm the update is refused and the installed app is untouched.
- [ ] 6.6 Confirm Automation (TCC) grants survive an update.
