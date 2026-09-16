## MODIFIED Requirements

### Requirement: CLI runner helpers in cli.go
The framework SHALL expose `MustRunCLI`, `MustRunCLIInDir`, `MustRunCLIInDirWithHome`, `RunCLIInDirWithHome`, and `LogStatusPreamble` functions. Additionally, the framework SHALL expose `MustRunBinaryInDirWithHome(t, binary, dir, home string, args ...string) string` and `RunBinaryInDirWithHome(t, binary, dir, home string, args ...string) (string, error)` that accept a configurable binary name instead of hardcoding `"memory"`. The existing `MustRunCLI*` functions SHALL continue to work unchanged, using `"memory"` as the binary.

#### Scenario: MustRunCLI fails test on non-zero exit
- **WHEN** `MustRunCLI(t, args...)` is called and the CLI exits non-zero
- **THEN** `t.Fatal` is called with the CLI output

#### Scenario: MustRunBinaryInDirWithHome uses specified binary
- **WHEN** `MustRunBinaryInDirWithHome(t, "mycli", "", home, "version")` is called
- **THEN** the command `mycli version` is executed (not `memory version`)

#### Scenario: MustRunBinaryInDirWithHome fails test on non-zero exit
- **WHEN** `MustRunBinaryInDirWithHome(t, "mycli", "", home, "bad-cmd")` is called and the command exits non-zero
- **THEN** `t.Fatal` is called with the command, error, and output

#### Scenario: RunBinaryInDirWithHome returns error
- **WHEN** `RunBinaryInDirWithHome(t, "mycli", "", home, "bad-cmd")` is called and the command exits non-zero
- **THEN** a non-nil error and the combined output are returned without failing the test
