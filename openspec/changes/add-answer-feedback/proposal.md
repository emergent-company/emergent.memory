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
  carrying the chat message id, the retrieval/query trace id it refers to, a thumbs
  up/down, an optional free-text comment, the submitting user, and the project.
- Extend the **existing** persisted `RetrievalTrace` with a user id and a stable
  query key rather than building a parallel query-log store; expose a read path for
  search-query history (`TraceStore` already has `GetByTraceID` — add a
  list-by-project/user path).
- Add API endpoints to submit feedback (idempotent per message+user) and to
  list/aggregate feedback counts (by time, by model, by embedding/model config).
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
  and the project, and make it queryable for retrieval-quality evaluation.

### Modified Capabilities

- `retrieval-trace-persistence`: the persisted trace gains user attribution and a stable
  query key, and a search-query history read path.
- `usage-dashboard`: (conceptual) the dashboard gains a quality/feedback aggregation
  surface in a follow-on lane.

## Impact

- **DB** (`apps/server/migrations/`): new migration creating `kb.answer_feedback`
  (unique on `(message_id, user_id)` for idempotent submit; FKs to `kb.chat_messages`
  and `kb.retrieval_traces`; indexes on `(project_id, created_at)` and
  `(model, config)` for aggregation).
- **Server** (`apps/server/domain/`): new `answerfeedback` domain
  (`store.go`/`service.go`/`handler.go`/`module.go`) or an extension of
  `domain/monitoring`; `domain/search/trace_store.go` gains `user_id`/`query_key`
  columns and a list query.
- **Gateway** (`apps/web-ui/gateway/`): `usage.templ` + `usage.go` feedback aggregation
  (follow-on lane, documented only).
- **No reuse of `kb.work_item_feedback`** — that table is append-only rework rounds for
  object-driven work, a different signal.
