## Why

The Mac connector app has a Sparkle 2 auto-update pipeline, but it has never delivered
a single update in practice:

- The release workflows (`mac-release.yml`, `ios-release.yml`) skip their build job on
  **every** tag push. The change-detection step resolves the comparison base with
  `git describe --tags --abbrev=0 HEAD^`, which matches *any* tag — including the many
  per-component tags under `apps/server/pkg/sdk/` — so the `apps/connector.mac/` diff
  comes back empty even when the app changed (v0.82.1: 26 files, v0.83.0: 97, v0.85.0: 10,
  v0.86.0: 14 — all skipped).
- Consequently the rolling Sparkle feed release `memory-appcast` was never created and the
  embedded `SUFeedURL` returns 404. No `.dmg` ships in any recent `vX.Y.Z` release.
- The app can only ever follow that one feed. There is no development channel, so an
  unreleased `main` build cannot be deployed to test machines, and there is no way to
  switch channels without reinstalling.
- Two-way channel switching is impossible under the current `CFBundleVersion` scheme
  (`major*1e6 + minor*1e3 + patch`): any dev build number that outranks the current stable
  also outranks the next stable.

The goal is that a test Mac can be pointed at a channel once and then automatically update
to the newest build on that channel — with an install prompt, not a silent install.

## What Changes

- **Fix the change-detection gate.** Resolve the previous release with
  `git describe --tags --abbrev=0 --match 'v[0-9]*' HEAD^` (version tags only) in both
  `mac-release.yml` and `ios-release.yml`, and echo the resolved base/decision so the gate
  is diagnosable.
- **Repair and verify the stable appcast.** Ensure a forced/normal release actually builds
  the DMG, publishes it to the `vX.Y.Z` release, and regenerates + uploads the signed
  `memory-appcast` `appcast.xml`.
- **Add a rolling development channel for the Mac app** (`mac-dev.yml`), mirroring
  `connector-dev.yml`: on push to `main` touching `apps/connector.mac/**`, build on the
  self-hosted Mac runner, **sign with the same Developer ID but skip notarization**, and
  publish a rolling `mac-dev` prerelease with its own signed appcast.
- **Add an in-app update-channel switch** (Stable ⇄ Development), persisted in
  `ConnectorSettings`, applied at runtime through Sparkle's
  `feedURLStringForUpdater:` delegate override, surfaced in the About → Updates card.
- **Introduce a unified monotonic build-number encoding** so switching works both ways:
  `CFBundleVersion = version_code * 100000 + variant`, with `variant = 99999` for stable
  and `1..99998` for dev builds of the next version. A dev build then sorts above the
  current stable and below the next stable, so `stable → dev` and `dev → stable (next
  release)` both install.
- **Keep prompt-before-install.** `SUAllowsAutomaticUpdates` stays false; automatic
  *checks* remain enabled.

## Capabilities

### Modified Capabilities

- `mac-connector-update`: the app now selects between two signed feeds (stable and
  development) and persists the choice; update ordering uses a cross-channel monotonic
  build number; a development channel publishes a rolling signed feed; release builds run
  whenever the app changed.

## Impact

- **CI:** `.github/workflows/mac-release.yml`, `.github/workflows/ios-release.yml`,
  new `.github/workflows/mac-dev.yml`.
- **Build script:** `apps/connector.mac/Scripts/build-dmg.sh` (channel-aware feed URL,
  unified build-number scheme, optional `BUILD_NUMBER`, sign-only dev).
- **App:** `apps/connector.mac/MemoryConnector/Info.plist`,
  `Sources/ConnectorSettings.swift`, `Sources/UpdaterModel.swift`,
  `Sources/AboutPage.swift`, `Sources/AppEnvironment.swift`, plus unit tests.
- **No server/API changes.** Uses the existing GitHub releases + Sparkle appcast mechanism.
- **Out of scope:** silent/fully-automatic installs, a Mac App Store distribution path,
  and changing the iOS TestFlight distribution.
