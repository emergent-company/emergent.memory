## ADDED Requirements

### Requirement: Framework exposes `Use`, `Option`, `Fixture`, and option functions
The `github.com/emergent-company/runlog` library SHALL export `Use`, `Option`, `Fixture`, `WithProject`, `WithSchema`, `WithDocument`, and `WithBinary` from a new file `fixture.go`. These SHALL be importable alongside all existing exports without conflict.

#### Scenario: New symbols do not conflict with existing API
- **WHEN** a test file imports `framework "github.com/emergent-company/runlog"` and calls both `framework.NewTest(t, opts)` and `framework.Use(t, opts...)`
- **THEN** both compile and run without conflict

#### Scenario: `fixture.go` compiles as part of the package
- **WHEN** `go build github.com/emergent-company/runlog` is run
- **THEN** the build succeeds including `fixture.go`
