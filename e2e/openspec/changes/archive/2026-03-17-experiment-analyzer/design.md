## Context

The e2e test suite records every test run in a SQLite DB (`logs/runs.db`). Runs can be grouped under an `experiment` string label — multiple runs of the same test with different parameters (model, blueprint, etc.) form an experiment batch. The TUI (`cmd/runlog`) has an "Experiments" tab showing these batches.

Currently, analysis is entirely manual: engineers visually scan pass/fail ratios, token summaries, and individual trace spans to identify patterns. As experiment sizes grow (10–50 runs per batch), this becomes impractical.

The Memory server (`emergent.memory`) already uses Google ADK Go (`google.golang.org/adk v0.3.0`) with `llmagent.New` + `runner.New` + `functiontool.New` for its agent execution pipeline. The same SDK is available to this repo. The `GOOGLE_AI_API_KEY` env var is already present in `.env`.

## Goals / Non-Goals

**Goals:**
- LLM agent reads all experiment data (runs, events, trace spans, structured DB logs) and produces structured improvement suggestions stored in SQLite
- `'a'` key in the TUI Experiments view triggers analysis asynchronously, transitions to `viewExpAnalysis` on completion
- Standalone `cmd/analyze` binary for CI / post-batch scripting
- Suggestions are structured: title, body, category, priority, evidencing run IDs
- New `experiment_suggestions` table with proper migration (migration 5)

**Non-Goals:**
- No changes to how tests run or record events
- No server-side component — everything is local SQLite + local LLM call
- No suggestion lifecycle management (accept/reject/act) in this change
- No multi-model comparison or prompt tuning in this change
- Not streaming the LLM response into the TUI in real-time (async batch result only)

## Decisions

### 1. Use ADK v0.4.0 (upgrade from v0.3.0)

ADK v0.4.0 is already in the module cache (`/root/go/pkg/mod/google.golang.org/adk@v0.4.0`). The `functiontool.New[TArgs, TResults]` generic API is cleaner and has better schema inference than v0.3.0. The memory server uses v0.3.0 but this is a separate module — no conflict.

**Alternative considered**: stay on v0.3.0. Rejected because v0.4.0 is available locally and `functiontool.New` makes tool registration significantly simpler.

### 2. Single `Analyzer` struct in `framework/analyzer.go`

All agent logic — credential wiring, tool registration, DB read, suggestion write — lives in one exported struct `Analyzer` with a single `Run(ctx, experimentName) ([]Suggestion, error)` method. Both the TUI and the CLI binary import this.

**Alternative considered**: separate package `framework/analyzer/`. Rejected — over-engineering for a single file; the e2e framework package is already a flat set of files.

### 3. Tools the agent receives

| Tool | Input | Output |
|---|---|---|
| `get_experiment_summary` | `experiment: string` | `ExperimentSummary` JSON |
| `list_runs` | `experiment: string` | `[]RunRow` JSON (id, name, started_at, passed, tags, description, token_summary) |
| `get_run_events` | `run_id: int64` | `[]EventRow` JSON — all events including `trace_span`, `metric`, `token_summary`, `gantt`, `log` |
| `get_run_log` | `run_id: int64` | flat string — the chronological `run.log` file content for that run (reads from `logs/<timestamp>-<TestName>/run.log` via path stored as a tag or reconstructed from started_at + test_name) |
| `create_suggestion` | `experiment, title, body, category, priority string, run_ids []int64` | confirmation string |

The `get_run_log` tool gives access to the raw flat log file — the "trails and DB logs" the user explicitly requested. It reads the most recent matching log directory from disk.

**Alternative considered**: only expose DB-structured data. Rejected — raw flat logs often contain error messages and stack traces not captured in structured events.

### 4. `experiment_suggestions` table (migration 5)

```sql
CREATE TABLE experiment_suggestions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    experiment   TEXT    NOT NULL,
    generated_at TEXT    NOT NULL,   -- RFC3339
    category     TEXT,               -- "model" | "retry" | "prompt" | "infra" | "cost" | "other"
    priority     TEXT,               -- "high" | "medium" | "low"
    title        TEXT    NOT NULL,   -- one-liner for list view
    body         TEXT    NOT NULL,   -- full markdown text for drawer
    run_ids      TEXT                -- JSON array of int64 run IDs that evidence this suggestion
);
CREATE INDEX IF NOT EXISTS idx_exp_suggestions_experiment ON experiment_suggestions(experiment);
```

Added as migration 5 in existing `migrations` slice — auto-applied at `SharedDB()` open time, idempotent.

**Alternative considered**: store suggestions as `run_events` with `kind="suggestion"`. Rejected — suggestions belong to an experiment, not a single run; the run_events table is run-scoped.

### 5. TUI: async goroutine + `tea.Cmd` message passing

When `'a'` is pressed, the TUI immediately shows a spinner ("Analyzing experiment…") and fires a `tea.Cmd` that runs `Analyzer.Run` in a goroutine. On completion it sends `analysisCompleteMsg{suggestions, err}` which transitions the model to `viewExpAnalysis`.

`viewExpAnalysis` is a new `viewState` with: left panel = suggestion list (title + priority badge), right drawer = full body + category + generated_at + run_ids.

**Alternative considered**: block the TUI event loop. Rejected — analysis may take 10–30 seconds; blocking the TUI is not acceptable.

### 6. Credential source: `GOOGLE_AI_API_KEY` env var only

This repo has no DB credential hierarchy (that's the server's domain). The analyzer reads `GOOGLE_AI_API_KEY` directly from the environment (loaded from `.env` by `framework.LoadDotEnv` which is called in `TestMain`). The standalone binary calls `framework.LoadDotEnv` explicitly before creating the analyzer.

Model is configurable via `ANALYZER_MODEL` env var; defaults to `gemini-2.0-flash`.

**Alternative considered**: support Vertex AI. Deferred — `GOOGLE_AI_API_KEY` covers the current test environment; Vertex can be added later without breaking changes.

## Risks / Trade-offs

- **LLM latency** (10–30s per analysis) → Mitigated by async TUI execution + spinner; analysis result is cached in DB so repeat views are instant.
- **Token cost** — a large experiment (50 runs × 200 events each) could be expensive → `get_run_events` truncates events to top-100 per run and trace_span events to top-50; raw log files are capped at 50KB.
- **ADK session persistence** — the analyzer uses `memory.NewInMemory()` session store (no DB), since analysis is a single-shot interaction; no session cleanup needed.
- **Flat log path resolution** — `get_run_log` reconstructs the log dir from `started_at` + `test_name` glob; if logs are on a different machine this returns empty string. Graceful: tool returns `"(log file not available)"` rather than erroring.
- **go.mod churn** — adding `google.golang.org/adk v0.4.0` and its transitive deps will significantly expand `go.sum`. Acceptable since the module cache already has all required packages.

## Migration Plan

1. Add migration 5 to `framework/db.go` migrations slice — automatically applied at first DB open after deploy.
2. No rollback needed — if the column/table doesn't exist old TUI versions simply won't show the Analysis view (the new `viewExpAnalysis` state is unreachable without the new binary).
3. Existing `logs/runs.db` files will be migrated on first open.

## Open Questions

- Should `create_suggestion` replace all existing suggestions for the experiment (re-run analysis = fresh slate) or append? → **Replace**: delete existing suggestions for the experiment before the agent run, so re-running always gives a clean result. The agent can create as many as it needs.
- Should the standalone `cmd/analyze` binary print suggestions as JSON (for piping) or human-readable? → **Both**: `--json` flag for JSON output, default is human-readable table.
