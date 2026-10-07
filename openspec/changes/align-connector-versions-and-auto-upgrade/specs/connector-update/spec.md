## Purpose

The Linux `memory-connector` self-updates from, and checks for updates against,
the core monorepo releases so connector and core versions stay aligned.

## ADDED Requirements

### Requirement: Self-update resolves core `v*` releases

`memory-connector upgrade` SHALL resolve the newest core `vX.Y.Z` release of
`emergent-company/emergent.memory` and download the
`memory-connector_<X.Y.Z>_<os>_<arch>.tar.gz` asset. It SHALL ignore tags that
do not start with `v` (notably Go module tags under `apps/server/pkg/sdk/`). A
pinned `--version X.Y.Z` SHALL resolve tag `vX.Y.Z`.

#### Scenario: Newest core release is selected

- **WHEN** the repository has releases `v0.84.0`, `v0.86.0`, and a module tag `apps/server/pkg/sdk/v9.9.9`
- **THEN** `upgrade` resolves `v0.86.0` and its `memory-connector_0.86.0_<os>_<arch>.tar.gz` asset

#### Scenario: Module tags are ignored

- **WHEN** the only tags newer than the current version carry an `apps/...` prefix
- **THEN** they are not treated as connector releases

#### Scenario: Pinned version resolves the core tag

- **WHEN** the user runs `memory-connector upgrade --version 0.86.0`
- **THEN** the client resolves tag `v0.86.0` and its asset URL

### Requirement: Background automatic update check

The connector `relay`/`daemon` SHALL perform a background update check at
startup and repeated at the configured interval, backed by a rate-limited local
cache. The check SHALL NOT block or delay startup and SHALL treat network
failures as non-fatal.

#### Scenario: Fresh cache avoids a network call

- **WHEN** a check completed within the configured `check_interval`
- **THEN** the cached result is used and no release API request is made

#### Scenario: Network failure is non-fatal

- **WHEN** the update check fails (offline, timeout, non-200)
- **THEN** the relay continues serving and no error is surfaced to the hub

#### Scenario: Development version is skipped

- **WHEN** the connector version is `dev`, `unknown`, or empty
- **THEN** no update check is performed

### Requirement: Notify by default, opt-in auto-install

The connector SHALL be enabled for update checking by default in `notify` mode,
which only reports that a newer version is available. `auto` mode SHALL download
and replace the binary using the existing self-update path, then cause the
daemon to exit non-zero so its supervisor relaunches the new binary. In-place
install SHALL be refused when the binary is inside a macOS `.app` bundle.

#### Scenario: Notify mode reports only

- **WHEN** `auto_update.mode` is `notify` (default) and a newer release exists
- **THEN** the connector logs the available version and does not install anything

#### Scenario: Auto mode installs and restarts

- **WHEN** `auto_update.mode` is `auto` and a newer release exists
- **THEN** the connector installs the newer binary and exits non-zero so systemd (`Restart=on-failure`) relaunches it

#### Scenario: Bundled engine is not self-updated

- **WHEN** the binary runs inside a macOS `.app` bundle and `auto` mode is on
- **THEN** the connector skips the in-place install

### Requirement: Update checking is configurable and opt-out

Update checking SHALL be configurable via the `auto_update` section
(`enabled`, `mode`, `check_interval`) and SHALL be fully disabled by
`auto_update.enabled=false` or the environment variable
`MEMORY_CONNECTOR_NO_AUTO_UPDATE=1`.

#### Scenario: Config disables checks

- **WHEN** `auto_update.enabled` is `false`
- **THEN** no version check, cache read, or notification occurs

#### Scenario: Environment variable wins

- **WHEN** `MEMORY_CONNECTOR_NO_AUTO_UPDATE=1` is set
- **THEN** update checking is skipped regardless of the config file

#### Scenario: Invalid mode or interval is rejected

- **WHEN** `auto_update.mode` is not `notify`/`auto` or `check_interval` is not a duration
- **THEN** config loading fails with a validation error

### Requirement: Handshake reports the build version

The connector's MCP `initialize` client version SHALL be the build-injected
connector version, not a hardcoded literal.

#### Scenario: Version matches the binary

- **WHEN** a build reports version `X.Y.Z`
- **THEN** the MCP handshake `clientInfo.version` is `X.Y.Z`
