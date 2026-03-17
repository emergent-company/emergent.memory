## ADDED Requirements

### Requirement: Version check on every CLI invocation
The CLI SHALL perform an automatic version check against the latest GitHub release on every command invocation. The check SHALL run in a background goroutine and SHALL NOT block or delay command execution. If the check does not complete within 100ms after command execution, it SHALL be silently discarded.

#### Scenario: Normal command with fresh cache
- **WHEN** user runs any CLI command and the cached version info is less than 24 hours old
- **THEN** no network request is made, and the cached version is used for comparison

#### Scenario: Normal command with stale or missing cache
- **WHEN** user runs any CLI command and the cached version info is older than the configured check interval or does not exist
- **THEN** a background HTTP request is made to the GitHub Releases API to fetch the latest version, and the cache file is updated with the result

#### Scenario: Network failure during check
- **WHEN** the background version check fails due to network error or timeout
- **THEN** the CLI completes normally with no error message and no update notification

### Requirement: Update notification display
The CLI SHALL display a one-line update notification to stderr after command output when a newer version is available. The notification SHALL include the latest version number and a command to run for upgrading.

#### Scenario: Newer version available
- **WHEN** the version check determines a newer release exists (comparing current `Version` to latest release tag)
- **THEN** the CLI prints to stderr a notification including the latest version, a changelog teaser summary (e.g., "3 new features, 2 fixes"), and the upgrade command: `New version available: <latest> (you have <current>) — <teaser>. Run 'memory upgrade' to update.`

#### Scenario: Already up to date
- **WHEN** the version check determines the current version matches or exceeds the latest release
- **THEN** no notification is displayed

#### Scenario: Piped output
- **WHEN** the user pipes CLI output to another command (e.g., `memory query ... | jq`)
- **THEN** the notification is written to stderr only and does not appear in stdout

### Requirement: Background auto-download mode
The CLI SHALL support an optional mode where it automatically downloads and installs the latest version in the background, so the next invocation uses the new binary. This mode SHALL be opt-in via configuration.

#### Scenario: Auto mode enabled and newer version available
- **WHEN** `auto_update.mode` is set to `"auto"` and a newer version is detected
- **THEN** the CLI downloads the new binary in the background and replaces the current binary using atomic file operations, and prints to stderr: `Memory CLI has been updated to <latest>. Restart to use the new version.`

#### Scenario: Auto mode download failure
- **WHEN** background download fails (network error, disk space, permission denied)
- **THEN** the CLI prints a brief warning to stderr: `Auto-update failed: <error>. Run 'memory upgrade' to update manually.`

#### Scenario: Notify mode (default)
- **WHEN** `auto_update.mode` is set to `"notify"` (or not set, since notify is the default)
- **THEN** the CLI only prints the notification message and does NOT download or install anything

### Requirement: Rate-limited version checking
The CLI SHALL rate-limit GitHub API calls by caching the latest version information locally. The cache SHALL be stored as a JSON file at `~/.memory/cache/latest-version.json`.

#### Scenario: Cache file format
- **WHEN** a successful version check completes
- **THEN** the cache file is written with the latest version string, the release body (markdown), the release URL, and a timestamp in RFC 3339 format

#### Scenario: Cache within interval
- **WHEN** the cache file exists and its timestamp is within the configured `auto_update.check_interval` (default 24h)
- **THEN** the cached version is used and no network request is made

#### Scenario: Cache expired
- **WHEN** the cache file exists but its timestamp is older than the configured check interval
- **THEN** a new network request is made and the cache file is updated

#### Scenario: Corrupted or unreadable cache
- **WHEN** the cache file exists but cannot be parsed
- **THEN** the CLI treats it as expired and performs a fresh check

### Requirement: Opt-out via environment variable
The CLI SHALL allow users to completely disable auto-update checks by setting the environment variable `MEMORY_NO_AUTO_UPDATE=1`. When disabled, no version check, notification, or download SHALL occur.

#### Scenario: Environment variable set to 1
- **WHEN** `MEMORY_NO_AUTO_UPDATE` is set to `1` or `true`
- **THEN** the auto-update system is entirely skipped — no network calls, no cache reads, no notifications

#### Scenario: Environment variable not set
- **WHEN** `MEMORY_NO_AUTO_UPDATE` is not set or is empty
- **THEN** the auto-update system operates according to config settings

### Requirement: Opt-out via config file
The CLI SHALL allow users to disable auto-update via the config key `auto_update.enabled` in `~/.memory/config.yaml`. The environment variable SHALL take precedence over the config file.

#### Scenario: Config disables auto-update
- **WHEN** `auto_update.enabled` is set to `false` in config.yaml and no env var override is present
- **THEN** the auto-update system is entirely skipped

#### Scenario: Environment variable overrides config
- **WHEN** `MEMORY_NO_AUTO_UPDATE=1` is set but `auto_update.enabled` is `true` in config
- **THEN** the auto-update system is skipped (env var wins)

#### Scenario: Config sets custom check interval
- **WHEN** `auto_update.check_interval` is set to a valid Go duration (e.g., `"12h"`, `"30m"`)
- **THEN** the cache expiry uses that duration instead of the 24h default

### Requirement: Skip auto-update for special builds
The CLI SHALL skip auto-update entirely for development builds and package-manager installations.

#### Scenario: Development build
- **WHEN** the CLI `Version` variable equals `"dev"`
- **THEN** no version check, notification, or download occurs

#### Scenario: Package manager install
- **WHEN** the CLI detects it was installed via a system package manager (e.g., pacman)
- **THEN** no version check, notification, or download occurs

### Requirement: Skip auto-update for certain commands
The CLI SHALL skip auto-update for commands where update notifications would be inappropriate or disruptive.

#### Scenario: Upgrade command
- **WHEN** the user runs `memory upgrade`
- **THEN** auto-update check is skipped (the command itself handles version checking)

#### Scenario: Version command
- **WHEN** the user runs `memory version`
- **THEN** auto-update check is skipped

#### Scenario: Completion command
- **WHEN** the user runs `memory completion` (shell completion generation)
- **THEN** auto-update check is skipped (output must be clean for shell eval)
