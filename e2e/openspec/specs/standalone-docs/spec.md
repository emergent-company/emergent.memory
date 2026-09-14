## ADDED Requirements

### Requirement: README with project overview and quick-start
The repository SHALL contain a `README.md` with: project name and one-line description, feature highlights, installation methods (go install, binary download, install script), quick-start example showing framework usage in a Go test, and a link to full documentation.

#### Scenario: README exists and is non-empty
- **WHEN** a user visits the repository on GitHub
- **THEN** the README renders with all required sections: overview, features, installation, quick-start, and documentation link

#### Scenario: Quick-start example compiles
- **WHEN** the Go code block from the quick-start section is placed in a `_test.go` file with the correct imports
- **THEN** the code compiles without errors

### Requirement: Installation section covers all methods
The README SHALL document four installation methods: `go install`, GitHub Releases binary download, install script (`curl | sh`), and homebrew tap.

#### Scenario: Each install method has a code block
- **WHEN** the README installation section is inspected
- **THEN** each method has a copy-pasteable command in a fenced code block

### Requirement: Feature overview section
The README SHALL contain a feature overview section listing: TUI with run history and real-time updates, structured test logging (sections, steps, CLI/HTTP results), SQLite-backed run database, Gantt chart visualization, LLM-powered run analysis, and test launcher.

#### Scenario: Feature list is present
- **WHEN** the README feature section is inspected
- **THEN** each feature listed above is mentioned with a brief (1-2 sentence) description

### Requirement: Migration guide for existing users
The repository SHALL contain a `MIGRATION.md` (or section in README) documenting how to migrate from the embedded `emergent.memory.e2e/framework` package to the standalone `runlog` module, including: import path changes, config file setup for knownTests replacement, and binary installation.

#### Scenario: Import path change is documented
- **WHEN** a user reads the migration guide
- **THEN** they find a before/after comparison showing the old import path (`github.com/emergent-company/emergent.memory.e2e/framework`) and the new import path (`github.com/emergent-company/runlog`)

### Requirement: CHANGELOG for release tracking
The repository SHALL contain a `CHANGELOG.md` following the Keep a Changelog format, with an entry for each release starting from v0.1.0.

#### Scenario: CHANGELOG has initial release entry
- **WHEN** v0.1.0 is released
- **THEN** `CHANGELOG.md` contains a `## [0.1.0]` section listing initial features
