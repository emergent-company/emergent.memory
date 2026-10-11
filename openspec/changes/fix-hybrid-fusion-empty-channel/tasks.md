## 1. Helper

- [ ] 1.1 Add `EffectiveFusionWeights` to `apps/server/pkg/mathutil/math.go`
- [ ] 1.2 Add a table-driven unit test (empty-lexical, empty-vector, both, neither)

## 2. Call sites

- [ ] 2.1 Apply the helper in `domain/graph/service.go` `HybridSearch` after `CalcMeanStd`
- [ ] 2.2 Apply the helper in `domain/search/repository.go` `HybridSearch` after `CalcMeanStd`

## 3. Verification

- [ ] 3.1 `go build ./...` in `apps/server` passes
- [ ] 3.2 `go test ./pkg/mathutil/... ./domain/search/... ./domain/graph/... -count=1` passes
- [ ] 3.3 `gofmt -l` clean on touched files
- [ ] 3.4 `openspec validate fix-hybrid-fusion-empty-channel --strict` passes
