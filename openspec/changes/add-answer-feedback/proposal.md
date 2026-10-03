## Why

Memory records how much model usage costs (`kb.llm_usage_events`, tracked in
`domain/provider` and surfaced by the usage dashboard at
`apps/web-ui/gateway/usage.templ`), but it records **nothing about answer quality**.
There is no chat thumbs-up/thumbs-down, and there is no history of what the user
actually searched for. Without a quality signal tied to the retrieval and the model
that produced an answer, retrieval-quality evaluation is impossible: we cannot tell
whether a thumbs-down correlates with a bad chunk, a bad model, or a bad config.

Onyx ships exactly this — per-message answer feedback plus a search-query history —
precisely because it is the minimum signal needed to evaluate retrieval quality.
This change adds that signal to Memory without inventing a parallel store: the
retrieval trace already exists (`domain/search/trace_store.go`, `RetrievalTrace`,
spec `retrieval-trace-persistence`) and is the natural home for the "what did we
retrieve for this query" half of the signal.

## What Changes

- Add `kb.answer_feedback`: one row per (chat message/response) feedback action,
  carrying the chat message id, the agent `run_id` (nullable, for model attribution),
  the retrieval trace id (nullable), a thumbs up/down (`SMALLINT CHECK (thumbs IN
  (-1,1))`), an optional free-text comment, the submitting user, and the project.
- Add nullable `kb.chat_messages.retrieval_trace_id` (FK to `kb.retrieval_traces.trace_id`,
  the public trace id already returned in the search response `TraceID`,
  `domain/search/service.go:177-180`) and `kb.chat_messages.run_id` (from the producing
  agent run), both stamped server-side at chat time; the trace is persisted synchronously
  before the message so the FK is satisfied. Feedback then links back to the retrieval and
  the producing run without a separate store or an indirect session join.
- Extend the **existing** persisted `RetrievalTrace` with a user id (no query-key
  column) rather than building a parallel query-log store; expose a read path for
  search-query history (`TraceStore` already has `GetByTraceID` — add a
  list-by-project/user path).
- Add API endpoints to submit feedback (idempotent per message+user) and to
  list/aggregate feedback counts (by time, by model, by search-fusion config).
- Reuse `kb.chat_messages` / `domain/chat` for message identity — do **not** build a
  new feedback subsystem. (Work-item rework feedback already exists separately at
  `migrations/00209_work_item_feedback.sql` and is untouched.)
- Web-UI surface (a thumbs up/down on chat responses, and a feedback/aggregation view
  on the usage dashboard) is in scope conceptually but is a follow-on lane; noted
  here, not implemented in this change.

## Capabilities

### New Capabilities

- `answer-feedback`: capture, persist, and aggregate per-answer quality signal (thumbs
  up/down + optional comment) keyed to the chat message, the retrieval trace, the user,
  and the project, and make it queryable by model and by search-fusion config for
  retrieval-quality evaluation.

### Modified Capabilities

- `retrieval-trace-persistence`: the persisted trace gains user attribution and a
  search-query history read path.

### Related / Consumed Capabilities

- `usage-dashboard`: (conceptual) the dashboard gains a quality/feedback aggregation
  surface in a follow-on lane. No delta written here.

## Impact

- **DB** (`apps/server/migrations/`): new migration creating `kb.answer_feedback`
  (unique on `(message_id, user_id)` for idempotent submit; FKs to `kb.chat_messages(id)`,
  `kb.agent_runs(id)`, `kb.retrieval_traces(trace_id)`, `core.user_profiles(id)`; `CHECK
  (thumbs IN (-1,1))`; indexes on `(project_id, created_at)` and `(user_id, created_at)`).
  Also nullable `retrieval_trace_id` and `run_id` columns on `kb.chat_messages`, plus a
  UNIQUE constraint on `kb.retrieval_traces.trace_id` (the public trace id).
- **Server** (`apps/server/domain/`): new `answerfeedback` domain
  (`store.go`/`service.go`/`handler.go`/`module.go`) or an extension of
  `domain/monitoring`; `domain/search/trace_store.go` gains a `user_id` column and a list
  query; `domain/chat` gains `retrieval_trace_id` + `run_id`.
- **Gateway** (`apps/web-ui/gateway/`): `usage.templ` + `usage.go` feedback aggregation
  (follow-on lane, documented only).
- **No reuse of `kb.work_item_feedback`** — that table is append-only rework rounds for
  object-driven work, a different signal.
