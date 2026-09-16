## ADDED Requirements

### Requirement: CLIResult provides chainable content assertions
The framework SHALL expose a `CLIResult` struct with methods that assert on CLI output and return `*CLIResult` for chaining. Each assertion SHALL log its check (pass or fail) to RunLog. Failed assertions SHALL call `rl.Failf`.

#### Scenario: Contains passes
- **WHEN** `s.CLI("projects", "list").Contains("my-project")` is called and stdout contains "my-project"
- **THEN** an assertion-pass event is logged and the `*CLIResult` is returned for further chaining

#### Scenario: Contains fails
- **WHEN** `.Contains("nonexistent")` is called and stdout does not contain "nonexistent"
- **THEN** `rl.Failf` is called with a message showing the expected substring and a truncated version of the actual output

#### Scenario: Contains with multiple substrings
- **WHEN** `.Contains("foo", "bar", "baz")` is called
- **THEN** all three substrings MUST be present; the first missing one triggers `rl.Failf`

### Requirement: CLIResult.ContainsAny asserts at least one substring matches
The framework SHALL expose `.ContainsAny(substrs ...string) *CLIResult` that passes if ANY of the given substrings are found in stdout.

#### Scenario: ContainsAny matches one
- **WHEN** `.ContainsAny("deleted", "removed")` is called and stdout contains "deleted" but not "removed"
- **THEN** the assertion passes

#### Scenario: ContainsAny matches none
- **WHEN** `.ContainsAny("deleted", "removed")` is called and stdout contains neither
- **THEN** `rl.Failf` is called

### Requirement: CLIResult.NotContains asserts substrings are absent
The framework SHALL expose `.NotContains(substrs ...string) *CLIResult` that passes if NONE of the given substrings are found in stdout.

#### Scenario: NotContains passes
- **WHEN** `.NotContains("error", "failed")` is called and stdout contains neither
- **THEN** the assertion passes

#### Scenario: NotContains fails
- **WHEN** `.NotContains("error")` is called and stdout contains "error"
- **THEN** `rl.Failf` is called

### Requirement: CLIResult.Matches asserts a regex pattern
The framework SHALL expose `.Matches(pattern string) *CLIResult` that compiles the pattern as a regexp and asserts it matches stdout.

#### Scenario: Matches passes
- **WHEN** `.Matches(`v\d+\.\d+\.\d+`)` is called and stdout contains "v1.2.3"
- **THEN** the assertion passes

#### Scenario: Matches fails
- **WHEN** `.Matches(`v\d+\.\d+\.\d+`)` is called and stdout is "unknown"
- **THEN** `rl.Failf` is called

### Requirement: CLIResult.Empty asserts output is empty
The framework SHALL expose `.Empty() *CLIResult` that asserts stdout is empty or whitespace-only.

#### Scenario: Empty passes
- **WHEN** `.Empty()` is called and stdout is `"  \n"`
- **THEN** the assertion passes

### Requirement: CLIResult.ParseID extracts a UUID from output
The framework SHALL expose `.ParseID(dst *string) *CLIResult` that extracts a UUID (36-char, 4 hyphens) from stdout and stores it in `*dst`. If no UUID is found, it SHALL call `rl.Failf`.

#### Scenario: ParseID succeeds
- **WHEN** `.ParseID(&id)` is called and stdout contains "Created agent abc12345-1234-1234-1234-123456789abc"
- **THEN** `id` is set to "abc12345-1234-1234-1234-123456789abc"

#### Scenario: ParseID fails
- **WHEN** `.ParseID(&id)` is called and stdout contains no UUID
- **THEN** `rl.Failf` is called

### Requirement: CLIResult.JSONField extracts a field from JSON output
The framework SHALL expose `.JSONField(field string, dst *string) *CLIResult` that parses stdout as JSON and extracts the named top-level field into `*dst`.

#### Scenario: JSONField succeeds
- **WHEN** `.JSONField("id", &projectID)` is called and stdout is `{"id":"abc","name":"test"}`
- **THEN** `projectID` is set to "abc"

#### Scenario: JSONField from non-JSON output
- **WHEN** `.JSONField("id", &projectID)` is called and stdout is not valid JSON
- **THEN** `rl.Failf` is called

### Requirement: CLIResult.JSON unmarshals full output
The framework SHALL expose `.JSON(dst any) *CLIResult` that unmarshals stdout into the provided destination.

#### Scenario: JSON unmarshal succeeds
- **WHEN** `.JSON(&result)` is called with a struct pointer and stdout is valid JSON
- **THEN** the struct is populated from the JSON

### Requirement: CLIResult.ExitCode asserts the exit code
The framework SHALL expose `.ExitCode(expected int) *CLIResult` that asserts the command's exit code matches the expected value. This is primarily useful with `CLIExpectError`.

#### Scenario: ExitCode matches
- **WHEN** `s.CLIExpectError("bad-cmd").ExitCode(1)` is called and the command exited with code 1
- **THEN** the assertion passes

#### Scenario: ExitCode mismatch
- **WHEN** `.ExitCode(1)` is called and the command exited with code 2
- **THEN** `rl.Failf` is called

### Requirement: CLIResult.Output and Stderr provide raw access
The framework SHALL expose `.Output() string` and `.Stderr() string` methods on CLIResult that return the raw stdout and stderr strings for cases where chainable assertions are insufficient.

#### Scenario: Output escape hatch
- **WHEN** `out := s.CLI("projects", "list", "--json").Output()` is called
- **THEN** `out` contains the raw stdout string

### Requirement: HTTPResult provides chainable status and body assertions
The framework SHALL expose an `HTTPResult` struct with methods: `.Status(expected int) *HTTPResult`, `.JSONField(field string, dst *string) *HTTPResult`, `.JSONContains(field, expected string) *HTTPResult`, `.BodyContains(substr string) *HTTPResult`, `.JSON(dst any) *HTTPResult`, and `.Body() string`.

#### Scenario: Status assertion passes
- **WHEN** `s.HTTP("GET", "/v1/projects").Status(200)` is called and the response status is 200
- **THEN** the assertion passes

#### Scenario: Status assertion fails
- **WHEN** `.Status(200)` is called and the response status is 404
- **THEN** `rl.Failf` is called with the expected and actual status codes

#### Scenario: JSONField on HTTPResult
- **WHEN** `s.HTTP("POST", "/v1/projects", body).Status(201).JSONField("id", &id)` is called
- **THEN** the response body is parsed as JSON and the "id" field is extracted

#### Scenario: BodyContains on HTTPResult
- **WHEN** `.BodyContains("created")` is called and the response body contains "created"
- **THEN** the assertion passes

### Requirement: All assertions log to RunLog
Every assertion method on CLIResult and HTTPResult SHALL emit a log event to RunLog indicating the check performed and whether it passed. This provides a complete audit trail in the TUI.

#### Scenario: Passed assertion logged
- **WHEN** `.Contains("foo")` passes
- **THEN** a `log` event like `"assert: output contains 'foo' ✓"` is recorded

#### Scenario: Failed assertion logged before Failf
- **WHEN** `.Contains("foo")` fails
- **THEN** a `failure` event is recorded (via `rl.Failf`) before the test stops
