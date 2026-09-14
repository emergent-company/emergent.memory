## Why

After running an experiment (a batch of e2e test runs grouped by the `EXPERIMENT` label), engineers must manually read through run results, token summaries, trace spans, and failure logs to identify what went wrong and what to improve. This is tedious and error-prone, especially as experiments grow to dozens of runs. An LLM-powered analysis step would automatically surface patterns, root causes, and actionable improvement suggestions.

## What Changes

- New `experiment_suggestions` table in `logs/runs.db` storing structured LLM-generated suggestions per experiment
- New `framework/analyzer.go` package providing an ADK-based LLM agent with tools to read experiment data (runs, events, trace spans, DB logs) and write suggestions back to the DB
- New `cmd/analyze/main.go` standalone binary — `./analyze <experiment-name>` — for CLI/CI use
- Extended `cmd/runlog/main.go` TUI with:
  - New `viewExpAnalysis` view state (navigable suggestion list + detail drawer)
  - `'a'` key in `viewExpRuns` and `viewExperiments` to trigger analysis (async, with spinner)
  - New `suggestionsLoadedMsg` and `analyzeStartedMsg` message types
- New `framework/db.go` helpers: `InsertSuggestion`, `ListSuggestions`, `DeleteSuggestions`

## Capabilities

### New Capabilities

- `experiment-analyzer`: LLM agent (Google ADK Go) that reads an experiment's runs, events, trace spans, and structured logs, then creates prioritized improvement suggestions stored in SQLite and surfaced in the TUI's Experiments view.

### Modified Capabilities

- `e2e-framework`: New DB migration (migration 5) adding `experiment_suggestions` table; new read/write helpers on `RunDB`.

## Impact

- **`framework/db.go`**: Migration 5 + `InsertSuggestion` / `ListSuggestions` / `DeleteSuggestions`
- **`framework/analyzer.go`** (new): ADK LlmAgent, 5 tools, credential wiring from `GOOGLE_AI_API_KEY`
- **`cmd/analyze/main.go`** (new): standalone binary in existing cmd/ layout
- **`cmd/runlog/main.go`**: new view state, key handlers, render function, async messaging
- **`go.mod`**: adds `google.golang.org/adk` dependency (v0.3.0 already present in the module cache)
- No API changes; no Docker changes; no breaking changes to existing test helpers
