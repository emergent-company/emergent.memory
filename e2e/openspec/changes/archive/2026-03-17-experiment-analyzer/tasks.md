## 1. DB: Migration 5 and RunDB helpers

- [x] 1.1 Add migration 5 to the `migrations` slice in `framework/db.go` — creates `experiment_suggestions` table with all columns and index on `experiment`
- [x] 1.2 Add `SuggestionRow` struct to `framework/db.go` with fields: `ID int64`, `Experiment string`, `GeneratedAt time.Time`, `Category string`, `Priority string`, `Title string`, `Body string`, `RunIDs []int64`
- [x] 1.3 Implement `InsertSuggestion(experiment, title, body, category, priority string, runIDs []int64) (int64, error)` on `RunDB` — serialises `runIDs` as JSON, inserts row, returns last insert ID
- [x] 1.4 Implement `ListSuggestions(experiment string) ([]SuggestionRow, error)` on `RunDB` — queries `experiment_suggestions` ordered by `id ASC`, decodes `run_ids` JSON column
- [x] 1.5 Implement `DeleteSuggestions(experiment string) error` on `RunDB` — deletes all rows where `experiment = ?`

## 2. go.mod: Add ADK v0.4.0 dependency

- [x] 2.1 Run `go get google.golang.org/adk@v0.4.0` in the module root to add the dependency and update `go.mod` / `go.sum`
- [x] 2.2 Run `go mod tidy` to ensure no unused dependencies are left and all transitive deps are resolved
- [x] 2.3 Verify `go build ./...` succeeds after the dependency addition

## 3. framework/analyzer.go: Analyzer struct and ADK tools

- [x] 3.1 Create `framework/analyzer.go` — package `e2eframework` — with `Analyzer` struct holding `*RunDB` and `agent.Agent`
- [x] 3.2 Implement `NewAnalyzer(db *RunDB) (*Analyzer, error)` — reads `GOOGLE_AI_API_KEY` and `ANALYZER_MODEL` env vars, creates Gemini model via `gemini.NewModel`, returns error if API key missing
- [x] 3.3 Implement `get_experiment_summary` tool using `functiontool.New` — calls `db.ListExperiments()`, finds the named experiment, returns JSON; returns `"experiment not found: <name>"` if absent
- [x] 3.4 Implement `list_runs` tool — calls `db.ListRuns(time.Time{})`, filters by experiment name, returns JSON array of run summaries including token_summary
- [x] 3.5 Implement `get_run_events` tool — calls `db.ListEvents(runID)`, caps at 100 rows, appends `"(truncated)"` entry if needed, returns JSON array
- [x] 3.6 Implement `get_run_log` tool — resolves log directory from `run.StartedAt` + `run.TestName` glob under the logs directory, reads up to 50 KB from `run.log`, returns `"(log file not available)"` if not found
- [x] 3.7 Implement `create_suggestion` tool — validates/normalises `priority`, calls `db.InsertSuggestion`, returns `"suggestion created: <title>"`
- [x] 3.8 Implement `Analyzer.Run(ctx context.Context, experiment string) ([]SuggestionRow, error)` — calls `db.DeleteSuggestions(experiment)`, creates `runner.New` with in-memory session, runs agent with the system prompt, collects events, returns `db.ListSuggestions(experiment)`
- [x] 3.9 Write the system prompt constant: instructs the agent to analyse experiment runs, identify failure patterns, model/token/cost anomalies, trace span errors, and create specific actionable suggestions using the available tools

## 4. cmd/analyze: Standalone binary

- [x] 4.1 Create `cmd/analyze/main.go` with `main()` that parses `--json` flag and positional `<experiment>` argument, exits with usage message if argument missing
- [x] 4.2 Call `framework.LoadDotEnv()` at startup to load `.env` / named overlay
- [x] 4.3 Open `framework.SharedDB()` and call `framework.NewAnalyzer(db)` — exit with error message if either fails
- [x] 4.4 Run `analyzer.Run(ctx, experiment)` with a reasonable timeout (5 minutes)
- [x] 4.5 Print results: default = formatted table (priority | category | title); `--json` = JSON array of `SuggestionRow`
- [x] 4.6 Exit non-zero on error; exit 0 on success even if zero suggestions produced (print "no suggestions generated" message in that case)

## 5. TUI: viewExpAnalysis view state

- [x] 5.1 Add `viewExpAnalysis` constant to the `viewState` iota block in `cmd/runlog/main.go`
- [x] 5.2 Add TUI model fields: `suggestions []framework.SuggestionRow`, `suggCursor int`, `suggOffset int`, `analyzingExp bool` (spinner flag), `analyzeErr string`
- [x] 5.3 Add `analysisLoadedMsg struct{ suggestions []framework.SuggestionRow }` and `analyzeErrMsg struct{ err error }` message types
- [x] 5.4 Add `loadSuggestions(experiment string) tea.Cmd` method — reads from DB (fast, no LLM) for instant cached display
- [x] 5.5 Add `runAnalysis(experiment string) tea.Cmd` method — runs `Analyzer.Run` in a goroutine, sends `analysisLoadedMsg` or `analyzeErrMsg` on completion
- [x] 5.6 Handle `'a'` key in `viewExperiments` update: if suggestions exist in DB → fire `loadSuggestions` + transition to `viewExpAnalysis`; if none → fire `runAnalysis` + show spinner
- [x] 5.7 Handle `'a'` key in `viewExpRuns` update: same logic as above for `m.selectedExp`
- [x] 5.8 Handle `'A'` (shift-A) key in `viewExpAnalysis`: fire `runAnalysis` (force re-analysis), set `analyzingExp = true`
- [x] 5.9 Handle `analysisLoadedMsg` in the Update function: set `m.suggestions`, reset `m.suggCursor = 0`, set `m.analyzingExp = false`, transition to `viewExpAnalysis` if not already there
- [x] 5.10 Handle `analyzeErrMsg` in the Update function: set `m.analyzeErr`, set `m.analyzingExp = false`
- [x] 5.11 Handle `Esc` in `viewExpAnalysis`: transition to `viewExpRuns` for the current experiment
- [x] 5.12 Handle `↑`/`↓`/`j`/`k` navigation in `viewExpAnalysis` with scroll clamping (same pattern as `viewExpRuns`)
- [x] 5.13 Implement `viewExpAnalysis()` render function: title bar showing experiment name + "Analysis", left panel = suggestion list with priority badge (colour-coded: high=red, medium=yellow, low=green) + truncated title, right drawer = full body + category + generated_at + run IDs
- [x] 5.14 Show `"(no suggestions — press 'a' to analyze)"` placeholder in left panel when `m.suggestions` is empty and `!m.analyzingExp`
- [x] 5.15 Show `"Analyzing… <spinner>"` line in left panel when `m.analyzingExp` is true
- [x] 5.16 Wire `viewExpAnalysis` into `View()` switch and the refresh tick handler
- [x] 5.17 Add `'a'` hint to help bars in `viewExperiments` and `viewExpRuns`

## 6. Integration verification

- [x] 6.1 Run `go build ./...` — confirm all new code compiles cleanly
- [x] 6.2 Run `go vet ./...` — confirm no vet warnings
- [x] 6.3 Manually test the standalone binary: `GOOGLE_AI_API_KEY=... ./analyze <experiment>` (or with `--json`)
- [x] 6.4 Manually test the TUI: open `./runlog`, navigate to Experiments tab, press `'a'`, verify spinner appears and suggestions load into `viewExpAnalysis`
