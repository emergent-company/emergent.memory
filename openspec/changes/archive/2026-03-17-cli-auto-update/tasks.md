## 1. Config & Types

- [x] 1.1 Add `AutoUpdateConfig` struct to `internal/config/config.go` with fields: `Enabled` (bool, default true), `Mode` (string, default "notify"), `CheckInterval` (string, default "24h")
- [x] 1.2 Wire `auto_update.*` keys into Viper defaults in `internal/cmd/root.go` (`initConfig`) and bind `MEMORY_AUTO_UPDATE_ENABLED`, `MEMORY_AUTO_UPDATE_MODE`, `MEMORY_AUTO_UPDATE_CHECK_INTERVAL` env vars
- [x] 1.3 Register `MEMORY_NO_AUTO_UPDATE` env var check as the master kill-switch (checked before any config loading)

## 2. Version Cache

- [x] 2.1 Create `internal/autoupdate/cache.go` with `VersionCache` struct (`Version string`, `CheckedAt time.Time`, `ReleaseBody string`, `ReleaseURL string`) and JSON marshal/unmarshal helpers
- [x] 2.2 Implement `LoadCache(path string) (*VersionCache, error)` — reads `~/.memory/cache/latest-version.json`, returns nil on missing/corrupt file
- [x] 2.3 Implement `SaveCache(path string, cache *VersionCache) error` — writes atomically (write to tmp, rename)
- [x] 2.4 Implement `IsFresh(cache *VersionCache, interval time.Duration) bool` — returns true if `CheckedAt` is within interval

## 3. Version Checker

- [x] 3.1 Create `internal/autoupdate/checker.go` with `CheckResult` struct (fields: `Available bool`, `LatestVersion string`, `CurrentVersion string`, `ReleaseBody string`, `ReleaseURL string`, `Error error`)
- [x] 3.2 Extract `getLatestRelease()` from `internal/cmd/upgrade.go` into a shared exported function that also captures the release `Body` field (markdown release notes) and `HTMLURL`
- [x] 3.3 Implement `CheckForUpdate(currentVersion string, cacheDir string, checkInterval time.Duration) *CheckResult` — loads cache, skips if fresh, otherwise fetches latest release and updates cache
- [x] 3.4 Add unit tests for `CheckForUpdate` with mocked HTTP responses (fresh cache, stale cache, no cache, network error)

## 4. Auto-Downloader

- [x] 4.1 Create `internal/autoupdate/download.go` with `DownloadAndInstall(release *Release) error` — reuses `findAsset()` and `installUpdate()` from upgrade.go
- [x] 4.2 Ensure `installUpdate` in `upgrade.go` is exported or accessible to the autoupdate package (extract to shared helper if needed)

## 5. Root Command Integration

- [x] 5.1 Add a `PersistentPreRunE` wrapper in `root.go` that spawns the background check goroutine (writes `CheckResult` to a channel), gated by skip conditions: `Version == "dev"`, package-manager install, `MEMORY_NO_AUTO_UPDATE`, config disabled, excluded commands (upgrade, version, completion)
- [x] 5.2 Add `PersistentPostRunE` on root command that reads the check-result channel with a 100ms timeout, then prints the notification to stderr with changelog teaser (using changelog formatter from `cli-upgrade-changelog` if available, fallback to plain version message)
- [x] 5.3 In `PersistentPostRunE`, if mode is `"auto"` and a newer version was found, call `DownloadAndInstall` and print the auto-update result message to stderr
- [x] 5.4 Build the skip-command list: detect if current command (or parent) is `upgrade`, `version`, or `completion` by walking `cmd.Name()` / `cmd.Parent()`

## 6. Config Command Support

- [x] 6.1 Add `auto_update_enabled`, `auto_update_mode`, and `auto_update_check_interval` to `configYAMLKeys` and the `config set` switch statement in `internal/cmd/config.go`
- [x] 6.2 Display auto-update config in `config show` output table

## 7. Testing

- [x] 7.1 Add unit tests for cache load/save/freshness logic
- [x] 7.2 Add unit tests for skip-condition logic (dev build, env var, config, excluded commands)
- [x] 7.3 Add integration test: run CLI command and verify no crash / no blocking when offline (mock HTTP or use unreachable host with short timeout)
