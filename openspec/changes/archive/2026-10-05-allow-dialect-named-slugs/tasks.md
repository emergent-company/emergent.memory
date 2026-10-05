## 1. Relax slug validation

- [x] 1.1 Remove the reserved-dialect check from `validateProviderSlug` in `apps/server/domain/provider/service.go` and update its doc comment. Verify `go build ./...` from `apps/server/`.
- [x] 1.2 Update `TestValidateProviderSlug` and `TestResolveSlug` in `service_slug_test.go` so dialect-name slugs now expect success (no error). Verify `go test ./domain/provider/... -run 'TestValidateProviderSlug|TestResolveSlug'`.

## 2. Regression coverage

- [x] 2.1 Add a regression test proving slug-first resolution wins over a same-named dialect fallback (an openai-dialect instance named `deepseek` resolves a `deepseek` reference to that instance). Verify `go test ./domain/provider/... -run TestResolveByRef_DialectNameSlug_Wins`.

## 3. Verification

- [x] 3.1 Run `go build ./...` and `go vet ./domain/provider/...` from `apps/server/`; all pass.
- [x] 3.2 Run `gofmt -l` on the changed Go files; no output (all formatted).
