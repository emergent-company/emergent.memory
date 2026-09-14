# Framework API Reference

All functions are in package `runlog` at `github.com/emergent-company/runlog`.

In test files, access them through the thin wrappers in `helpers_test.go` and `install_test.go` (no explicit import needed for those), or import the package directly when needed.

---

## Server / environment

```go
// ServerURL returns MEMORY_TEST_SERVER (default: http://test-emergent-server:3002).
func ServerURL() string

// E2ETestToken returns MEMORY_TEST_TOKEN.
func E2ETestToken() string

// AuthMode returns MEMORY_AUTH_MODE ("standalone" or "account").
func AuthMode() string

// SetToken returns MEMORY_SET_TOKEN (bearer value written to credentials.json in account mode).
func SetToken() string

// OrgID returns MEMORY_ORG_ID (optional; appended to project/org CLI args when set).
func OrgID() string

// SkipIfServerDown skips t if the server /health endpoint is unreachable.
// Optional rl records a skip event to the RunLog.
func SkipIfServerDown(t *testing.T, rl ...*RunLog)

// SkipIfEndpointMissing skips t when a HEAD request to path returns 404/405.
func SkipIfEndpointMissing(t *testing.T, path, bearerToken string, rl ...*RunLog)

// FilteredEnv returns os.Environ() with HOME and MEMORY_* removed (for clean subprocess env).
func FilteredEnv() []string

// LoadDotEnv loads a .env file from the repo root if present (called from TestMain).
func LoadDotEnv()

// LoadDotEnvFrom loads .env from a specific directory (for compiled binaries in cmd/*).
func LoadDotEnvFrom(dir string)
```

---

## CLI invocation (`framework/cli.go`)

```go
// MustRunCLI runs `memory <args>` in the working directory; calls t.Fatal on non-zero exit.
func MustRunCLI(t *testing.T, args ...string) string

// MustRunCLIInDir runs `memory <args>` in dir; calls t.Fatal on non-zero exit.
func MustRunCLIInDir(t *testing.T, dir string, args ...string) string

// MustRunCLIInDirWithHome runs `memory <args>` with HOME=home; calls t.Fatal on non-zero exit.
func MustRunCLIInDirWithHome(t *testing.T, dir, home string, args ...string) string

// RunCLIInDirWithHome is like MustRunCLIInDirWithHome but returns (output, error) instead of
// failing the test — used in poll loops where transient failures are acceptable.
func RunCLIInDirWithHome(t *testing.T, dir, home string, args ...string) (string, error)

// MustRunBinaryInDirWithHome runs `<binary> <args>` with HOME=home; calls t.Fatal on non-zero exit.
// Generic form of MustRunCLIInDirWithHome — pass any binary name.
func MustRunBinaryInDirWithHome(t *testing.T, binary, dir, home string, args ...string) string

// RunBinaryInDirWithHome is like MustRunBinaryInDirWithHome but returns (output, error).
func RunBinaryInDirWithHome(t *testing.T, binary, dir, home string, args ...string) (string, error)

// SetupCLIAuth configures authentication in the isolated home directory.
// Behaviour depends on MEMORY_AUTH_MODE: "standalone" sets api_key; "account" writes credentials.json.
func SetupCLIAuth(t *testing.T, home string)

// LogStatusPreamble logs `memory status` output to t.Log for test context.
func LogStatusPreamble(t *testing.T, home ...string)

// LogSession writes a structured CLI invocation + output to the session log directory.
// Best-effort: failures are logged but never fail the test.
func LogSession(t *testing.T, invocation, output string)
```

Constant:
```go
// CLITimeout is the per-command timeout for CLI invocations (30s).
const CLITimeout = 30 * time.Second
```

Wrappers available without explicit import (defined in `install_test.go` / `helpers_test.go`):
- `mustRunCLIInDirWithHome(t, dir, home, args...)` → `framework.MustRunCLIInDirWithHome`
- `logStatusPreamble(t, home...)` → `framework.LogStatusPreamble`
- `serverURL()` → `framework.ServerURL()`
- `e2eTestToken()` → `framework.E2ETestToken()`
- `skipIfServerDown(t)` → `framework.SkipIfServerDown(t)`
- `filteredEnv()` → `framework.FilteredEnv()`

---

## Parse helpers (`framework/parse.go`)

```go
// ParseProjectID extracts a UUID from `memory projects create` output.
// Looks for lines of the form: Created project "name" (<uuid>)
func ParseProjectID(output string) string

// ParseAgentID extracts an agent ID from `memory agents create` output.
// Looks for lines of the form: ID:   <uuid>
func ParseAgentID(output string) string

// ParseAgentDefsModel extracts the first model name from `memory agent-definitions list` output.
// Returns the first "Model: <name>" value found, or "" if none.
func ParseAgentDefsModel(output string) string

// ParseJSONField extracts a top-level string field from a JSON object.
func ParseJSONField(jsonStr, field string) string

// ParseLineField finds the first line containing key and returns the trimmed value after it.
// Useful for extracting labelled fields from CLI table output, e.g. "ID: abc123".
func ParseLineField(s, key string) string

// CompactRunsOutput returns a compact one-line-per-run summary of `memory agents runs` output.
func CompactRunsOutput(runsOut string) string

// AllRunsTerminal returns true when all runs in the output are in a terminal state.
func AllRunsTerminal(runsOut string) bool

// PrettyJSONOutput reformats JSON embedded in CLI output for readability.
func PrettyJSONOutput(output string) string

// ParseFrontmatterFields extracts name and description from YAML frontmatter.
func ParseFrontmatterFields(content string) (name, description string)
```

Wrappers in `helpers_test.go`:
- `parseProjectID(output)`, `parseAgentID(output)`, `parseJSONField(jsonStr, field)`
- `parseLineField(s, key)`, `compactRunsOutput(runsOut)`, `allRunsTerminal(runsOut)`

---

## HTTP client (`framework/client.go`)

```go
// DoJSON sends an HTTP request with JSON body and auth headers; calls t.Fatal on network error.
func DoJSON(t *testing.T, method, url, token, projectID string, body []byte) *http.Response

// ReadBody reads and closes the response body; calls t.Fatal on error.
func ReadBody(t *testing.T, resp *http.Response) string

// SetAuthHeader sets Authorization and X-Project-ID headers on a request.
func SetAuthHeader(req *http.Request, token string)

// DoMCPJSON sends an MCP-specific HTTP request with session and protocol-version headers.
func DoMCPJSON(t *testing.T, url, token, projectID, sessionID, protocolVersion string, body []byte) *http.Response

// Truncate returns s[:n]+"..." if len(s) > n, else s.
func Truncate(s string, n int) string
```

Wrappers in `helpers_test.go`: `doJSON`, `readBody`, `setAuthHeader`, `doMCPJSON`, `truncate`

---

## Project helpers (`framework/project.go`)

```go
// CreateProject creates a project via CLI and returns its ID; calls t.Fatal on parse failure.
// Automatically appends --org-id when MEMORY_ORG_ID is set.
func CreateProject(t *testing.T, home, srv, name string) string

// DeleteProjectOnCleanup registers t.Cleanup to delete the project. Non-fatal.
func DeleteProjectOnCleanup(t *testing.T, home, projectID string)

// RevokeTokenOnCleanup registers a t.Cleanup that runs `memory tokens revoke <tokenID>`.
// If rl is non-nil, the cleanup records structured RunLog events; otherwise falls back to t.Logf.
// Non-fatal: failures are logged but never fail the test.
func RevokeTokenOnCleanup(t *testing.T, rl *RunLog, home, tokenID string)

// ConfigureGoogleProvider runs `memory provider configure google`.
func ConfigureGoogleProvider(t *testing.T, home, apiKey, model string)

// InstallBlueprint runs `memory blueprints <url> --project <name> --upgrade`.
func InstallBlueprint(t *testing.T, home, blueprintURL, projectName string) string

// UniqueProjectName returns a timestamped project name: "<prefix>-<unixmilli>".
func UniqueProjectName(prefix string) string

// OrgIDArgs returns ["--org-id", "<id>"] when MEMORY_ORG_ID is set, else nil.
// Append to any CLI call that requires --org-id.
func OrgIDArgs() []string

// ProjectCreateOrgArgs is an alias for OrgIDArgs — use for `projects create` calls.
func ProjectCreateOrgArgs() []string
```

---

## Agent helpers (`framework/agents.go`)

```go
// PollUntilSuccess polls agent runs until a successful run appears or timeout.
// Only logs on status transitions (not every tick) to keep logs focused.
// Returns true if a successful run was found within the timeout.
func PollUntilSuccess(
    t *testing.T,
    rl *RunLog,
    home, srv, token, projectID, agentID, agentName string,
    timeout, pollInterval time.Duration,
) bool

// DumpAgentRunDetails fetches and logs all runs + tool calls for each agent.
// agentNames and agentIDs must be parallel slices of the same length.
func DumpAgentRunDetails(
    t *testing.T, rl *RunLog,
    srv, token, projectID string,
    agentNames, agentIDs []string,
)

// TriggerAgent triggers an agent via `memory agents trigger <agentID> --project <projectID>`.
// Logs the result to rl when rl is non-nil. Returns raw CLI output.
func TriggerAgent(t *testing.T, rl *RunLog, home, projectID, agentID string) string

// ListPendingQuestions lists pending agent questions for a project via the CLI.
// Returns parsed questions, raw output, and error. Does NOT call t.Fatal — safe for poll loops.
func ListPendingQuestions(t *testing.T, rl *RunLog, home, projectID string) ([]AgentQuestion, string, error)

// ListAllQuestions lists all agent questions for a project (no status filter).
// Returns parsed questions, raw output, and error. Does NOT call t.Fatal.
func ListAllQuestions(t *testing.T, rl *RunLog, home, projectID string) ([]AgentQuestion, string, error)

// RespondToQuestion responds to a pending agent question via the CLI.
// Returns raw CLI output and error. Does NOT call t.Fatal.
func RespondToQuestion(t *testing.T, rl *RunLog, home, projectID, questionID, response string) (string, error)

// AgentQuestion represents a single agent question from the CLI JSON output.
type AgentQuestion struct {
    ID       string `json:"id"`
    Question string `json:"question"`
    Status   string `json:"status"`
}
```

Wrappers in `helpers_test.go`:
- `pollAgentUntilSuccess(t, rl, home, srv, token, projectID, agentID, agentName, timeout)` — uses package-level `pollInterval`
- `dumpAgentRunDetails(t, rl, srv, token, projectID, agentNames, agentIDs)`

---

## Graph helpers (`framework/graph.go`)

```go
// ListByType calls GET /api/projects/:id/graph/objects?type=<objectType> and returns parsed objects.
func ListByType(t *testing.T, srv, token, projectID, objectType string) []map[string]any

// ListByLabel calls GET /api/projects/:id/graph/objects?label=<label> and returns parsed objects.
func ListByLabel(t *testing.T, srv, token, projectID, label string) []map[string]any

// ListRelationships calls GET /api/projects/:id/graph/relationships?targetId=<id>&type=<type>.
func ListRelationships(t *testing.T, srv, token, projectID, targetID, relType string) []map[string]any

// PropString extracts a string property by trying each key in order.
func PropString(props map[string]any, keys ...string) string
```

Wrappers in `helpers_test.go`:
- `listGraphObjectsByType(t, srv, token, projectID, objectType)`
- `listGraphObjectsByLabel(t, srv, token, projectID, label)`
- `listRelationshipsByTarget(t, srv, token, projectID, targetID, relType)`
- `propString(props, keys...)`

---

## RunLog + telemetry (`framework/runlog.go`)

```go
// NewRunLog creates a per-test RunLog that writes a structured log file to logs/<timestamp>-<TestName>/.
// Also registers with ActiveRunLogs and wires up the shared runs DB.
func NewRunLog(t *testing.T) *RunLog

// ActiveRunLogs maps t.Name() → *RunLog for tests that have an active run log.
var ActiveRunLogs sync.Map

// DoSkipf records a skip reason via rl.Skipf when rl is non-nil, otherwise calls t.Skipf.
// Use in skip-guard helpers that accept an optional *RunLog. Never returns.
func DoSkipf(t *testing.T, rl *RunLog, format string, args ...any)

// Methods on *RunLog:
//
//   Section(name string)
//       Writes a section header and starts a new collapsible DB group.
//       All subsequent events are stored as children until the next Section or Close.
//
//   Printf(format string, args ...any)
//       Writes a timestamped line and calls t.Log.
//
//   CLI(invocation, output string)
//       Logs a CLI invocation + output. Message shown in run list: "$ <invocation>".
//
//   CLIErr(invocation, output string, err error)
//       Like CLI but also records the exit code so the TUI highlights the row in red.
//
//   CLIStep(desc, invocation, output string)
//       Like CLI but uses desc as the short message shown in the run list.
//
//   CLIStepErr(desc, invocation, output string, err error)
//       Like CLIStep but also records the exit code.
//
//   Event(kind, message string, details any)
//       Emits a structured event of any kind with arbitrary JSON-serialisable details.
//       kind should be snake_case, e.g. "state_change", "metric", "gantt_row".
//
//   Group(kind, title string, fn func(g *GroupLogger))
//       Emits one parent event whose children are the lines logged inside fn.
//       In the TUI the group appears collapsed; pressing Enter expands it.
//
//   Failf(format string, args ...any)
//       Records a "failure" event to the log and DB, then calls t.Fatal.
//
//   Skipf(format string, args ...any)
//       Records a "skip" event to the log and DB, then calls t.Skip.
//
//   Describe(summary string, bullets ...string)
//       Stores a human-readable description in the DB run row. Call once after NewRunLog.
//
//   Tag(tags ...string)
//       Appends "key:value" variant tags to the run and persists them to the DB.
//
//   SetExperiment(name string)
//       Assigns an experiment name to the run for grouping in the TUI.
//       Called automatically by NewRunLog when EXPERIMENT env var is set.
//
//   StartTracePoller(serverURL, token, projectID string)
//       Starts a background poller that writes Tempo trace spans to the DB as events.
//       Stopped automatically in Close(). No-op when DB is unavailable.
//
//   Dir() string
//       Returns the folder that contains run.log and sibling detail files.
//
//   Close()
//       Flushes, closes the log file, and records outcome in the DB.
//       Call via `defer rl.Close()`.

// GroupLogger is passed to the function given to RunLog.Group.
// Its methods capture sub-lines for the parent group event.
type GroupLogger struct { /* opaque */ }

func (g *GroupLogger) Printf(format string, args ...any)
func (g *GroupLogger) Event(kind, message string, details any)

// FetchRunTokenUsage fetches input/output token counts and estimated cost for a run.
func FetchRunTokenUsage(t *testing.T, srv, token, projectID, runID string) (inputTokens, outputTokens int64, estimatedCostUSD float64)

// BuildAgentRunIntervals fetches run history for multiple agents and builds timeline data.
func BuildAgentRunIntervals(t *testing.T, rl *RunLog, srv, token, projectID string, agents []AgentInfo) []AgentRunInterval

// PrintGantt renders a Gantt-style ASCII timeline of agent runs into the RunLog.
// Also emits a "gantt" event with GanttData so the TUI can re-render.
func PrintGantt(rl *RunLog, intervals []AgentRunInterval)

// PrintTokenSummary renders a token usage summary table into the RunLog.
func PrintTokenSummary(rl *RunLog, intervals []AgentRunInterval)

// FormatInt formats a large integer with comma separators.
func FormatInt(n int64) string
```

### Types

```go
type RunLog struct { /* opaque */ }

// AgentInfo is the minimal shape for BuildAgentRunIntervals.
// Cron is optional — when empty, helpers use the default dormant cron.
type AgentInfo struct {
    Name string
    ID   string
    Cron string
}

type AgentRunInterval struct {
    AgentName        string
    RunID            string
    Start            time.Time
    End              time.Time // zero means still running
    DurationMs       int
    InputTokens      int64
    OutputTokens     int64
    EstimatedCostUSD float64
}

// GanttRow is the serialisable form of one agent run's timing (stored in "gantt" event details).
type GanttRow struct {
    AgentName        string  `json:"agent_name"`
    RunID            string  `json:"run_id"`
    StartS           float64 `json:"start_s"`
    EndS             float64 `json:"end_s"`
    DurationMs       int     `json:"duration_ms"`
    InputTokens      int64   `json:"input_tokens"`
    OutputTokens     int64   `json:"output_tokens"`
    EstimatedCostUSD float64 `json:"cost_usd"`
}

// GanttData is stored as the details JSON of a "gantt" event.
type GanttData struct {
    TotalS float64    `json:"total_s"`
    Rows   []GanttRow `json:"rows"`
}
```

Type aliases in `helpers_test.go` (no import needed):
- `type runLog = framework.RunLog`
- `type agentInfo = framework.AgentInfo`
- `type agentRunInterval = framework.AgentRunInterval`

Wrappers in `helpers_test.go`:
- `newRunLog(t)`, `buildAgentRunIntervals(...)`, `printGanttTimeline(...)`, `printTokenUsageSummary(...)`

---

## Pre-checks (`framework/precheck.go`)

```go
// RequireServerReady skips t if the server is not reachable or configured credentials
// do not authenticate successfully. Replaces the old SkipIfServerDown + SetupCLIAuth pair.
//
// home is the isolated HOME directory. Pass t.TempDir() — RequireServerReady calls
// SetupCLIAuth internally, so the caller does NOT need to call it again.
// If rl is non-nil the skip reason is recorded in the runs DB.
//
// Usage:
//   home := t.TempDir()
//   framework.RequireServerReady(t, home)
//   // home is now fully authenticated
func RequireServerReady(t *testing.T, home string, rl ...*RunLog)
```

---

## Credentials (`framework/credentials.go`)

```go
// VerifyCredentialsWritten asserts that credentials.json exists under <home>/.memory/
// and contains a non-empty token value. Failures are via t.Errorf (non-fatal).
// If rl is non-nil, a "credentials" event is emitted with path, field list, and server URL
// (but never the token value).
func VerifyCredentialsWritten(t *testing.T, rl *RunLog, home string) CredentialsInfo

// CredentialsInfo holds the parsed fields from ~/.memory/credentials.json.
type CredentialsInfo struct {
    Path         string   // absolute path of the file that was read
    HasToken     bool     // true when a non-empty token field is present
    HasServerURL bool     // true when server_url is non-empty
    ServerURL    string   // server URL found in the file, or ""
    RawKeys      []string // every top-level key present in the file
}
```

---

## Skills verification (`framework/skills.go`)

```go
// VerifySkillInstalled asserts that the named skill directory contains a valid SKILL.md
// with non-empty name and description fields, and that the name matches the directory name.
// Failures are reported via t.Errorf (non-fatal).
// If rl is non-nil, a "skill" event is emitted recording parsed fields.
func VerifySkillInstalled(t *testing.T, rl *RunLog, skillsDir, skillName string)
```

---

## Fixtures (`fixtures/bookstore.go`)

```go
// Package e2efixtures — import as fixtures "github.com/emergent-company/emergent.memory.e2e/fixtures"

// NewBookstoreWorkspace creates a temp directory with a minimal bookstore project.
// Returns *BookstoreWorkspace with fields: Dir string, t *testing.T.
func NewBookstoreWorkspace(t *testing.T) *BookstoreWorkspace

// BookstoreWorkspace.WriteEnvLocal writes a .env.local file with server/project/token config.
func (ws *BookstoreWorkspace) WriteEnvLocal(serverURL, projectID, projectToken string)
```

---

## Environment helpers (`framework/env.go`)

```go
// LoadDotEnvFrom loads .env (and optional .env.<MEMORY_TEST_ENV> overlay) from a specific directory.
// Use instead of LoadDotEnv when calling from a compiled binary (cmd/*) where runtime.Caller
// would resolve to the source dir, not the repo root.
func LoadDotEnvFrom(dir string)

// BlueprintEnvVar reads a key from a blueprint's .env or .env.local file.
func BlueprintEnvVar(dir, key string) string

// ParseBlueprintEnvFiles parses all .env* files in a directory into a map.
func ParseBlueprintEnvFiles(dir string) map[string]string
```

---

## Analyzer (`framework/analyzer.go`)

LLM-powered analyzer that uses Google Gemini (via ADK) to analyze test runs and produce
structured improvement suggestions. Requires `GOOGLE_AI_API_KEY` env var.

```go
// NewAnalyzer creates an Analyzer. Reads GOOGLE_AI_API_KEY (required) and
// ANALYZER_MODEL (optional, defaults to "gemini-2.5-flash") from the environment.
func NewAnalyzer(db *RunDB) (*Analyzer, error)

// Run analyzes all runs in an experiment and produces suggestions.
func (a *Analyzer) Run(ctx context.Context, experiment string) ([]SuggestionRow, error)

// RunByTestName analyzes all runs matching a test name and produces suggestions.
func (a *Analyzer) RunByTestName(ctx context.Context, testName string) ([]SuggestionRow, error)

// RunByRunID analyzes a single run by its DB row ID and produces suggestions.
func (a *Analyzer) RunByRunID(ctx context.Context, runID int64) ([]SuggestionRow, error)
```

### Types

```go
type AnalyzerEventKind string

// Constants: AEThought, AEText, AEToolCall, AEToolResult, AETokenUsage,
//            AEError, AETurnComplete, AESystemPrompt, AEUserMessage

type AnalyzerEvent struct {
    Kind          AnalyzerEventKind
    Author        string         // agent name
    Content       string         // text/thought content or formatted tool call/result
    ToolName      string         // tool_call events
    ToolArgs      map[string]any // tool_call events
    ToolResponse  map[string]any // tool_result events
    PromptTokens  int32          // token_usage events
    OutputTokens  int32
    ThoughtTokens int32
    TotalTokens   int32
    ErrorCode     string         // error events
    ErrorMessage  string
}

type Analyzer struct {
    // Set OnEvent to trace agent behavior in real time (thoughts, tool calls, tokens).
    OnEvent func(AnalyzerEvent)
    // ... unexported fields ...
}
```

### Tools available to the analyzer agent

| Tool | Description |
|---|---|
| `get_experiment_summary` | Overview of an experiment's runs (experiment mode) |
| `list_runs` | List runs filtered by experiment or test name |
| `get_run_events` | Fetch events for a specific run |
| `get_run_log` | Read the flat log file for a run |
| `create_suggestion` | Store a structured suggestion in the DB |
| `memory_ask` | Ask the Memory platform a question (secondary; CLI answers must be verified) |
| `run_cli` | Run a read-only `memory` CLI command (primary investigation tool) |

---

## Suggestion & trace persistence (`framework/db.go`)

```go
type SuggestionRow struct {
    ID          int64
    Experiment  string
    GeneratedAt time.Time
    Category    string
    Priority    string
    Title       string
    Body        string
    RunIDs      []int64
}

// InsertSuggestion stores one suggestion for an experiment. runIDs is serialised as JSON.
func (rdb *RunDB) InsertSuggestion(experiment, title, body, category, priority string, runIDs []int64) (int64, error)

// ListSuggestions returns all suggestions for an experiment.
func (rdb *RunDB) ListSuggestions(experiment string) ([]SuggestionRow, error)

// DeleteSuggestions removes all suggestions for an experiment.
func (rdb *RunDB) DeleteSuggestions(experiment string) error

type AnalyzerTraceRow struct {
    ID            int64
    SuggestionKey string
    RunID         *int64
    StartedAt     time.Time
    FinishedAt    *time.Time
}

// InsertAnalyzerTrace creates a new trace row and returns its ID.
func (rdb *RunDB) InsertAnalyzerTrace(suggestionKey string, runID *int64) (int64, error)

// FinishAnalyzerTrace sets the finished_at timestamp on a trace.
func (rdb *RunDB) FinishAnalyzerTrace(traceID int64) error

// InsertAnalyzerTraceEvent appends one event to a trace.
func (rdb *RunDB) InsertAnalyzerTraceEvent(traceID int64, seq int, ev AnalyzerEvent) error

// GetLatestTraceForRun returns the most recent trace ID for a given run.
func (rdb *RunDB) GetLatestTraceForRun(runID int64) (int64, error)

// ListAnalyzerTraceEvents returns all events for a trace, ordered by sequence.
func (rdb *RunDB) ListAnalyzerTraceEvents(traceID int64) ([]AnalyzerEvent, error)

// DeleteAnalyzerTraces removes all traces matching a suggestion key.
func (rdb *RunDB) DeleteAnalyzerTraces(suggestionKey string) error
```

---

## Structured Step API (NEW)

The structured step API eliminates manual dual bookkeeping in tests. Instead of manually
calling `rl.Section`, `rl.CLI`, `strings.Contains` + `t.Errorf`, and `ParseProjectID` separately,
the new API composes these into typed, chainable, auto-logged actions.

### TestContext & TestOpts (`framework/test_context.go`)

```go
// TestOpts configures a TestContext created by NewTest.
type TestOpts struct {
    Describe string     // One-line summary for RunLog
    Bullets  []string   // Detail bullets for RunLog inspector
    Project  string     // Non-empty → auto-create project + cleanup
    Tags     []string   // Variant tags (e.g. "model:gemini")
    Binary   string     // CLI binary name (default: "memory")
}

// TestContext bundles per-test lifecycle state. All fields are exported.
type TestContext struct {
    T         *testing.T
    RunLog    *RunLog
    Home      string  // Isolated temp dir with CLI auth configured
    Server    string  // Server URL from MEMORY_TEST_SERVER
    Token     string  // Auth token from MEMORY_TEST_TOKEN
    ProjectID string  // Set when TestOpts.Project is non-empty
    Binary    string  // CLI binary name
}

// NewTest creates a TestContext with automatic preamble:
//   - NewRunLog + t.Cleanup(rl.Close)
//   - TempDir as Home + SetupCLIAuth
//   - SkipIfServerDown
//   - Describe + Tags
//   - Optional project creation + cleanup
func NewTest(t *testing.T, opts TestOpts) *TestContext

// Step creates a RunLog Section, constructs a *Step, calls fn, records duration.
func (tc *TestContext) Step(name string, fn func(s *Step))

// Done is an idempotent finalizer. Call via defer tc.Done() after NewTest.
func (tc *TestContext) Done()

// Delegate methods:
func (tc *TestContext) Log(format string, args ...any)   // → rl.Printf
func (tc *TestContext) Tag(tags ...string)                // → rl.Tag
func (tc *TestContext) Skip(reason string)                // → rl.Skipf
```

### Step (`framework/step.go`)

```go
// Step is a scoped action block created by tc.Step. All actions auto-log to RunLog.
type Step struct { /* internal fields */ }

// CLI executes the configured binary with args. Fails test on non-zero exit.
// Returns *CLIResult for chainable assertions.
func (s *Step) CLI(args ...string) *CLIResult

// CLIExpectError is like CLI but does NOT fail on non-zero exit.
func (s *Step) CLIExpectError(args ...string) *CLIResult

// HTTP makes an authenticated request to tc.Server + path.
// Returns *HTTPResult for chainable assertions.
func (s *Step) HTTP(method, path string, body ...[]byte) *HTTPResult

// Log writes a scoped log message to RunLog.
func (s *Step) Log(format string, args ...any)

// WriteFile creates a file at path (relative to tc.Home) with content.
func (s *Step) WriteFile(path, content string)
```

### CLIResult (`framework/step_result.go`)

```go
// CLIResult holds CLI output. All assertion methods log to RunLog and call rl.Failf on failure.
// Methods return *CLIResult for chaining.
type CLIResult struct { /* internal fields */ }

func (r *CLIResult) Contains(substrs ...string) *CLIResult      // ALL must be present
func (r *CLIResult) ContainsAny(substrs ...string) *CLIResult   // At least one
func (r *CLIResult) NotContains(substrs ...string) *CLIResult   // None may be present
func (r *CLIResult) Matches(pattern string) *CLIResult           // Regex assertion
func (r *CLIResult) Empty() *CLIResult                           // Whitespace-only
func (r *CLIResult) ParseID(dst *string) *CLIResult              // Extract UUID
func (r *CLIResult) JSONField(field string, dst *string) *CLIResult // Top-level JSON field
func (r *CLIResult) JSON(dst any) *CLIResult                     // Full unmarshal
func (r *CLIResult) ExitCode(expected int) *CLIResult            // Assert exit code
func (r *CLIResult) Output() string                              // Raw stdout
func (r *CLIResult) StderrOutput() string                        // Raw stderr
```

### HTTPResult (`framework/step_result.go`)

```go
// HTTPResult holds HTTP response. All assertion methods log to RunLog and call rl.Failf on failure.
// Methods return *HTTPResult for chaining.
type HTTPResult struct { /* internal fields */ }

func (r *HTTPResult) Status(expected int) *HTTPResult                    // Assert status code
func (r *HTTPResult) JSONField(field string, dst *string) *HTTPResult    // Top-level JSON field
func (r *HTTPResult) JSONContains(field, expected string) *HTTPResult    // Assert field value
func (r *HTTPResult) BodyContains(substr string) *HTTPResult             // Substring in body
func (r *HTTPResult) JSON(dst any) *HTTPResult                           // Full unmarshal
func (r *HTTPResult) Body() string                                       // Raw body
func (r *HTTPResult) Header(name string) string                          // Response header
func (r *HTTPResult) StatusCode() int                                    // Raw status code
```

### Binary-parameterized CLI helpers (`framework/cli.go`)

```go
// MustRunBinaryInDirWithHome runs `<binary> <args>` with HOME=home; calls t.Fatal on non-zero exit.
func MustRunBinaryInDirWithHome(t *testing.T, binary, dir, home string, args ...string) string

// RunBinaryInDirWithHome is the non-fatal variant — returns (output, error).
func RunBinaryInDirWithHome(t *testing.T, binary, dir, home string, args ...string) (string, error)
```

### Example: Step API test

```go
func TestCLIInstalled_ProjectCRUD_StepAPI(t *testing.T) {
    tc := framework.NewTest(t, framework.TestOpts{
        Describe: "Verify project create → get → delete lifecycle",
        Bullets:  []string{"Create project", "Get by ID", "Delete"},
    })
    defer tc.Done()

    name := framework.UniqueProjectName("e2e-crud")
    var projectID string

    tc.Step("Create project", func(s *framework.Step) {
        s.CLI("projects", "create", "--name", name).
            Contains(name).
            ParseID(&projectID)
        s.CLI("config", "set", "project_id", projectID)
    })

    tc.Step("Get project by ID", func(s *framework.Step) {
        s.CLI("projects", "get", projectID).
            Contains(name).
            Contains(projectID)
    })

    tc.Step("Delete project", func(s *framework.Step) {
        s.CLI("projects", "delete", projectID).
            ContainsAny("delet", "remov", "success")
    })
}
```

Thin wrappers in `helpers_test.go`:
- `type testOpts = framework.TestOpts`
- `type testContext = framework.TestContext`
- `type step = framework.Step`
- `newTest(t, opts)` → `framework.NewTest(t, opts)`
