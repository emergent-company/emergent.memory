# Tasks — fix-cli-blueprint-install-preflight

## 1. Validate + normalize models before apply

- [x] 1.1 Add `AgentModel.Provider` to `apps/cli/internal/blueprints/types.go` (structured model shape, mirrors the server's `ModelConfig.Provider`).
- [x] 1.2 In `apps/cli/internal/blueprints/validator.go`, check every agent model with the shared `sdkmodelref.ParseWithProvider` (replacing the name-only `Parse`), and report the agent, offending value, and expected `provider/model` form.
- [x] 1.3 Add `NormalizeAgentModels` (folds a structured provider into the canonical name, clears the provider field, preserves slash-bearing model paths) and `Preflight` (full offline validation, then normalization).

## 2. Run preflight in install

- [x] 2.1 In `apps/cli/internal/cmd/blueprints.go`, call `blueprints.Preflight` after loading and before constructing/running the applier; abort with a non-zero exit on errors and print warnings otherwise.

## 3. Tests

- [x] 3.1 Extend `apps/cli/internal/blueprints/validator_test.go` with structured-provider validation subtests and `NormalizeAgentModels` canonicalization tests.
- [x] 3.2 Add `apps/cli/internal/blueprints/preinstall_test.go`: bad model aborts with **zero** API requests (no partial apply); valid structured model normalizes and applies.
- [x] 3.3 Mutation-test the pre-check: disabling the model validation makes the abort guard test fail (applier runs, requests > 0).

## 4. OpenSpec delta

- [x] 4.1 Add the `blueprint-api` delta requirement pinning the CLI install preflight + model normalization.
- [x] 4.2 `openspec validate fix-cli-blueprint-install-preflight --strict` passes.

## 5. Verification

- [x] 5.1 `cd apps/cli && PATH="/root/go/bin:$PATH" go build ./...`
- [x] 5.2 `cd apps/cli && PATH="/root/go/bin:$PATH" go test -count=1 ./internal/blueprints/...`
- [x] 5.3 `gofmt -l` on changed Go files reports nothing.
