## Context

The Mac connector app is a macOS 15+, Swift 6, XcodeGen-generated SwiftUI menu-bar app. It is **not sandboxed** (`MemoryConnector.entitlements` is an empty dict) and is built with `ENABLE_HARDENED_RUNTIME=YES`. Release builds are archived and exported with `method=developer-id` (`Scripts/ExportOptions.plist`), the embedded Go engine and reminders helper are re-signed with `Developer ID Application`, and the app is notarized and stapled by `Scripts/build-dmg.sh`. `.github/workflows/mac-release.yml` runs on `v*` tags on a self-hosted macOS runner and uploads `Memory-<v>.dmg` to the tag's release.

Two facts drive the design:

1. There is no updater today, and the app version does not track the tag (`project.yml` pins `MARKETING_VERSION: 0.1.0`, `CURRENT_PROJECT_VERSION: 1`; `Info.plist` reads them via `$(...)`).
2. The embedded `memory-connector` engine has its own GitHub-releases self-updater (`apps/connector.linux/internal/upgrade/`) that atomically replaces `os.Executable()`. Inside the app bundle that path is `Contents/Resources/memory-connector`, so an engine self-upgrade mutates a signed nested binary and invalidates the app's signature and notarization ticket.

## Goals / Non-Goals

**Goals:** authenticated automatic updates; immutable shipped bundle; release-artifact correctness (signature, notarization, version ordering); no regression to existing TCC Automation grants.

**Non-Goals:** sandboxing the app; moving the app to the Mac App Store; changing the engine's standalone distribution channel; adding a Homebrew cask (none exists for this app today).

## Decisions

### D1 — Use Sparkle 2 rather than bespoke update code

Add `https://github.com/sparkle-project/Sparkle` as a SwiftPM package (2.x, currently 2.10.0) and drive updates with `SPUStandardUpdaterController`. Rationale: Sparkle verifies an EdDSA signature over the update archive **and** validates the new bundle against the installed bundle's Apple designated requirement, handles atomic replacement, quarantine removal, and relaunch — all of which bespoke scripting gets wrong. Alternatives: a custom GitHub-API + DMG swap (status quo ante in sibling projects — no archive authentication, brittle detached-script install, breaks notarization assumptions); Squirrel.Mac (ZIP/JSON/Electron-oriented, no appcast or public-key model); Homebrew-only (misses direct-DMG users). The app is unsandboxed with hardened runtime, which Sparkle supports directly.

- **Do not** set Sparkle's sandbox-only XPC keys or entitlements (`SUEnableInstallerLauncherService`, mach-lookup temporary exceptions). They are for sandboxed apps and can break signing/install for an unsandboxed app.
- Guard controller start-up (a misconfigured public key makes the framework `abort()` the process) and skip it entirely for Debug/dev builds without a feed.

### D2 — Host the feed at a rolling release tag, not `releases/latest`

`SUFeedURL` points at a permanent URL backed by a dedicated rolling release tagged **`memory-appcast`** in this public repository: `https://github.com/emergent-company/emergent.memory/releases/download/memory-appcast/appcast.xml`. Rationale: this monorepo tags `v*` frequently for the server/CLI, and `mac-release.yml` *skips* the Mac build when `apps/connector.mac/**` is unchanged — so a per-tag feed would frequently not exist, and `releases/latest` is mutable (older feed entries' enclosure URLs would 404 after the next release). The rolling release holds `appcast.xml` plus all historical `Memory-*.dmg` / delta archives so any older install can update directly.

Alternatives considered: GitHub Pages for the feed (fine, but adds a publishing surface and repo-size concerns); per-tag feed + `latest` alias (rejected: mutable).

### D3 — Appcast generation in CI

Run Sparkle's `generate_appcast` over a `build/appcast/` directory containing the newly built DMG plus previously published archives, with `--download-url-prefix` targeting the rolling release and `--ed-key-file -` reading the private key from stdin. The tool lives in the SwiftPM artifacts directory (`<DerivedData>/**/artifacts/sparkle/Sparkle/bin/`), which is why the workflow locates it dynamically rather than hardcoding a DerivedData hash. Regenerating with history preserved is what makes stable enclosure URLs and delta updates work.

### D4 — EdDSA key management

Generate the key pair once on a trusted Mac (`generate_keys`). Commit only the **public** key into `Info.plist` as `SUPublicEDKey`; store the **private** key as a CI secret and pass it to `generate_appcast` on stdin.

Resolved: the pair was generated on `mcj-mini` with the version-matched Sparkle 2.10.0 tools, through a throwaway keychain so the user's login keychain was untouched. Public key `PmNdMg8V1L//noPSUCvjjmBy14WQfnlyeVPAFySIE2A=`; the private key lives only at `mcj-mini:~/.config/sparkle/ed25519-private.key` (mode 600) and in the CI secret `SPARKLE_PRIVATE_KEY`. Because generation used a throwaway keychain there is **no keychain copy on the Mac** — local `sign_update`/`generate_appcast` runs need `-f`/`--ed-key-file` until someone imports it (`generate_keys -f`) from an unlocked login session. With `SUVerifyUpdateBeforeExtraction` enabled, an EdDSA key rotation is only accepted when the update archive is a Developer-ID-signed DMG, so rotation is possible but deliberate. Key loss means users must reinstall manually.

### D5 — Signing and notarization ordering

Any byte change after signing invalidates the signature and notarization ticket, so the order is: finalize bundle contents → sign nested Mach-Os then the app (already done in `build-dmg.sh`) → notarize + `stapler staple` the app → build the DMG → code-sign the DMG → notarize + staple the DMG → **then** `generate_appcast`. Sparkle compares the app's designated requirement before/after, so the Developer ID identity must be identical across releases. (Local Debug builds use a stable Apple Development identity to preserve TCC grants; the update path only ever runs on release builds.)

### D6 — Tag-driven, monotonic versioning

`CFBundleShortVersionString` must reflect the released tag, and `CFBundleVersion` must strictly increase on every published app release because Sparkle orders updates by the build number (the appcast `sparkle:version`), not the marketing string. Derive both in the release job and pass them to `xcodebuild archive` as build settings (they override the static `0.1.0` / `1` in `project.yml`). Resolved encoding: marketing version = the tag minus `v`; build number = `major*1000000 + minor*1000 + patch` (e.g. `v0.2.0` → `2000`), and the release job fails if the new `CFBundleVersion` is not strictly greater than the largest `sparkle:version` already published. Optionally failsafe: refuse to publish an appcast whose build number is not greater than the previous entry.

### D7 — Pin the embedded engine

The app never invokes the engine's upgrade path; the bundled binary is a build artifact of the app release. Independently, the engine must refuse to self-update when `os.Executable()` resolves inside an `.app` bundle, reporting that it is managed by the app. Standalone engine installs keep the existing behaviour. The app reports the engine version alongside its own so drift is visible.

### D8 — UI surface

The app is a menu-bar agent, so the manual "Check for Updates…" action and the automatic-check preference live in the About/Settings surface rather than a standard application menu, alongside the app and engine versions.

### D9 — Homebrew cask (deferred)

No cask exists for this app in `emergent-company/homebrew-tap` today. If one is added, it must declare `auto_updates true` and a `:sparkle` livecheck against the feed; `brew upgrade --cask` skips `auto_updates` casks unless `--greedy-auto-updates`, so the feed remains the source of truth.

## Risks / Trade-offs

- **Swift 6 strict concurrency vs Sparkle**: the framework's `SPUUpdater` is not `Sendable` and its KVO publisher is not `@MainActor`-annotated; the official SwiftUI sample predates Swift 6. Mitigation: bridge through a `@MainActor` observable model and/or `@preconcurrency import Sparkle`; verify on the CI toolchain before wiring UI.
- **Monorepo tagging**: shared `v*` tags mean the app release is a subset of a repo-wide release. The rolling feed decouples app updates from server tags, but the release job must not regenerate an appcast when the app did not change.
- **Version regressions**: a non-monotonic or incorrectly injected build number silently disables updates (Sparkle sees the installed build as newest). Mitigation: derive from the tag in one place and assert monotonicity in the pipeline.
- **Key management**: losing the EdDSA private key requires a signed-DMG rotation path; leaking it is equivalent to update-signing compromise.
- **Notarization/stapling order**: stapling after `generate_appcast` would invalidate the feed signature; the pipeline encodes the order explicitly.
- **TCC regressions**: replacing the bundle must keep the same bundle id and signing identity; a changed certificate would require users to re-grant Automation permissions.

## Migration Plan

There are no installed self-updating users to migrate (the app has never had an updater), so the first Sparkle-enabled release is installed manually like any previous release. From then on, updates flow through the feed. Rollback is "install the previous DMG manually"; Sparkle's own install is atomic, so a failed update leaves the current app intact.

## Verification

- **Unit (Swift)**: version parsing / monotonic build-number derivation; updater state mapping (up to date / available / failed); Debug builds do not start the updater.
- **Unit (Go)**: `apps/connector.linux` engine upgrade refuses to run when the executable is inside an `.app` bundle; standalone path still upgrades (existing `upgrade_test.go` extended).
- **Build/lint**: `cd apps/connector.linux && go build ./... && go test ./...`; `xcodegen generate` + `xcodebuild` for the Mac app on the macOS runner; repo linters.
- **Release dry-run**: run the release workflow on a non-published tag; assert the DMG is signed and notarized, `spctl -a -vv` / `stapler validate` pass, the appcast contains a matching signature and a strictly greater build number, and the feed + archive URLs resolve.
- **Manual end-to-end**: with a local HTTP feed, install build N, publish N+1, confirm the app offers, installs, and relaunches on N+1; tamper with the archive and confirm the update is refused.
