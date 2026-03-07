## ADDED Requirements

### Requirement: Server management commands are accessible under memory server
A new `memory server` parent command SHALL exist. The following commands SHALL be subcommands of `memory server` and SHALL NOT be registered at the root level: `install`, `upgrade`, `uninstall`, `ctl`, `doctor`.

#### Scenario: memory server --help shows server subcommands
- **WHEN** a user runs `memory server --help`
- **THEN** the output lists `install`, `upgrade`, `uninstall`, `ctl`, and `doctor` as subcommands

#### Scenario: server install works at new path
- **WHEN** a user runs `memory server install`
- **THEN** the install command executes (same behavior as the former `memory install`)

#### Scenario: server ctl works at new path
- **WHEN** a user runs `memory server ctl start`
- **THEN** the ctl start command executes (same behavior as the former `memory ctl start`)

#### Scenario: server upgrade works at new path
- **WHEN** a user runs `memory server upgrade`
- **THEN** the upgrade command executes (same behavior as the former `memory upgrade`)

#### Scenario: server uninstall works at new path
- **WHEN** a user runs `memory server uninstall`
- **THEN** the uninstall command executes (same behavior as the former `memory uninstall`)

#### Scenario: server doctor works at new path
- **WHEN** a user runs `memory server doctor`
- **THEN** the doctor command executes (same behavior as the former `memory doctor`)

### Requirement: Old server command paths are removed
The commands `install`, `upgrade`, `uninstall`, `ctl`, and `doctor` SHALL NOT be registered as direct subcommands of the root `memory` command.

#### Scenario: memory install no longer works
- **WHEN** a user runs `memory install`
- **THEN** the CLI returns an "unknown command" error

#### Scenario: memory ctl no longer works
- **WHEN** a user runs `memory ctl start`
- **THEN** the CLI returns an "unknown command" error

### Requirement: memory server appears in root help output
The `memory server` command SHALL appear in the root `memory --help` output (in "Additional Commands" section) with a short description indicating it manages a self-hosted server.

#### Scenario: server entry visible in root help
- **WHEN** a user runs `memory --help`
- **THEN** `server` appears with short description "Manage a self-hosted Memory server"
