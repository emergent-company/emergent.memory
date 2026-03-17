## Context

The Memory CLI (`tools/cli/`) is a Cobra-based Go CLI distributed as a single binary. It already has:

- **Version tracking**: Build-time ldflags set `Version`, `Commit`, `BuildDate` in `internal/cmd/version.go`
- **Manual upgrade**: `memory upgrade` in `internal/cmd/upgrade.go` — fetches the latest GitHub release, downloads the platform-specific archive, and atomically replaces the binary via rename
- **Config system**: Viper-based YAML config at `~/.memory/config.yaml` with env-var overrides (`MEMORY_` prefix). Config struct in `internal/config/config.go`
- **Existing cache directory**: `internal/cache/` package already used for completion caching
- **Package-manager detection**: `upgrade.go` already detects pacman installs and skips self-upgrade
- **GitHub release API**: `getLatestRelease()` in `upgrade.go` already fetches `emergent-company/emergent.memory/releases/latest`

The auto-update feature hooks into this existing infrastructure.

## Goals / Non-Goals

**Goals:**
- Check for new CLI versions automatically on every invocation (non-blocking)
- Display an update notice with changelog teaser when a newer version is available
- Optionally auto-download and install the update in the background for the next invocation
- Allow users to disable auto-update via env var or config
- Rate-limit GitHub API calls with a local cache (default: check at most once per 24h)
- Respect dev builds and package-manager installs (skip auto-update entirely)

**Non-Goals:**
- Hot-swapping the running binary mid-command (update applies on next run)
- Auto-updating the server installation (that remains `memory server upgrade`)
- Adding a daemon/service for background update polling
- Supporting update channels (stable/beta) — only "latest" GitHub release

## Decisions

### 1. Hook point: `PersistentPostRunE` on root command

**Decision**: Run the update check in `PersistentPostRunE` (after every command completes), not `PersistentPreRunE`.

**Rationale**: Printing a notice *after* command output avoids confusing users who pipe output. The check runs in a goroutine started in `PersistentPreRunE` (so it overlaps with command execution), but results are only read and printed in `PersistentPostRunE`.

**Alternative considered**: Pre-run check — rejected because it would delay command startup or mix update messages with command output.

### 2. Non-blocking goroutine for version check

**Decision**: Spawn a goroutine at pre-run time that performs the HTTP check and writes to a channel. Post-run reads the channel with a short timeout (100ms). If the check hasn't completed, silently skip.

**Rationale**: CLI commands must never be slowed down by network calls. Users on slow connections or offline should see zero latency impact.

### 3. Rate-limiting via cache file

**Decision**: Store last-check result in `~/.memory/cache/latest-version.json` with a timestamp. Skip the network call if the file is fresh (within `auto_update.check_interval`, default `24h`).

**Rationale**: GitHub API rate limits (60 req/hr unauthenticated). Daily check is sufficient for a CLI tool.

**File format**:
```json
{
  "version": "0.8.8",
  "checked_at": "2026-03-17T12:00:00Z",
  "release_body": "## What's Changed\n- Fix: agent timeout...",
  "release_url": "https://github.com/.../releases/tag/v0.8.8"
}
```

### 7. Changelog integration

**Decision**: The version cache stores the GitHub release `body` (markdown) alongside the version. The auto-update notification includes a changelog teaser (summary counts extracted from the release body). The full changelog is displayed after `memory upgrade` completes, and available on-demand via `memory changelog`.

**Rationale**: "There's an update" + "here's why you should care" together drive adoption. The release body is already available from the same GitHub API call — zero extra cost.

**Boundary**: The auto-update package provides the cached release data. The changelog formatting/display is owned by `cli-upgrade-changelog` — the auto-update notification calls a formatter function to build the teaser line.

### 4. Opt-out via env var and config

**Decision**: 
- `MEMORY_NO_AUTO_UPDATE=1` — env var, disables both check and auto-download
- Config key `auto_update.enabled` (default `true`) — same effect via config file
- Config key `auto_update.mode` — `"notify"` (default, print notice only) or `"auto"` (download in background)

**Rationale**: Environment variable provides quick per-session/per-environment opt-out (CI, scripts). Config file provides persistent preference.

**Precedence**: env var wins over config file (consistent with existing MEMORY_ env var behavior).

### 5. Reuse existing upgrade machinery

**Decision**: Extract `getLatestRelease()`, `findAsset()`, and `installUpdate()` from `upgrade.go` into a shared helper or call them directly from the auto-update package.

**Rationale**: No code duplication. The download/replace logic is already battle-tested.

### 6. Skip conditions

**Decision**: Auto-update is completely skipped when any of these are true:
- `Version == "dev"` (development build)
- Package manager install detected (`isPackageManagerInstalled()`)
- `MEMORY_NO_AUTO_UPDATE=1`
- `auto_update.enabled == false` in config
- Command is `upgrade`, `version`, `completion`, or any hidden/help command

**Rationale**: Dev builds change constantly; package managers have their own update path; upgrade command already handles updates explicitly.

## Risks / Trade-offs

- **[Risk] GitHub API rate limiting** → Mitigated by 24h cache interval. Even shared IPs in CI environments will only hit once per interval per user.
- **[Risk] Background download could corrupt binary if machine loses power** → Mitigated by existing atomic rename strategy in `installUpdate()` (write to `.new`, rename old to `.old`, rename new to current).
- **[Risk] Auto-update in CI pipelines** → `MEMORY_NO_AUTO_UPDATE=1` env var provides clean opt-out. Dev builds also skip automatically.
- **[Risk] Notification noise for users** → Notice is a single line, printed only once per check interval, only when a newer version actually exists.
- **[Trade-off] Post-run notification may be missed in piped output** → Notification goes to stderr, not stdout, so it doesn't corrupt piped data. Users who redirect stderr will miss it, which is acceptable.
