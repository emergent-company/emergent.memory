## ADDED Requirements

### Requirement: Step type provides scoped test actions
The framework SHALL expose a `Step` struct that is passed to step functions via `tc.Step(name, func(s *Step))`. The Step SHALL provide typed action methods that automatically record to RunLog.

#### Scenario: Step creates a RunLog section
- **WHEN** `tc.Step("Create agent", fn)` is called
- **THEN** `rl.Section("Create agent")` is called before `fn` executes

#### Scenario: Step measures duration
- **WHEN** a step function executes and returns
- **THEN** the step's elapsed duration is recorded in the RunLog event details

### Requirement: Step.CLI executes a CLI command and returns CLIResult
The framework SHALL expose `s.CLI(args ...string) *CLIResult` that executes the configured binary (from TestOpts.Binary) with the given arguments, using the TestContext's Home directory and environment. The invocation, stdout, stderr, and exit code SHALL be automatically logged to RunLog as a `cli` event.

#### Scenario: CLI command succeeds
- **WHEN** `s.CLI("projects", "list")` is called and the command exits 0
- **THEN** a `*CLIResult` is returned with stdout captured, and a `cli` event is emitted to RunLog with the invocation string `"<binary> projects list"` and the output

#### Scenario: CLI command fails
- **WHEN** `s.CLI("projects", "get", "bad-id")` is called and the command exits non-zero
- **THEN** the test fails via `rl.Failf` with the command, exit code, and output

#### Scenario: CLI uses configured binary
- **WHEN** TestOpts.Binary is `"mycli"` and `s.CLI("version")` is called
- **THEN** the command executed is `mycli version`, not `memory version`

#### Scenario: CLI uses TestContext home dir
- **WHEN** `s.CLI("config", "get")` is called
- **THEN** the command runs with `HOME` set to `tc.Home`

### Requirement: Step.CLIExpectError executes a CLI command that should fail
The framework SHALL expose `s.CLIExpectError(args ...string) *CLIResult` that is identical to `s.CLI` except it does NOT fail the test on non-zero exit. The exit code, stdout, and stderr are captured for assertion.

#### Scenario: Expected error captured
- **WHEN** `s.CLIExpectError("projects", "get", "nonexistent")` is called and the command exits 1
- **THEN** a `*CLIResult` is returned with exit code 1, and the test does NOT fail

#### Scenario: Unexpected success
- **WHEN** `s.CLIExpectError("projects", "get", "nonexistent")` is called and the command exits 0
- **THEN** a `*CLIResult` is returned with exit code 0 (no automatic failure — the test can assert `.ExitCode(1)` if needed)

### Requirement: Step.HTTP makes an authenticated API request and returns HTTPResult
The framework SHALL expose `s.HTTP(method, path string, body ...[]byte) *HTTPResult` that makes an HTTP request to `tc.Server + path` with auth headers set from `tc.Token`. The method, URL, status code, request body, and response body SHALL be automatically logged to RunLog.

#### Scenario: HTTP GET returns result
- **WHEN** `s.HTTP("GET", "/v1/projects")` is called
- **THEN** an authenticated GET request is made and an `*HTTPResult` is returned with status code and body

#### Scenario: HTTP POST with body
- **WHEN** `s.HTTP("POST", "/v1/projects", jsonBody)` is called
- **THEN** a POST request is made with Content-Type application/json and the given body

#### Scenario: HTTP auto-logs to RunLog
- **WHEN** any HTTP step is executed
- **THEN** a `cli` event (kind: "http") is recorded with method, URL, status, and response body

### Requirement: Step.Log writes a scoped log message
The framework SHALL expose `s.Log(format string, args ...any)` that writes a log event scoped to the current step.

#### Scenario: Log within step
- **WHEN** `s.Log("created ID: %s", id)` is called inside a step
- **THEN** a `log` event with message "created ID: <value>" is recorded under the current section

### Requirement: Step.WriteFile creates a fixture file
The framework SHALL expose `s.WriteFile(path, content string)` that writes a file to the TestContext's Home directory and logs the action.

#### Scenario: WriteFile creates file
- **WHEN** `s.WriteFile("schema.json", jsonContent)` is called
- **THEN** a file at `tc.Home/schema.json` is created with the given content, and a `log` event is recorded noting the file path and size
