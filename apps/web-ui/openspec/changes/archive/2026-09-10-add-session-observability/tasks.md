## 1. Memory usage client

- [x] 1.1 Add `UsageSummaryRow`, `UsageTimeSeriesRow`, and their response wrappers plus `MemoryClient.GetProjectUsageSummary` and `MemoryClient.GetProjectUsageTimeSeries` methods in `gateway/memory.go` (calling `/projects/{projectId}/usage` and `/projects/{projectId}/usage/timeseries?granularity=day`). Verify `go build ./...` from `gateway/`.
- [x] 1.2 Add a unit test using an `httptest` server returning canned usage JSON, asserting the client decodes summary rows and time-series rows correctly and passes `since`/`until`/`granularity` query params. Verify `go test ./... -run Usage -count=1` from `gateway/`.

## 2. Usage dashboard

- [x] 2.1 Add `GET /usage` route and handler in `gateway/main.go` that fetches project usage summary + time series (daily) and `ListConversations`, computes sessions-per-day for the selected range, and renders the page. Verify `go build ./...` from `gateway/`.
- [x] 2.2 Add the usage dashboard templ page: summary cards (total tokens, current-month spend when available), daily token chart, sessions-per-day chart, per-model usage table, and a time-range selector (7/30/90 days). Verify `templ generate` and `go build ./...`.
- [x] 2.3 Add a small vanilla-JS/SVG chart helper (bar + line) served from the gateway static assets, consuming the time-series JSON embedded as a data payload. Verify `go build ./...` and a browser smoke render of `/usage`.
- [x] 2.4 Add a handler unit test with a stubbed Memory client returning canned summary/time-series/conversation data, asserting the page contains the totals, chart data payload, and per-model rows. Verify `go test ./... -run Usage -count=1`.
- [x] 2.5 Add unit tests for the empty state (no usage rows) and the backend-failure state (client error → error message, no crash). Verify `go test ./... -count=1`.

## 3. Session trace/log viewer

- [x] 3.1 Add `GET /sessions` and `GET /sessions/:id` routes and handlers in `gateway/main.go`: list via `ListConversations`, detail via the existing conversation dump (`getConversationDump` JSON path). Verify `go build ./...`.
- [x] 3.2 Add templ pages for the session list (most recent first, linking to detail) and the session timeline (turns and tool calls with name/args/output/error, per-run usage where present). Verify `templ generate` and `go build ./...`.
- [x] 3.3 Add unit tests for session-list rendering, timeline rendering (turns + tool-call details), unknown-session (empty timeline, not error), and message-only sessions. Verify `go test ./... -run Session -count=1`.
- [x] 3.4 Add unit tests covering per-run usage present (input/output tokens + cost shown) and absent (usage omitted, view still renders). Verify `go test ./... -count=1`.

## 4. Navigation

- [x] 4.1 Add Usage and Sessions links to the gateway sidepanel/nav (alongside the existing Dashboard/Sessions nav). Verify `go build ./...` and a browser check that both links resolve to their pages.

## 5. Verification

- [x] 5.1 Run `go build ./...` and `go vet ./...` from `gateway/`; all pass.
- [x] 5.2 Run `templ generate` and `task lint` (golangci-lint); no new issues.
- [x] 5.3 Run the full unit test suite `go test ./... -count=1` from `gateway/`; all pass.
- [x] 5.4 Restart the dev server (`task dev`) and manually verify in the browser: `/usage` shows charts and totals, `/sessions` lists sessions, and a session detail shows its timeline and tool calls.
## 6. Run trace waterfall (trace enrichment)

- [x] 6.1 Add `MemoryClient.GetAgentRun` (GET `/api/projects/{projectID}/agent-runs/{runID}`) returning `AgentRun` `{TraceID, Spans []TraceSpan}` — memory now returns the run's flattened spans inline in the DTO (`spanId`/`parentSpanId`/`name`/`startUnixNano`/`endUnixNano`/`inputTokens`/`outputTokens`/`model`), so no separate `/api/traces/:id` call and no OTLP parsing. Verify `go build ./...` from `gateway/`.
- [x] 6.2 Enrich the session detail handler (`gateway/sessions.go` `uiSession`): group timeline items by run, then for each run with a `run_id` fetch the run and attach `run.Spans` to the run group. A failed run fetch or a run with empty/nil spans must not break the page (no trace section for that run). Verify `go build ./...`.
- [x] 6.3 Render each run's trace as a collapsible waterfall inside the run group (`gateway/sessions.templ`): spans indented by depth computed from `parentSpanId` (root = 0, child = parent + 1, orphan = 1), each showing operation name and duration (`1.23s` / `45ms`), plus `input → output` token counts on `call_llm` spans from the span's `InputTokens`/`OutputTokens` fields. Runs without spans render nothing. Verify `templ generate` and `go build ./...`.
- [x] 6.4 Add client unit tests (agent-run path + decode of the inline `spans` array incl. int64 times/tokens/model) and handler tests asserting the waterfall renders (span name, duration, tokens, indentation), runs without spans render no section, and agent-run fetch errors degrade gracefully. Verify `go test ./... -run 'Session|Trace|AgentRun' -count=1`.

