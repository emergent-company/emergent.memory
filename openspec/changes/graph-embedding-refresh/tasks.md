## 1. Shared embedding-text builder

- [x] 1.1 Add `apps/server/pkg/embeddingtext` with `Text` (deterministic, sorted keys, existing token order + cap), `Hash` (SHA-256), and `IsSkippedKey`.
- [x] 1.2 (TDD) Unit test: determinism across map order; only embedding-relevant fields change the hash; skipped keys / URL / UUID / large numerics do not; key namespace stripping; cap/truncation.

## 2. Refactor the worker onto the shared builder

- [x] 2.1 `extraction.GraphEmbeddingWorker.extractText` delegates to `embeddingtext.Text`; remove the duplicated builder/skip-list/`isUUID`; update the sweep worker's skip-list use.
- [x] 2.2 Existing `extractText` unit tests still pass (boosted ×2, ordering, cap).

## 3. Content-aware enqueue guard

- [x] 3.1 Add `embeddingTextUnchanged` + `carryEmbeddingIfUnchanged`; guard `Patch` and `CreateOrUpdate` (update branch). `BulkUpdateObjects` inherits via `Patch`.
- [x] 3.2 Add `Repository.CopyEmbedding` and carry the vector forward when the text is unchanged and the previous row was embedded.
- [x] 3.3 Guard `MoveObject` (only enqueue when the moved row has no embedding).
- [x] 3.4 (TDD) DB tests: `TestPatch_NonSemanticPropertyChange_DoesNotEnqueue`, `TestPatch_SemanticPropertyChange_Enqueues`, `TestCreateOrUpdate_PropertyOnlyChange_DoesNotEnqueue`, `TestBulkUpdateObjects_PropertyOnly_DoesNotEnqueue`, `TestPatch_NonSemanticChange_CarriesEmbedding`.

## 4. Verify

- [x] 4.1 `go build ./...`
- [x] 4.2 `TEST_DATABASE_URL=... REQUIRE_DB=1 go test -count=1 ./domain/graph/... ./domain/extraction/... ./pkg/embeddingtext/...`
- [x] 4.3 `bash apps/server/scripts/lint-ratchet.sh`
- [x] 4.4 `gofmt -l` clean on touched packages.
