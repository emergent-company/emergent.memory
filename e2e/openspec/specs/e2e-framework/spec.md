## ADDED Requirements

### Requirement: Framework package exists as importable Go module
The framework library SHALL live in the standalone module `github.com/emergent-company/runlog` (package `runlog`). All test files SHALL import it with the alias `framework "github.com/emergent-company/runlog"`. The library SHALL NOT contain any `Test*` functions.

#### Scenario: Package compiles without test files
- **WHEN** `go build ./framework/...` is run
- **THEN** the build succeeds with no errors

### Requirement: HTTP client helpers in client.go
The framework SHALL expose `DoJSON`, `ReadBody`, `SetAuthHeader`, and `DoMCPJSON` functions covering all HTTP plumbing currently duplicated across test files.

#### Scenario: DoJSON sends authenticated request
- **WHEN** `DoJSON(t, method, url, token, body)` is called
- **THEN** an HTTP request is made with the correct method, JSON body, and `Authorization: Bearer <token>` header, and the response `*http.Response` is returned

#### Scenario: ReadBody reads and closes response body
- **WHEN** `ReadBody(t, resp)` is called with an `*http.Response`
- **THEN** the full body bytes are returned and `resp.Body` is closed

### Requirement: Server utilities in server.go
The framework SHALL expose `ServerURL()`, `SkipIfServerDown(t)`, `E2ETestToken()`, and `FilteredEnv()` functions.

#### Scenario: ServerURL reads environment variable
- **WHEN** `SERVER_URL` environment variable is set
- **THEN** `ServerURL()` returns that value

#### Scenario: SkipIfServerDown skips when server unreachable
- **WHEN** `SkipIfServerDown(t)` is called and the server health endpoint returns an error
- **THEN** the test is skipped via `t.Skip`

### Requirement: Project lifecycle helpers in project.go
The framework SHALL expose `CreateProject(t, token, serverURL, name) string`, `DeleteProjectOnCleanup(t, token, serverURL, projectID)`, `ConfigureGoogleProvider(t, token, serverURL, projectID)`, and `InstallBlueprint(t, env []string, blueprintPath, projectID string)` functions.

#### Scenario: CreateProject returns project ID
- **WHEN** `CreateProject` is called with valid credentials
- **THEN** a project is created via the API and its string ID is returned

#### Scenario: DeleteProjectOnCleanup registers cleanup
- **WHEN** `DeleteProjectOnCleanup(t, ...)` is called
- **THEN** `t.Cleanup` is registered to DELETE the project after the test completes

#### Scenario: Replaces 80-line boilerplate
- **WHEN** an orchestrator test calls `CreateProject` and `DeleteProjectOnCleanup`
- **THEN** no more than 5 lines of project-setup code are needed in the test body

### Requirement: Agent helpers in agents.go
The framework SHALL expose `CreateAgent`, `TriggerAgent`, `PollUntilSuccess`, `ParseAgentID`, and `DumpAgentRunDetails` functions, consolidating the duplicated implementations from `ai_news_blueprint_test.go` and `orchestrator_test.go`.

#### Scenario: PollUntilSuccess polls until terminal state
- **WHEN** `PollUntilSuccess(t, token, serverURL, projectID, runID, timeout)` is called
- **THEN** it polls the agent run status endpoint until the run reaches a terminal state (success or failure) or the timeout expires

#### Scenario: DumpAgentRunDetails logs run info on failure
- **WHEN** `DumpAgentRunDetails(t, ...)` is called and `t.Failed()` is true
- **THEN** it logs agent run details to the test output

### Requirement: Graph query helpers in graph.go
The framework SHALL expose `ListByType`, `ListByLabel`, and `ListRelationships` functions extracted from `ai_news_blueprint_test.go`.

#### Scenario: ListByType returns entities of given type
- **WHEN** `ListByType(t, token, serverURL, projectID, typeName)` is called
- **THEN** the graph API is queried and matching entity objects are returned

### Requirement: CLI runner helpers in cli.go
The framework SHALL expose `MustRunCLI`, `MustRunCLIInDir`, `MustRunCLIInDirWithHome`, and `LogStatusPreamble` functions extracted from `install_test.go`.

#### Scenario: MustRunCLI fails test on non-zero exit
- **WHEN** `MustRunCLI(t, args...)` is called and the CLI exits non-zero
- **THEN** `t.Fatal` is called with the CLI output

### Requirement: RunLog and Gantt helpers in runlog.go
The framework SHALL expose the `RunLog` struct and `PrintGantt` / `PrintTokenSummary` functions extracted from `helpers_test.go`.

#### Scenario: PrintGantt renders timeline
- **WHEN** `PrintGantt(t, runs)` is called with a slice of run records
- **THEN** an ASCII Gantt timeline is printed to test output

### Requirement: Environment helpers in env.go
The framework SHALL expose `LoadDotEnv(path string)`, `BlueprintEnvVar(name string) string`, and `ParseBlueprintEnvFiles(paths ...string) []string` functions extracted from `testmain_test.go` and orchestrator helpers.

#### Scenario: LoadDotEnv loads .env file
- **WHEN** `LoadDotEnv(".env")` is called and the file exists
- **THEN** environment variables from the file are loaded into the process environment

### Requirement: Parse utilities in parse.go
The framework SHALL expose `ParseProjectID`, `ParseAgentID`, `ParseJSONField`, `CompactRunsOutput`, and `AllRunsTerminal` functions.

#### Scenario: ParseProjectID extracts ID from API response
- **WHEN** `ParseProjectID(t, body)` is called with a JSON create-project response
- **THEN** the project `id` field string is returned

#### Scenario: AllRunsTerminal detects terminal states
- **WHEN** `AllRunsTerminal(runs)` is called with a list of run status strings
- **THEN** it returns `true` only if every run is in a terminal state (success, failed, cancelled)

### Requirement: All test files updated to use framework package
Every test file in the root `dockertests` package SHALL import `framework/` for shared helpers. No helper function SHALL be duplicated between test files.

#### Scenario: No duplicate doJSON in test files
- **WHEN** the codebase is searched for `func doJSON`
- **THEN** zero occurrences are found in `*_test.go` files (only `framework/client.go` defines it)

#### Scenario: No duplicate readBody in test files
- **WHEN** the codebase is searched for `func readBody`
- **THEN** zero occurrences are found in `*_test.go` files

#### Scenario: Codebase compiles cleanly
- **WHEN** `go build ./...` is run from the repo root
- **THEN** build succeeds with no errors

#### Scenario: All tests still run
- **WHEN** `go vet ./...` is run
- **THEN** no vet errors are reported
