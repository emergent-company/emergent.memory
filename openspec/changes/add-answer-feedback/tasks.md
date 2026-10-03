<!-- openspec:archive-hold: spec-only change; implementation intentionally deferred (PR #1410) -->
## 1. Migration — `kb.answer_feedback`

- [ ] 1.1 New migration `apps/server/migrations/<n>_create_answer_feedback.sql`: create `kb.answer_feedback` per design D2 — id, project_id, message_id, run_id, retrieval_trace_id, user_id, thumbs (`SMALLINT NOT NULL CHECK (thumbs IN (-1, 1))`), comment, created_at, updated_at, `UNIQUE (message_id, user_id)`. FKs: `message_id → kb.chat_messages(id)`, `run_id → kb.agent_runs(id) ON DELETE SET NULL`, `retrieval_trace_id → kb.retrieval_traces(trace_id) ON DELETE SET NULL` (the public trace id, matching `UnifiedSearchResponse.TraceID`), `user_id → core.user_profiles(id)` (mirrors `00108`). Indexes on `(project_id, created_at)` and `(user_id, created_at)`. A companion migration adds a UNIQUE constraint on `kb.retrieval_traces.trace_id` (today only non-uniquely indexed via `00143` `idx_retrieval_traces_trace`) so it is a valid FK target.
- [ ] 1.2 (TDD) Migration round-trip test in the migration test suite: up then down drops the table cleanly; the unique indexes are present (`(message_id, user_id)` and the new UNIQUE on `kb.retrieval_traces.trace_id`); the `CHECK (thumbs IN (-1, 1))` constraint rejects out-of-range values; the `retrieval_trace_id` FK targets `kb.retrieval_traces.trace_id` (the public trace id, not `id`); deleting a trace row sets `retrieval_trace_id` to NULL (`ON DELETE SET NULL`) and leaves the feedback row intact.

## 2. Trace extension — user attribution + query history

- [ ] 2.1 Add `user_id` (nullable) to the `RetrievalTrace` Bun model in `domain/search/trace_store.go`, plus a new migration adding the column (FK `core.user_profiles(id)`) and a `user_id` index. Do NOT add a redundant `(project_id, created_at)` index — `00143` already indexes `project_id` and `created_at`.
- [ ] 2.2 Add `UserID uuid.UUID` to `SearchContext` (`domain/search/dto.go`) and populate it in `domain/search/handler.go` from `auth.MustGetUser(c)`; in `domain/search/service.go`, read `searchCtx.UserID` to stamp the trace before `persistTraceAsync`.
- [ ] 2.3 (TDD) Unit test: `RetrievalTrace` insert persists `user_id`; unauthenticated search persists `user_id = NULL`.
- [ ] 2.4 Add `TraceStore.ListByProject(ctx, projectID, opts)` and `ListByUser` read methods returning reverse-chronological traces (reusing existing project/created indexes).
- [ ] 2.5 (TDD) Unit test: `ListByProject` returns only that project's traces newest-first; `ListByUser` filters to the user (excluding null-user traces); empty result returns `[]` not nil.

## 3. Chat message → provenance linkage (run + trace)

- [ ] 3.1 New migration adding nullable `retrieval_trace_id` (FK `kb.retrieval_traces(trace_id) ON DELETE SET NULL` — the public trace id, not the surrogate `id`) and nullable `run_id` (FK `kb.agent_runs(id) ON DELETE SET NULL`) to `kb.chat_messages`, plus the `domain/chat` entity fields.
- [ ] 3.2 At chat-response time, capture the unified search response `TraceID` (`domain/search/service.go:177-180`) and persist it on the chat message's `retrieval_trace_id`; stamp `run_id` from the producing agent run id already in scope at persist time (chat → gateway → message persist). Persist the trace **synchronously** (await `TraceStore.Insert` for the trace's `trace_id`) before persisting the message so the FK is satisfied — do NOT rely on the async `persistTraceAsync`, and do not double-write the same trace. Both keys are server-derived, never client-supplied.
- [ ] 3.3 (TDD) Unit test: a chat response backed by a search stores the search `TraceID` (equal to `kb.retrieval_traces.trace_id`, not `id`) and its producing `run_id`; a response with no search stores `retrieval_trace_id = NULL`; a non-agent response stores `run_id = NULL`; the trace row is committed before the message (FK holds — no async-insert race, no FK violation).

## 4. Feedback domain — submit / list / aggregate

- [ ] 4.1 New `domain/answerfeedback` package: Bun entity, `store.go` (upsert by `(message_id, user_id)`, delete for clear, list, aggregate), `service.go` (submit-with-idempotency, project access check, aggregation by model via `run_id` and by search-fusion config via `retrieval_traces.filters`), `handler.go` (Echo routes), `module.go` (fx wiring).
- [ ] 4.2 (TDD) Store unit test: first submit inserts; re-submit updates the same row (no duplicate); clear deletes; two users on one message produce two rows; `thumbs` CHECK rejects any value other than `-1`/`1`.
- [ ] 4.3 (TDD) Service unit test: submit derives user from context (request `user_id` field ignored); `run_id` and `retrieval_trace_id` are derived server-side (both denormalized from the chat message, which carries them per §3), not from the submit DTO; project access denied for a non-member; aggregation groups correctly by model (via `run_id`) and by search-fusion config (via `filters`), returns zero counts for an empty range, and reports `NULL` for a dimension with no provenance.
- [ ] 4.4 (TDD) Handler unit test: routes `POST /feedback`, `GET /feedback/aggregate` validate and return the expected DTOs.

## 5. API + DTOs

- [ ] 5.1 Define submit DTO (`message_id`, `thumbs`, `comment` — no `trace_id`/`run_id`, both derived server-side) and aggregate DTO (range, breakdown by model and by search-fusion config), with validation (`thumbs` ∈ {up, down}).
- [ ] 5.2 (TDD) DTO validation unit test: invalid thumbs value rejected; missing message_id rejected; valid submit accepted; a submit carrying a client-supplied `trace_id` is ignored (server derives it).

## 6. Verify + follow-on lane note

- [ ] 6.1 `task build` (server compile); `task lint` for the touched modules.
- [ ] 6.2 Deferred (documented in proposal, not implemented): gateway thumbs widget on chat responses and a feedback-aggregation view on the usage dashboard.
