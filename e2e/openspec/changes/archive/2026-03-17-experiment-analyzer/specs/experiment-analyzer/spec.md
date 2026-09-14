## ADDED Requirements

### Requirement: Analyzer creates LLM agent with experiment data tools
The `Analyzer` struct in `framework/analyzer.go` SHALL create a Google ADK `LlmAgent` (via `llmagent.New`) backed by a Gemini model configured from `GOOGLE_AI_API_KEY` (and `ANALYZER_MODEL` env var, defaulting to `gemini-2.0-flash`). The agent SHALL be given five tools: `get_experiment_summary`, `list_runs`, `get_run_events`, `get_run_log`, and `create_suggestion`.

#### Scenario: Analyzer created with valid API key
- **WHEN** `NewAnalyzer(db)` is called and `GOOGLE_AI_API_KEY` is set
- **THEN** the function returns a non-nil `*Analyzer` with no error

#### Scenario: Analyzer returns error when API key missing
- **WHEN** `NewAnalyzer(db)` is called and `GOOGLE_AI_API_KEY` is empty
- **THEN** the function returns a non-nil error describing the missing credential

### Requirement: get_experiment_summary tool returns experiment overview
The `get_experiment_summary` tool SHALL accept an `experiment` string argument and return a JSON-serialized `ExperimentSummary` (run count, pass count, fail count, last run time, union of tags).

#### Scenario: Tool returns summary for known experiment
- **WHEN** the agent calls `get_experiment_summary` with a valid experiment name
- **THEN** the tool returns a JSON object with `name`, `run_count`, `pass_count`, `fail_count`, and `last_run_at` fields

#### Scenario: Tool returns not-found message for unknown experiment
- **WHEN** the agent calls `get_experiment_summary` with an experiment name that has no runs
- **THEN** the tool returns a string `"experiment not found: <name>"`

### Requirement: list_runs tool returns all runs for an experiment
The `list_runs` tool SHALL accept an `experiment` string and return a JSON array of run summaries — each with `id`, `test_name`, `started_at`, `finished_at`, `passed`, `tags`, `description`, and `token_summary` fields.

#### Scenario: Tool returns runs array
- **WHEN** the agent calls `list_runs` with a valid experiment name
- **THEN** the tool returns a JSON array where each element has at minimum `id` and `test_name`

### Requirement: get_run_events tool returns structured events including traces
The `get_run_events` tool SHALL accept a `run_id` integer and return a JSON array of up to 100 top-level `EventRow` values from `run_events`, ordered by `seq`. Events with `kind = "trace_span"` SHALL be included so the agent can observe actual agent execution spans, timings, and tool call patterns.

#### Scenario: Tool includes trace_span events
- **WHEN** a run has `trace_span` events and the agent calls `get_run_events(run_id)`
- **THEN** the returned JSON array includes entries with `kind = "trace_span"` containing span details

#### Scenario: Tool caps at 100 events to control token cost
- **WHEN** a run has more than 100 top-level events
- **THEN** the tool returns only the first 100 events (by seq) and appends a `"(truncated)"` entry

### Requirement: get_run_log tool reads raw flat log file
The `get_run_log` tool SHALL accept a `run_id` integer, locate the corresponding flat log file on disk (under `logs/<timestamp>-<TestName>/run.log`), and return its contents as a string capped at 50 KB. This gives the agent access to error messages, stack traces, and CLI output not captured in structured events.

#### Scenario: Tool returns log content when file exists
- **WHEN** `get_run_log(run_id)` is called and the log directory exists for that run
- **THEN** the tool returns the raw text content of `run.log` (up to 50 KB)

#### Scenario: Tool returns graceful message when log unavailable
- **WHEN** `get_run_log(run_id)` is called but no matching log directory is found
- **THEN** the tool returns the string `"(log file not available)"`

#### Scenario: Tool truncates large logs
- **WHEN** the log file exceeds 50 KB
- **THEN** the tool returns the first 50 KB followed by `"\n...(truncated)"`

### Requirement: create_suggestion tool writes structured suggestion to DB
The `create_suggestion` tool SHALL accept `experiment`, `title`, `body`, `category`, `priority`, and `run_ids` (JSON array of run IDs) and insert a row into `experiment_suggestions`. It SHALL return a confirmation string `"suggestion created: <title>"`.

#### Scenario: Tool inserts suggestion row
- **WHEN** the agent calls `create_suggestion` with valid arguments
- **THEN** a row is inserted in `experiment_suggestions` with the provided fields and `generated_at` set to current UTC time

#### Scenario: Tool validates priority value
- **WHEN** the agent calls `create_suggestion` with a `priority` value not in `["high", "medium", "low"]`
- **THEN** the tool normalises it to `"medium"` rather than returning an error

### Requirement: Analyzer.Run deletes existing suggestions before analysis
Before invoking the LLM agent, `Analyzer.Run(ctx, experimentName)` SHALL delete all existing `experiment_suggestions` rows for that experiment so that re-running analysis always produces a fresh result.

#### Scenario: Existing suggestions replaced on re-run
- **WHEN** `Analyzer.Run` is called twice for the same experiment
- **THEN** only the suggestions from the second run are present in `experiment_suggestions` after the second call

### Requirement: Standalone analyze binary
A `cmd/analyze/main.go` binary SHALL accept an experiment name as its first positional argument, call `framework.LoadDotEnv`, open `SharedDB`, run `Analyzer.Run`, and print suggestions to stdout. A `--json` flag SHALL output raw JSON; default output SHALL be a human-readable table.

#### Scenario: Human-readable output
- **WHEN** `./analyze my-experiment` is run and analysis completes
- **THEN** suggestions are printed as a table with columns: priority, category, title

#### Scenario: JSON output
- **WHEN** `./analyze --json my-experiment` is run
- **THEN** suggestions are printed as a JSON array to stdout

#### Scenario: Missing API key exits with error
- **WHEN** `./analyze my-experiment` is run without `GOOGLE_AI_API_KEY` set
- **THEN** the binary exits with a non-zero code and prints an error message to stderr

### Requirement: TUI viewExpAnalysis view
The TUI SHALL add a `viewExpAnalysis` view state reachable by pressing `'a'` from `viewExperiments` or `viewExpRuns`. The view SHALL display a navigable list of suggestions (left panel: priority badge + title) with a detail drawer (right panel: body, category, generated_at, evidencing run IDs).

#### Scenario: 'a' key triggers analysis from experiments list
- **WHEN** the user presses `'a'` while in `viewExperiments`
- **THEN** an async analysis goroutine is started, a spinner line is shown, and on completion the view transitions to `viewExpAnalysis`

#### Scenario: 'a' key triggers analysis from experiment runs list
- **WHEN** the user presses `'a'` while in `viewExpRuns`
- **THEN** same behaviour as above — analysis runs for the currently selected experiment

#### Scenario: Suggestions list is navigable
- **WHEN** in `viewExpAnalysis` with at least one suggestion loaded
- **THEN** `↑`/`↓`/`j`/`k` move the cursor, the right drawer updates to show the selected suggestion's full body

#### Scenario: Esc returns to previous view
- **WHEN** the user presses `Esc` in `viewExpAnalysis`
- **THEN** the TUI transitions back to `viewExpRuns` for the same experiment

#### Scenario: No suggestions shows placeholder
- **WHEN** `viewExpAnalysis` is entered and `experiment_suggestions` has no rows for the experiment
- **THEN** the left panel shows `"(no suggestions — press 'a' to analyze)"`

### Requirement: TUI shows cached suggestions without re-running LLM
If `experiment_suggestions` already has rows for the selected experiment, pressing `'a'` SHALL first display existing suggestions immediately (loaded from DB), then offer `'A'` (shift-a) to force a fresh analysis.

#### Scenario: Cached suggestions displayed instantly
- **WHEN** suggestions exist in DB and user presses `'a'`
- **THEN** `viewExpAnalysis` is entered immediately with the cached suggestions (no LLM call)

#### Scenario: Force re-analysis with shift-A
- **WHEN** in `viewExpAnalysis` and the user presses `'A'`
- **THEN** a fresh LLM analysis is triggered (old suggestions deleted, new ones created)
