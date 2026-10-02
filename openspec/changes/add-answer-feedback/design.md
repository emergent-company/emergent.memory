## Context

Two halves of a quality signal already exist but are disjoint: (1) the persisted
retrieval trace (`domain/search/trace_store.go`, `RetrievalTrace`, spec
`retrieval-trace-persistence`) records *what was retrieved*, and (2) `domain/chat`
records *what was answered*. What is missing is the user's verdict on the answer and a
link from the verdict back to the retrieval. Onyx's answer-feedback + search-query
history supplies exactly this link.

## Goals / Non-Goals

**Goals**

- A thumbs up/down + optional comment per chat message, idempotent per (message, user).
- Trace-level user attribution and a stable query key, so "what did this user search and
  what did we retrieve" is answerable without a parallel store.
- Aggregable, filterable feedback for retrieval-quality evaluation (by model, by config).
- Reuse `domain/chat` and the existing trace store; no new rework subsystem.

**Non-Goals**

- The web-UI thumbs widget and dashboard aggregation view (follow-on lane).
- Any scoring or auto-tuning loop that consumes the feedback — this change only
  produces the signal.
- Touching `kb.work_item_feedback` (append-only rework rounds; a different signal).

## Decisions

### D1 — Extend the trace, do not build a query-log store

A search-query history could be a new `kb.search_queries` table. That is rejected:
the trace already persists every query with its filters and results, so a parallel
query log would duplicate data and drift from what actually ran. Instead the existing
`RetrievalTrace` gains `user_id` (nullable — some searches are unauthenticated) and a
`query_key` (a deterministic normalized form of the raw query, e.g. lowercased and
whitespace-collapsed). The read path is a new `ListByProject`/`ListByUser` on
`TraceStore`, filtering on the existing `project_id` and new `user_id`.

`SearchContext` (`domain/search/dto.go`) currently carries only `{OrgID, ProjectID,
Scopes}` — no user. This change adds `UserID uuid.UUID` to `SearchContext` and populates
it in `domain/search/handler.go` (`Search` already holds `user := auth.MustGetUser(c)`,
so `UserID` is set from that user). The service then reads `searchCtx.UserID` to stamp
the trace. Traces written by unauthenticated/internal paths leave `UserID` as zero/NULL.

### D2 — Feedback row is keyed to (message_id, user_id)

`kb.answer_feedback`:

```
id                  UUID PK
project_id          UUID NOT NULL
message_id          UUID NOT NULL          -- FK kb.chat_messages (id)
retrieval_trace_id  UUID                   -- FK kb.retrieval_traces (id) (nullable)
user_id             UUID NOT NULL          -- submitting user
thumbs              SMALLINT               -- 1 = up, -1 = down
comment             TEXT
created_at          TIMESTAMPTZ NOT NULL
updated_at          TIMESTAMPTZ NOT NULL
UNIQUE (message_id, user_id)
```

Note the FK is pinned to the trace row's **primary key** `id`, not the `trace_id`
column — `RetrievalTrace` (`trace_store.go:22-24`) has both a PK `id` and a separate
`trace_id` column, so the feedback column is named `retrieval_trace_id` and references
`kb.retrieval_traces(id)`. The feedback row's own `created_at` is independent of the
trace's bounded retention/TTL, so a thumbs-down remains queryable even after its trace
row expires.

The unique constraint makes submit idempotent: an upsert on `(message_id, user_id)`.
Clearing a vote deletes the row. This mirrors Onyx's one-vote-per-user-per-message
semantics and keeps aggregation a `COUNT ... GROUP BY` over a small table rather than a
delta/revision reconstruction.

### D3 — Aggregation by model/config uses real provenance

`domain/chat/entity.go` `Message` carries only `{ID, ConversationID, Role, Content,
Citations, CreatedAt, ContextSummary, RetrievalContext}` — there is **no** model, run, or
trace provenance on the chat message. So model/config aggregation cannot join
`answer_feedback → chat_messages (model)`. Real provenance is:

- **model** comes from `kb.llm_usage_events` (which already records model/provider and
  run linkage) or, when available, the agent run that produced the answer;
- **embedding/config** context comes from joining `answer_feedback →
  kb.retrieval_traces` (via `retrieval_trace_id`).

Aggregation therefore joins `answer_feedback` → `retrieval_traces` for the embedding
config, and → `llm_usage_events` (or the agent run) for the model. Where provenance is
missing (legacy rows, no trace, no usage event), the aggregation reports `NULL` for that
dimension rather than failing — the requirement is queryable-by-model/config, not
guaranteed-non-null for legacy rows.

### D4 — New small domain, not a monitoring extension

Feedback is a small CRUD + aggregation surface. A dedicated `domain/answerfeedback`
package (`store.go`/`service.go`/`handler.go`/`module.go`, fx-wired) keeps the
aggregation SQL and the chat/trace joins out of the already-large `domain/monitoring`.
The handler validates project access via the existing membership path.

## Risks / Trade-offs

- **Null user on unauthenticated searches.** Traces from internal/system searches may
  have `user_id = NULL`; query-history-by-user simply excludes them. Documented, not a
  failure.
- **query_key collisions.** Normalization can fold distinct-but-equivalent queries into
  one key; acceptable because the key is a grouping hint, and the full raw query is
  always retained on the trace.
- **Trace retention deletes query history.** The trace TTL (bounded retention) will
  eventually expire old queries, so search-query history is not immutable audit. That is
  consistent with `retrieval-trace-persistence`; long-term audit is out of scope.
