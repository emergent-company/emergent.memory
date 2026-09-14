# iOS bundle id rename — com.mcj.alfred → com.emergent.memory

**Status:** done
**Created:** 2026-09-04
**Source:** [2026-09-04-rename-alfred-to-memory](../sessions/2026-09-04-rename-alfred-to-memory.md)

## Resolution (2026-09-09)

Target chosen: `com.emergent.memory` (main app), `.broadcast` / `.tests`
extensions, app group `group.com.emergent.memory`. Renamed across the xcconfig,
pbxproj, BroadcastExtension entitlements, logger subsystems / trace queue,
`tools/memory-trace.sh`, `tools/ios-debug-mac.sh`, `client/ios/README.md`, and
the `ios-debug` / `memory-trace` skills. Simulator build verified on mcj-mini
(`xcodebuild … CODE_SIGNING_ALLOWED=NO build` → exit 0).

**External follow-up (Apple Developer portal, team `74LC88G9SC`):** create
provisioning profiles for `com.emergent.memory`, `com.emergent.memory.broadcast`,
`com.emergent.memory.tests` before any device build / signed release. Installing
the next build orphans the previous `com.mcj.alfred` app + its keychain entries —
a deliberate, one-time migration.

## What

Rename the iOS bundle identifiers from `com.mcj.alfred` to `com.memory.*` (or a TBD reverse-DNS under the new org) across:
- `client/ios/VoiceAgent/VoiceAgent.xcconfig` (`PRODUCT_BUNDLE_IDENTIFIER = com.mcj.alfred`)
- `client/ios/VoiceAgent.xcodeproj/project.pbxproj` (`com.mcj.alfred.broadcast`, `com.mcj.alfred.tests`)
- `BroadcastExtension.entitlements`

## Why

Deferred from the rename: changing the bundle id orphans the installed app, its keychain entries, and the provisioning profile. Needs a deliberate migration, not a mechanical sweep.

## Depends on

none

## Notes

- Signing team `DEVELOPMENT_TEAM` is `74LC88G9SC` (`VoiceAgent.xcconfig`); a new bundle id needs a matching provisioning profile.
- Consider a new reverse-DNS under `emergent-company` rather than `com.memory` to match the org rename.
- The `memory-trace` / `ios-debug` skills already keep `com.mcj.alfred` as the referenced bundle id — update them in the same change.
