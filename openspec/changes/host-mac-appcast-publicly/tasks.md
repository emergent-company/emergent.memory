## 1. Public feed hosting

- [x] 1.1 Add `.github/scripts/publish-appcast.sh` to publish `appcast.xml` + `Memory-*.dmg` (+ deltas) to `appcast/<channel>/` on the `gh-pages` branch, with shallow-clone isolation and rebase-retry for concurrent docs deploys
- [x] 1.2 Wire `mac-release.yml` (stable) and `mac-dev.yml` (dev) to call it after `generate_appcast`
- [x] 1.3 Point `generate_appcast --download-url-prefix` at the public Pages directory so the DMG URLs inside the feed resolve publicly
- [x] 1.4 Update `Info.plist`: `SUFeedURL`, `MemoryUpdateFeedURLStable`, `MemoryUpdateFeedURLDev` → the public Pages URLs
- [x] 1.5 Update the `mac-connector-update` delta spec to require a publicly reachable feed host

## 2. Verification

- [ ] 2.1 Confirm the published feed URL is reachable **unauthenticated** (`curl` with no token) and returns the signed appcast
- [ ] 2.2 Confirm the DMG referenced by the feed is reachable unauthenticated
- [ ] 2.3 On the Mac build machine, confirm the app's update check succeeds against the public feed (no "retrieving update information" error)
- [ ] 2.4 Confirm channel switching works against the public stable/dev feeds
