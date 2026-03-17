## Why

Users running the Memory CLI may be on outdated versions without realizing it, missing bug fixes, new features, and security patches. Currently, upgrading requires manually running `memory upgrade`. An automatic update check on each invocation — with a non-intrusive notification and optional background self-update — keeps users current with zero friction while respecting those who prefer manual control.

## What Changes

- Add a background version check that runs on every CLI invocation (non-blocking, does not slow down commands)
- When a newer version is detected, print a short notice after command output with a changelog teaser (e.g., "New version available: 0.9.1 (you have 0.9.0) — 3 new features, 2 fixes. Run `memory upgrade` to update.")
- Optionally auto-download and replace the binary in the background so the *next* invocation uses the new version
- Add an opt-out mechanism via environment variable (`MEMORY_NO_AUTO_UPDATE=1`) and config key (`auto_update.enabled: false`)
- Rate-limit the check using a local cache file so the GitHub API is hit at most once per configurable interval (default: 24 hours)
- Skip auto-update for dev builds (`Version == "dev"`) and package-manager installs

## Capabilities

### New Capabilities
- `cli-auto-update`: Automatic version checking, user notification, background self-update, and opt-out configuration for the Memory CLI

### Modified Capabilities
<!-- No existing spec-level requirements are changing — the existing `memory upgrade` command stays as-is. -->

## Impact

- **Code**: New package `tools/cli/internal/autoupdate/` plus integration hook in `root.go` `PersistentPostRunE`
- **Config**: New `auto_update` section in `~/.memory/config.yaml`; new `MEMORY_NO_AUTO_UPDATE` / `MEMORY_AUTO_UPDATE_ENABLED` env vars
- **Filesystem**: New cache file `~/.memory/cache/latest-version.json` for rate-limiting checks
- **Network**: One HTTPS call to GitHub Releases API per check interval (non-blocking goroutine)
- **Dependencies**: No new external dependencies — reuses existing `net/http`, `encoding/json`, and the `upgrade.go` helpers
