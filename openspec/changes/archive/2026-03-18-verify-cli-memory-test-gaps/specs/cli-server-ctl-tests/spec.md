## ADDED Requirements

### Requirement: Server ctl --help lists subcommands
`memory server ctl --help` SHALL list available control operations (start, stop, status, restart).

#### Scenario: CTL help output
- **WHEN** `memory server ctl --help` is invoked
- **THEN** the command exits 0
- **THEN** the output contains "status"

### Requirement: Server ctl status exits without panic
`memory server ctl status` SHALL exit cleanly (with or without error) and SHALL NOT produce a Go panic trace, regardless of whether a server installation is present.

#### Scenario: CTL status non-panic on uninstalled server
- **WHEN** `memory server ctl status` is invoked on a host where server may or may not be installed
- **THEN** the command exits with any code
- **THEN** the output does NOT contain "panic" or "runtime error"
- **THEN** the output is non-empty

### Requirement: Server install/uninstall/upgrade --help are well-formed
Each server lifecycle subcommand SHALL respond to `--help` with exit 0 and non-empty usage output.

#### Scenario: Server install help
- **WHEN** `memory server install --help` is invoked
- **THEN** the command exits 0
- **THEN** the output contains "install"

#### Scenario: Server uninstall help
- **WHEN** `memory server uninstall --help` is invoked
- **THEN** the command exits 0
- **THEN** the output contains "uninstall" or "remove"

#### Scenario: Server upgrade help
- **WHEN** `memory server upgrade --help` is invoked
- **THEN** the command exits 0
- **THEN** the output contains "upgrade"
