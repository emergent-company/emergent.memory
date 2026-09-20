## Why

The Mac connector app (`apps/connector.mac`, product **Memory**) is published as a signed, notarized `Memory-<v>.dmg` from tag releases, but it has **no update path at all** — a user who installed it must notice a new version exists and manually re-download the DMG. Separately, the embedded connector engine already carries its own self-updater (`memory-connector upgrade`), which resolves the running executable and replaces it in place; inside the app bundle that path is `Contents/Resources/memory-connector`, so an engine self-update rewrites a nested Mach-O after signing and invalidates the app's signature and notarization ticket.

We want updates to be automatic, authenticated, and safe to install — and the app bundle to be immutable once shipped.

## What Changes

- Integrate **Sparkle 2** in the Mac app: appcast-based update feed, EdDSA archive signatures, Apple code-signature continuity check, atomic install and relaunch. The app stops having zero update path; the update mechanism is a maintained dependency rather than bespoke code.
- Add **appcast generation and publication** to the Mac release workflow: sign the DMG with the Sparkle EdDSA key, generate `appcast.xml` (with deltas where useful), and publish feed + archives to a **stable, public feed URL** that does not depend on a mutable `releases/latest`.
- Make the app's **version source tag-driven and monotonic**: `CFBundleVersion` (the build number Sparkle orders updates by) must increase on every published app release, and `CFBundleShortVersionString` must reflect the released tag. Today both are static (`0.1.0` / `1`) and do not track the tag.
- **Pin the embedded engine**: the bundled `memory-connector` is a read-only snapshot that matches the app release; it must not self-update while running from inside the app bundle. The app owns the engine's version.
- Add **in-app update surfaces** for a menu-bar app: a manual "Check for Updates…" action and an automatic-check preference surfaced in the About/Settings area.
- Feed/archive hosting and the EdDSA key are release-pipeline concerns: private key in CI secrets only, public key in the app bundle.

## Capabilities

### New Capabilities

- `mac-connector-update`: how the Mac connector app discovers, verifies, installs, and reports application updates, and the release artifacts that make those updates possible (signed appcast and archive, monotonic build version, stable feed URL).

### Modified Capabilities

- `mac-connector-app`: adds a requirement that the embedded connector engine is a pinned, non-self-updating snapshot owned by the app version, so an in-bundle engine upgrade cannot break the signed app.

## Impact

- **App**: `apps/connector.mac/MemoryConnector/project.yml` (Sparkle package, version settings), `Info.plist` (Sparkle feed/signature keys), new updater model + UI wiring in `Sources/`, `MemoryConnector.entitlements` unchanged (not sandboxed).
- **Release**: `apps/connector.mac/Scripts/build-dmg.sh` (sign/notarize/staple ordering, appcast), `.github/workflows/mac-release.yml` (EdDSA secret, `generate_appcast`, publish feed + archives), a stable feed release/tag.
- **Engine**: `apps/connector.linux/cmd/memory-connector/upgrade.go` and `internal/upgrade/` — refuse to self-update when the running executable lives inside an `.app` bundle.
- **Dependency**: adds `https://github.com/sparkle-project/Sparkle` (2.x) to the Mac app; new CI secret for the EdDSA private key.
- **Docs/spec**: `openspec/specs/mac-connector-app` and the new `mac-connector-update` capability.
