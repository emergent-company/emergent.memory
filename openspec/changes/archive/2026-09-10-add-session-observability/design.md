## Context

See proposal.md - Why. Key constraints that shape the approach:

- The gateway is a Go web app (echo + templ + tailwind/daisyUI) that talks to the Memory backend over HTTP only — it has no direct DB access. It authenticates to Memory with a project-bound `emt_*` token and a known `MEMORY_PROJECT_ID`.
- Memory already records and aggregates LLM usage. The relevant HTTP endpoints (confirmed in `apps/server/domain/provider/handler.go`):
  - `GET /projects/{projectId}/usage?since&until` → `{note, data: [{provider, model, total_text, total_image, total_video, total_audio, total_output, total_cached, estimated_cost_usd}]}` (grouped by provider+model).
  - `GET /projects/{projectId}/usage/timeseries?granularity=day|week|month&since&until` → `{note, data: [{period, provider, model, total_*, estimated_cost_usd}]}`.
  - User-scoped variants: `GET /users/me/usage`, `GET /users/me/usage/timeseries`.
  - All responses carry a `note` stating costs are retail-pricing estimates.
- Session logs are already exposed read-only by the gateway at `GET /api/conversations/:id/dump?format=json` → `{meta, timeline[]}`, where timeline items include turns and tool calls with `tool_name`, `tool_input`, `tool_output`, `tool_status` (`gateway/session_dump.go`). The gateway also lists sessions via `ListConversations` (`GET /api/conversations`).
- The gateway has no chart library today.

## Goals / Non-Goals

**Goals:**
- Surface token usage (daily, per-model, total, spend) and session activity in the gateway UI, backed entirely by Memory's existing HTTP APIs — no backend changes.
- Provide a session trace/log viewer over the existing dump endpoint.

**Non-Goals:**
- No changes to the Memory backend (no new usage recording, no new aggregation endpoints).
- No org-level multi-project scoping in this change — project-scoped usage only.
- No cost-management features (budget editing, alerts). The existing budget-alert behavior in Memory is untouched.
- No changes to the iOS app or the bridge worker.

## Decisions

1. **Proxy Memory's usage endpoints rather than re-aggregate.**
   The gateway gains `MemoryClient` methods for `GetProjectUsageSummary` and `GetProjectUsageTimeSeries` (and `GetProjectCurrentMonthSpend` if a route exists) that call `/projects/{projectId}/usage` and `/projects/{projectId}/usage/timeseries`. Rationale: Memory already computes grouping and time bucketing; the gateway cannot do it without DB access. Alternative considered: replicating aggregation in the gateway — rejected, no DB access and duplicates logic.

2. **Derive "sessions created per day" from `ListConversations`, not a new endpoint.**
   Memory's usage API reports tokens, not session counts. The gateway already fetches conversations; it groups their `createdAt` by day for the selected range. Rationale: reuses an existing call; no backend change. Alternative: a new Memory metric endpoint — rejected as out of scope for a backend we are not changing here.

3. **Chart rendering with no new dependency.**
   Charts are drawn with a small vanilla-JS/SVG helper served from the existing static/JS assets; the templ page embeds the time-series JSON as a `data-*` payload. Rationale: the gateway has no chart library, data volume is tiny (≤ 90 points), and this keeps the build (templ + tailwind + go) unchanged. Alternative: add chart.js — rejected (vendor/versioning weight for a small fixed set of charts).

4. **Two new UI surfaces following the existing agent-dashboard pattern.**
   - `/usage` — usage dashboard (summary cards + daily tokens chart + sessions chart + per-model table + time-range selector).
   - `/sessions` — session list; `/sessions/:id` — timeline viewer (turns, tool calls, per-run usage).
   Both render server-side via templ with handlers mirroring `agent.templ`/`handlers.go`, and both get a nav link in the existing sidepanel. Rationale: consistent with the current single-page-per-concern structure and reuse of the existing Memory client + error-state patterns.

5. **Session viewer builds on the dump endpoint and adds per-run usage.**
   Reuse `getConversationDump`'s typed timeline (`TimelineItem`) and `toolError` detection for the viewer; render turns and tool calls (name, args, output, error) directly. Per-run token usage is surfaced from Memory's run-usage data (agents SDK `RunTokenUsage{totalInputTokens, totalOutputTokens, estimatedCostUsd}`) when the conversation/run API exposes it; otherwise the viewer omits usage per the spec's "usage absent" scenario. The `note` about estimate pricing is shown where spend is displayed.

## Risks / Trade-offs

- **Per-run usage endpoint uncertainty** → the exact Memory run/usage endpoint is confirmed during implementation by reading `apps/server` route registration; if unavailable, the viewer gracefully omits usage (spec already permits this). No spec change needed.
- **Usage endpoints may 403 on non-project tokens** → the gateway's project-bound `emt_*` token is the expected caller; handler already resolves org context for project tokens. Verified against `assertCallerOwnsProject`.
- **Time-series granularity mismatch with the range selector** → the dashboard pins `granularity=day` and lets the selector change `since`/`until` only, keeping the chart stable.
- **Cost figures are estimates** → Memory returns a `note`; the UI always displays it with spend to avoid implying invoice accuracy.
- **No chart library means custom SVG code** → scoped to a single small JS helper with unit-tested Go-side payload shaping; the rendering itself is simple bars/lines.

## Migration Plan

1. Add `MemoryClient` usage methods + structs (`gateway/memory.go`).
2. Add routes + handlers (`gateway/main.go`, new `usage.go` / `sessions.go`).
3. Add templ pages + static chart JS.
4. Add nav links.
5. Deploy is a normal gateway redeploy (`task dev` / docker). No data or schema migration. Rollback = revert the commit.

## Open Questions

- Whether to also surface per-user usage (`/users/me/usage`) vs project-only — deferred; project-only is the initial scope and the spec does not require user scoping.
