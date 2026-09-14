## ADDED Requirements

### Requirement: Upgrade --help is well-formed
The `memory upgrade --help` output SHALL document the `--force` and `--dir` flags.

#### Scenario: Help flag lists documented flags
- **WHEN** `memory upgrade --help` is invoked
- **THEN** the command exits 0
- **THEN** the output contains "--force"
- **THEN** the output contains "--dir"

### Requirement: Upgrade --force on dev build produces recognizable output
When invoked with `--force` on a dev build, `memory upgrade` SHALL either succeed with a download message or fail with a clear error. It SHALL NOT replace the binary in-place when run in the test environment (guarded by temp HOME).

#### Scenario: Force upgrade outputs a meaningful message
- **WHEN** `memory upgrade --force` is invoked with a temporary HOME directory
- **THEN** the command produces non-empty output
- **THEN** the output does NOT contain "panic" or an unhandled stack trace

### Requirement: Server upgrade --help is well-formed
`memory server upgrade --help` SHALL document the subcommand purpose and available flags.

#### Scenario: Server upgrade help
- **WHEN** `memory server upgrade --help` is invoked
- **THEN** the command exits 0
- **THEN** the output contains "upgrade"
