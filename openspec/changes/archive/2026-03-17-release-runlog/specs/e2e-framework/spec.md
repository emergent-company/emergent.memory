## MODIFIED Requirements

### Requirement: Framework package exists as importable Go sub-package
The `framework/` directory in `emergent.memory.e2e` SHALL become a thin re-export wrapper that imports and re-exports all types from `github.com/emergent-company/runlog`. The package name SHALL remain `e2eframework` for backward compatibility. All existing test files SHALL continue to compile without import path changes during the transition period.

#### Scenario: Package compiles without test files
- **WHEN** `go build ./framework/...` is run in the `emergent.memory.e2e` repo
- **THEN** the build succeeds with no errors

#### Scenario: Existing tests compile unchanged
- **WHEN** `go vet ./...` is run in the `emergent.memory.e2e` repo after the framework becomes a re-export wrapper
- **THEN** no vet errors are reported and all test files compile

#### Scenario: Re-export wrapper delegates to new module
- **WHEN** the `framework/` package source is inspected
- **THEN** each exported symbol is a type alias or variable assignment referencing `github.com/emergent-company/runlog`

### Requirement: CLI runner helpers in cli.go
The framework SHALL expose `MustRunCLI`, `MustRunCLIInDir`, `MustRunCLIInDirWithHome`, `MustRunBinaryInDirWithHome`, `RunBinaryInDirWithHome`, `RunCLIInDirWithHome`, `LogStatusPreamble`, and `SetupCLIAuth` functions. In the new module, these are direct exports. In the `emergent.memory.e2e` wrapper, these are re-exports.

#### Scenario: MustRunCLI fails test on non-zero exit
- **WHEN** `MustRunCLI(t, args...)` is called and the CLI exits non-zero
- **THEN** `t.Fatal` is called with the CLI output

### Requirement: RunLog and Gantt helpers in runlog.go
The framework SHALL expose the `RunLog` struct and `PrintGantt` / `PrintTokenSummary` functions. The `RunLog` struct SHALL work identically whether imported from the new standalone module or via the re-export wrapper.

#### Scenario: PrintGantt renders timeline
- **WHEN** `PrintGantt(t, runs)` is called with a slice of run records
- **THEN** an ASCII Gantt timeline is printed to test output

### Requirement: go.mod adds dependency on new module
The `emergent.memory.e2e` repository's `go.mod` SHALL add `github.com/emergent-company/runlog` as a dependency. The `framework/` wrapper package SHALL import from this dependency.

#### Scenario: go.mod references new module
- **WHEN** `go.mod` is inspected after migration
- **THEN** it contains a `require github.com/emergent-company/runlog v0.x.x` line
