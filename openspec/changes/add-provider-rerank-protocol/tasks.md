## 1. Registry rerank dimension

- [x] 1.1 Add `RerankAPI` type + `RerankAPICohere`/`RerankAPIDashScope`/`RerankAPIVolcengine`/`RerankAPITencentLKEAP` constants (`domain/provider/protocol.go`).
- [x] 1.2 Add `ProviderDefinition.RerankAPI` and `Registry.RerankAPIFor` (`registry.go`); add `ModelTypeRerank` and `ProviderCohere` (`entity.go`).
- [x] 1.3 Declare rerank support on the built-ins: new rerank-only Cohere vendor; Aliyun (DashScope), Volcengine (knowledgebase), Tencent LKEAP (`builtin.go`).
- [x] 1.4 (TDD) Unit tests: per-vendor `RerankAPIFor`, rerank model type + default base URL per vendor, `rerankApi` JSON key.

## 2. Rerank adapters (`pkg/rerank`)

- [x] 2.1 `Reranker` interface + `Result{Index,Score}` + `Config` + `New` dispatcher; results normalized best-first and bounded by `TopN`.
- [x] 2.2 Cohere-compatible flat `/rerank` adapter.
- [x] 2.3 DashScope nested `input`/`parameters` adapter.
- [x] 2.4 Volcengine knowledgebase `datas`/`scores` adapter (accepts both response forms).
- [x] 2.5 Tencent LKEAP `RunRerank` adapter with TC3-HMAC-SHA256 signing.
- [x] 2.6 (TDD) `httptest` unit tests per adapter (request shape, response parsing, ordering/top-N, error paths) + dispatcher and no-op tests.

## 3. Registry-keyed selection

- [x] 3.1 `provider.NewReranker(def, cred, model)` mapping `RerankAPI` → adapter, reusing `APIKey`/`BaseURL`/`Extra`/`ExtraHeaders`; `Registry.NewRerankerFor`.
- [x] 3.2 (TDD) Selection test proving each vendor is routed to its distinct wire path; unsupported vendor and nil credential error.

## 4. Optional search stage

- [x] 4.1 Add `Service.reranker`/`rerankTopN`, `SetReranker`, and the post-`filterBelowMinScore` stage with timeout + graceful fallback (`domain/search`).
- [x] 4.2 Wire opt-in `SEARCH_RERANK_*` config in the search fx module; default off.
- [x] 4.3 (TDD) Unit tests: nil/disabled reranker leaves order unchanged; configured reranker reorders only the top-N block and preserves the tail; provider error falls back to fused order; text extraction per result type.

## 5. Verify

- [x] 5.1 `go build ./...` (server compile).
- [x] 5.2 `go test -count=1 ./pkg/rerank/... ./domain/provider/... ./domain/search/...`.
- [x] 5.3 `go vet` + `gofmt` on touched packages; `apps/server/scripts/lint-ratchet.sh`.
- [x] 5.4 Mutation-check the selection mapping and one parsing path (break, observe failure, restore).
- [ ] 5.5 Deferred: wiring the held `add-retrieval-reranking` modelconfig/per-project stage on top of this registry axis.

<!-- openspec:archive-hold: §5.5 intentionally deferred — this change ships the registry rerank axis + adapters, but the per-project/modelconfig retrieval-reranking stage is held on the separate `add-retrieval-reranking` change. Archiving now would sync delta specs ahead of that follow-up. Tick 5.5 (or move this reason) before archiving. -->
