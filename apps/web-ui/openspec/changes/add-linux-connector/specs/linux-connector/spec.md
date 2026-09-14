## Purpose

Delivers the Memory connector on Linux as a headless CLI plus a long-running daemon managed by systemd user services, reusing the shared connector core so account, project, configuration, and status behavior matches other platforms.

## ADDED Requirements

### Requirement: Provide a daemon subcommand

The connector SHALL provide a long-running daemon mode that connects and serves the configured tool set until signalled, running in the foreground so an external supervisor owns its lifecycle, SHALL stop cleanly on SIGTERM or SIGINT, and SHALL re-establish the relay connection automatically after a transient network loss without operator intervention.

#### Scenario: Daemon starts and connects
- **WHEN** the daemon starts with a valid configuration
- **THEN** it registers the instance and its tool set with the hub and serves relayed calls until stopped

#### Scenario: Reconnect after transient loss
- **WHEN** the relay connection drops because the network or server is temporarily unavailable
- **THEN** the daemon reconnects and re-registers with backoff, without exiting or requiring a restart

#### Scenario: Clean stop on signal
- **WHEN** the daemon receives SIGTERM or SIGINT
- **THEN** it closes the relay connection and exits with status 0

#### Scenario: Invalid configuration
- **WHEN** the daemon starts without a usable configuration
- **THEN** it exits non-zero with a message directing the user to configure the connector, leaving restart handling to the supervisor

### Requirement: Prevent duplicate daemon instances

The daemon SHALL ensure that at most one instance serves a given configuration, so that duplicate processes cannot register the same instance id with the hub, and a second start SHALL fail fast with a clear message rather than running concurrently.

#### Scenario: Second instance refused
- **WHEN** a daemon is already running for the configuration and another start is attempted
- **THEN** the second start exits with a clear "already running" message instead of connecting

#### Scenario: Lock released on exit
- **WHEN** a daemon exits, whether cleanly or after a crash
- **THEN** a subsequent start acquires the lock and runs normally

### Requirement: Install and run as a systemd user service

The connector SHALL provide install and uninstall operations that create, enable, start, stop, disable, and remove a systemd user unit for the daemon, SHALL restart the daemon on failure, and SHALL NOT require root privileges.

#### Scenario: Install enables and starts the service
- **WHEN** the user runs install with a valid configuration
- **THEN** a systemd user unit is written, enabled, and started without root privileges

#### Scenario: Restart on failure
- **WHEN** the daemon exits unexpectedly while installed
- **THEN** systemd restarts it according to the unit's restart policy

#### Scenario: Uninstall removes the service
- **WHEN** the user runs uninstall
- **THEN** the service is stopped, disabled, and its unit file removed

#### Scenario: Install and uninstall are idempotent
- **WHEN** install is run again on an already-installed connector, or uninstall is run when nothing is installed
- **THEN** the operation succeeds without error and leaves the system in the expected state

### Requirement: Use Linux platform paths and permissions

On Linux the connector SHALL honor XDG base directories for configuration, state, and logs, SHALL keep secret and configuration files at mode 0600 inside a 0700 directory, and SHALL direct daemon logs to the systemd journal.

#### Scenario: Respect XDG configuration directory
- **WHEN** the XDG configuration directory environment variable is set
- **THEN** the connector reads and writes its configuration under that directory

#### Scenario: Secret permissions on Linux
- **WHEN** the connector writes a secret or configuration file
- **THEN** the file mode is 0600 and its parent directory mode is 0700

### Requirement: Support headless sign-in

Because a Linux daemon has no native custom-scheme callback, the connector SHALL support signing in without a graphical app, primarily through the Memory platform's device authorization flow (no redirect URI) and optionally a loopback redirect, and SHALL continue to support token-only configuration when interactive sign-in is unavailable.

#### Scenario: Loopback sign-in
- **WHEN** the user signs in on a machine with a local browser
- **THEN** the provider callback is received on a loopback address and the session is established

#### Scenario: Device flow sign-in
- **WHEN** the user signs in on a host without a usable browser
- **THEN** the connector presents a device code and completes sign-in after the user authorizes it elsewhere

#### Scenario: Token-only configuration
- **WHEN** interactive sign-in is not possible
- **THEN** the connector can be configured with a manually provided project token and relays with it

### Requirement: Report platform tool availability

The Linux connector SHALL run and remain connected when no platform tools are available, SHALL register an empty tool set in that case, and SHALL explain in status output that platform tool hooks are not yet available on Linux.

#### Scenario: Connected with no local tools
- **WHEN** the daemon runs on Linux with no platform tool hooks implemented
- **THEN** it connects to the hub with an empty tool set and remains usable

#### Scenario: Status explains absence
- **WHEN** status is requested and no local tools are registered
- **THEN** the output states that no platform tools are available on Linux and why

### Requirement: Ship standalone Linux binaries

The connector SHALL be distributed as standalone Linux binaries for the amd64 and arm64 architectures that require no runtime dependency beyond the base system, and SHALL report a version.

#### Scenario: Run from a downloaded binary
- **WHEN** a Linux binary is downloaded and executed with no arguments
- **THEN** it prints its version and usage without additional dependencies
