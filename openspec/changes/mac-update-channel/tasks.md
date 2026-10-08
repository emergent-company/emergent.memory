# Verification status

Locally verifiable work is complete and checked below. Tasks that can only be
observed by a real `v*` tag push / a runner publish are left unchecked
deliberately: they run after this change merges, and the `mac-release-uploaded/*`
marker tag only exists once a build has actually shipped.
This change is **not** declared archive-ready.

## 1. Release change-detection gate

- [x] 1.1 Add `.github/scripts/mac-release-should-build.sh` (marker-tag anchor, mirroring the iOS #1612 fix) and wire `mac-release.yml` to it; the build job now writes a `mac-release-uploaded/vX.Y.Z` marker so the next tag diffs against the last *built* commit, never a nearest-tag guess
- [x] 1.2 Wire the new skip-check test into meta CI (`.github/workflows/ci.yml`), alongside the existing iOS skip-check test
- [x] 1.3 Hermetic unit test `mac-release-should-build.test.sh` passes (7/7): bootstrap on the previous app tag, skip when unchanged, re-run skip, force override, build on change, skipped intermediate tag does not consume a change, and the SDK tag series is never the anchor
- [ ] 1.4 Post-merge: confirm on a real `v*` tag whose `apps/connector.mac/**` changed that the build job runs, and on the next unchanged tag that it skips

## 2. Stable appcast pipeline

- [ ] 2.1 Post-merge: force/real release creates the `memory-appcast` release and publishes `appcast.xml` + `Memory-<version>.dmg`
- [ ] 2.2 Post-merge: confirm `SUFeedURL` serves a signed feed (`SPARKLE_PRIVATE_KEY` + `generate_appcast` present)
- [ ] 2.3 Post-merge: a stable install discovers and installs the published update with a prompt, preserving Automation permissions

## 3. Unified build-number encoding

- [x] 3.1 Update `build-dmg.sh` to derive `CFBundleVersion = version_code * 100000 + variant` (`version_code = major*1e6 + minor*1e3 + patch`; `variant = 99999` stable) and accept an explicit `BUILD_NUMBER` for dev; verified with a shell check that stable build numbers strictly increase and that a dev build of `V+1` sorts above stable `V` and below stable `V+1`
- [x] 3.2 The ordering invariant is asserted in `mac-dev.yml`/`build-dmg.sh` comments and validated during review with an explicit numeric check (stable `V` < dev `V+1` < stable `V+1`)

## 4. Development channel workflow

- [x] 4.1 Add `.github/workflows/mac-dev.yml`: push to `main` touching `apps/connector.mac/**`, `concurrency: cancel-in-progress`, self-hosted Mac runner, sign-only (no `--notarize`), marketing `0.0.0-dev.<shorthash>`, build number `(V+1)` variant from the last `v*` tag
- [x] 4.2 Dev workflow generates a signed appcast and publishes a rolling `mac-dev` prerelease (`appcast.xml`, DMG, deltas) with a monotonicity guard against the existing `mac-dev` feed
- [ ] 4.3 Post-merge: confirm the rolling `mac-dev` release + appcast are published and the dev DMG is Developer-ID signed like stable

## 5. In-app channel switch

- [x] 5.1 `UpdateChannel` + persisted `connector.updateChannel` in `ConnectorSettings`; unit tests cover default `.stable`, round-trip via injected `UserDefaults`, no-op on unchanged, and reset via `clearConfiguration()`
- [x] 5.2 `UpdaterModel.feedURLString(for:infoDictionary:)` (https-only, trimmed) + Sparkle `feedURLString(for:)` delegate override backed by an injected channel provider; unit tests cover stable/dev URLs, `SUFeedURL` fallback (stable only), missing/empty/non-https, and whitespace
- [x] 5.3 Add `MemoryUpdateFeedURLStable` / `MemoryUpdateFeedURLDev` to `Info.plist`; `SUFeedURL` kept as the stable fallback
- [x] 5.4 Wire the channel provider in `AppEnvironment` and add the channel picker + caption to the About → Updates card; app compiles and hosted unit tests pass on the Mac build machine (full suite exit 0; `UpdateChannelTests` 8/8)
- [x] 5.5 `SUAllowsAutomaticUpdates` remains false and automatic checks remain enabled; switching channels re-checks the selected feed
- [ ] 5.6 Post-merge: end-to-end two-way switch (stable → dev installs a dev build; after the next `VERSION` bump dev → stable installs the newer stable build)

## 6. Verification and docs

- [x] 6.1 `swiftformat`/`swiftlint` clean on the changed Swift files (`lint-swift.sh`, 0 violations) and `bash -n` passes on `build-dmg.sh`; changed/added workflow YAML parses
- [x] 6.2 App builds and hosted unit tests pass on the Mac build machine (`apps/connector.mac/Scripts/mac-build.sh --test`, exit 0)
- [ ] 6.3 Update the Mac connector release/update documentation to describe the two channels, the build-number scheme, and how a test machine opts into Development and returns to Stable
