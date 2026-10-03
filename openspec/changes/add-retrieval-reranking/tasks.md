## 1. Reranker interface + adapters (TDD)

- [ ] 1.1 New `pkg/rerank`: `Candidate` + `Reranker` interface per design D2, plus a `Nil`/no-op sentinel for the unconfigured case.
- [ ] 1.2 (TDD) Interface unit test (canned fake reranker): re-scores and re-orders candidates deterministically; returns an error to exercise the fallback path.
- [ ] 1.3 `llmReranker` adapter: resolves model via `domain/modelconfig`, credentials via `domain/provider`, builds the ranked-list prompt, parses the response.
- [ ] 1.4 (TDD) LLM adapter unit test with a canned model: well-formed ranked output re-orders; malformed output returns an error (does not panic).
- [ ] 1.5 `cohereReranker`/cross-encoder adapter path (interface-complete; a stub HTTP client for tests).
- [ ] 1.6 (TDD) Cohere adapter unit test with a canned response: maps relevance scores to re-ordering; non-200 response returns an error.

## 2. Config + validation (TDD)

- [ ] 2.1 New Goose migration adding `rerank_model` to `ProjectModelConfig` (`domain/modelconfig`), plus the resolution-chain entry that resolves `rerank_model` per project using `projectID` (like generative/embedding model selection); env `RERANK_MODEL`/`RERANK_TOP_N` as bootstrap fallback only; default empty → off.
- [ ] 2.2 (TDD) Config validation unit test: empty model → off; unknown model → validation error; `RerankTopN <= 0` or `> 100` → validation error; valid config passes; per-project resolution returns a project's model, not a global one.

## 3. Stage placement in `Search` (TDD)

- [ ] 3.1 In `domain/search/service.go` `Search`, insert the rerank call after `filterBelowMinScore` and before `countTypes`/assembly; skip when the reranker is nil. Build each `Candidate.Text` per result type (graph → Key/Fields serialization, text → Snippet, relationship → TripletText).
- [ ] 3.2 (TDD) Unit test: nil reranker → fused order unchanged (byte-for-byte today's order); configured reranker re-orders only the top-N block and appends the rest in original order; fewer-than-N candidates reranks all.

## 4. Fallback + trace metadata (TDD)

- [ ] 4.1 Wrap the rerank call with a timeout; on error/timeout/malformed output, fall back to the pre-rerank order and set a `fallback` flag.
- [ ] 4.2 (TDD) Unit test: provider error → search completes with fused order + `fallback=true`; no error surfaces to the caller.
- [ ] 4.3 New Goose migration adding nullable `rerank_metadata` jsonb to `kb.retrieval_traces` (with a down migration), extend the `RetrievalTrace` Bun model with `rerank_metadata` (`{model, topN, fallback, scores}`), populate in `persistTraceAsync`, and add a migration round-trip (up/down) test.
- [ ] 4.4 (TDD) Unit test: a reranked search persists the rerank model, top-N, and fallback flag on the trace; an unreranked search persists no rerank metadata.

## 5. Verify

- [ ] 5.1 `task build` (server compile).
- [ ] 5.2 `task lint` for the touched modules.
- [ ] 5.3 Deferred (documented): evaluation of rerank quality against the `answer-feedback` signal once both land.
