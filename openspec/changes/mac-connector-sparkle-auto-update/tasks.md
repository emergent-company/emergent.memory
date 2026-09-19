# Tasks — mac-connector-sparkle-auto-update

## 1. App dependency and configuration

- [ ] 1.1 Add Sparkle 2 as a SwiftPM package in `apps/connector.mac/MemoryConnector/project.yml` and attach it to the `MemoryConnector` target.
- [ ] 1.2 Add the Sparkle feed/signature keys to `apps/connector.mac/MemoryConnector/Info.plist`: `SUFeedURL`, `SUPublicEDKey`, `SUEnableAutomaticChecks`, `SUScheduledCheckInterval`, `SUVerifyUpdateBeforeExtraction`, `SURequireSignedFeed`. Do **not** add sandbox XPC keys (app is unsandboxed).
- [ ] 1.3 Replace the static `MARKETING_VERSION: 0.1.0` / `CURRENT_PROJECT_VERSION: 1` with tag-derived values so the marketing version tracks the release tag and the build number increases monotonically.
- [ ] 1.4 Confirm the `.xcodeproj` remains XcodeGen-generated (`xcodegen generate`) and committed artefacts are refreshed.

## 2. Updater integration (Swift)

- [ ] 2.1 Write failing unit tests for version parsing, monotonic build-number comparison, and updater state mapping (up to date / checking / available / installing / failed).
- [ ] 2.2 Add a `@MainActor` observable update model wrapping `SPUStandardUpdaterController`, bridging Sparkle KVO under Swift 6 (`@preconcurrency import Sparkle` or equivalent).
- [ ] 2.3 Start the updater only for release builds with a configured feed; guard controller start-up so a misconfigured public key cannot abort the process. Debug/dev builds must not check.
- [ ] 2.4 Wire the automatic-check preference to Sparkle's automatic-check setting and persist it across launches; enforce the ≥1 hour automatic-check interval.
- [ ] 2.5 Map update outcomes (including failure) into published state for the UI. Verify: `xcodebuild -scheme MemoryConnector test`.

## 3. UI surfaces

- [ ] 3.1 Add a "Check for Updates…" action and an automatic-check toggle to the About/Settings surface (the app is a menu-bar agent).
- [ ] 3.2 Show current version, bundled engine version, and the latest check outcome (including "check failed").
- [ ] 3.3 Add unit/UI coverage for the version display and update state (deterministic, no network).

## 4. Engine pinning (Go)

- [ ] 4.1 Extend `apps/connector.linux/internal/upgrade` (or the command in `cmd/memory-connector/upgrade.go`) so an upgrade is refused when the running executable resolves inside an `.app` bundle, with a message stating the engine is managed by the app.
- [ ] 4.2 Add unit tests: in-bundle path refuses and leaves the file unmodified; standalone path still upgrades.
- [ ] 4.3 Ensure the app never invokes `memory-connector upgrade`; bundle the engine built for the app release.
- [ ] 4.4 Expose the bundled engine version to the app for display. Verify: `cd apps/connector.linux && go build ./... && go test ./...`.

## 5. Release pipeline

- [ ] 5.1 Update `apps/connector.mac/Scripts/build-dmg.sh`: inject the tag-derived marketing version and build number, then sign nested binaries + app, notarize + staple the app, build the DMG, sign the DMG, and notarize + staple the DMG — with `generate_appcast` run **after** all mutations.
- [ ] 5.2 Integrate `generate_appcast` with `--ed-key-file -` (private key from a CI secret) and `--download-url-prefix` targeting the permanent rolling feed release.
- [ ] 5.3 Update `.github/workflows/mac-release.yml` to download previously published archives, regenerate the appcast with history preserved, publish the DMG + appcast to the rolling feed release, and skip appcast regeneration when the app did not change.
- [ ] 5.4 Add monotonicity assertion: fail the release if the new build number is not greater than the previous appcast entry.
- [ ] 5.5 Create the rolling feed release and document the feed URL; store the EdDSA private key as a CI secret and commit only the public key.

## 6. Verification

- [ ] 6.1 `cd apps/connector.linux && go build ./... && go test ./...`
- [ ] 6.2 `cd apps/connector.mac/MemoryConnector && xcodegen generate && xcodebuild -scheme MemoryConnector build test`
- [ ] 6.3 Release dry-run on a throwaway tag: `codesign --verify --deep --strict`, `spctl -a -vv`, `stapler validate`, and a feed entry whose signature and build number are correct.
- [ ] 6.4 Manual end-to-end against a local feed: install N, publish N+1, confirm the app offers → installs → relaunches on N+1.
- [ ] 6.5 Tamper test: corrupt the archive and confirm the update is refused and the installed app is untouched.
- [ ] 6.6 Confirm Automation (TCC) grants survive an update.
