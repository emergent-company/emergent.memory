## Context

Two halves of a quality signal already exist but are disjoint: (1) the persisted
retrieval trace (`domain/search/trace_store.go`, `RetrievalTrace`, spec
`retrieval-trace-persistence`) records *what was retrieved*, and (2) `domain/chat`
records *what was answered*. What is missing is the user's verdict on the answer and a
link from the verdict back to the retrieval and the model that produced it. Onyx's
answer-feedback + search-query history supplies exactly this link.

## Goals / Non-Goals

**Goals**

- A thumbs up/down + optional comment per chat message, idempotent per (message, user).
- Trace-level user attribution, so "what did this user search and what did we retrieve"
  is answerable without a parallel store.
- Aggregable, filterable feedback for retrieval-quality evaluation (by model, by
  search-fusion config).
- Reuse `domain/chat` and the existing trace store; no new rework subsystem.

**Non-Goals**

- The web-UI thumbs widget and dashboard aggregation view (follow-on lane).
- Any scoring or auto-tuning loop that consumes the feedback — this change only
  produces the signal.
- Per-embedding-model aggregation (see D3; not available, out of scope).
- Touching `kb.work_item_feedback` (append-only rework rounds; a different signal).

## Decisions

### D1 — Extend the trace with user attribution; no query-key store

A search-query history could be a new `kb.search_queries` table. That is rejected: the
trace already persists every query (the `query` column) with its filters and results, so
a parallel query log would duplicate data and drift from what actually ran. Instead the
existing `RetrievalTrace` gains only `user_id` (nullable — some searches are
unauthenticated). There is **no** `query_key` column: the raw query is already on the
trace and has no independent consumer, so a normalized key would be dead weight. The read
path is a new `ListByProject`/`ListByUser` on `TraceStore`, filtering on the existing
`project_id` and new `user_id` (reusing the existing `idx_retrieval_traces_project` and
`idx_retrieval_traces_created` indexes from `00143`; only a new `user_id` index is added).

`SearchContext` (`domain/search/dto.go`) currently carries only `{OrgID, ProjectID,
Scopes}` — no user. This change adds `UserID uuid.UUID` to `SearchContext` and populates
it in `domain/search/handler.go` (`Search` already holds `user := auth.MustGetUser(c)`,
so `UserID` is set from that user). The service then reads `searchCtx.UserID` to stamp
the trace. Traces written by unauthenticated/internal paths leave `UserID` as zero/NULL.

### D2 — Feedback row keyed to (message_id, user_id)

`kb.answer_feedback`:

```
id                  UUID PK
project_id          UUID NOT NULL
message_id          UUID NOT NULL          -- FK kb.chat_messages (id)
run_id              UUID                   -- FK kb.agent_runs (id) ON DELETE SET NULL
retrieval_trace_id  UUID                   -- FK kb.retrieval_traces (id) ON DELETE SET NULL
user_id             UUID NOT NULL          -- FK core.user_profiles (id) (mirrors 00108)
thumbs              SMALLINT NOT NULL CHECK (thumbs IN (-1, 1))   -- -1 = down, 1 = up
comment             TEXT
created_at          TIMESTAMPTZ NOT NULL
updated_at          TIMESTAMPTZ NOT NULL
UNIQUE (message_id, user_id)
```

- `retrieval_trace_id` references `kb.retrieval_traces.trace_id` — the **public, addressable
  trace identifier** already returned in `UnifiedSearchResponse.TraceID` and used by
  `TraceStore.GetByTraceID` — NOT the surrogate primary key `id` (`RetrievalTrace` at
  `trace_store.go:22-24` has both). This makes the FK target and the response value one and
  the same identifier. `trace_id` is currently only non-uniquely indexed (`00143`
  `idx_retrieval_traces_trace`), so this change adds a UNIQUE constraint on `trace_id` to
  make it a valid FK target (see D2c).
- `user_id` mirrors `llm_usage_events.user_id → core.user_profiles(id)` (`00108`).
- The feedback row's own `created_at` is independent of the trace's bounded retention/TTL,
  so a thumbs-down remains queryable even after its trace row expires.

The unique constraint makes submit idempotent: an upsert on `(message_id, user_id)`.
Clearing a vote deletes the row. This mirrors Onyx's one-vote-per-user-per-message
semantics and keeps aggregation a `COUNT ... GROUP BY` over a small table rather than a
delta/revision reconstruction.

### D2b — Provenance keys (run + trace) are carried on the chat message

`chat.Message` carries no run, model, or trace provenance — `RetrievalContext` is a JSON
array of graph object IDs, not a trace pointer. `kb.chat_messages` stores no `run_id`
(`00001` baseline has only `id/conversation_id/role/content/citations/created_at`), and the
indirect `conversation_id → chat_conversations.session_id → agent_runs.session_id` chain is
**not deterministic**: `agent_runs.session_id` is indexed non-uniquely (`00195`
`idx_agent_runs_session_id`), so one session maps to many runs (resume/sub-runs). Both
provenance keys must therefore be threaded end-to-end and stamped on the message at
chat-response time, when the producing run and the search trace are both known server-side:

1. Chat retrieval invokes unified search, whose response carries `TraceID`
   (`domain/search/service.go:177-180`, `resp.TraceID = &traceIDStr`).
2. `kb.chat_messages` gains two nullable uuid columns, populated at chat-response time
   (chat → gateway → message persist):
   - `retrieval_trace_id` (FK `kb.retrieval_traces(trace_id) ON DELETE SET NULL`) — set to
     the search response `TraceID`, which IS the trace's `trace_id` column value;
   - `run_id` (FK `kb.agent_runs(id) ON DELETE SET NULL`) — the agent run that produced
     the assistant message, stamped from the run id already in scope at persist time.
3. `answer_feedback` denormalizes both at submit time: `run_id` and `retrieval_trace_id`
   are copied from the message (FKs to `kb.agent_runs(id)` / `kb.retrieval_traces(trace_id)`),
   so aggregation joins feedback → runs / traces without re-joining through chat.

This removes the earlier ambiguity of an "optional trace id with no stated source": the
source is the chat message's stored provenance keys, which in turn come from the run and
the search response at persist time. No key is client-supplied.

### D2c — Synchronous trace persist closes the FK race

`TraceID` (the `trace_id` value) is generated client-side in `Search`
(`traceID := uuid.New()`, `service.go:197`) before `persistTraceAsync` fires, so the value
is known synchronously even though the existing write is async and best-effort. Because the
FK now targets `trace_id` (not the DB-generated `id`), the only remaining hazard is
parent/child ordering: the trace row must be committed before the chat message that
references it. The chat-response path SHALL therefore persist the trace **synchronously** —
await `TraceStore.Insert` before persisting the message — and SHALL NOT also let the search
path fire the async best-effort write for the same trace (single writer, no duplicate row).
`trace_id` is the ONE stable identifier used for both the search response and the
message/feedback FK, and the FK is satisfied by construction (committed parent before child).

### D3 — Aggregation provenance (model via run, config via filters)

`domain/chat/entity.go` `Message` carries only `{ID, ConversationID, Role, Content,
Citations, CreatedAt, ContextSummary, RetrievalContext}` — there is **no** model, run, or
trace provenance on the chat message, and `kb.retrieval_traces` stores only `filters`
(resultTypes/fusionStrategy/weights), `candidates`, `selected_ids`, `scores` — **no**
embedding/model-config column. Therefore:

- **Model aggregation** goes through `run_id`: `answer_feedback.run_id →
  kb.llm_usage_events.run_id` (which carries `model`), where `answer_feedback.run_id` is
  denormalized from the message's `run_id` (D2b). A single run MAY emit multiple usage
  events (multiple LLM calls), so model aggregation is **counts/sums grouped by model**,
  not a single-model attribution. `kb.llm_usage_events` also carries `root_run_id`
  (`00053`), so aggregation MAY group by `run_id` or roll up to `root_run_id`. The earlier
  `chat_messages.conversation_id → chat_conversations.session_id → agent_runs.session_id`
  chain is **rejected**: `agent_runs.session_id` is indexed non-uniquely (`00195`), so a
  session maps to many runs and the join is non-deterministic. `run_id` is nullable
  (`ON DELETE SET NULL`, `00050`), so a run-less legacy message reports `NULL` model.
- **Search-fusion config aggregation** uses `kb.retrieval_traces.filters` (which stores
  resultTypes/fusionStrategy/weights), joined via `answer_feedback.retrieval_trace_id →
  kb.retrieval_traces`.
- **Per-embedding-model aggregation is NOT available and is out of scope**: neither the
  trace nor the chat message records the embedding model, and this change does not add it.

Where provenance is missing (legacy rows, no trace, no run), the aggregation reports
`NULL` for that dimension rather than failing — the requirement is queryable-by-model/
config, not guaranteed-non-null for legacy rows.

### D4 — New small domain, not a monitoring extension

Feedback is a small CRUD + aggregation surface. A dedicated `domain/answerfeedback`
package (`store.go`/`service.go`/`handler.go`/`module.go`, fx-wired) keeps the
aggregation SQL and the chat/trace joins out of the already-large `domain/monitoring`.
The handler validates project access via the existing membership path.

## Risks / Trade-offs

- **Null user on unauthenticated searches.** Traces from internal/system searches may
  have `user_id = NULL`; query-history-by-user simply excludes them. Documented, not a
  failure.
- **Model attribution is many-to-one.** A run may emit multiple usage events across
  models; aggregation is counts/sums per model, not a single definitive model. Documented
  in D3.
- **Trace retention deletes query history.** The trace TTL (bounded retention) will
  eventually expire old queries, so search-query history is not immutable audit. That is
  consistent with `retrieval-trace-persistence`; long-term audit is out of scope.
- **Provenance-key set-null cascades.** If a trace row is purged, the chat message's
  `retrieval_trace_id` (FK `ON DELETE SET NULL`) and the feedback row's copy become NULL;
  if a run is deleted, `run_id` likewise nulls. Feedback is still retained (its
  `created_at` is independent); only the trace/run join dims.
