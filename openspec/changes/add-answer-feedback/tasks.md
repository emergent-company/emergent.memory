## 1. Migration — `kb.answer_feedback`

- [ ] 1.1 New migration `apps/server/migrations/<n>_create_answer_feedback.sql`: create `kb.answer_feedback` per design D2 (id, project_id, message_id, retrieval_trace_id, user_id, thumbs, comment, created_at, updated_at, `UNIQUE (message_id, user_id)`), FKs to `kb.chat_messages(id)` and `kb.retrieval_traces(id)` (NOT `trace_id`), indexes on `(project_id, created_at)` and `(user_id, created_at)`.
- [ ] 1.2 (TDD) Migration round-trip test in the migration test suite: up then down drops the table cleanly; the unique index is present; the `retrieval_trace_id` FK targets `kb.retrieval_traces.id`.

## 2. Trace extension — user + query key

- [ ] 2.1 Add `user_id` (nullable) and `query_key` (nullable, string) columns to the `RetrievalTrace` Bun model in `domain/search/trace_store.go`, plus a new migration adding the columns and a `(project_id, created_at DESC)` and `(user_id, created_at DESC)` index.
- [ ] 2.2 Add `UserID uuid.UUID` to `SearchContext` (`domain/search/dto.go`) and populate it in `domain/search/handler.go` from `auth.MustGetUser(c)`; in `domain/search/service.go`, read `searchCtx.UserID` to stamp the trace and compute `query_key` (deterministic lowercase + whitespace-collapse) before `persistTraceAsync`.
- [ ] 2.3 (TDD) Unit test: `RetrievalTrace` insert persists `user_id` and `query_key`; same query twice yields identical `query_key`; unauthenticated search persists `user_id = NULL`.
- [ ] 2.4 Add `TraceStore.ListByProject(ctx, projectID, opts)` and `ListByUser` read methods returning reverse-chronological traces.
- [ ] 2.5 (TDD) Unit test: `ListByProject` returns only that project's traces newest-first; `ListByUser` filters to the user; empty result returns `[]` not nil.

## 3. Feedback domain — submit / list / aggregate

- [ ] 3.1 New `domain/answerfeedback` package: Bun entity, `store.go` (upsert by `(message_id, user_id)`, delete for clear, list, aggregate), `service.go` (submit-with-idempotency, project access check, aggregation by model/config), `handler.go` (Echo routes), `module.go` (fx wiring).
- [ ] 3.2 (TDD) Store unit test: first submit inserts; re-submit updates the same row (no duplicate); clear deletes; two users on one message produce two rows.
- [ ] 3.3 (TDD) Service unit test: submit derives user from context (request `user_id` field ignored); project access denied for a non-member; aggregation groups correctly by model and by config and returns zero counts for an empty range.
- [ ] 3.4 (TDD) Handler unit test: routes `POST /feedback`, `GET /feedback/aggregate` validate and return the expected DTOs.

## 4. API + DTOs

- [ ] 4.1 Define submit DTO (message_id, trace_id optional, thumbs, comment) and aggregate DTO (range, breakdown by model/config), with validation (`thumbs` ∈ {up, down}).
- [ ] 4.2 (TDD) DTO validation unit test: invalid thumbs value rejected; missing message_id rejected; valid submit accepted.

## 5. Verify + follow-on lane note

- [ ] 5.1 `task build` (server compile); `task lint` for the touched modules.
- [ ] 5.2 Deferred (documented in proposal, not implemented): gateway thumbs widget on chat responses and a feedback-aggregation view on the usage dashboard.
