## ADDED Requirements

### Requirement: Standalone Go module with correct module path
The new repository SHALL use the Go module path `github.com/emergent-company/runlog`. The library package SHALL be the root package (`package runlog`). The TUI binary SHALL live at `cmd/runlog/main.go`.

#### Scenario: Module path is correct
- **WHEN** a user runs `go get github.com/emergent-company/runlog@v0.1.0`
- **THEN** the module resolves and downloads successfully

#### Scenario: Library is importable as root package
- **WHEN** a Go file contains `import runlog "github.com/emergent-company/runlog"`
- **THEN** the import resolves to the root package and all exported types (`RunLog`, `RunDB`, `Step`, `TestContext`, etc.) are accessible

#### Scenario: Binary is installable via go install
- **WHEN** a user runs `go install github.com/emergent-company/runlog/cmd/runlog@latest`
- **THEN** the `runlog` binary is built and placed in `$GOBIN`

### Requirement: All framework source files are present in the new module
The new module SHALL contain all source files currently in `framework/` of `emergent.memory.e2e`, with package declaration changed from `package e2eframework` to `package runlog`.

#### Scenario: No framework file is missing
- **WHEN** the file list of the new module root is compared to `framework/` in the old repo
- **THEN** every `.go` file from `framework/` has a corresponding file in the new module root (excluding `_test.go` files that reference test-suite-specific fixtures)

### Requirement: TUI binary source is present under cmd/runlog
The new module SHALL contain the TUI binary source at `cmd/runlog/main.go`, with import paths updated to reference the new module.

#### Scenario: TUI binary builds from new module
- **WHEN** `go build ./cmd/runlog/` is run in the new module
- **THEN** the build succeeds and produces a `runlog` binary

### Requirement: No circular or cross-repo imports
The new module SHALL NOT import any package from `github.com/emergent-company/emergent.memory.e2e`. All dependencies SHALL be either standard library or external modules.

#### Scenario: Clean dependency graph
- **WHEN** `go mod graph` is run in the new module
- **THEN** no line references `emergent.memory.e2e`
