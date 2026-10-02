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

### D2 — Feedback row is keyed to (message_id, user_id)

`kb.answer_feedback`:

```
id          UUID PK
project_id  UUID NOT NULL
message_id  UUID NOT NULL          -- FK kb.chat_messages
trace_id    UUID                   -- FK kb.retrieval_traces (nullable)
user_id     UUID NOT NULL          -- submitting user
thumbs      SMALLINT               -- 1 = up, -1 = down
comment     TEXT
created_at  TIMESTAMPTZ NOT NULL
updated_at  TIMESTAMPTZ NOT NULL
UNIQUE (message_id, user_id)
```

The unique constraint makes submit idempotent: an upsert on `(message_id, user_id)`.
Clearing a vote deletes the row. This mirrors Onyx's one-vote-per-user-per-message
semantics and keeps aggregation a `COUNT ... GROUP BY` over a small table rather than a
delta/revision reconstruction.

### D3 — Aggregation by model/config needs the answer's provenance

A thumbs-down is only useful for eval if we know *which model and config* produced the
answer. The chat message already has model provenance (or can be resolved via the run
that produced it); the retrieval trace has the embedding/config used. Aggregation
queries join `answer_feedback` → `chat_messages` (model) and → `retrieval_traces`
(embedding config). If chat-message model provenance is incomplete, the aggregation
reports `NULL` model rather than failing — the requirement is queryable-by-model/config,
not guaranteed-non-null for legacy rows.

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
