## Why

Memory already records rich observability data — per-run LLM token usage (`kb.llm_usage_events`), per-session `total_tokens`, and session logs/timelines — but none of it is reachable from the gateway web UI. Users must drop to the CLI (session dumps, `/tmp/alfred-trace` JSONL) to see what an agent did or how many tokens were spent. Expose this data in the UI so usage and session activity can be reviewed at a glance, the way model providers surface LLM usage.

## What Changes

- Add a gateway **Usage dashboard** page showing daily token consumption, sessions created per day, total tokens consumed, and current-month spend — backed by Memory's existing provider usage summary + time-series API (proxied through the gateway), with charts like a typical LLM-provider usage view.
- Add a gateway **Session trace/log viewer**: browse recorded sessions, then inspect one session's timeline (user/assistant turns, tool calls with args/output/error) and its per-run token usage.
- Extend the gateway's Memory REST client to call the provider usage endpoints (project usage summary, time series with daily granularity) and the per-run token usage data.
- Surface per-session token usage (run-level input/output tokens + estimated cost) in the session viewer.
- Add navigation from the existing dashboard/sidepanel to the new Usage and Sessions observability pages.

No changes to the Memory backend are required — usage recording and aggregation already exist there.

## Capabilities

### New Capabilities
- `usage-dashboard`: a gateway web UI that displays token usage over time (daily tokens, sessions created, total tokens, spend) and per-session usage, backed by Memory's usage-summary and usage-time-series APIs.
- `session-trace-viewer`: a gateway web UI that lists recorded sessions and shows a session's timeline (turns, tool calls with arguments/output/error) and per-run token usage.

### Modified Capabilities
<!-- none -->

## Impact

- **Gateway** (Go, echo + templ + tailwind/daisyUI): new routes, handlers, templ pages, and Memory client methods (`gateway/main.go`, `gateway/handlers.go`, `gateway/memory.go`).
- **Memory REST client**: add usage-summary/time-series calls (`gateway/memory.go`).
- **Charting**: lightweight client-side chart rendering; no existing chart library is present.
- **Specs**: new `usage-dashboard` and `session-trace-viewer` capability specs.
