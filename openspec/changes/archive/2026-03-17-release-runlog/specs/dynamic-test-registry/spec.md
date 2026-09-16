## ADDED Requirements

### Requirement: TUI discovers tests from the database
The TUI SHALL populate its test list by querying `SELECT DISTINCT test_name FROM test_runs` from the connected SQLite database. The hardcoded `knownTests` slice SHALL be removed.

#### Scenario: Tests appear without configuration
- **WHEN** the TUI starts and connects to a `runs.db` that contains runs for tests `TestFoo`, `TestBar`, and `TestBaz`
- **THEN** all three tests appear in the Tests tab without any config file or code changes

#### Scenario: New tests appear automatically
- **WHEN** a new test `TestNewFeature` writes its first run to the database
- **THEN** `TestNewFeature` appears in the TUI's test list on the next refresh cycle (within 2 seconds)

### Requirement: Optional config file for test categories and display names
The TUI SHALL support an optional `.runlog.yaml` configuration file that allows users to assign categories and display names to tests.

#### Scenario: Config file assigns categories
- **WHEN** `.runlog.yaml` contains `categories: { "cli/install": ["TestCLIInstalled_Version", "TestCLIInstalled_Help"] }`
- **THEN** those tests are displayed under the `cli/install` category in the Tests tab

#### Scenario: Tests without category assignment
- **WHEN** a test exists in the database but has no entry in `.runlog.yaml`
- **THEN** the test is displayed under an "Uncategorized" group

#### Scenario: Config file not required
- **WHEN** no `.runlog.yaml` file exists in the search paths
- **THEN** the TUI starts normally with all tests listed as uncategorized

### Requirement: Config file search path
The TUI SHALL search for `.runlog.yaml` in the following order: (1) path specified by `$RUNLOG_CONFIG` environment variable, (2) the directory containing `runs.db`, (3) the current working directory. The first file found SHALL be used.

#### Scenario: Environment variable overrides default paths
- **WHEN** `$RUNLOG_CONFIG` is set to `/custom/path/.runlog.yaml` and that file exists
- **THEN** the TUI loads configuration from that file regardless of other `.runlog.yaml` files

### Requirement: Simplified database path resolution
The TUI SHALL resolve the database path using: (1) `$RUNLOG_DB` environment variable, (2) `db` field in `.runlog.yaml`, (3) `./runs.db`, (4) `./logs/runs.db`. Project-specific and Docker-specific hardcoded paths SHALL be removed.

#### Scenario: Environment variable specifies DB path
- **WHEN** `$RUNLOG_DB` is set to `/data/myproject.db`
- **THEN** the TUI connects to that database file

#### Scenario: Default path discovery
- **WHEN** no `$RUNLOG_DB` is set and no config file exists, and `./logs/runs.db` exists
- **THEN** the TUI connects to `./logs/runs.db`

### Requirement: Configurable test launch command
The TUI's test launcher SHALL use a configurable command template (default: `go test -v -run {name} ./...`) that can be overridden in `.runlog.yaml`.

#### Scenario: Default launch command
- **WHEN** no `testCommand` is specified in config and the user launches test `TestFoo`
- **THEN** the TUI executes `go test -v -run TestFoo ./...`

#### Scenario: Custom launch command
- **WHEN** `.runlog.yaml` contains `testCommand: "go test -v -count 1 -run {name} ./tests/..."`
- **THEN** launching `TestFoo` executes `go test -v -count 1 -run TestFoo ./tests/...`
