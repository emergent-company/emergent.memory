## 1. Validation

- [x] 1.1 Replace the `strings.Contains(name, "/")` checks in `CreateDefinition` and `UpdateDefinition` with `modelref.Parse`; normalize the stored value to `ref.String()`.
- [x] 1.2 Add `apps/server/domain/agents/model_validation_test.go` covering empty-provider, no-provider, and empty-model rejection on create, plus empty-provider on update.

## 2. Spec

- [x] 2.1 Add the `agent-definitions` capability delta and proposal.

## 3. Verification

- [x] 3.1 `go build ./...` from `apps/server`.
- [x] 3.2 `go test ./domain/agents/ -run 'Model|Definition' -count=1` from `apps/server`.
- [x] 3.3 `openspec validate fix-agent-model-ref-validation` from the repo root.
